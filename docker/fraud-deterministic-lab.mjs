import { randomBytes } from 'node:crypto';
import { setTimeout as delay } from 'node:timers/promises';
import { fixtureHash } from './fraud-fixture-data.mjs';

export class LabError extends Error {
  constructor(message, status = 422) { super(message); this.status = status; }
}

const durationMs = value => {
  const match = /^(?:P(\d+)D|(\d+)([hm]))$/.exec(value);
  if (!match) throw new LabError(`Unsupported fixture window: ${value}`);
  return match[1] ? Number(match[1]) * 86400000 : Number(match[2]) * (match[3] === 'h' ? 3600000 : 60000);
};

// This small fixture oracle supports only the AST used by these five scenario
// definitions. Actual acceptance and decisions always come from the live APIs.
export function evaluateFixture(node, target, dataset) {
  const fn = (node.function || node.name || '').toLowerCase();
  const children = node.children || [];
  const named = node.named_children || {};
  const evaluate = child => evaluateFixture(child, target, dataset);
  const value = key => evaluate(named[key]);
  if (!fn || fn === 'constant') return node.constant;
  if (fn === 'field_ref') return target[value('field')];
  if (fn === 'and') return children.every(evaluate);
  if (fn === 'or') return children.some(evaluate);
  if (fn === 'not') return !evaluate(children[0]);
  if (fn === 'related_field') {
    if (!['wallet_to_customer', 'transaction_to_customer'].includes(value('path'))) throw new LabError('Unsupported fixture relationship');
    return dataset.customers.find(row => row.object_id === target.user_id)?.[value('field')];
  }
  if (fn === 'timeadd') return new Date(Date.parse(value('timestampField')) - durationMs(value('duration'))).toISOString();
  if (fn === 'aggregator') {
    const table = value('tableName');
    if (!['transactions', 'wallets'].includes(table)) throw new LabError('Unsupported fixture aggregate table');
    const filters = named.filters?.children || [];
    const rows = dataset[table].filter(row => filters.every(filter => {
      const args = filter.named_children;
      return compare(args.operator.constant, row[args.fieldName.constant], evaluate(args.value));
    }));
    const aggregate = value('aggregator');
    const field = value('fieldName');
    if (aggregate === 'COUNT') return rows.length;
    if (aggregate === 'COUNT_DISTINCT') return new Set(rows.map(row => row[field]).filter(item => item != null)).size;
    if (aggregate === 'SUM') return rows.length ? rows.reduce((sum, row) => sum + (row[field] || 0), 0) : null;
    throw new LabError(`Unsupported fixture aggregate: ${aggregate}`);
  }
  if (fn === 'multiply' || fn === 'divide') {
    const [a, b] = children.map(evaluate);
    if (a == null || b == null) throw new LabError('Fixture contains a NULL arithmetic operand');
    return fn === 'multiply' ? a * b : a / b;
  }
  if (fn === 'in') return evaluate(children[1]).includes(evaluate(children[0]));
  if (['eq', 'neq', 'gt', 'gte', 'lt', 'lte'].includes(fn)) return compare(fn, evaluate(children[0]), evaluate(children[1]));
  throw new LabError(`Unsupported fixture function: ${fn}`);
}

function compare(operator, left, right) {
  if (left == null || right == null) return operator === 'eq' || operator === '=' ? left == null && right == null : false;
  switch (operator) {
    case '=': case 'eq': return left === right;
    case 'neq': return left !== right;
    case '>': case 'gt': return left > right;
    case '>=': case 'gte': return left >= right;
    case '<': case 'lt': return left < right;
    case '<=': case 'lte': return left <= right;
    default: throw new LabError(`Unsupported fixture comparison: ${operator}`);
  }
}

function* product(...dimensions) {
  if (!dimensions.length) { yield []; return; }
  const [first, ...rest] = dimensions;
  for (const value of first) for (const tail of product(...rest)) yield [value, ...tail];
}

function outcome(score, thresholds) {
  return score >= thresholds[2] ? 'decline' : score >= thresholds[1] ? 'block_and_review' : score >= thresholds[0] ? 'review' : 'approve';
}

