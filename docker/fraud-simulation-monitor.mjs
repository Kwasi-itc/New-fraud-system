export { startDashboard } from './fraud-simulation-server.mjs';
import { fixturePerson } from './fraud-fixture-data.mjs';

const LIMITS = { recent: 60, events: 30, clients: 8 };
const rank = { approve: 0, review: 1, block_and_review: 2, decline: 3 };
const text = (value, limit = 240) => String(value ?? '').replace(/[\u0000-\u001f\u007f]/g, ' ').slice(0, limit);

function transactionSummary(transaction) {
  return {
    id: text(transaction.object_id), amount: transaction.amount ?? null,
    customerName: text(transaction.display_name || transaction.account_holder_name || fixturePerson(transaction.user_id || transaction.object_id).name),
    currency: text(transaction.currency || 'GHS', 12), leg: text(transaction.leg, 20),
    state: text(transaction.state, 40), user: text(transaction.user_id),
    destination: text(transaction.destination_ref),
  };
}

// Live HTTP responses use DTO names; async result_body currently serializes
// decision-domain structs with Go field names. Support both owner contracts.
function decisionSummary(item) {
  const decision = item.decision;
  return {
    id: decision?.id ?? decision?.ID,
    scenarioId: decision?.scenario_id ?? decision?.ScenarioID,
    outcome: decision?.outcome ?? decision?.Outcome,
    score: decision?.score ?? decision?.Score,
  };
}

// Retain a bounded response projection. AST evidence and input records are not
// copied into a second, growing investigation history for the dashboard.
function responseSummary(result, scenarioNames) {
  const results = Array.isArray(result.results) ? result.results : [result];
  let remainingRules = 40;
  return results.slice(0, 12).map(item => {
    const decision = decisionSummary(item);
    const rules = (item.rule_executions || []).slice(0, remainingRules);
    remainingRules -= rules.length;
    return {
      triggered: item.triggered === true,
      scenario: scenarioNames.get(decision.scenarioId) || text(decision.scenarioId || 'Scenario'),
      decisionId: text(decision.id), outcome: text(decision.outcome),
      score: decision.score ?? null,
      rules: rules.map(rule => ({
        name: text(rule.rule_name || rule.RuleName), outcome: text(rule.outcome || rule.Outcome),
        modifier: rule.score_modifier ?? rule.ScoreModifier ?? 0, reason: text(rule.skip_reason ?? rule.SkipReason),
      })),
    };
  });
}

export class SimulationMonitor {
  constructor({ runId, totalTransactions, demo = false, deterministic = false, now = Date.now }) {
    this.now = now;
    this.runId = runId;
    this.target = totalTransactions;
    this.mode = deterministic ? 'deterministic' : demo ? 'demo' : 'live';
    this.preview = demo;
    this.startedAt = now();
    this.status = 'preflight';
    this.message = 'Checking the model and published scenarios';
    this.upcoming = [];
    this.active = new Map();
    this.recent = [];
    this.events = [];
    this.scenarioNames = new Map();
    this.nextDispatchAt = null;
    this.counts = { ingested: 0, evaluated: 0, errors: 0, triggered: 0, hits: 0, skipped: 0, flagged: 0 };
    this.outcomes = { approve: 0, review: 0, block_and_review: 0, decline: 0, no_trigger: 0 };
    this.rateBuckets = [];
    this.details = new Map();
    this.detailBytes = 0;
  }

  note(message, kind = 'info') {
    this.events.unshift({ at: this.now(), kind, message: text(message, 700) });
    this.events.length = Math.min(this.events.length, LIMITS.events);
  }

  phase(status, message) {
    this.status = status;
    this.message = text(message, 700);
    this.note(message, status === 'failed' ? 'error' : 'info');
    if (status !== 'running') this.nextDispatchAt = null;
    if (['completed', 'stopped', 'failed', 'cancelled', 'mismatch'].includes(status)) this.endedAt = this.now();
    else this.endedAt = undefined;
  }

