import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { generateClientKeyPair } from '../backend/auth-service/scripts/client-proof.mjs';
import { buildScenarioDefinitions, portableModel } from './provision-fraud-scenarios-ec2.mjs';
import {
  buildTestCases,
  parseArgs,
  CLIENT_SCOPES,
  checkExistingModel,
  getEvaluation,
  evaluateTransactionBatch,
  runContinuousSimulation,
  ingestBatchWithRetry,
  ingestSimulationEntities,
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
  assert.throws(() => parseArgs(['--host', '10.0.0.2:8088', '--tenant-id', tenantId, '--confirm-write']), /--client-id/);
  assert.throws(() => parseArgs(['--host', '10.0.0.2:8088', '--tenant-id', tenantId, '--client-id', clientId]), /--confirm-write/);
  assert.equal(parseArgs(['--host', '10.0.0.2:8088', '--tenant-id', tenantId, '--client-id', clientId, '--confirm-write']).origin, 'http://10.0.0.2:8088');
  assert.equal(parseArgs(['--dry-run']).dryRun, true);
});

test('model mismatch requires an explicit override and does not hide export errors', async (t) => {
  const envelope = JSON.parse(await readFile(new URL('../fraud-data-model.json', import.meta.url), 'utf8'));
  const model = portableModel(envelope.data_model);
  const different = structuredClone(envelope);
  different.data_model.tables[0].description = 'different';
  let response = new Response(JSON.stringify(different), { status: 200 });
  t.mock.method(globalThis, 'fetch', async () => response);
  const warn = t.mock.method(console, 'warn', () => {});
  const options = parseArgs(['--host', 'localhost:8088', '--tenant-id', tenantId, '--client-id', clientId, '--confirm-write']);
  const tokenManager = { getToken: async () => 'token' };

  await assert.rejects(checkExistingModel(options, tokenManager, model), /differs.*refusing to seed/);
  response = new Response(JSON.stringify(different), { status: 200 });
  assert.equal(await checkExistingModel({ ...options, allowModelMismatch: true }, tokenManager, model), false);
  assert.match(warn.mock.calls[0].arguments[0], /WARNING.*may fail or have unintended meaning/);
  response = new Response(JSON.stringify(envelope), { status: 200 });
  assert.equal(await checkExistingModel({ ...options, allowModelMismatch: true }, tokenManager, model), true);
  assert.equal(warn.mock.callCount(), 1);
  response = new Response(JSON.stringify({ error: 'unavailable' }), { status: 503 });
  await assert.rejects(checkExistingModel({ ...options, allowModelMismatch: true }, tokenManager, model), /HTTP 503/);
  response = new Response(JSON.stringify({}), { status: 200 });
  await assert.rejects(checkExistingModel({ ...options, allowModelMismatch: true }, tokenManager, model), /response did not contain/);
});

test('rejects invalid run IDs, host origins, and rule thresholds', () => {
  assert.throws(() => parseArgs(['--host', 'host:8088/path', '--tenant-id', tenantId, '--confirm-write']), /origin/);
  assert.throws(() => parseArgs(['--host', 'host:8088', '--tenant-id', tenantId, '--run-id', '../x', '--confirm-write']), /run-id/);
  assert.throws(() => parseArgs(['--host', 'host:8088', '--tenant-id', tenantId, '--structuring-band-percent', '101', '--confirm-write']), /no more than 100/);
  assert.throws(() => parseArgs(['--dry-run', '--client-id', 'invalid-uuid']), /UUID/);
  assert.throws(() => parseArgs(['--dry-run', '--resume']), /not idempotent/);
  assert.throws(() => parseArgs(['--dry-run', '--batch-size', '501']), /between 1 and 500/);
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
  assert.equal(parsed.tokenUrl, 'http://127.0.0.1:8088/oauth/token');
  assert.match(parsed.keyDir, /\.local\/client-auth$/);

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

test('refreshes client tokens before their actual expiry and shares concurrent exchanges', async () => {
  let now = 0;
  let calls = 0;
  const exchange = async (dir, id, url, scopes) => {
    assert.equal(dir, '/keys');
    assert.equal(id, clientId);
    assert.equal(url, 'http://auth.example/oauth/token');
    assert.deepEqual(scopes, CLIENT_SCOPES);
    calls++;
    return { access_token: `token-${calls}`, expires_in: 100 };
  };
  const manager = new AuthTokenManager({ keyDir: '/keys', clientId, tokenUrl: 'http://auth.example/oauth/token', exchange, now: () => now });
  assert.deepEqual(await Promise.all([manager.getToken(), manager.getToken()]), ['token-1', 'token-1']);
  now = 89_999;
  assert.equal(await manager.getToken(), 'token-1');
  now = 90_000;
  assert.equal(await manager.getToken(), 'token-2');
  assert.equal(calls, 2);
});

test('fails closed when a token refresh fails', async () => {
  let now = 0;
  let calls = 0;
  const manager = new AuthTokenManager({
    keyDir: '/keys', clientId, tokenUrl: 'http://auth.example/oauth/token', now: () => now,
    exchange: async () => {
      if (++calls > 1) throw new Error('unavailable');
      return { access_token: 'old-token', expires_in: 10 };
    },
  });
  assert.equal(await manager.getToken(), 'old-token');
  now = 9_000;
  await assert.rejects(manager.getToken(), /Client token exchange failed: unavailable/);
});

test('polls deferred evaluations with a fresh client token', async (t) => {
  const requests = [];
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    requests.push({ url, authorization: options.headers.authorization });
    const body = requests.length === 1
      ? { async_decision_execution: { id: 'execution-1' } }
      : { async_decision_execution: { status: 'completed', result_body: { triggered: true } } };
    return new Response(JSON.stringify(body), { status: requests.length === 1 ? 202 : 200 });
  });
  let tokenCalls = 0;
  const result = await getEvaluation(
    { origin: 'http://127.0.0.1:8088', tenantId },
    { getToken: async () => `token-${++tokenCalls}` },
    { id: 'scenario-1' },
    { payload: { object_id: 'tx-1' }, scenario: { trigger_object_type: 'transactions' }, rule: { stable_rule_id: 'A1' } },
  );
  assert.equal(result.triggered, true);
  assert.equal(requests[0].authorization, 'Bearer token-1');
  assert.equal(requests[1].authorization, 'Bearer token-2');
  assert.match(requests[1].url, /\/client-async-decision-executions\/execution-1$/);
});

