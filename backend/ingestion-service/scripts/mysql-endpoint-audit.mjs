import assert from 'node:assert/strict';
import {createHash, randomUUID} from 'node:crypto';
import {appendFileSync, mkdirSync, readFileSync, writeFileSync} from 'node:fs';
import {spawn, spawnSync} from 'node:child_process';
import {dirname, resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {setTimeout as delay} from 'node:timers/promises';

// Retained local fixtures only. Never resets databases, changes schema/grants,
// or injects failure into application tables. The deferred test briefly pauses
// the local worker and locks only a fresh test receipt bucket; cleanup restores it.
const service = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const repo = resolve(service, '../..');
const origin = process.env.INGESTION_LOCAL_ORIGIN ?? 'http://localhost:8081';
assert.ok(['http://localhost:8081', 'http://127.0.0.1:8081'].includes(origin), 'Local ingestion origin required');
const scope = Object.fromEntries(readFileSync(resolve(repo, '.local/mysql-dev/ingestion.env'), 'utf8').trim().split(/\r?\n/).map(line => line.split('=')));
const tenant = scope.INGESTION_TENANT_ID;
const object = scope.INGESTION_OBJECT_TYPE;
assert.match(tenant, /^[0-9a-f-]{36}$/);
assert.match(object, /^[a-z][a-z0-9_]{0,62}$/);
const prefix = `audit-${randomUUID()}`;
const startedAt = new Date().toISOString();
const outputArg = process.argv.indexOf('--output');
const output = resolve(service, outputArg < 0 ? `evidence/mysql-endpoint-audit/${startedAt.replaceAll(':', '-')}` : process.argv[outputArg + 1]);
mkdirSync(output, {recursive: true});
const trace = resolve(output, 'http.jsonl');
writeFileSync(trace, '');
const mysql = 'fraud-mysql-dev-mysql-1';
const worker = 'fraud-mysql-dev-ingestion-worker-1';
const requests = [], suites = [];
let currentSuite = '';
const ingest = `/v1/tenants/${tenant}/ingest/${object}`;
const records = `/v1/tenants/${tenant}/records/${object}`;
const aggregate = `/v1/tenants/${tenant}/query/aggregate`;
const metricsPath = '/v1/admin/deferred-ingest-metrics';
const id = label => `${prefix}-${label}`;
const key = label => `${prefix}:${label}`;

function docker(args, input) {
  const child = spawnSync('docker', args, {input, encoding: 'utf8', timeout: 15000, maxBuffer: 4 << 20});
  if (child.error || child.status !== 0) throw new Error(`docker ${args[0]} failed: ${child.error?.message ?? child.stderr}`);
  return child.stdout.trim();
}
// Shell text is constant, and SQL goes through stdin rather than shell escaping.
const sqlCommand = 'export MYSQL_PWD="$(cat /run/secrets/mysql_root_password)"; exec mysql --protocol=socket -uroot --database=fraud --batch --raw --skip-column-names --unbuffered';
function sql(statement) { return docker(['exec', '-i', mysql, 'sh', '-ec', sqlCommand], `${statement};\n`); }
async function call(method, path, {body, raw, headers = {}, expected = 200, form} = {}) {
  const wire = raw ?? (body === undefined ? undefined : JSON.stringify(body));
  const entry = {suite: currentSuite, method, path: path.split('?')[0], query: path.split('?')[1] ?? '', expected, at: new Date().toISOString()};
  if (wire) { entry.requestBytes = Buffer.byteLength(wire); if (entry.requestBytes < 8192) entry.requestBody = wire; }
  try {
    const response = await fetch(origin + path, {method, headers: {...(wire ? {'Content-Type': 'application/json'} : {}), ...headers}, body: form ?? wire, signal: AbortSignal.timeout(25000)});
    const text = await response.text();
    Object.assign(entry, {status: response.status, body: text});
    assert.ok([expected].flat().includes(response.status), `${method} ${path}: ${response.status}, wanted ${expected}: ${text}`);
    return text.startsWith('{') || text.startsWith('[') ? JSON.parse(text) : text;
  } catch (error) { entry.error = error.message; throw error; }
  finally { requests.push(entry); appendFileSync(trace, JSON.stringify(entry) + '\n'); }
}
async function suite(name, run) {
  currentSuite = name;
  const started = performance.now();
  try { await run(); suites.push({name, passed: true, elapsedMs: Math.round(performance.now() - started)}); console.log(`PASS ${name}`); }
  catch (error) { suites.push({name, passed: false, elapsedMs: Math.round(performance.now() - started), error: error.stack}); console.error(`FAIL ${name}: ${error.stack}`); }
}
async function post(body, label, method = 'POST', path = ingest, expected = 200) {
  return call(method, path, {body, headers: {'Idempotency-Key': key(label)}, expected});
}
async function metrics() {
  const value = (await call('GET', metricsPath)).deferred_ingest_metrics;
  for (const field of ['queued_count', 'processing_count', 'completed_count', 'failed_count', 'retry_pending_count', 'retried_execution_count', 'recent_success_count', 'recent_failure_count', 'recent_retry_count']) {
    assert.ok(Number.isSafeInteger(value[field]) && value[field] >= 0, `${field} must be a nonnegative integer`);
  }
  assert.ok(value.oldest_queued_age_seconds >= 0);
  assert.equal(value.drain_rate_per_minute_last_5m, value.recent_success_count / 5);
  assert.ok(Number.isFinite(Date.parse(value.snapshot_at)));
  if (value.queued_count > 0) assert.ok(Number.isFinite(Date.parse(value.oldest_queued_requested_at)));
  else { assert.equal(value.oldest_queued_requested_at, undefined); assert.equal(value.oldest_queued_age_seconds, 0); }
  return value;
}
async function csv(content, expected = 202) {
  const form = new FormData();
  form.append('file', new Blob([content], {type: 'text/csv'}), 'audit.csv');
  return call('POST', ingest + '/csv?mode=patch', {form, expected});
}
async function waitWork(path, state) {
  const deadline = Date.now() + 45000;
  while (Date.now() < deadline) {
    const body = await call('GET', path);
    const result = body.upload_log ?? body.deferred_ingest;
    if (result.status === state) return result;
    assert.notEqual(result.status, 'failed', JSON.stringify(result));
    await delay(300);
  }
  throw new Error(`Work did not reach ${state}: ${path}`);
}
function lockBucket(digest) {
  const child = spawn('docker', ['exec', '-i', mysql, 'sh', '-ec', sqlCommand], {stdio: ['pipe', 'pipe', 'pipe']});
  let stdout = '', stderr = '';
  child.stdout.setEncoding('utf8'); child.stderr.setEncoding('utf8');
  child.stderr.on('data', chunk => { stderr += chunk; });
  const closed = new Promise((resolve, reject) => { child.on('error', reject); child.on('close', code => code === 0 ? resolve() : reject(new Error(`Lock session exited ${code}: ${stderr}`))); });
  // Observe rejection immediately, even when cleanup will await it later.
  closed.catch(() => {});
  const ready = new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('Receipt lock was not acquired')), 5000);
    child.stdout.on('data', chunk => {
      stdout += chunk;
      const match = stdout.match(/locked:(\d+)/);
      if (match) { clearTimeout(timer); resolve(Number(match[1])); }
    });
    closed.catch(error => { clearTimeout(timer); reject(error); });
  });
  const tenantHex = tenant.replaceAll('-', '');
  child.stdin.write(`SET SESSION innodb_lock_wait_timeout=5; START TRANSACTION; SELECT HEX(key_digest) FROM fraud.ingestion_receipt_buckets WHERE tenant_id=UNHEX('${tenantHex}') AND key_digest=UNHEX('${digest}') FOR UPDATE; SELECT CONCAT('locked:',CONNECTION_ID());\n`);
  return {ready, async release() { if (!child.stdin.destroyed) child.stdin.end('ROLLBACK;\nquit\n'); await closed; }};
}

