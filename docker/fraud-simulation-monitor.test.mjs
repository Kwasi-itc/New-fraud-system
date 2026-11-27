import test from 'node:test';
import assert from 'node:assert/strict';
import { request } from 'node:http';
import { SimulationMonitor, startDashboard } from './fraud-simulation-monitor.mjs';

const transaction = id => ({ object_id: id, amount: 125, currency: 'GHS', leg: 'debit', state: 'settled' });
const result = (id, outcome = 'approve') => ({ object_id: id, results: [{ triggered: true,
  decision: { id: `decision-${id}`, scenario_id: 'scenario-1', outcome, score: outcome === 'approve' ? 0 : 50 },
  rule_executions: [{ rule_name: 'Velocity', outcome: outcome === 'approve' ? 'no_hit' : 'hit', score_modifier: 50 },
    { rule_name: 'Optional baseline', outcome: 'skipped', skip_reason: 'Missing baseline', score_modifier: 20 }],
}] });

test('observed stages distinguish accepted ingestion, async admission and final rule outcomes', () => {
  let now = 1000;
  const monitor = new SimulationMonitor({ runId: 'run', totalTransactions: 2, now: () => now });
  monitor.setScenarios(new Map([['scenario', { scenario: { id: 'scenario-1', name: 'Risk checks' } }]]));
  monitor.queue([transaction('tx-2')], 5000);
  monitor.event({ type: 'ingestion_started', transactions: [transaction('tx-1')] });
  assert.equal(monitor.snapshot().stages.ingesting, 1);
  assert.equal(monitor.snapshot().counts.ingested, 0);
  monitor.event({ type: 'ingested', transactions: [transaction('tx-1')] });
  monitor.event({ type: 'evaluation_started', objectId: 'tx-1' });
  monitor.event({ type: 'deferred', objectId: 'tx-1', executionId: 'async-1' });
  assert.equal(monitor.snapshot().stages.async_queued, 1);
  assert.equal(monitor.snapshot().counts.evaluated, 0);
  monitor.event({ type: 'async_status', objectId: 'tx-1', status: 'running' });
  assert.equal(monitor.snapshot().stages.async_running, 1);
  now = 1750;
  monitor.event({ type: 'evaluated', objectId: 'tx-1', result: result('tx-1', 'review') });
  const snapshot = monitor.snapshot();
  assert.equal(snapshot.activeCount, 0);
  assert.equal(snapshot.counts.ingested, 1);
  assert.equal(snapshot.counts.evaluated, 1);
  assert.equal(snapshot.counts.triggered, 1);
  assert.equal(snapshot.counts.hits, 1);
  assert.equal(snapshot.counts.skipped, 1);
  assert.equal(snapshot.counts.flagged, 1);
  assert.equal(snapshot.recent[0].latencyMs, 750);
  assert.equal(snapshot.recent[0].httpStatus, 202);
  assert.equal(snapshot.recent[0].executionId, 'async-1');
  assert.equal(snapshot.recent[0].responses[0].scenario, 'Risk checks');
  assert.equal(snapshot.upcoming[0].id, 'tx-2');
});

test('unknown and rejected requests never count as completed evaluations', () => {
  const monitor = new SimulationMonitor({ runId: 'run', totalTransactions: 2 });
  for (const [id, unknown, status] of [['tx-1', true, undefined], ['tx-2', false, 422]]) {
    monitor.beginCase(transaction(id));
    monitor.event({ type: 'evaluation_started', objectId: id });
    monitor.event({ type: 'evaluation_error', objectId: id, unknown, httpStatus: status, message: 'Not completed' });
  }
  const snapshot = monitor.snapshot();
  assert.equal(snapshot.counts.errors, 2);
  assert.equal(snapshot.counts.evaluated, 0);
  assert.equal(snapshot.counts.flagged, 0);
  assert.deepEqual(snapshot.recent.map(row => row.outcome), ['rejected', 'unknown']);
  assert.ok(snapshot.recent.every(row => !row.responses));
});