test('evaluates every transaction with bounded concurrency, including deferred results', async (t) => {
  const transactions = Array.from({ length: 8 }, (_, i) => ({ object_id: `tx-${i}`, amount: i }));
  const requested = [];
  let inFlight = 0;
  let peak = 0;
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    if (url.includes('/client-async-decision-executions/')) {
      return new Response(JSON.stringify({
        async_decision_execution: { status: 'completed', result_body: { object_id: 'tx-0', results: [{ triggered: true }] } },
      }), { status: 200 });
    }
    assert.match(url, /\/decisions\/all$/);
    const body = JSON.parse(options.body);
    assert.equal(body.object_type, 'transactions');
    assert.equal(body.fields.object_id, body.object_id);
    requested.push(body.object_id);
    peak = Math.max(peak, ++inFlight);
    await new Promise(resolve => setTimeout(resolve, 5));
    inFlight--;
    if (body.object_id === 'tx-0') {
      return new Response(JSON.stringify({ async_decision_execution: { id: 'execution-1' } }), { status: 202 });
    }
    return new Response(JSON.stringify({ result: { object_id: body.object_id, results: [{ triggered: false }] } }), { status: 200 });
  });
  const summary = await evaluateTransactionBatch(
    { origin: 'http://localhost:8088', tenantId }, { getToken: async () => 'token' }, transactions,
  );
  assert.deepEqual(requested.sort(), transactions.map(tx => tx.object_id).sort());
  assert.ok(peak <= 5 && peak > 1);
  assert.deepEqual(summary, { completed: 8, triggered: 1 });
});

test('stops with transaction IDs when evaluations fail or have unknown outcomes', async (t) => {
  const requested = [];
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    const { object_id } = JSON.parse(options.body);
    requested.push(object_id);
    if (object_id === 'tx-1') return new Response(JSON.stringify({ error: 'unavailable' }), { status: 503 });
    return new Response(JSON.stringify({ result: { object_id, results: [] } }), { status: 200 });
  });
  await assert.rejects(
    evaluateTransactionBatch(
      { origin: 'http://localhost:8088', tenantId }, { getToken: async () => 'token' },
      [{ object_id: 'tx-1' }, { object_id: 'tx-2' }],
    ),
    /1\/2 transaction evaluations completed; 1 failed or have an unknown outcome: tx-1:.*Do not retry without reconciliation/,
  );
  assert.deepEqual(requested.sort(), ['tx-1', 'tx-2']);
});