const runtime = {databaseVersion: sql('SELECT VERSION()'), containers: {}};
for (const name of ['fraud-mysql-dev-ingestion-service-1', worker, mysql]) {
  runtime.containers[name] = JSON.parse(docker(['inspect', '--format', '{{json .}}', name]));
  const c = runtime.containers[name];
  runtime.containers[name] = {id: c.Id, image: c.Image, imageName: c.Config.Image, state: c.State.Status, health: c.State.Health?.Status, paused: c.State.Paused};
}
writeFileSync(resolve(output, 'runtime.json'), JSON.stringify(runtime, null, 2) + '\n');
assert.equal(runtime.databaseVersion, '8.4.8');
assert.equal(runtime.containers[worker].state, 'running');
assert.equal(runtime.containers[worker].paused, false, 'Worker must initially be unpaused');

await suite('health-documentation', async () => {
  assert.equal((await call('GET', '/healthz')).status, 'ok');
  assert.equal((await call('GET', '/readyz')).status, 'ready');
  for (const [path, text] of [['/docs', 'SwaggerUIBundle'], ['/redoc', 'redoc.standalone.js'], ['/openapi.yaml', 'openapi: 3.0.3']]) assert.ok((await call('GET', path)).includes(text));
  await metrics();
});
await suite('single-writes-replay-conflict-and-validation', async () => {
  const body = {object_id: id('single'), amount: 12.5, currency: 'USD'};
  assert.equal((await post(body, 'single')).result.action, 'created');
  assert.equal((await post(body, 'single')).result.replayed, true);
  await post({...body, amount: 13}, 'single', 'POST', ingest, 409);
  await post({object_id: id('single'), amount: 20}, 'patch', 'PATCH');
  const read = (await call('GET', records + '/' + id('single'))).record;
  assert.equal(read.fields.amount, 20); assert.equal(read.fields.currency, 'USD');
  await post({object_id: id('invalid'), amount: 'not-a-number'}, 'invalid', 'POST', ingest, 422);
  await call('GET', records + '/' + id('invalid'), {expected: 404});
  await post({amount: 2}, 'missing-id', 'POST', ingest, 422);
  await call('POST', ingest, {body: {object_id: id('unkeyed'), currency: 'EUR'}});
});
await suite('batch-writes-atomic-validation-and-replay', async () => {
  const body = [{object_id: id('batch-a'), amount: 1, currency: 'USD'}, {object_id: id('batch-b'), amount: 2, currency: 'USD'}];
  const first = await post(body, 'batch', 'POST', ingest + '/batch');
  assert.equal(first.results.length, 2);
  const replay = await post(body, 'batch', 'POST', ingest + '/batch');
  assert.ok(replay.results.every(r => r.replayed));
  await post(body.map((r, i) => ({object_id: r.object_id, amount: 10 + i})), 'batch-patch', 'PATCH', ingest + '/batch');
  assert.equal((await call('GET', records + '/' + id('batch-a'))).record.fields.currency, 'USD');
  await post([{object_id: id('atomic-good'), amount: 1}, {object_id: id('atomic-bad'), amount: 'bad'}], 'atomic-invalid', 'POST', ingest + '/batch', 422);
  await call('GET', records + '/' + id('atomic-good'), {expected: 404});
  await post([], 'empty-batch', 'POST', ingest + '/batch', 400);
  const duplicate = await post([{object_id: id('duplicate')}, {object_id: id('duplicate')}], 'duplicate-batch', 'PATCH', ingest + '/batch', 422);
  assert.equal(duplicate.validation_errors[0].code, 'duplicate_object_id');
  await call('GET', records + '/' + id('duplicate'), {expected: 404});
});
await suite('reads-search-and-all-aggregates', async () => {
  const page = await call('GET', records + '?limit=2'); assert.ok(page.records.length <= 2);
  const search = await call('GET', records + '/search?field=object_id&value=' + encodeURIComponent(id('single')));
  assert.equal(search.records.length, 1); assert.equal(search.records[0].object_id, id('single'));
  const filter = {kind: 'predicate', field: 'object_id', op: 'starts_with', value: prefix + '-batch-'};
  for (const [operation, value] of [['count', 2], ['sum', 21], ['avg', 10.5], ['min', 10], ['max', 11], ['count_distinct', 2]]) {
    assert.equal((await call('POST', aggregate, {body: {object_type: object, aggregate: operation, field: 'amount', filter}})).value, value);
  }
  await call('GET', records + '/missing-' + randomUUID(), {expected: 404});
  await call('GET', records + '?limit=1001', {expected: 413});
  await call('GET', records + '/search', {expected: 400});
  await call('GET', records + '/search?field=unknown&value=x', {expected: 400});
  await call('POST', aggregate, {body: {object_type: object, aggregate: 'arbitrary_sql', field: 'amount'}, expected: 400});
});
await suite('tenant-scope-resource-identities-and-json-limits', async () => {
  const foreign = randomUUID();
  for (const [method, path, body] of [['POST', ingest, {object_id: id('foreign')}], ['PATCH', ingest, {object_id: id('foreign')}], ['POST', ingest + '/batch', [{object_id: id('foreign')}]], ['PATCH', ingest + '/batch', [{object_id: id('foreign')}]], ['GET', records], ['GET', records + '/search?field=object_id&value=x'], ['GET', records + '/x'], ['GET', ingest + '/upload-logs'], ['POST', aggregate, {object_type: object, aggregate: 'count', field: 'object_id'}]]) {
    await call(method, path.replace(tenant, foreign), {body, expected: 403});
  }
  await call('POST', `/v1/tenants/${tenant}/ingest/foreign_object`, {body: {object_id: id('wrong-object')}, expected: 403});
  for (const path of ['/v1/upload-logs/', '/v1/deferred-ingests/']) {
    await call('GET', path + 'not-a-uuid', {expected: 400});
    await call('GET', path + randomUUID(), {expected: 404});
  }
  await call('POST', ingest, {raw: '{"object_id":"one"} {}', expected: 400});
  await call('POST', ingest, {raw: '{"a":' + '['.repeat(65) + '0' + ']'.repeat(65) + '}', expected: 400});
  await call('POST', ingest, {body: {object_id: id('large'), currency: 'x'.repeat((1 << 20) + (65 << 10))}, expected: 413});
  await call('GET', '/docs', {headers: {Origin: 'http://evil.example'}, expected: 403});
});
await suite('csv-worker-lifecycle-and-invalid-input', async () => {
  const upload = (await csv(`object_id,amount,currency\n${id('csv')},31.5,GBP\n`)).upload_log;
  const done = await waitWork('/v1/upload-logs/' + upload.id, 'completed');
  assert.equal(done.successful_rows, 1); assert.equal(done.total_rows, 1); assert.equal(done.attempt_count, 1);
  assert.equal((await call('GET', records + '/' + id('csv'))).record.fields.amount, 31.5);
  const logs = (await call('GET', ingest + '/upload-logs')).upload_logs;
  assert.ok(logs.some(l => l.id === upload.id));
  await call('POST', ingest + '/csv', {expected: 400});
  await call('POST', ingest + '/csv?mode=bad', {expected: 400});
  const bad = (await csv('object_id,object_id\na,b\n')).upload_log;
  let terminal;
  for (let i = 0; i < 100; i++) {
    terminal = (await call('GET', '/v1/upload-logs/' + bad.id)).upload_log;
    if (terminal.status === 'failed') break;
    await delay(200);
  }
  assert.equal(terminal.status, 'failed'); assert.equal(terminal.attempt_count, 1); assert.equal(terminal.error_message, 'invalid_payload');
});
await suite('deferred-overload-polling-and-worker-completion', async () => {
  const config = JSON.parse(readFileSync(resolve(repo, '.local/mysql-dev/ingestion-runtime/config.json'), 'utf8'));
  assert.equal(config.TenantID, tenant); assert.equal(config.ObjectType, object);
  assert.ok(config.Pool > config.WriteConcurrency, 'API must reserve connections for deferred admission');
  const heldKey = key('held');
  const digest = createHash('sha256').update(heldKey).digest('hex');
  sql(`INSERT INTO fraud.ingestion_receipt_buckets(tenant_id,key_digest) VALUES(UNHEX('${tenant.replaceAll('-', '')}'),UNHEX('${digest}'))`);
  let paused = false, lock, pending = [];
  try {
    docker(['pause', worker]); paused = true;
    const before = await metrics();
    lock = lockBucket(digest);
    const connection = await lock.ready;
    const heldBody = {object_id: id('held'), amount: 7};
    pending = Array.from({length: config.WriteConcurrency}, () => post(heldBody, 'held').then(value => ({value}), error => ({error})));
    const deadline = Date.now() + 4000;
    let waiting = 0;
    while (Date.now() < deadline) {
      waiting = Number(sql(`SELECT COUNT(DISTINCT w.REQUESTING_ENGINE_TRANSACTION_ID) FROM performance_schema.data_lock_waits w JOIN performance_schema.threads t ON t.THREAD_ID=w.BLOCKING_THREAD_ID WHERE t.PROCESSLIST_ID=${connection}`));
      if (waiting === config.WriteConcurrency) break;
      await delay(50);
    }
    assert.equal(waiting, config.WriteConcurrency, 'Synchronous slots did not all reach the fixture receipt lock');
    const deferred = (await post({object_id: id('deferred'), amount: 9}, 'deferred', 'PATCH', ingest, 202)).deferred_ingest;
    assert.equal((await call('GET', '/v1/deferred-ingests/' + deferred.id)).deferred_ingest.status, 'queued');
    assert.equal((await metrics()).queued_count, before.queued_count + 1);
    await lock.release(); lock = null;
    for (const outcome of await Promise.all(pending)) { if (outcome.error) throw outcome.error; }
    pending = [];
    docker(['unpause', worker]); paused = false;
    const done = await waitWork('/v1/deferred-ingests/' + deferred.id, 'completed');
    assert.equal(done.attempt_count, 1);
    const after = await metrics();
    assert.ok(after.completed_count >= before.completed_count + 1);
    assert.ok(after.recent_success_count >= 1);
    await post({object_id: id('deferred'), amount: 9}, 'deferred', 'PATCH');
    assert.equal((await call('GET', records + '/' + id('deferred'))).record.fields.amount, 9);
  } finally {
    try { if (lock) await lock.release(); }
    finally { if (paused) docker(['unpause', worker]); await Promise.all(pending); }
  }
});
await suite('deferred-metrics-contract-and-configured-scope', async () => {
  const value = await metrics();
  const overridden = (await call('GET', metricsPath + '?tenant_id=' + randomUUID() + '&object_type=foreign')).deferred_ingest_metrics;
  for (const field of ['queued_count', 'processing_count', 'completed_count', 'failed_count', 'retried_execution_count']) assert.equal(overridden[field], value[field]);
  const tenantSQL = `UNHEX('${tenant.replaceAll('-', '')}')`;
  const table = sql(`SELECT HEX(table_id) FROM fraud.data_model_ingestion_contracts WHERE tenant_id=${tenantSQL} AND table_name_key_id=fraud.data_model_find_ingestion_contract_key_v1(${tenantSQL},'${object}')`);
  assert.match(table, /^[0-9A-F]{32}$/);
  const expected = sql(`SELECT CONCAT(COUNT(CASE WHEN j.state='available' THEN 1 END),',',COUNT(CASE WHEN j.state='running' THEN 1 END),',',COUNT(CASE WHEN j.state='completed' THEN 1 END),',',COUNT(CASE WHEN j.state IN ('discarded','cancelled') THEN 1 END),',',COUNT(CASE WHEN j.state='available' AND j.attempt>0 THEN 1 END),',',COUNT(CASE WHEN j.attempt>1 THEN 1 END)) FROM fraud.ingestion_work_resources r JOIN fraud.ingestion_work_jobs j ON j.tenant_id=r.tenant_id AND j.resource_id=r.id AND j.id=COALESCE(r.current_job_id,r.id) WHERE r.tenant_id=${tenantSQL} AND r.table_id=UNHEX('${table}') AND r.kind='deferred_ingest_execute'`);
  assert.deepEqual([value.queued_count, value.processing_count, value.completed_count, value.failed_count, value.retry_pending_count, value.retried_execution_count], expected.split(',').map(Number));
});
await suite('durable-audit-outbox-and-replay-evidence', async () => {
  const tenantSQL = `UNHEX('${tenant.replaceAll('-', '')}')`;
  const auditCount = Number(sql(`SELECT COUNT(*) FROM fraud.ingestion_audit WHERE tenant_id=${tenantSQL} AND object_id='${id('single')}' AND status='succeeded'`));
  assert.equal(auditCount, 2, 'Create/replay/PATCH must retain two success effects');
  const outboxCount = Number(sql(`SELECT COUNT(*) FROM fraud.ingestion_outbox_events WHERE tenant_id=${tenantSQL} AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.object_id'))='${id('single')}' AND status='pending'`));
  assert.equal(outboxCount, 2, 'Receipt replay must not append another event');
});

