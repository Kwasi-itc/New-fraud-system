import assert from 'node:assert/strict';
import {randomUUID} from 'node:crypto';
import {spawnSync} from 'node:child_process';
import {mkdirSync, writeFileSync} from 'node:fs';
import {dirname, resolve} from 'node:path';

// Allocates fresh fixtures through the API. SQL only inspects their durable
// delivery and physical index; it never claims, completes, or simulates a worker.
const origin = process.env.DATA_MODEL_LOCAL_ORIGIN ?? 'http://localhost:8080';
const container = process.env.DATA_MODEL_MYSQL_CONTAINER ?? 'fraud-mysql-dev-mysql-1';
async function call(method, path, body, expected = 200) {
  const response = await fetch(origin + path, {
    method, headers: {'Content-Type': 'application/json', 'Idempotency-Key': randomUUID()},
    body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(75000),
  });
  const raw = await response.text();
  assert.equal(response.status, expected, `${method} ${path}: ${raw}`);
  return JSON.parse(raw);
}
function sql(query) {
  const child = spawnSync('docker', ['exec', '-i', container, 'sh', '-c',
    'MYSQL_PWD="$(cat /run/secrets/mysql_root_password)" exec mysql --protocol=socket -uroot --database=fraud --batch --skip-column-names'],
  {input: query, encoding: 'utf8', timeout: 15000});
  assert.equal(child.status, 0, child.stderr ?? child.error?.message);
  return child.stdout.trim();
}
const hex = id => {
  assert.match(id, /^[a-f0-9-]{36}$/);
  return `UNHEX('${id.replaceAll('-', '')}')`;
};
const startedAt = new Date().toISOString();
assert.equal((await call('GET', '/readyz')).database, 'mysql');
const tenant = (await call('POST', '/v1/tenants', {
  name: 'MySQL worker runtime audit', external_key: 'worker-audit-' + randomUUID(),
}, 201)).tenant.id;
await call('POST', `/v1/tenants/${tenant}/provision`);
const table = (await call('POST', `/v1/tenants/${tenant}/tables`, {name: 'worker_records'}, 201)).table.id;
await call('POST', `/v1/tables/${table}/fields`, {name: 'score', data_type: 'int', nullable: true}, 201);
const job = (await call('POST', `/v1/tenants/${tenant}/index-jobs`, {
  table_id: table, index_type: 'search', columns: ['score'],
}, 201)).index_job.id;
const before = sql(`SELECT j.status,j.attempt_count,d.state,d.attempt FROM fraud.data_model_index_jobs j JOIN fraud.data_model_index_delivery_reservations r ON r.tenant_id=j.tenant_id AND r.job_id=j.id JOIN fraud.data_model_index_deliveries d ON d.tenant_id=r.tenant_id AND d.id=r.delivery_id WHERE j.tenant_id=${hex(tenant)} AND j.id=${hex(job)};`);
const deadline = Date.now() + 15000;
let current;
do {
  current = (await call('GET', `/v1/index-jobs/${job}`)).index_job;
  if (['applied', 'failed', 'cancelled'].includes(current.status)) break;
  await new Promise(resolve => setTimeout(resolve, 1000));
} while (Date.now() < deadline);
const after = sql(`SELECT j.status,j.attempt_count,d.state,d.attempt FROM fraud.data_model_index_jobs j JOIN fraud.data_model_index_delivery_reservations r ON r.tenant_id=j.tenant_id AND r.job_id=j.id JOIN fraud.data_model_index_deliveries d ON d.tenant_id=r.tenant_id AND d.id=r.delivery_id WHERE j.tenant_id=${hex(tenant)} AND j.id=${hex(job)};`);
const indexRows = Number(sql(`SELECT COUNT(*) FROM information_schema.statistics s JOIN fraud.data_model_physical_tables p ON s.table_schema='fraud' AND s.table_name=p.physical_name WHERE p.tenant_id=${hex(tenant)} AND p.table_id=${hex(table)} AND s.index_name='record_index_${job.replaceAll('-', '')}';`));
const report = {
  startedAt, finishedAt: new Date().toISOString(), origin, container, tenant, table, job,
  requested: {index_type: 'search', columns: ['score']},
  observationWindowMs: 15000, before, after, jobStatus: current.status, physicalIndexRows: indexRows,
  passed: current.status === 'applied' && indexRows === 1,
  meaning: 'Pass requires actual worker completion and a physical native index. Pending/available rows alone prove queueing, not execution.',
};
const outputIndex = process.argv.indexOf('--output');
if (outputIndex >= 0) {
  const path = resolve(process.argv[outputIndex + 1]);
  mkdirSync(dirname(path), {recursive: true});
  writeFileSync(path, JSON.stringify(report, null, 2) + '\n');
}
console.log(JSON.stringify(report, null, 2));
process.exitCode = report.passed ? 0 : 1;
