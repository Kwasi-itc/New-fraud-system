import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { fixtureHash, fixturePerson } from './fraud-fixture-data.mjs';
import { completeRecord, createEntityPool, RealisticTransactionGenerator, buildTestCases } from './test-fraud-scenarios-ec2.mjs';
import { buildScenarioDefinitions, portableModel } from './provision-fraud-scenarios-ec2.mjs';
import { generateDeterministicPlan } from './fraud-deterministic-lab.mjs';
import { SimulationMonitor } from './fraud-simulation-monitor.mjs';

const model = portableModel(JSON.parse(await readFile(new URL('../fraud-data-model.json', import.meta.url), 'utf8')).data_model);
const tables = new Map(model.tables.map(table => [table.name, table]));
const makeRecord = (table, id, fields, now) => completeRecord(tables, table, id, fields, now);

test('synthetic identities are reproducible and use plausible contact formats', () => {
  const person = fixturePerson('customer-1');
  assert.deepEqual(person, fixturePerson('customer-1'));
  assert.match(person.name, /^[A-Z][a-z]+ [A-Z][a-z]+$/);
  assert.match(person.phone, /^\+233(?:24|20|27)\d{7}$/);
  assert.match(person.email, /^[a-z]+\.[a-z]+\.\d{4}@example\.test$/);
  assert.match(person.maskedPhone, /^\+233 \d{2} \*{3} \d{4}$/);
  assert.match(fixtureHash('wallet-1'), /^[a-f0-9]{64}$/);
  assert.notEqual(fixtureHash('wallet-1'), fixtureHash('wallet-2'));
});

test('customer names, wallet name enquiries, phone contacts and visible traffic use the same owner', () => {
  const pool = createEntityPool(model, 'realistic', { customerCount: 40, walletCount: 60, destinationCount: 20 });
  const generator = new RealisticTransactionGenerator(pool, model, 'realistic', { structuringLimit: 1000, nearLimitPercent: 10, lateCallbackSeconds: 300 });
  const transactions = generator.generateBatch(100);
  for (const wallet of pool.wallets) {
    const customer = pool.customers.find(row => row.object_id === wallet.user_id);
    assert.equal(wallet.account_holder_name, customer.display_name);
    assert.match(wallet.wallet_number_masked, /^\+233 \d{2} \*{3} \d{4}$/);
    if (wallet.name_match === 'exact') assert.equal(wallet.name_enquiry_name, customer.display_name);
    if (wallet.name_match === 'none') assert.notEqual(wallet.name_enquiry_name, customer.display_name);
    if (wallet.name_match === 'unknown') assert.equal(wallet.name_enquiry_name, null);
  }
  for (const tx of transactions) {
    assert.equal(tx.source_msisdn, fixturePerson(tx.user_id).phone);
    assert.equal(tx.funding_network, pool.wallets.find(row => row.object_id === tx.wallet_id).financial_entity_id);
    assert.match(tx.payment_ref, /^PAY-GH-[A-F0-9]{12}$/);
    assert.equal(tx.original_amount, tx.amount);
  }
  const monitor = new SimulationMonitor({ runId: 'realistic', totalTransactions: 100 });
  monitor.queue(transactions);
  assert.equal(monitor.snapshot().upcoming[0].customerName, pool.customers.find(row => row.object_id === transactions[0].user_id).display_name);
  assert.ok(new Set(pool.customers.map(row => row.display_name)).size > 10);
  assert.equal(pool.wallets[0].wallet_number_hash, pool.wallets[1].wallet_number_hash);
  assert.equal(pool.wallets[0].wallet_number_masked, pool.wallets[1].wallet_number_masked);
});

test('explicit overrides and rule-driving fields survive enrichment, and unsupported fields stay out', () => {
  const fields = { display_name: 'Custom Customer', has_identity_number: true, days_since_last_activity: 120 };
  const customer = makeRecord('customers', 'custom-id', fields);
  assert.equal(customer.display_name, 'Custom Customer');
  assert.equal(customer.has_identity_number, true);
  assert.equal(customer.days_since_last_activity, 120);
  const reduced = new Map([['customers', { name: 'customers', fields: [{ name: 'display_name', data_type: 'string', nullable: false }] }]]);
  assert.deepEqual(Object.keys(completeRecord(reduced, 'customers', 'small')), ['object_id', 'display_name']);
  const tx = makeRecord('transactions', 'custom-tx', { user_id: 'custom-id', amount: 900, state: 'failed', source_msisdn: '+233240001111', payment_ref: 'explicit-payment', user_median_amount_90d: null });
  assert.equal(tx.source_msisdn, '+233240001111');
  assert.equal(tx.payment_ref, 'explicit-payment');
  assert.equal(tx.amount, 900);
  assert.equal(tx.state, 'failed');
  assert.equal(tx.user_median_amount_90d, null);
  assert.equal(tx.response_code, '05');
});

test('both deterministic fixture paths use complete model-valid names and preserve shared instruments', () => {
  const definitions = buildScenarioDefinitions();
  const fixtures = buildTestCases(definitions, model, 'named-cases');
  const plan = generateDeterministicPlan({ definition: definitions[0], expectations: { 'many-senders-one-destination': 'hit' }, runId: 'named-lab', makeRecord });
  for (const customer of [...fixtures.customers, ...plan.records.filter(record => record.table === 'customers').map(record => record.data)]) {
    assert.equal(customer.display_name, fixturePerson(customer.object_id).name);
    assert.match(customer.username_hash, /^[a-f0-9]{64}$/);
    assert.ok(!customer.display_name.startsWith('test-'));
  }
  for (const record of plan.records) {
    const available = new Set(tables.get(record.table).fields.map(field => field.name));
    for (const name of Object.keys(record.data)) assert.ok(name === 'object_id' || available.has(name), `${record.table}.${name}`);
    if (record.table === 'transactions') {
      const wallet = plan.records.find(item => item.table === 'wallets' && item.data.object_id === record.data.wallet_id);
      assert.equal(record.data.funding_network, wallet.data.financial_entity_id);
    }
  }
});