export function generateDeterministicPlan({ definition, expectations, runId, makeRecord, parameters = {}, now = new Date() }) {
  if (!expectations || typeof expectations !== 'object' || Array.isArray(expectations)) throw new LabError('Choose the expected rule outcomes.');
  const ids = new Set(definition.rules.map(rule => rule.stable_rule_id));
  for (const [id, expected] of Object.entries(expectations)) {
    if (!ids.has(id) || !['hit', 'no_hit', 'ignore'].includes(expected)) throw new LabError(`Invalid rule selection: ${id}`);
  }
  if (!Object.values(expectations).some(value => value !== 'ignore')) throw new LabError('Select at least one rule to match or not match.');
  const wanted = id => expectations[id] === 'hit';
  const planId = randomBytes(8).toString('hex');
  const prefix = `${runId}-lab-${planId}`;
  const userId = `${prefix}-user`;
  const walletId = `${prefix}-wallet`;
  const txId = `${prefix}-target`;
  const nearFloor = (parameters.structuringLimit ?? 1000) * (1 - (parameters.nearLimitPercent ?? 10) / 100);
  const late = parameters.lateCallbackSeconds ?? 300;
  const baseCustomer = { has_identity_number: true, days_since_last_activity: 30, auth_change_hours_ago: 48 };
  const baseWallet = { user_id: userId, wallet_type: 'mobile', wallet_number_hash: fixtureHash(`wallet/${walletId}`), is_third_party: false, is_verified: false, name_match: 'exact', attached_at: now.toISOString(), minutes_since_signup: 120, third_party_wallet_count: 1 };
  const baseTx = { trans_id: txId, leg: 'debit', state: 'settled', operation: 'TRANSFER', channel: 'app', amount: 100, user_median_amount_90d: 1000, currency: 'GHS', country_id: 'GH', user_id: userId, wallet_id: walletId, destination_ref: `${prefix}-destination`, is_recurring: false, expected_callback: false, initiated_at: now.toISOString(), account_age_hours: 48, user_txn_count_24h: 2, destination_first_seen_hours: 48, seconds_to_callback: 0 };
  const record = (table, id, fields) => makeRecord(table, id, fields, now);
  const candidate = (tx = {}, customer = {}, wallet = {}, history = [], extraWallet = false) => {
    const customers = [record('customers', userId, { ...baseCustomer, ...customer })];
    const targetWallet = record('wallets', walletId, { ...baseWallet, ...wallet });
    const target = definition.trigger_object_type === 'wallets' ? targetWallet : record('transactions', txId, { ...baseTx, ...tx });
    const wallets = [targetWallet];
    if (extraWallet) {
      const otherUser = `${prefix}-other-user`;
      customers.push(record('customers', otherUser, baseCustomer));
      wallets.push(record('wallets', `${prefix}-other-wallet`, { ...baseWallet, user_id: otherUser }));
    }
    const transactions = history.map((entry, i) => {
      const { ago, ...fields } = entry;
      const id = `${prefix}-history-${i + 1}`;
      const row = record('transactions', id, { ...baseTx, ...fields, trans_id: id, initiated_at: new Date(now.getTime() - ago).toISOString() });
      if (!customers.some(item => item.object_id === row.user_id)) {
        customers.push(record('customers', row.user_id, baseCustomer));
        const senderWallet = `${prefix}-sender-wallet-${i}`;
        wallets.push(record('wallets', senderWallet, { ...baseWallet, user_id: row.user_id, wallet_number_hash: fixtureHash(`wallet/${senderWallet}`) }));
        row.wallet_id = senderWallet;
        if (Object.hasOwn(row, 'funding_network')) row.funding_network = wallets.at(-1).financial_entity_id;
      }
      return row;
    });
    if (definition.trigger_object_type === 'transactions') transactions.push(target);
    return { target, customers, wallets, transactions };
  };
  let candidates;
  switch (definition.name) {
    case 'Suspicious destination concentration': {
      const history = [];
      const many = wanted('many-senders-one-destination');
      const rapid = wanted('rapid-repeat-to-destination');
      if (rapid || many) for (let i = 0; i < 2; i++) history.push({ leg: 'credit', amount: 1,
        user_id: many ? `${prefix}-sender-${i}` : userId, ago: rapid ? 120000 + i : 7200000 + i });
      if (wanted('destination-volume-spike-average')) history.push({ leg: 'credit', amount: 300, ago: 172800000 });
      candidates = [candidate({ leg: 'credit', amount: 100, destination_first_seen_hours: wanted('destination-newly-seen') ? 1 : 48 }, {}, {}, history)];
      break;
    }
    case 'Funding instrument risk':
      candidates = [...product(['exact', 'none', 'unknown'], [false, true])].map(([name, verified]) => candidate({},
        { has_identity_number: !wanted('third-party-wallet-no-kyc') },
        { is_third_party: wanted('third-party-wallet-no-kyc'), name_match: name, is_verified: verified,
          minutes_since_signup: wanted('wallet-attached-near-signup') ? 30 : 120,
          third_party_wallet_count: wanted('multiple-third-party-wallets') ? 2 : 1 }, [], wanted('instrument-seen-on-another-account')));
      break;
    case 'Account lifecycle risk':
      candidates = [...product([48, 2, 0], [100, 1001], [true, false], [2, 1])].map(([age, amount, identity, count]) => candidate(
        { state: 'initiated', account_age_hours: age, amount, user_txn_count_24h: count },
        { has_identity_number: identity, days_since_last_activity: wanted('dormant-reactivation') ? 120 : 30,
          auth_change_hours_ago: wanted('recent-credential-change') ? 1 : 48 }));
      break;
    case 'Anomalous transaction behaviour':
      candidates = [...product(['settled', 'failed'], [...new Set([100, 600, nearFloor, 1001])], [1000, 10])].map(([state, amount, median]) => {
        const history = [];
        const failures = wanted('repeated-failed-debits') ? state === 'failed' ? 2 : 3 : 0;
        for (let i = 0; i < failures; i++) history.push({ state: 'failed', amount: 0, ago: 120000 + i });
        if (wanted('success-after-failed-debits') && failures < 3) for (let i = 0; i < 3; i++) history.push({ state: 'failed', amount: 0, ago: 172800000 + i });
        if (wanted('probe-then-escalate')) history.push({ amount: Math.min(5, nearFloor / 2), state: 'settled', ago: 60000 });
        if (wanted('structuring-near-limit')) for (let i = 0; i < (amount >= nearFloor && amount < (parameters.structuringLimit ?? 1000) ? 3 : 4); i++) history.push({ amount: nearFloor, state: 'blocked', ago: 180000 + i });
        if (wanted('burst-velocity')) while (history.filter(row => row.ago < 86400000).length < 9) {
          history.push({ amount: 0, state: 'blocked', ago: 240000 + history.length });
        }
        const count24h = history.filter(row => row.ago < 86400000).length + 1;
        return candidate({ state, amount, user_median_amount_90d: median, user_txn_count_24h: count24h }, {}, {}, history);
      });
      break;
    case 'Transaction resolution quality':
      candidates = [...product(['callback_received', 'undetermined'], ['debit', 'credit'], [100, 1001], [...new Set([0, late + 1, Math.max(0, late - 1), 899, 900, 901])])].map(([state, leg, amount, seconds]) => {
        const history = [];
        if (wanted('repeated-undetermined-resolution')) for (let i = 0; i < (state === 'undetermined' ? 1 : 2); i++) history.push({ state: 'undetermined', ago: 120000 + i });
        return candidate({ state, leg, amount, seconds_to_callback: seconds }, {}, {}, history);
      });
      break;
    default: throw new LabError('This scenario does not have a deterministic data recipe.');
  }
  for (const dataset of candidates) {
    if (!evaluateFixture(definition.trigger, dataset.target, dataset)) continue;
    const rules = definition.rules.map(rule => ({ id: rule.stable_rule_id, name: rule.name,
      expected: expectations[rule.stable_rule_id] || 'ignore',
      predicted: evaluateFixture(rule.formula, dataset.target, dataset) ? 'hit' : 'no_hit', scoreModifier: rule.score_modifier }));
    if (!rules.every(rule => rule.expected === 'ignore' || rule.expected === rule.predicted)) continue;
    const score = rules.filter(rule => rule.predicted === 'hit').reduce((sum, rule) => sum + rule.scoreModifier, 0);
    const records = [];
    for (const table of ['customers', 'wallets', 'transactions']) for (const row of dataset[table]) {
      if (row === dataset.target) continue;
      records.push({ table, role: table === 'transactions' ? 'history' : 'supporting', data: row, status: 'queued' });
    }
    records.push({ table: definition.trigger_object_type, role: 'target', data: dataset.target, status: 'queued' });
    return { id: planId, scenario: definition.name, objectType: definition.trigger_object_type,
      targetId: dataset.target.object_id, createdAt: now.toISOString(), status: 'ready', records, rules,
      predictedScore: score, predictedOutcome: outcome(score, definition.thresholds), submittedRecords: 0 };
  }
  throw new LabError('These rule outcomes cannot be combined by this scenario’s data recipe. Try changing the conflicting expectations to “Any”. Day-zero implies new-account; wallet name mismatch excludes unknown-name; callback thresholds and failed-debit rules can also imply one another. No data was submitted.');
}

