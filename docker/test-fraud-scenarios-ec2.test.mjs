import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { generateKeyPairSync, createPublicKey, verify } from 'node:crypto';
import { buildScenarioDefinitions, portableModel } from './provision-fraud-scenarios-ec2.mjs';
import {
  buildTestCases,
  parseArgs,
  createClientAssertion,
  calculateDiurnalMultiplier,
  TrafficPacer,
  createEntityPool,
  RealisticTransactionGenerator,
  AuthTokenManager,
} from './test-fraud-scenarios-ec2.mjs';

const tenantId = 'd7943f99-4402-428a-a539-0b2ecb6fcc17';
const clientId = 'c5598d88-7517-48f8-8094-11883be78399';
const runId = 'fixture-test';

async function loadModel() {
  const envelope = JSON.parse(await readFile(new URL('../fraud-data-model.json', import.meta.url), 'utf8'));
  return portableModel(envelope.data_model);
}

test('requires tenant, gateway, and explicit write acknowledgement', () => {
  assert.throws(() => parseArgs([]), /--host/);
  assert.throws(() => parseArgs(['--host', '10.0.0.2:8088']), /--tenant-id/);
  assert.throws(() => parseArgs(['--host', '10.0.0.2:8088', '--tenant-id', tenantId]), /--confirm-write/);
  assert.equal(parseArgs(['--host', '10.0.0.2:8088', '--tenant-id', tenantId, '--confirm-write']).origin, 'http://10.0.0.2:8088');
  assert.equal(parseArgs(['--dry-run']).dryRun, true);
});

test('rejects invalid run IDs, host origins, and rule thresholds', () => {
  assert.throws(() => parseArgs(['--host', 'host:8088/path', '--tenant-id', tenantId, '--confirm-write']), /origin/);
  assert.throws(() => parseArgs(['--host', 'host:8088', '--tenant-id', tenantId, '--run-id', '../x', '--confirm-write']), /run-id/);
  assert.throws(() => parseArgs(['--host', 'host:8088', '--tenant-id', tenantId, '--structuring-band-percent', '101', '--confirm-write']), /no more than 100/);
  assert.throws(() => parseArgs(['--dry-run', '--client-id', 'invalid-uuid']), /UUID/);
});

test('parses new 24-hour simulation arguments and duration options', () => {
  const args = [
    '--host', '127.0.0.1:8088',
    '--tenant-id', tenantId,
    '--confirm-write',
    '--total-transactions', '200000',
    '--duration-hours', '24',
    '--client-id', clientId,
    '--batch-size', '20',
  ];
  const parsed = parseArgs(args);
  assert.equal(parsed.totalTransactions, 200000);
  assert.equal(parsed.durationHours, 24);
  assert.equal(parsed.durationMs, 24 * 3600 * 1000);
  assert.equal(parsed.clientId, clientId);
  assert.equal(parsed.batchSize, 20);

  const parsedMinutes = parseArgs(['--dry-run', '--duration-minutes', '30']);
  assert.equal(parsedMinutes.durationMs, 30 * 60 * 1000);
});

test('generates isolated fire and no-fire fixtures for every provisioned rule (deterministic mode)', async () => {
  const model = await loadModel();
  const definitions = buildScenarioDefinitions();
  const { cases, customers, wallets, transactions } = buildTestCases(definitions, model, runId);
  const rules = definitions.flatMap((scenario) => scenario.rules);
  assert.equal(cases.length, rules.length * 2);
  assert.equal(new Set(cases.map(({ payload }) => payload.object_id)).size, cases.length);
  for (const rule of rules) {
    const ruleCases = cases.filter(({ rule: candidate }) => candidate.stable_rule_id === rule.stable_rule_id);
    assert.deepEqual(ruleCases.map(({ shouldHit }) => shouldHit).sort(), [false, true]);
  }
  for (const [objectType, records] of [['customers', customers], ['wallets', wallets], ['transactions', transactions]]) {
    const fields = new Set(model.tables.find((table) => table.name === objectType).fields.map(({ name }) => name));
    for (const record of records) {
      assert.ok(record.object_id.startsWith(`${runId}-`));
      for (const name of Object.keys(record)) {
        assert.ok(name === 'object_id' || name === 'updated_at' || fields.has(name), `${objectType}.${name} is not in the model`);
      }
      for (const field of model.tables.find((table) => table.name === objectType).fields) {
        if (!field.nullable) assert.notEqual(record[field.name], undefined, `${objectType}.${field.name} is required`);
      }
    }
  }
  assert.ok(customers.length > 0);
  assert.ok(wallets.length > 0);
  assert.ok(transactions.length > 0);
  const baselineObjectIds = new Set(
    cases
      .filter(({ rule }) => rule.stable_rule_id === 'outside-user-baseline')
      .map(({ payload }) => payload.object_id),
  );
  assert.ok(transactions.every(({ object_id, user_median_amount_90d }) =>
    baselineObjectIds.has(object_id)
      ? user_median_amount_90d === 10
      : user_median_amount_90d === null));
});