  setScenarios(scenarios) {
    for (const { scenario } of scenarios.values()) this.scenarioNames.set(scenario.id, scenario.name);
  }

  queue(transactions, dispatchAt = null) {
    this.upcoming = transactions.map(transactionSummary);
    for (const transaction of transactions) this.capture(transaction.object_id, 'queuedRequest', {
      object_id: transaction.object_id, fields: transaction,
    });
    this.nextDispatchAt = dispatchAt;
  }

  beginCase(transaction) {
    this.active.set(transaction.object_id, { ...transactionSummary(transaction), stage: 'ready', startedAt: this.now() });
  }

  capture(id, section, value) {
    const previous = this.details.get(id);
    if (previous) { this.detailBytes -= previous.bytes; this.details.delete(id); }
    const bytes = Buffer.from(JSON.stringify(value, null, 2));
    const detail = { ...(previous?.value || {}), [section]: {
      json: bytes.subarray(0, 65536).toString('utf8'), truncated: bytes.length > 65536,
    } };
    const retained = Buffer.byteLength(JSON.stringify(detail));
    this.details.set(id, { value: detail, bytes: retained });
    this.detailBytes += retained;
    while (this.detailBytes > 8 * 1024 * 1024 || this.details.size > 1100) {
      const first = this.details.keys().next().value;
      this.detailBytes -= this.details.get(first).bytes;
      this.details.delete(first);
    }
  }

  detail(id) { return this.details.get(id)?.value; }

  verdict(id, passed, message) {
    const row = this.recent.find(item => item.id === id);
    if (row) row.assertion = passed ? 'passed' : 'failed';
    if (!passed) {
      if (row?.stage !== 'error') this.counts.errors++;
      if (row) row.error = text(message, 700);
    }
    this.note(`${passed ? 'PASS' : 'FAIL'} ${message}`, passed ? 'info' : 'error');
  }