export class DeterministicLab {
  constructor({ definitions, generate, submitRecord, evaluate, verify = async () => {}, monitor, preview = false, wait = delay }) {
    Object.assign(this, { definitions, generate, submitRecord, evaluate, verify, monitor, preview, wait });
    this.plan = null;
    this.ready = false;
    this.task = null;
  }

  snapshot(full = false) {
    const plan = this.plan && (full ? this.plan : { ...this.plan, records: this.plan.records.map(({ data, ...record }) => ({ ...record, id: data.object_id })) });
    return { ready: this.ready, preview: this.preview, scenarios: this.definitions.map(definition => ({ name: definition.name,
      objectType: definition.trigger_object_type, rules: definition.rules.map(rule => ({ id: rule.stable_rule_id, name: rule.name, description: rule.description, score: rule.score_modifier })) })), plan };
  }

  create({ scenario, expectations }) {
    if (!this.ready) throw new LabError('Model and scenario checks have not completed.', 409);
    if (this.task) throw new LabError('Wait for the current submission to finish.', 409);
    const definition = this.definitions.find(item => item.name === scenario);
    if (!definition) throw new LabError('Select a known scenario.');
    this.plan = this.generate(definition, expectations);
    this.monitor.target = this.monitor.counts.evaluated + 1;
    this.monitor.queue(this.plan.records.map(record => record.data));
    this.monitor.phase('ready', 'Generated data is ready for review. Nothing has been submitted.');
    return this.snapshot(true);
  }

