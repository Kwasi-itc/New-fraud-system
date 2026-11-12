import assert from 'node:assert/strict';
import {randomUUID} from 'node:crypto';
import {appendFileSync, mkdirSync, readFileSync, writeFileSync} from 'node:fs';
import {spawn, spawnSync} from 'node:child_process';
import {dirname, resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {setTimeout as delay} from 'node:timers/promises';

// Retain fresh fixtures. Never resets a database or injects failing application
// triggers. The worker suite stops/restarts only the local decision worker and
// locks only this run's committed iteration to force bounded persistence retry.
// Cleanup releases that lock and restores the worker on failure.
const service = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const repo = resolve(service, '../..');
const origin = 'http://localhost:8082';
const tenant = JSON.parse(readFileSync(resolve(repo, '.local/mysql-dev/decision-runtime/config.json'), 'utf8')).TenantID;
const env = Object.fromEntries(readFileSync(resolve(repo, '.local/mysql-dev/ingestion.env'), 'utf8').trim().split(/\r?\n/).map(line => line.split('=')));
assert.equal(tenant, env.INGESTION_TENANT_ID, 'Audit requires the shared prepared tenant');
const object = env.INGESTION_OBJECT_TYPE;
const prefix = 'decision-audit-' + randomUUID();
const outputArg = process.argv.indexOf('--output');
const output = outputArg < 0 ? resolve(service, 'evidence/mysql-endpoint-audit', new Date().toISOString().replaceAll(':', '-')) : resolve(process.argv[outputArg + 1]);
mkdirSync(output, {recursive: true});
if (outputArg < 0) writeFileSync(resolve(service, 'evidence/mysql-endpoint-audit/latest.txt'), output + '\n');
const worker = 'fraud-mysql-dev-decision-engine-worker-1';
const requests = [], suites = [], workers = [];
let currentSuite = '', workerStopped = false;
const base = `/v1/tenants/${tenant}`;
const id = label => `${prefix}-${label}`;
let parent, scenario, iteration, path, rule, clone, decision, unroutedEvidence, disabledEvidence;

function docker(args) {
  const result = spawnSync('docker', args, {encoding: 'utf8', timeout: 45000, maxBuffer: 2 << 20});
  if (result.error || result.status !== 0) throw new Error(result.error?.message ?? result.stderr);
  return result.stdout.trim();
}
function runtime() {
  return Object.fromEntries(['fraud-mysql-dev-decision-engine-service-1', worker, 'fraud-mysql-dev-mysql-1'].map(name => {
    const item = JSON.parse(docker(['inspect', '--format', '{{json .}}', name]));
    return [name, {id: item.Id, image: item.Image, imageName: item.Config.Image, state: item.State.Status, health: item.State.Health?.Status}];
  }));
}
async function call(method, url, {body, expected = 200, headers = {}, form, raw, host = origin, probe = false} = {}) {
  const entry = {suite: currentSuite, method, path: url.split('?')[0], query: url.split('?')[1] ?? '', host, expected, probe};
  const wire = raw ?? (body === undefined ? undefined : JSON.stringify(body));
  if (wire) entry.requestBody = wire;
  try {
    const response = await fetch(host + url, {method, headers: {...(wire === undefined ? {} : {'Content-Type': 'application/json'}), ...headers}, body: form ?? wire, signal: AbortSignal.timeout(25000)});
    entry.status = response.status; entry.body = await response.text();
    if (expected !== null) assert.ok([expected].flat().includes(entry.status), `${method} ${url}: ${entry.status}: ${entry.body}`);
    return entry.body.startsWith('{') || entry.body.startsWith('[') ? JSON.parse(entry.body) : entry.body;
  } catch (error) {entry.error = error.message; throw error;}
  finally {requests.push(entry); appendFileSync(resolve(output, 'http.jsonl'), JSON.stringify(entry) + '\n');}
}
async function suite(name, run) {
  currentSuite = name;
  try {await run(); suites.push({name, passed: true}); console.log('PASS ' + name);}
  catch (error) {suites.push({name, passed: false, error: error.stack}); console.error('FAIL ' + name + ': ' + error.message);}
}
async function wait(url, key, target = 'completed') {
  const deadline = Date.now() + 60000;
  while (Date.now() < deadline) {
    const wire = await call('GET', url);
    const item = wire[key] ?? wire;
    if (item.status === target) return item;
    if (target === 'completed') assert.notEqual(item.status, 'failed', JSON.stringify(item));
    await delay(500);
  }
  throw new Error(`Timed out waiting for ${target}: ${url}`);
}
const item = label => ({object_id: id(label), object_type: object, fields: {amount: 125.5, currency: 'USD'}});
const asyncBody = label => ({scenario_id: scenario.id, object_type: object, idempotency_key: id(label), items: [item(label)]});
const scheduledBody = label => ({scheduled_for: new Date().toISOString(), idempotency_key: id(label), items: [item(label)]});
async function workerReady() {
  for (let i = 0; i < 40; i++) {
    const state = JSON.parse(docker(['inspect', '--format', '{{json .State}}', worker]));
    if (state.Running && state.Health?.Status === 'healthy') return;
    await delay(500);
  }
  throw new Error('Decision worker did not become healthy');
}
function lockOwnIteration() {
  for (const value of [tenant, scenario.id, iteration.id]) assert.match(value, /^[0-9a-f-]{36}$/);
  const command = 'export MYSQL_PWD="$(cat /run/secrets/mysql_root_password)"; exec mysql --protocol=socket -uroot --database=fraud --batch --raw --skip-column-names --unbuffered';
  const child = spawn('docker', ['exec', '-i', 'fraud-mysql-dev-mysql-1', 'sh', '-ec', command], {stdio: ['pipe', 'pipe', 'pipe']});
  let stdout = '', stderr = '';
  child.stdout.setEncoding('utf8'); child.stderr.setEncoding('utf8');
  child.stderr.on('data', chunk => {stderr += chunk;});
  const closed = new Promise((resolve, reject) => {child.on('error', reject); child.on('close', code => code === 0 ? resolve() : reject(new Error(`Iteration lock session: ${stderr}`)));});
  closed.catch(() => {});
  const ready = new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('Own iteration lock acquisition timed out')), 5000);
    child.stdout.on('data', chunk => {stdout += chunk; if (stdout.includes('audit-locked')) {clearTimeout(timer); resolve();}});
    closed.catch(error => {clearTimeout(timer); reject(error);});
  });
  const hex = value => value.replaceAll('-', '');
  child.stdin.write(`SET SESSION wait_timeout=120; SET SESSION innodb_lock_wait_timeout=5; START TRANSACTION; SELECT HEX(id) FROM fraud.decision_engine_scenario_iterations FORCE INDEX(de_iteration_binding) WHERE tenant_id=UNHEX('${hex(tenant)}') AND scenario_id=UNHEX('${hex(scenario.id)}') AND id=UNHEX('${hex(iteration.id)}') FOR UPDATE; SELECT 'audit-locked';\n`);
  return {ready, async release() {if (!child.stdin.destroyed) child.stdin.end('ROLLBACK;\nquit\n'); await closed;}};
}