  event(event) {
    const at = this.now();
    const records = event.transactions || [];
    if (event.type === 'ingestion_started') {
      for (const tx of records) {
        this.active.set(tx.object_id, { ...transactionSummary(tx), stage: 'ingesting', startedAt: at });
        if (!this.detail(tx.object_id)?.ingestionRequest) this.capture(tx.object_id, 'ingestionRequest', { record: tx, simulated: this.preview });
      }
      this.note(`Sending ${records.length} transactions to ingestion`);
      return;
    }
    if (event.type === 'ingested') {
      this.counts.ingested += records.length;
      for (const tx of records) {
        const row = this.active.get(tx.object_id);
        if (row) row.stage = 'ready';
        if (!this.detail(tx.object_id)?.ingestionResponse) this.capture(tx.object_id, 'ingestionResponse', { status: 'accepted', simulated: this.preview });
      }
      this.note(`${records.length} transactions accepted by ingestion`);
      return;
    }
    if (event.type === 'fixture_accepted') {
      this.counts.ingested++;
      const row = this.active.get(event.objectId);
      if (row) {
        if (event.target) row.stage = 'ready';
        else this.complete(event.objectId, { stage: 'completed', outcome: 'accepted', httpStatus: 200, role: event.role });
      }
      this.note(`Accepted ${event.role} record ${event.objectId}`);
      return;
    }
    if (event.type === 'ingestion_error') {
      for (const tx of records) this.complete(tx.object_id, { stage: 'error', outcome: event.unknown === false ? 'rejected' : 'unknown',
        httpStatus: event.httpStatus, error: text(event.message, 700) });
      this.counts.errors += records.length;
      this.note(event.message, 'error');
      return;
    }
    const row = this.active.get(event.objectId);
    if (!row) return;
    if (event.type === 'evaluation_started') {
      row.stage = 'evaluating';
      row.evaluationStartedAt = at;
    } else if (event.type === 'deferred') {
      row.stage = 'async_queued';
      row.executionId = text(event.executionId);
      row.httpStatus = 202;
      this.note(`${row.id} deferred to an async worker`);
    } else if (event.type === 'async_status') {
      row.stage = event.status === 'running' ? 'async_running' : 'async_queued';
    } else if (event.type === 'evaluated') {
      this.capture(event.objectId, 'evaluationResponse', event.result);
      const responses = responseSummary(event.result, this.scenarioNames);
      // Totals use the full response, even though the inspection view is bounded.
      const all = Array.isArray(event.result.results) ? event.result.results : [event.result];
      const outcome = all.filter(item => item.triggered).map(decisionSummary).reduce((worst, decision) =>
        rank[decision.outcome] > (rank[worst] ?? -1) ? decision.outcome : worst, 'no_trigger');
      this.counts.evaluated++;
      this.counts.triggered += all.filter(item => item.triggered).length;
      const ruleHits = all.flatMap(item => item.rule_executions || []).filter(rule => (rule.outcome || rule.Outcome) === 'hit').length;
      this.counts.hits += ruleHits;
      this.counts.skipped += all.flatMap(item => item.rule_executions || []).filter(rule => (rule.outcome || rule.Outcome) === 'skipped').length;
      if (rank[outcome] > 0) this.counts.flagged++;
      this.outcomes[outcome] = (this.outcomes[outcome] || 0) + 1;
      const second = Math.floor(at / 1000);
      let bucket = this.rateBuckets.at(-1);
      if (!bucket || bucket.second !== second) {
        bucket = { second, count: 0 };
        this.rateBuckets.push(bucket);
        this.rateBuckets = this.rateBuckets.filter(item => item.second > second - 60);
      }
      bucket.count++;
      this.complete(row.id, { stage: 'completed', outcome, responses, ruleHits,
        responseTruncated: all.length > 12 || all.reduce((count, item) => count + (item.rule_executions?.length || 0), 0) > 40,
        httpStatus: row.httpStatus || 200,
        latencyMs: at - (row.evaluationStartedAt ?? row.startedAt) });
    } else if (event.type === 'evaluation_error') {
      this.counts.errors++;
      this.complete(row.id, { stage: 'error', outcome: event.unknown === false ? 'rejected' : 'unknown',
        httpStatus: row.httpStatus || event.httpStatus, failureHttpStatus: event.httpStatus,
        error: text(event.message, 700), latencyMs: at - (row.evaluationStartedAt ?? row.startedAt) });
      this.note(`${row.id}: ${event.message}`, 'error');
    }
  }

  complete(id, data) {
    const row = this.active.get(id);
    if (!row) return;
    this.active.delete(id);
    this.recent.unshift({ ...row, ...data, completedAt: this.now() });
    this.recent.length = Math.min(this.recent.length, LIMITS.recent);
  }

  snapshot() {
    const now = this.now();
    const second = Math.floor(now / 1000);
    const buckets = new Map(this.rateBuckets.map(bucket => [bucket.second, bucket.count]));
    return {
      runId: this.runId, target: this.target, mode: this.mode, preview: this.preview, status: this.status, message: this.message,
      startedAt: this.startedAt, endedAt: this.endedAt ?? null, now, nextDispatchAt: this.nextDispatchAt,
      counts: { ...this.counts }, outcomes: { ...this.outcomes },
      upcomingCount: this.upcoming.length, upcoming: this.upcoming.slice(0, 12),
      activeCount: this.active.size, active: [...this.active.values()].slice(0, 40),
      stages: [...this.active.values()].reduce((counts, row) => {
        counts[row.stage] = (counts[row.stage] || 0) + 1;
        return counts;
      }, {}),
      recent: this.recent, events: this.events,
      throughput: Array.from({ length: 60 }, (_, i) => buckets.get(second - 59 + i) || 0),
    };
  }
}