  submit({ planId, initialDelayMs = 3000, intervalMs = 1000 }) {
    if (!this.ready || !this.plan || this.plan.id !== planId) throw new LabError('Generate a current dataset first.', 409);
    if (this.task || this.plan.status !== 'ready') throw new LabError('This dataset has already been submitted. Generate a new dataset to run another case.', 409);
    for (const value of [initialDelayMs, intervalMs]) if (!Number.isInteger(value) || value < 0 || value > 120000) throw new LabError('Submission waits must be whole milliseconds from 0 to 120000.');
    const plan = this.plan;
    plan.status = 'waiting';
    this.abort = new AbortController();
    this.task = this.run(plan, initialDelayMs, intervalMs).catch(error => {
      plan.status = this.abort.signal.aborted ? 'cancelled' : 'failed';
      plan.error = error.message;
      this.monitor.phase(plan.status, error.message);
    }).finally(() => { this.task = null; });
    return this.snapshot();
  }

  async run(plan, initialDelayMs, intervalMs) {
    const signal = this.abort.signal;
    const wait = async ms => {
      plan.nextSubmitAt = Date.now() + ms;
      this.monitor.nextDispatchAt = plan.nextSubmitAt;
      await this.wait(ms, undefined, { signal });
      if (signal.aborted) throw new Error('Submission stopped. Already accepted records remain saved.');
    };
    this.monitor.phase('running', 'Waiting before the first record submission');
    await wait(initialDelayMs);
    await this.verify(plan.scenario);
    plan.status = 'submitting';
    for (const [index, record] of plan.records.entries()) {
      if (index) await wait(intervalMs);
      if (signal.aborted) throw new Error('Submission stopped. Already accepted records remain saved.');
      record.status = 'submitting';
      this.monitor.queue(plan.records.slice(index + 1).map(item => item.data));
      try {
        const response = await this.submitRecord(record);
        record.status = 'accepted';
        record.response = response;
        plan.submittedRecords++;
      } catch (error) {
        record.status = 'failed';
        record.error = error.message;
        throw error;
      }
    }
    plan.nextSubmitAt = null;
    if (signal.aborted) throw new Error('Submission stopped. Already accepted records remain saved.');
    plan.status = 'evaluating';
    this.monitor.beginCase(plan.records.at(-1).data);
    const result = await this.evaluate(plan);
    plan.result = result;
    const executions = new Map((result.rule_executions || []).map(rule => [rule.rule_id ?? rule.RuleID, rule]));
    plan.checks = plan.rules.map(rule => {
      const actualRule = this.liveRules?.find(item => item.stable_rule_id === rule.id);
      const execution = executions.get(actualRule?.id || rule.id);
      const actual = execution?.outcome ?? execution?.Outcome ?? 'missing';
      return { ...rule, actual, passed: rule.expected === 'ignore' ? null : rule.expected === actual };
    });
    plan.status = plan.checks.some(check => check.passed === false) ? 'mismatch' : 'completed';
    this.monitor.phase(plan.status, plan.status === 'completed' ? 'The generated case matched its selected rule outcomes.' : 'The engine returned different rule outcomes. Inspect the comparison below.');
  }

  async stop() {
    this.abort?.abort();
    if (this.task) await this.task;
  }
}
