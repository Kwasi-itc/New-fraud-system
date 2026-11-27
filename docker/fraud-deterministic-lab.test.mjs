import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { buildScenarioDefinitions, portableModel } from './provision-fraud-scenarios-ec2.mjs';
import { buildTestCases, completeRecord, createDeterministicWorkbench, AuthTokenManager } from './test-fraud-scenarios-ec2.mjs';
import { DeterministicLab, evaluateFixture, generateDeterministicPlan } from './fraud-deterministic-lab.mjs';
import { SimulationMonitor } from './fraud-simulation-monitor.mjs';

const envelope = JSON.parse(await readFile(new URL('../fraud-data-model.json', import.meta.url), 'utf8'));
const model = portableModel(envelope.data_model);
const definitions = buildScenarioDefinitions();
const tables = new Map(model.tables.map(table => [table.name, table]));
const makeRecord = (table, id, fields, now) => completeRecord(tables, table, id, fields, now);
const generate = (definition, expectations) => generateDeterministicPlan({ definition, expectations, runId: 'lab-test', makeRecord });

test('fixture oracle agrees with all existing positive and negative owner-rule fixtures', () => {
  const data = buildTestCases(definitions, model, 'oracle-test');
  for (const item of data.cases) assert.equal(evaluateFixture(item.rule.formula, item.payload, data), item.shouldHit, item.rule.stable_rule_id);
});

test('combined aggregation case has real failed, probe and structuring history before its target', () => {
  const definition = definitions[3];
  const plan = generate(definition, Object.fromEntries(definition.rules.map(rule => [rule.stable_rule_id, 'hit'])));
  const transactions = plan.records.filter(record => record.table === 'transactions').map(record => record.data);
  const target = transactions.at(-1);
  assert.equal(plan.records.at(-1).role, 'target');
  assert.equal(target.state, 'settled');
  assert.ok(transactions.filter(row => row.state === 'failed' && Date.parse(target.initiated_at) - Date.parse(row.initiated_at) < 3600000).length >= 3);
  assert.ok(transactions.some(row => row.state === 'settled' && row.amount < 10 && row.initiated_at < target.initiated_at));
  assert.ok(transactions.filter(row => row.amount >= 900 && row.amount < 1000).length >= 4);
  assert.equal(target.user_txn_count_24h, 10);
  assert.ok(target.amount >= target.user_median_amount_90d * 10);
  assert.equal(plan.predictedScore, 215);
  assert.equal(plan.predictedOutcome, 'decline');
  for (const record of plan.records) for (const field of tables.get(record.table).fields) {
    if (!field.nullable) assert.notEqual(record.data[field.name], undefined, `${record.table}.${field.name}`);
    if (field.is_enum && record.data[field.name] != null) assert.ok(field.enum_values.some(item => item.value === record.data[field.name]));
  }
});

test('destination history can match distinct senders without rapid-repeat and preserves ownership links', () => {
  const plan = generate(definitions[0], { 'many-senders-one-destination': 'hit', 'rapid-repeat-to-destination': 'no_hit' });
  const transactions = plan.records.filter(row => row.table === 'transactions').map(row => row.data);
  assert.equal(new Set(transactions.map(row => row.user_id)).size, 3);
  const target = transactions.at(-1);
  assert.ok(transactions.slice(0, -1).every(row => Date.parse(target.initiated_at) - Date.parse(row.initiated_at) > 3600000));
  for (const tx of transactions) {
    const wallet = plan.records.find(row => row.table === 'wallets' && row.data.object_id === tx.wallet_id);
    assert.equal(wallet.data.user_id, tx.user_id);
  }
});

test('contradictory rule expectations fail before submission and generated plans use isolated identities', () => {
  assert.throws(() => generate(definitions[2], { 'day-zero-account': 'hit', 'new-account': 'no_hit' }), /cannot be combined/);
  assert.throws(() => generate(definitions[1], { 'wallet-name-mismatch': 'hit', 'verified-with-unknown-name': 'hit' }), /cannot be combined/);
  assert.throws(() => generate(definitions[4], { 'very-late-callback': 'hit', 'late-callback': 'no_hit' }), /cannot be combined/);
  assert.throws(() => generate(definitions[0], { unknown: 'hit' }), /Invalid rule/);
  assert.throws(() => generate(definitions[0], {}), /at least one/);
  const one = generate(definitions[0], { 'many-senders-one-destination': 'hit' });
  const two = generate(definitions[0], { 'many-senders-one-destination': 'hit' });
  const ids = new Set(one.records.map(row => row.data.object_id));
  assert.ok(two.records.every(row => !ids.has(row.data.object_id)));
  assert.notEqual(one.records.at(-1).data.destination_ref, two.records.at(-1).data.destination_ref);
});

function controller(overrides = {}) {
  const submitted = [];
  const waits = [];
  const lab = new DeterministicLab({ definitions, monitor: new SimulationMonitor({ runId: 'controller', totalTransactions: 1 }),
    generate, wait: async ms => waits.push(ms), submitRecord: async record => { submitted.push(record.data.object_id); return { accepted: true }; },
    evaluate: async plan => ({ triggered: true, rule_executions: plan.rules.map(rule => ({ rule_id: rule.id, outcome: rule.predicted })) }),
    ...overrides,
  });
  lab.ready = true;
  return { lab, submitted, waits };
}