// Inventory all documented operations, including ones absent from this profile.
const spec = readFileSync(resolve(service, 'internal/httpapi/openapi.yaml'), 'utf8');
const operations = [];
let currentPath;
for (const line of spec.split(/\r?\n/)) {
  if (line === 'components:') break;
  const pathMatch = line.match(/^  (\/[^:]+):\s*$/);
  if (pathMatch) currentPath = pathMatch[1];
  const method = line.match(/^    (get|put|post|delete|patch|head|options):\s*$/);
  if (currentPath && method) operations.push({method: method[1].toUpperCase(), path: currentPath});
}
assert.ok(operations.length > 100, 'Shared OpenAPI inventory was not found');
const mountedRoutes = JSON.parse(docker(['exec', 'fraud-mysql-dev-decision-engine-service-1', '/app/mysql-api', '--routes'])).map(route => ({...route, path: route.path.replace(/:([A-Za-z][A-Za-z0-9]*)/g, '{$1}')}));
const extraOperations = mountedRoutes.filter(route => !operations.some(op => op.method === route.method && op.path === route.path));
writeFileSync(resolve(output, 'mounted-routes.json'), JSON.stringify(mountedRoutes, null, 2) + '\n');
const match = (template, url) => new RegExp('^' + template.replace(/\{[^}]+\}/g, '[^/]+') + '$').test(url);
const initialRuntime = runtime();
writeFileSync(resolve(output, 'runtime.json'), JSON.stringify(initialRuntime, null, 2) + '\n');