test('fixture thresholds follow provisioning options', async () => {
  const model = await loadModel();
  const options = { lateCallbackSeconds: 700, structuringLimit: 2000, nearLimitPercent: 5 };
  const definitions = buildScenarioDefinitions(options.lateCallbackSeconds, options);
  const { cases } = buildTestCases(definitions, model, runId, options);
  const lateCallback = cases.find(({ rule, shouldHit }) => rule.stable_rule_id === 'late-callback' && shouldHit);
  assert.equal(lateCallback.payload.seconds_to_callback, 701);
  const structuring = cases.find(({ rule, shouldHit }) => rule.stable_rule_id === 'structuring-near-limit' && shouldHit);
  assert.equal(structuring.payload.amount, 1900);
});

test('creates valid signed RS256 client assertions for proof exchange', () => {
  const { publicKey, privateKey } = generateKeyPairSync('rsa', {
    modulusLength: 2048,
    publicKeyEncoding: { type: 'spki', format: 'pem' },
    privateKeyEncoding: { type: 'pkcs8', format: 'pem' },
  });

  const kid = 'test-client-key-1';
  const tokenEndpoint = 'http://127.0.0.1:8088/oauth/token';
  const assertion = createClientAssertion(clientId, privateKey, kid, tokenEndpoint);

  const parts = assertion.split('.');
  assert.equal(parts.length, 3, 'JWT must have 3 parts: header.payload.signature');

  const header = JSON.parse(Buffer.from(parts[0], 'base64url').toString('utf8'));
  const payload = JSON.parse(Buffer.from(parts[1], 'base64url').toString('utf8'));

  assert.equal(header.alg, 'RS256');
  assert.equal(header.typ, 'JWT');
  assert.equal(header.kid, kid);
  assert.equal(payload.iss, clientId);
  assert.equal(payload.sub, clientId);
  assert.equal(payload.aud, tokenEndpoint);
  assert.ok(payload.jti);
  assert.ok(payload.exp > payload.iat);
  assert.equal(payload.exp - payload.iat, 60);

  // Verify cryptographic signature with the public key
  const signedData = Buffer.from(`${parts[0]}.${parts[1]}`);
  const signature = Buffer.from(parts[2], 'base64url');
  const pubKeyObj = createPublicKey(publicKey);
  const isValid = verify('RSA-SHA256', signedData, pubKeyObj, signature);
  assert.ok(isValid, 'RS256 signature verification must pass');
});

test('calculates bounded diurnal multiplier for 24-hour cycle', () => {
  const durationMs = 24 * 3600 * 1000;

  // Check bounds across all 24 hours
  for (let hour = 0; hour < 24; hour += 1) {
    const elapsed = hour * 3600 * 1000;
    const mult = calculateDiurnalMultiplier(elapsed, durationMs);
    assert.ok(mult >= 0.3 && mult <= 2.0, `Hour ${hour} multiplier ${mult} out of bounds`);
  }

  // Trough around early night/morning (~03:00) should be lower than peak afternoon (~15:00)
  const trough = calculateDiurnalMultiplier(3 * 3600 * 1000, durationMs);
  const peak = calculateDiurnalMultiplier(15 * 3600 * 1000, durationMs);
  assert.ok(peak > trough * 2, `Peak (${peak}) should be significantly higher than trough (${trough})`);
});

test('traffic pacer generates varying positive delays', () => {
  const pacer = new TrafficPacer({
    totalTransactions: 200_000,
    durationMs: 24 * 3600 * 1000,
    batchSize: 10,
  });

  const delays = [];
  for (let i = 0; i < 50; i += 1) {
    const delay = pacer.nextDelayMs(i * 10_000);
    assert.ok(delay >= 5 && delay <= 15000, `Delay ${delay} out of bounds`);
    delays.push(delay);
  }

  // Ensure there is delay variance (not a fixed constant tick)
  const uniqueDelays = new Set(delays);
  assert.ok(uniqueDelays.size > 10, 'Pacer must produce varied inter-arrival delays');
});

test('creates realistic entity pool and generates schema-conforming transactions with organic triggers', async () => {
  const model = await loadModel();
  const pool = createEntityPool(model, runId, { customerCount: 50, walletCount: 80, destinationCount: 20 });

  assert.equal(pool.customers.length, 50);
  assert.equal(pool.wallets.length, 80);
  assert.equal(pool.destinations.length, 20);

  const generator = new RealisticTransactionGenerator(pool, model, runId, {
    structuringLimit: 1000,
    nearLimitPercent: 10,
    lateCallbackSeconds: 300,
  });

  const batch = generator.generateBatch(200);
  assert.equal(batch.length, 200);

  const modelFields = new Set(model.tables.find((t) => t.name === 'transactions').fields.map((f) => f.name));
  for (const tx of batch) {
    assert.ok(tx.object_id.startsWith(`${runId}-tx-`));
    for (const key of Object.keys(tx)) {
      assert.ok(key === 'object_id' || key === 'updated_at' || modelFields.has(key), `Field ${key} not in transactions table`);
    }
  }

  // Check organic distribution of scenario signals
  const creditLegs = batch.filter((tx) => tx.leg === 'credit');
  const structuringTxns = batch.filter((tx) => tx.amount >= 900 && tx.amount < 1000 && tx.leg === 'debit');
  const resolutionTxns = batch.filter((tx) => tx.state === 'callback_received' || tx.state === 'undetermined');

  assert.ok(creditLegs.length > 0, 'Must include credit transactions (destination concentration)');
  assert.ok(structuringTxns.length > 0, 'Must include structuring transactions');
  assert.ok(resolutionTxns.length > 0, 'Must include callback/resolution transactions');
});