test('generation is read-only; submission locks once and waits before first record and between all records', async () => {
  const { lab, submitted, waits } = controller();
  lab.create({ scenario: definitions[0].name, expectations: { 'many-senders-one-destination': 'hit' } });
  assert.equal(submitted.length, 0);
  const planId = lab.plan.id;
  lab.submit({ planId, initialDelayMs: 2500, intervalMs: 750 });
  assert.equal(submitted.length, 0);
  assert.throws(() => lab.submit({ planId }), /already been submitted/);
  assert.throws(() => lab.create({ scenario: definitions[0].name, expectations: {} }), /current submission/);
  await lab.task;
  assert.equal(waits[0], 2500);
  assert.deepEqual(waits.slice(1), Array(lab.plan.records.length - 1).fill(750));
  assert.deepEqual(submitted, lab.plan.records.map(record => record.data.object_id));
  assert.equal(lab.plan.status, 'completed');
  assert.ok(lab.plan.checks.filter(check => check.expected !== 'ignore').every(check => check.passed));
  assert.throws(() => lab.submit({ planId }), /already been submitted/);
});

test('partial failures retain accepted progress and mismatch results do not report success', async () => {
  let calls = 0;
  const { lab } = controller({ submitRecord: async () => { if (++calls === 3) throw new Error('ingestion unavailable'); return { accepted: true }; } });
  lab.create({ scenario: definitions[0].name, expectations: { 'many-senders-one-destination': 'hit' } });
  lab.submit({ planId: lab.plan.id, initialDelayMs: 0, intervalMs: 0 });
  await lab.task;
  assert.equal(lab.plan.status, 'failed');
  assert.equal(lab.plan.submittedRecords, 2);
  assert.equal(lab.plan.records[2].status, 'failed');
  assert.equal(lab.plan.records.at(-1).status, 'queued');
  const mismatch = controller({ evaluate: async () => ({ triggered: true, rule_executions: [] }) }).lab;
  mismatch.create({ scenario: definitions[2].name, expectations: { 'day-zero-account': 'hit' } });
  mismatch.submit({ planId: mismatch.plan.id, initialDelayMs: 0, intervalMs: 0 });
  await mismatch.task;
  assert.equal(mismatch.plan.status, 'mismatch');
  assert.equal(mismatch.plan.checks.find(check => check.id === 'day-zero-account').actual, 'missing');
});

test('shutdown during the initial wait cancels the case before any write', async () => {
  const { setTimeout } = await import('node:timers/promises');
  const { lab, submitted } = controller({ wait: setTimeout });
  lab.create({ scenario: definitions[2].name, expectations: { 'day-zero-account': 'hit' } });
  lab.submit({ planId: lab.plan.id, initialDelayMs: 120000, intervalMs: 1000 });
  await lab.stop();
  assert.equal(submitted.length, 0);
  assert.equal(lab.plan.status, 'cancelled');
  assert.equal(lab.plan.submittedRecords, 0);
});

test('live workbench sends reviewed records in order and evaluates the final target only', async t => {
  t.mock.method(AuthTokenManager.prototype, 'getToken', async () => 'test-token');
  const scenarios = definitions.map((definition, i) => ({ id: `scenario-${i}`, name: definition.name, trigger_object_type: definition.trigger_object_type, live_iteration_id: `iteration-${i}` }));
  const writes = [];
  let lab;
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    let body;
    if (url.endsWith('/data-model/export')) body = envelope;
    else if (url.endsWith('/scenarios')) body = { scenarios };
    else if (url.endsWith('/rules')) {
      const index = scenarios.findIndex(scenario => url.includes(`/scenarios/${scenario.id}/`));
      body = { rules: definitions[index].rules.map(rule => ({ ...rule, id: rule.stable_rule_id })) };
    } else if (url.includes('/iterations/')) {
      const index = scenarios.findIndex(scenario => url.includes(`/scenarios/${scenario.id}/`));
      const definition = definitions[index];
      body = { iteration: { status: 'committed', trigger_formula: definition.trigger,
        score_review_threshold: definition.thresholds[0], score_block_and_review_threshold: definition.thresholds[1], score_decline_threshold: definition.thresholds[2] } };
    } else if (url.includes('/ingest/')) {
      const records = JSON.parse(options.body);
      assert.equal(records.length, 1);
      writes.push({ type: 'ingest', id: records[0].object_id });
      body = { results: records.map(record => ({ object_id: record.object_id })) };
    } else if (url.endsWith('/evaluate')) {
      writes.push({ type: 'evaluate', id: JSON.parse(options.body).object_id });
      body = { result: { triggered: true, decision: { outcome: 'review', score: 50, scenario_id: 'scenario-0' },
        rule_executions: definitions[0].rules.map(rule => ({ rule_id: rule.stable_rule_id, rule_name: rule.name,
          outcome: rule.stable_rule_id === 'many-senders-one-destination' ? 'hit' : 'no_hit', score_modifier: rule.score_modifier })) } };
    } else throw new Error(`Unexpected request: ${url}`);
    return new Response(JSON.stringify(body), { status: 200 });
  });
  const options = { runId: 'contract', tenantId: 'tenant-test', origin: 'http://localhost:8088', demo: false,
    monitor: new SimulationMonitor({ runId: 'contract', totalTransactions: 1 }) };
  lab = await createDeterministicWorkbench(options, model, definitions);
  lab.create({ scenario: definitions[0].name, expectations: { 'many-senders-one-destination': 'hit', 'rapid-repeat-to-destination': 'no_hit' } });
  assert.equal(writes.length, 0);
  lab.submit({ planId: lab.plan.id, initialDelayMs: 0, intervalMs: 0 });
  await lab.task;
  assert.equal(lab.plan.status, 'completed');
  assert.deepEqual(writes.filter(write => write.type === 'ingest').map(write => write.id), lab.plan.records.map(record => record.data.object_id));
  assert.deepEqual(writes.filter(write => write.type === 'evaluate'), [{ type: 'evaluate', id: lab.plan.targetId }]);
  assert.equal(options.monitor.snapshot().activeCount, 0);
  assert.equal(options.monitor.snapshot().counts.evaluated, 1);
});