test('continuous simulation can explicitly allow a mismatch and still evaluate every transaction', async (t) => {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'fraud-simulation-test-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const keyDir = path.join(directory, 'keys');
  generateClientKeyPair(keyDir, 'simulation-test');
  const envelope = JSON.parse(await readFile(new URL('../fraud-data-model.json', import.meta.url), 'utf8'));
  const model = portableModel(envelope.data_model);
  const different = structuredClone(envelope);
  different.data_model.tables[0].description = 'different';
  const warning = t.mock.method(console, 'warn', () => {});
  const logs = [];
  t.mock.method(console, 'log', (...args) => logs.push(args.join(' ')));
  const definitions = buildScenarioDefinitions();
  const scenarios = definitions.map((definition, index) => ({
    id: `scenario-${index}`, name: definition.name,
    trigger_object_type: definition.trigger_object_type, live_iteration_id: `iteration-${index}`,
  }));
  const ingested = [];
  const evaluated = [];
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    let response;
    if (url.endsWith('/oauth/token')) {
      response = { access_token: 'test-token', token_type: 'Bearer', expires_in: 900 };
    } else if (url.endsWith('/data-model/export')) {
      response = different;
    } else if (url.endsWith('/scenarios')) {
      response = { scenarios };
    } else if (url.endsWith('/rules')) {
      response = { rules: [] };
    } else if (url.includes('/iterations/')) {
      response = { iteration: { status: 'committed' } };
    } else if (url.includes('/ingest/')) {
      const records = JSON.parse(options.body);
      if (url.includes('/transactions/')) ingested.push(...records.map(record => record.object_id));
      response = { results: records.map(record => ({ object_id: record.object_id })) };
    } else if (url.endsWith('/decisions/all')) {
      const { object_id } = JSON.parse(options.body);
      assert.ok(ingested.includes(object_id), 'evaluation must follow ingestion');
      evaluated.push(object_id);
      response = { result: { object_id, results: [{ triggered: true }] } };
    } else {
      throw new Error(`unexpected URL ${url}`);
    }
    return new Response(JSON.stringify(response), { status: 200 });
  });
  const options = parseArgs([
    '--host', 'localhost:8088', '--tenant-id', tenantId, '--client-id', clientId,
    '--key-dir', keyDir, '--confirm-write', '--total-transactions', '3', '--batch-size', '3',
    '--allow-model-mismatch',
    '--duration-minutes', '1', '--checkpoint-file', path.join(directory, 'checkpoint.json'),
  ]);
  await runContinuousSimulation(options, model, definitions);
  assert.equal(warning.mock.callCount(), 1);
  assert.ok(logs.includes('Verified 5 live scenarios.'));
  assert.ok(!logs.some(line => line.includes('Verified data model')));
  assert.equal(ingested.length, 3);
  assert.deepEqual(evaluated.sort(), ingested.sort());
  const checkpoint = JSON.parse(await readFile(options.checkpointFile, 'utf8'));
  assert.equal(checkpoint.ingestedTransactions, 3);
  assert.equal(checkpoint.evaluatedTransactions, 3);
  assert.equal(checkpoint.triggeredScenarios, 3);
  assert.equal(checkpoint.incompleteBatch, null);
});

test('ingests simulation entities without metadata and within batch limits', async (t) => {
  const model = await loadModel();
  const pool = createEntityPool(model, runId);
  const batches = [];
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    const records = JSON.parse(options.body);
    const objectType = url.includes('/customers/') ? 'customers' : 'wallets';
    const fields = model.tables.find(table => table.name === objectType).fields;
    for (const record of records) {
      for (const name of Object.keys(record)) {
        assert.ok(name === 'object_id' || fields.some(field => field.name === name), `unexpected ${objectType}.${name}`);
      }
      for (const field of fields) {
        if (!field.nullable) assert.notEqual(record[field.name], undefined, `missing ${objectType}.${field.name}`);
        if (field.is_enum && record[field.name] != null) {
          assert.ok(field.enum_values.some(option => option.value === record[field.name]), `invalid ${objectType}.${field.name}`);
        }
      }
    }
    batches.push({ objectType, size: records.length });
    return new Response(JSON.stringify({ results: records.map(record => ({ object_id: record.object_id })) }), { status: 200 });
  });
  await ingestSimulationEntities({ origin: 'http://localhost:8088', tenantId }, { getToken: async () => 'token' }, pool);
  assert.deepEqual(batches, [
    { objectType: 'customers', size: 500 },
    { objectType: 'wallets', size: 500 },
    { objectType: 'wallets', size: 300 },
  ]);
  assert.equal(pool.wallets[0]._customer, pool.customers[0]);
  assert.equal(pool.customers[0]._accountAgeHours, 0);
});

test('reports validation fields without retrying rejected batches', async (t) => {
  let requests = 0;
  t.mock.method(globalThis, 'fetch', async () => {
    requests++;
    return new Response(JSON.stringify({
      error: { message: 'batch validation failed' },
      validation_errors: [{ field: 'status', code: 'invalid_value' }, { field: 'status', code: 'invalid_value' }],
    }), { status: 422 });
  });
  await assert.rejects(
    ingestBatchWithRetry({ origin: 'http://localhost:8088', tenantId }, { getToken: async () => 'token' }, 'customers', [{ object_id: 'test' }]),
    /2 validation errors; status: invalid_value/,
  );
  assert.equal(requests, 1);
});

test('generates integer account ages for day-zero transactions', async () => {
  const model = await loadModel();
  const pool = createEntityPool(model, runId, { customerCount: 1, walletCount: 1, destinationCount: 5 });
  const generator = new RealisticTransactionGenerator(pool, model, runId, {
    structuringLimit: 1000, nearLimitPercent: 10, lateCallbackSeconds: 300,
  });
  const fields = model.tables.find(table => table.name === 'transactions').fields;
  for (const record of generator.generateBatch(20)) {
    assert.equal(record.account_age_hours, 0);
    for (const field of fields) {
      const value = record[field.name];
      if (!field.nullable) assert.notEqual(value, undefined, `missing transactions.${field.name}`);
      if (value != null && field.data_type === 'int') assert.ok(Number.isInteger(value), `invalid transactions.${field.name}`);
      if (value != null && field.is_enum) assert.ok(field.enum_values.some(option => option.value === value), `invalid transactions.${field.name}`);
    }
  }
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