// Inventory comes from the shared spec, including documented-but-unmounted ops.
const spec = readFileSync(resolve(service, 'internal/httpapi/openapi.yaml'), 'utf8');
const inventory = [];
let path;
for (const line of spec.split(/\r?\n/)) {
  const route = line.match(/^  (\/[^:]+):$/); if (route) path = route[1];
  const method = line.match(/^    (get|post|put|patch|delete):$/);
  if (path && method) inventory.push({path, method: method[1].toUpperCase()});
}
const suitePassed = new Set(suites.filter(s => s.passed).map(s => s.name));
assert.ok(inventory.length > 0, 'The OpenAPI operation inventory must not be empty');
assert.equal(new Set(inventory.map(o => `${o.method} ${o.path}`)).size, inventory.length, 'Duplicate OpenAPI operation');
const coverage = inventory.map(({path, method}) => {
  const pattern = new RegExp('^' + path.split('/').map(p => /^\{\w+\}$/.test(p) ? '[^/]+' : p.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('/') + '$');
  const matching = requests.filter(r => r.method === method && pattern.test(r.path));
  const successful = matching.filter(r => suitePassed.has(r.suite) && !r.error && r.status >= 200 && r.status < 300);
  return {operation: `${method} ${path}`, covered: successful.length > 0, successfulRequests: successful.length, observedStatuses: [...new Set(matching.map(r => r.status ?? r.error))], suites: [...new Set(successful.map(r => r.suite))]};
});
const report = {
  startedAt, finishedAt: new Date().toISOString(), origin, tenant, objectType: object, fixturePrefix: prefix,
  suites, requestCount: requests.length, operationsTotal: coverage.length, operationsCovered: coverage.filter(o => o.covered).length, coverage,
  runtimeAcceptancePassed: suites.every(s => s.passed) && coverage.every(c => c.covered),
  knownUnsupported: [],
  limitations: ['Local login-free runtime, one tenant/table', 'Fixtures and audit/queue history retained', 'Worker briefly paused and restored for deterministic deferred admission', 'Production identity, sustained load and external event consumers are separate acceptance'],
};
report.passed = suites.every(s => s.passed) && coverage.every(c => c.covered);
writeFileSync(resolve(output, 'report.json'), JSON.stringify(report, null, 2) + '\n');
console.log(`${suites.filter(s => s.passed).length}/${suites.length} suites; ${report.operationsCovered}/${report.operationsTotal} documented operations; ${requests.length} requests. Report: ${resolve(output, 'report.json')}`);
// Any missing documented operation or failed assertion invalidates acceptance.
process.exitCode = report.passed ? 0 : 1;