test('retention is bounded and full-response totals survive truncated inspection details', () => {
  const monitor = new SimulationMonitor({ runId: 'run', totalTransactions: 100 });
  for (let i = 0; i < 100; i++) {
    monitor.beginCase(transaction(`tx-${i}`));
    monitor.event({ type: 'evaluated', objectId: `tx-${i}`, result: result(`tx-${i}`) });
    monitor.note(`Completed ${i}`);
  }
  monitor.beginCase(transaction('large'));
  monitor.event({ type: 'evaluated', objectId: 'large', result: { results: Array.from({ length: 25 }, (_, i) => ({
    triggered: true, decision: { outcome: i === 24 ? 'decline' : 'approve', score: 100 },
    rule_executions: Array.from({ length: 50 }, () => ({ rule_name: 'A rule', outcome: 'hit', score_modifier: 1 })),
  })) } });
  const snapshot = monitor.snapshot();
  assert.equal(snapshot.recent.length, 60);
  assert.equal(snapshot.events.length, 30);
  assert.equal(snapshot.counts.evaluated, 101);
  assert.equal(snapshot.counts.hits, 1250);
  assert.equal(snapshot.recent[0].outcome, 'decline');
  assert.equal(snapshot.recent[0].ruleHits, 1250);
  assert.equal(snapshot.recent[0].responses.length, 12);
  assert.equal(snapshot.recent[0].responses.flatMap(item => item.rules).length, 40);
  assert.equal(snapshot.recent[0].responseTruncated, true);
  assert.equal(snapshot.counts.flagged, 1);
});

test('direct-access dashboard authenticates data, rejects cross-origin commands and reconnects snapshots', async t => {
  const monitor = new SimulationMonitor({ runId: 'local-dashboard', totalTransactions: 2 });
  const dashboard = await startDashboard(monitor, { port: 0 });
  const fetchDashboard = (path, options = {}) => fetch(dashboard.origin + path, { ...options,
    headers: { Authorization: `Bearer ${dashboard.token}`, ...options.headers } });
  t.after(() => dashboard.close());
  const html = await fetch(dashboard.url);
  assert.equal(html.status, 200);
  assert.match(await html.text(), /Flight control/);
  assert.match(html.headers.get('content-security-policy'), /frame-ancestors 'none'/);
  for (const asset of ['/app.js', '/style.css']) {
    const response = await fetchDashboard(asset);
    assert.equal(response.status, 200);
    assert.ok((await response.text()).length > 100);
  }
  monitor.beginCase(transaction('tx-1'));
  const snapshot = await (await fetchDashboard('/api/snapshot')).json();
  assert.equal(snapshot.active[0].id, 'tx-1');
  assert.ok(!('keyDir' in snapshot));
  assert.ok(!('accessToken' in snapshot));
  assert.equal((await fetch(dashboard.origin + '/api/snapshot')).status, 401);
  assert.equal((await fetchDashboard('/api/snapshot', { method: 'POST' })).status, 405);
  assert.equal((await fetchDashboard('/api/snapshot', { headers: { Origin: 'https://other.example' } })).status, 403);
  const rebindingStatus = await new Promise((resolve, reject) => {
    const req = request(dashboard.origin + '/api/snapshot', { headers: { Host: 'other.example:8099' } }, response => {
      response.resume();
      resolve(response.statusCode);
    });
    req.on('error', reject);
    req.end();
  });
  assert.equal(rebindingStatus, 401);
  assert.equal((await fetchDashboard('/missing')).status, 404);
  const session = await fetch(dashboard.origin + '/api/session', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token: dashboard.token }) });
  assert.equal(session.status, 200);
  assert.match(session.headers.get('set-cookie'), /HttpOnly; SameSite=Strict/);
  const cookie = session.headers.get('set-cookie').split(';')[0];
  assert.equal((await fetch(dashboard.origin + '/api/snapshot', { headers: { Cookie: cookie } })).status, 200);
  for (let i = 0; i < 2; i++) {
    const response = await fetchDashboard('/api/events');
    assert.match(response.headers.get('content-type'), /text\/event-stream/);
    const reader = response.body.getReader();
    const { value } = await reader.read();
    const frame = new TextDecoder().decode(value);
    assert.match(frame, /local-dashboard/);
    assert.match(frame, /tx-1/);
    await reader.cancel();
  }
  await dashboard.close();
  await dashboard.close();
});

test('request detail retention is bounded and does not broadcast full payloads in snapshots', () => {
  const monitor = new SimulationMonitor({ runId: 'details', totalTransactions: 2000 });
  monitor.queue([{ object_id: 'queued', secretBusinessField: 'sample-request-data' }]);
  assert.match(monitor.detail('queued').queuedRequest.json, /sample-request-data/);
  assert.ok(!JSON.stringify(monitor.snapshot()).includes('sample-request-data'));
  for (let i = 0; i < 1200; i++) monitor.capture(`record-${i}`, 'queuedRequest', { text: 'x'.repeat(9000) });
  assert.ok(monitor.detailBytes <= 8 * 1024 * 1024);
  assert.ok(monitor.details.size <= 1100);
  assert.equal(monitor.detail('queued'), undefined);
  monitor.capture('large', 'evaluationResponse', { text: 'x'.repeat(100000) });
  assert.equal(monitor.detail('large').evaluationResponse.truncated, true);
});
