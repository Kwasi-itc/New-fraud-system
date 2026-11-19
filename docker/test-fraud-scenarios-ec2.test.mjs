import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { buildScenarioDefinitions, portableModel } from './provision-fraud-scenarios-ec2.mjs';
import { buildTestCases, parseArgs } from './test-fraud-scenarios-ec2.mjs';

const tenantId = 'd7943f99-4402-428a-a539-0b2ecb6fcc17';
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
});

test('generates isolated fire and no-fire fixtures for every provisioned rule', async () => {
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