try {
await suite('health-docs-catalog-and-origin', async () => {
  assert.equal((await call('GET', '/healthz')).status, 'ok');
  assert.equal((await call('GET', '/readyz')).database, 'mysql');
  const capabilities = await call('GET', '/capabilities');
  assert.equal(capabilities.profile, 'local-development'); assert.equal(capabilities.tenant_id, tenant);
  for (const key of ['callbacks', 'workflow_delivery', 'screening', 'additional_scoring', 'outbound_events']) assert.equal(capabilities[key], true);
  for (const [url, marker] of [['/docs', 'Local MySQL development profile'], ['/redoc', '<redoc'], ['/openapi.yaml', 'openapi: 3.0.3']]) assert.ok((await call('GET', url)).includes(marker));
  assert.ok(Array.isArray((await call('GET', '/v1/rule-functions')).rule_functions));
  await call('GET', '/v1/rule-functions?unknown=1', {expected: 400});
  await call('GET', '/docs', {headers: {Origin: 'http://foreign.example'}, expected: 403});
  await call('POST', base + '/scenarios', {body: {name: id('foreign-origin'), trigger_object_type: object}, headers: {Origin: 'http://foreign.example'}, expected: 403});
  await call('GET', `/v1/tenants/${randomUUID()}/scenarios`, {expected: 403});
});
await suite('scenario-authoring-and-validation', async () => {
  const fields = {name: prefix, trigger_object_type: object, description: 'Retained local audit fixture'};
  const disposable = (await call('POST', base + '/scenarios', {body: {...fields, name: id('disposable')}, expected: 201})).scenario;
  await call('DELETE', base + '/scenarios/' + disposable.id, {body: {}, expected: 204});
  await call('GET', base + '/scenarios/' + disposable.id, {expected: 404});
  scenario = (await call('POST', base + '/scenarios', {body: fields, expected: 201})).scenario;
  parent = base + '/scenarios/' + scenario.id;
  await call('PUT', parent, {body: {...fields, description: 'Updated local audit fixture'}});
  assert.equal((await call('GET', parent)).scenario.id, scenario.id);
  assert.ok((await call('GET', base + '/scenarios')).scenarios.some(s => s.id === scenario.id));
  await call('POST', base + '/scenarios', {raw: '{"name":', expected: 400});
  iteration = (await call('POST', parent + '/iterations', {body: {}, expected: 201})).iteration;
  path = parent + '/iterations/' + iteration.id;
  const thresholds = {score_review_threshold: 10, score_block_and_review_threshold: 20, score_decline_threshold: 30};
  await call('PUT', path, {body: {...thresholds, trigger_formula: {constant: 5}}});
  await call('POST', path + '/validate', {body: {}, expected: 422});
  await call('PUT', path, {body: {...thresholds, trigger_formula: {constant: true}}});
  const ruleFields = {name: 'Audit review', display_order: 1, formula: {constant: true}, score_modifier: 15, rule_group: 'audit'};
  rule = (await call('POST', path + '/rules', {body: ruleFields, expected: 201})).rule;
  await call('PUT', path + '/rules/' + rule.id, {body: {...ruleFields, name: 'Updated audit review'}});
  const temp = (await call('POST', path + '/rules', {body: {...ruleFields, name: 'Temporary rule', display_order: 2}, expected: 201})).rule;
  await call('DELETE', path + '/rules/' + temp.id, {body: {}, expected: 204});
  assert.equal((await call('GET', path + '/rules')).rules.length, 1);
  assert.equal((await call('GET', path)).iteration.id, iteration.id);
  await call('GET', parent + '/iterations'); await call('GET', parent + '/iterations/metadata');
  await call('GET', parent + '/rule-groups');
  assert.equal((await call('POST', path + '/validate', {body: {}})).validation.valid, true);
  await call('POST', path + '/commit', {body: {}});
  await call('PUT', path, {body: {...thresholds, trigger_formula: {constant: true}}, expected: 409});
});
await suite('publication-clone-and-test-run-administration', async () => {
  await call('GET', parent + '/publications/preparation?iteration_id=' + iteration.id);
  await call('POST', parent + '/publications/preparation', {body: {iteration_id: iteration.id}, expected: 202});
  await call('POST', parent + '/publications', {body: {action: 'publish', iteration_id: iteration.id}});
  await call('GET', parent + '/publications?limit=1'); await call('GET', parent + '/publications');
  assert.equal((await call('GET', parent + '/rules/latest')).rules.length, 1);
  clone = (await call('POST', path + '/draft', {body: {}, expected: 201})).iteration;
  await call('POST', parent + '/iterations/' + clone.id + '/commit', {body: {}});
  const run = (await call('POST', parent + '/test-runs', {body: {phantom_iteration_id: clone.id, expires_at: new Date(Date.now() + 3600000).toISOString()}, expected: 201})).test_run;
  await call('GET', base + '/test-runs/' + run.id); await call('GET', parent + '/test-runs');
  await call('POST', base + '/test-runs/' + run.id + '/cancel', {body: {}});
  const copied = (await call('POST', parent + '/copy', {body: {name: id('copy')}, expected: 201})).scenario;
  assert.notEqual(copied.id, scenario.id); assert.ok(copied.live_iteration_id);
});
await suite('custom-lists-csv-and-platform-records', async () => {
  const root = base + '/platform/custom-lists';
  const fields = {name: id('list'), kind: 'string', description: 'Audit fixture'};
  const list = (await call('POST', root, {body: fields, expected: 201})).custom_list;
  const url = root + '/' + list.id;
  await call('GET', root); await call('GET', url);
  await call('PUT', url, {body: {...fields, description: 'Updated audit fixture'}});
  const entry = (await call('POST', url + '/entries', {body: {value: id('value')}, expected: 201})).custom_list_entry;
  await call('PUT', url + '/entries/' + entry.id, {body: {value: id('updated-value')}});
  const form = new FormData(); form.append('file', new Blob(['value\n' + id('csv-one') + '\n' + id('csv-two') + '\n'], {type: 'text/csv'}), 'audit.csv');
  assert.equal((await call('POST', url + '/entries/import', {form})).imported_count, 2);
  assert.equal((await call('GET', url + '/entries')).custom_list_entries.length, 3);
  await call('GET', base + '/platform/custom-list-entries?list_name=' + encodeURIComponent(fields.name));
  await call('POST', url + '/entries', {body: {value: null}, expected: 400});
  await call('POST', url + '/entries/import', {body: {}, expected: 400});
  await call('DELETE', url + '/entries/' + entry.id, {body: {}, expected: 204});
  await call('DELETE', url, {body: {}, expected: 204});
  await call('GET', url, {expected: 404});
  const object_id = id('platform');
  await call('POST', base + '/platform/record-tags', {body: {object_type: object, object_id, tag: prefix}, expected: 201});
  assert.ok((await call('GET', base + '/platform/record-tags?object_type=' + object + '&object_id=' + object_id)).record_tags.some(t => t.tag === prefix));
  await call('POST', base + '/platform/risk-snapshots', {body: {object_type: object, object_id, risk_level: 'high'}, expected: 201});
  const ip = '2001:db8:' + randomUUID().slice(0, 4) + '::1';
  await call('POST', base + '/platform/ip-flags', {body: {ip_address: ip, flag: prefix}, expected: 201});
  assert.ok((await call('GET', base + '/platform/ip-flags?ip_address=' + encodeURIComponent(ip))).ip_flags.some(f => f.flag === prefix));
  await call('GET', base + '/platform/record-tags?object_type=' + object, {expected: 400});
});
await suite('async-scheduled-execution-history-and-replay', async () => {
  await call('POST', `/v1/tenants/${tenant}/ingest/${object}`, {host: 'http://localhost:8081', body: {object_id: id('stored'), amount: 125.5, currency: 'USD'}});
  const body = asyncBody('async');
  const created = await call('POST', base + '/async-decision-executions', {body, expected: [200, 202]});
  const execution = created.async_decision_execution;
  await wait(base + '/async-decision-executions/' + execution.id, 'async_decision_execution');
  const replay = await call('POST', base + '/async-decision-executions', {body, expected: [200, 202]});
  assert.equal(replay.async_decision_execution.id, execution.id); assert.equal(replay.replay, true);
  const original = await call('POST', base + '/async-decision-executions', {body: {...body, items: [{...body.items[0], fields: {amount: 20, currency: 'EUR'}}]}, expected: [200, 202]});
  assert.equal(original.async_decision_execution.id, execution.id);
  assert.equal(original.async_decision_execution.request_body.items[0].fields.amount, 125.5);
  const scheduled = (await call('POST', parent + '/scheduled-executions', {body: {...scheduledBody('scheduled'), items: [{object_id: id('stored'), object_type: object}]}, expected: 202})).scheduled_execution;
  await wait(parent + '/scheduled-executions/' + scheduled.id, 'scheduled_execution');
  await call('GET', parent + '/scheduled-executions'); await call('GET', parent + '/scheduled-executions/status-summary');
  await call('GET', base + '/async-decision-executions?limit=2'); await call('GET', base + '/async-decision-executions/status-summary');
  await call('GET', base + '/async-decision-executions?limit=0', {expected: 400});
  const history = await call('GET', parent + '/decisions?limit=10');
  decision = history.decisions.find(d => d.object_id === id('async'));
  assert.ok(decision); assert.equal(decision.score, 15); assert.equal(decision.outcome, 'review');
  const detail = await call('GET', base + '/decisions/' + decision.id);
  assert.ok(detail.rule_executions.length > 0);
  await call('GET', base + '/decisions?limit=2'); await call('GET', base + '/decisions/count');
  await call('GET', base + '/decisions?limit=0', {expected: 400});
  await call('GET', base + '/decisions/' + randomUUID(), {expected: 404});
  await call('GET', base + '/decisions/' + decision.id + '/workflow-executions');
  await call('GET', base + '/decisions/' + decision.id + '/scoring-requests');
  await call('GET', parent + '/workflows'); await call('GET', base + '/outbox-events?limit=2');
  const snooze = await call('POST', parent + '/rule-snoozes', {body: {object_type: object, object_id: id('snoozed'), snooze_group_id: randomUUID(), expires_at: new Date(Date.now() + 3600000).toISOString()}, expected: 201});
  assert.ok(snooze.rule_snooze.id); await call('GET', parent + '/rule-snoozes');
  workers.push({check: 'async and scheduled execution, stored record lookup and durable decision history', passed: true});
});
await suite('worker-cancellation-redrive-and-process-restart', async () => {
  docker(['stop', '--time', '20', worker]); workerStopped = true;
  try {
    const queued = (await call('POST', base + '/async-decision-executions', {body: asyncBody('cancelled'), expected: 202})).async_decision_execution;
    const url = base + '/async-decision-executions/' + queued.id;
    const cancelled = await call('POST', url + '/cancel', {body: {expected_generation: 0}});
    assert.equal(cancelled.status, 'cancelled');
    await call('POST', url + '/cancel', {body: {expected_generation: 0}});
    await call('POST', url + '/cancel', {body: {}, expected: 400});
    await call('POST', url + '/retry', {body: {expected_generation: 0}, expected: 409});
    const scheduled = (await call('POST', parent + '/scheduled-executions', {body: scheduledBody('scheduled-cancel'), expected: 202})).scheduled_execution;
    const scheduledURL = parent + '/scheduled-executions/' + scheduled.id;
    assert.equal((await call('POST', scheduledURL + '/cancel', {body: {expected_generation: 0}})).status, 'cancelled');
    await call('POST', scheduledURL + '/retry', {body: {expected_generation: 0}, expected: 409});
    const restart = (await call('POST', base + '/async-decision-executions', {body: asyncBody('restart'), expected: 202})).async_decision_execution;
    const scheduledRestart = (await call('POST', parent + '/scheduled-executions', {body: scheduledBody('scheduled-restart'), expected: 202})).scheduled_execution;
    docker(['start', worker]); workerStopped = false;
    await workerReady();
    await wait(base + '/async-decision-executions/' + restart.id, 'async_decision_execution');
    await wait(parent + '/scheduled-executions/' + scheduledRestart.id, 'scheduled_execution');
    assert.equal((await call('GET', url)).status, 'cancelled');
    assert.equal((await call('GET', scheduledURL)).status, 'cancelled');
    workers.push({check: 'pending cancellation and queued work across process stop/start', passed: true});
  } finally {if (workerStopped) {docker(['start', worker]); workerStopped = false; await workerReady();}}
  docker(['stop', '--time', '20', worker]); workerStopped = true;
  let lock;
  let asyncURL, scheduledURL;
  try {
    const queued = (await call('POST', base + '/async-decision-executions', {body: asyncBody('persistence-fault'), expected: 202})).async_decision_execution;
    const scheduled = (await call('POST', parent + '/scheduled-executions', {body: scheduledBody('scheduled-persistence-fault'), expected: 202})).scheduled_execution;
    asyncURL = base + '/async-decision-executions/' + queued.id;
    scheduledURL = parent + '/scheduled-executions/' + scheduled.id;
    lock = lockOwnIteration(); await lock.ready;
    docker(['start', worker]); workerStopped = false; await workerReady();
    const failed = await wait(asyncURL, 'async_decision_execution', 'failed');
    const scheduledFailed = await wait(scheduledURL, 'scheduled_execution', 'failed');
    assert.equal(failed.last_error, 'delivery_attempts_exhausted');
    assert.equal(scheduledFailed.last_error, 'delivery_attempts_exhausted');
    const history = (await call('GET', parent + '/decisions?limit=100')).decisions;
    assert.ok(!history.some(d => [id('persistence-fault'), id('scheduled-persistence-fault')].includes(d.object_id)), 'Partial decision committed on persistence failure');
  } finally {
    if (lock) await lock.release();
    if (workerStopped) {docker(['start', worker]); workerStopped = false; await workerReady();}
  }
  for (const [url, key] of [[asyncURL, 'async_decision_execution'], [scheduledURL, 'scheduled_execution']]) {
    const redriven = await call('POST', url + '/retry', {body: {expected_generation: 0}});
    assert.equal(redriven.generation, 1);
    await call('POST', url + '/retry', {body: {expected_generation: 0}, expected: 409});
    await wait(url, key);
  }
  workers.push({check: 'persistence lock timeout retries, exhaustion, rollback, generation-fenced redrive and recovery', passed: true});
  const invalid = asyncBody('invalid-typed-input'); invalid.items[0].fields.amount = 'not-a-number';
  await call('POST', base + '/async-decision-executions', {body: invalid, expected: 422});
});
await suite('recurring-schedule-configuration-and-preconditions', async () => {
  const schedule = parent + '/recurring-schedule';
  await call('GET', schedule);
  const scheduleId = randomUUID();
  const tomorrow = new Date(Date.now() + 86400000).toISOString().slice(0, 10) + 'T12:00';
  const body = {schedule_id: scheduleId, expected_schedule_id: '00000000-0000-0000-0000-000000000000', expected_watermark_version: 0, calendar: {version: 1, frequency: 'daily', timezone: 'UTC', time_of_day: '12:00', gap_policy: 'skip', overlap_policy: 'first'}, start_local: tomorrow, missed_policy: 'catch_up', lateness_ms: 0, candidate_limit: 1};
  const created = (await call('PUT', schedule, {body})).recurring_schedule;
  assert.ok(created.active);
  await call('GET', schedule + '/' + scheduleId);
  await call('PUT', schedule, {body, expected: 409});
  await call('DELETE', schedule, {body: {expected_schedule_id: scheduleId, expected_watermark_version: created.watermark_version}});
  assert.equal((await call('GET', schedule)).recurring_schedule, null);
});
await suite('local-provider-validation-and-publication-deactivation', async () => {
  for (const [method, url, body, expected] of [
    ['POST', parent + '/workflows', {name: 'Invalid'}, 422],
    ['POST', parent + '/scoring-configs', {name: 'Invalid'}, 422],
    ['GET', parent + '/scoring-configs', undefined, 200],
    ['POST', base + '/async-decision-executions', {...asyncBody('callback'), callback_url: 'https://example.com/callback'}, 422],
  ]) await call(method, url, {body, expected});
  await call('POST', path + '/deactivate', {body: {}});
  await call('POST', parent + '/publications', {body: {action: 'publish', iteration_id: iteration.id}});
});

await suite('documented-operation-mount-and-capability-probes', async () => {
  for (const operation of [...operations, ...extraOperations]) {
    const url = operation.path.replace(/\{([^}]+)\}/g, (_, key) => key === 'tenantId' ? tenant : key === 'objectType' ? object : randomUUID());
    await call(operation.method, url, {body: ['GET', 'HEAD'].includes(operation.method) ? undefined : {}, expected: null, probe: true});
    const probe = requests.at(-1);
    const mounted = !(probe.status === 404 && probe.body.trim() === '404 page not found');
    if (mounted && operation.path.includes('{tenantId}')) {
      const foreign = url.replace('/tenants/' + tenant, '/tenants/' + randomUUID());
      await call(operation.method, foreign, {body: ['GET', 'HEAD'].includes(operation.method) ? undefined : {}, expected: 403, probe: true});
    }
  }
});
await suite('formerly-unrouted-39-operations',async()=>{
  const folder=resolve(output,'unrouted');
  const child=spawnSync(process.execPath,[resolve(service,'scripts/mysql-unrouted-audit.mjs'),'--output',folder],{cwd:service,encoding:'utf8',timeout:180000,maxBuffer:2<<20});
  appendFileSync(resolve(output,'unrouted-run.log'),child.stdout+'\n'+child.stderr);
  assert.equal(child.status,0,child.error?.message??child.stdout+'\n'+child.stderr);
  const result=JSON.parse(readFileSync(resolve(folder,'report.json'),'utf8'));assert.equal(result.all39Passed,true);assert.equal(result.runtimeImage,initialRuntime['fraud-mysql-dev-decision-engine-service-1'].image);
  unroutedEvidence={directory:folder,verified:result.verifiedOperations,all39Passed:result.all39Passed,localExpressionAssistant:true};
  for(const check of result.suites)suites.push({...check,name:'unrouted/'+check.name});
  for(const line of readFileSync(resolve(folder,'http.jsonl'),'utf8').trim().split(/\r?\n/)){
    const entry=JSON.parse(line);entry.suite='unrouted/'+entry.suite;entry.host=origin;entry.probe=false;requests.push(entry);appendFileSync(resolve(output,'http.jsonl'),JSON.stringify(entry)+'\n');
  }
});
await suite('formerly-disabled-delivery-operations',async()=>{
  const folder=resolve(output,'delivery');
  const child=spawnSync(process.execPath,[resolve(service,'scripts/mysql-disabled-audit.mjs'),'--output',folder],{cwd:service,encoding:'utf8',timeout:360000,maxBuffer:2<<20});
  appendFileSync(resolve(output,'disabled-run.log'),child.stdout+'\n'+child.stderr);
  const result=JSON.parse(readFileSync(resolve(folder,'report.json'),'utf8'));
  disabledEvidence={directory:folder,verified:result.verifiedOperations,total:result.totalOperations,profilePassed:result.profilePassed};
  for(const check of result.suites)suites.push({...check,name:'delivery/'+check.name});
  workers.push(...result.workers);
  for(const line of readFileSync(resolve(folder,'http.jsonl'),'utf8').trim().split(/\r?\n/)){
    const entry=JSON.parse(line);entry.suite='delivery/'+entry.suite;entry.host=origin;entry.probe=false;requests.push(entry);appendFileSync(resolve(output,'http.jsonl'),JSON.stringify(entry)+'\n');
  }
  assert.equal(child.status,0,child.error?.message??child.stdout+'\n'+child.stderr);
  assert.equal(result.profilePassed,true);assert.equal(result.runtimeImage,initialRuntime['fraud-mysql-dev-decision-engine-service-1'].image);
});
} finally {
  if (workerStopped) {docker(['start', worker]); workerStopped = false;}
  const passing = new Set(suites.filter(s => s.passed).map(s => s.name));
  const classify = operation => {
    const relevant = requests.filter(r => r.host === origin && r.method === operation.method && match(operation.path, r.path));
    const success = relevant.filter(r => !r.probe && !r.error && r.status >= 200 && r.status < 300 && passing.has(r.suite));
    const probe = relevant.find(r => r.probe);
    let status, reason;
    if (success.length) {status = 'success_verified'; reason = 'Successful response with assertions in a passing live suite';}
    else if (probe?.status === 404 && probe.body.trim() === '404 page not found') {status = 'not_mounted'; reason = 'Route absent from the local MySQL host';}
    else {status = 'coverage_gap'; reason = 'Mounted operation lacks a successful asserted response in a passing suite';}
    return {...operation, status, reason, probeStatus: probe?.status ?? null, observedStatuses: [...new Set(relevant.map(r => r.status))], successfulSuites: [...new Set(success.map(r => r.suite))]};
  };
  const matrix = operations.map(classify);
  const extraMatrix = extraOperations.map(classify);
  const runtimeMatrix = mountedRoutes.map(classify);
  const totals = Object.fromEntries(['success_verified', 'not_mounted', 'capability_disabled', 'identity_unavailable', 'coverage_gap'].map(status => [status, matrix.filter(r => r.status === status).length]));
  const report = {reportedAt: new Date().toISOString(), tenantId: tenant, prefix, requestCount: requests.length, documentedOperations: operations.length, mountedOperations: mountedRoutes.length, mountedSuccessfulOperations: runtimeMatrix.filter(r => r.status === 'success_verified').length, runtimeCoverageGaps: runtimeMatrix.filter(r => r.status !== 'success_verified'), totals, suites, workers, unroutedEvidence, disabledEvidence, profilePassed: suites.length >= 10 && unroutedEvidence?.all39Passed === true && disabledEvidence?.profilePassed === true && suites.every(s => s.passed) && totals.success_verified === operations.length && runtimeMatrix.every(r => r.status === 'success_verified'), allDocumentedOperationsCovered: totals.success_verified === operations.length, matrix, extraMatrix};
  writeFileSync(resolve(output, 'report.json'), JSON.stringify(report, null, 2) + '\n');
  writeFileSync(resolve(output, 'runtime-after.json'), JSON.stringify(runtime(), null, 2) + '\n');
  console.log(JSON.stringify({output, requests: requests.length, totals, profilePassed: report.profilePassed, allDocumentedOperationsCovered: report.allDocumentedOperationsCovered}, null, 2));
  process.exitCode = report.profilePassed ? 0 : 1;
}
