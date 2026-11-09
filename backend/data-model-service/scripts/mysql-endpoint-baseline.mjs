import assert from 'node:assert/strict';
import {randomUUID} from 'node:crypto';

const origin = process.env.DATA_MODEL_LOCAL_ORIGIN ?? 'http://localhost:8080';
async function call(method, path, body, status = 200, key = randomUUID(), extraHeaders = {}) {
  const response = await fetch(origin + path, {
    method,
    headers: {'Content-Type': 'application/json', 'Idempotency-Key': key, ...extraHeaders},
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(75000),
  });
  const raw = await response.text();
  assert.equal(response.status, status, `${method} ${path}: ${raw}`);
  return raw ? (response.headers.get('content-type')?.includes('application/json') ? JSON.parse(raw) : raw) : null;
}

assert.equal((await call('GET', '/healthz')).status, 'ok');
const ready = await call('GET', '/readyz');
assert.equal(ready.database, 'mysql');
assert.equal(ready.status, 'ready');
const spec = await call('GET', '/openapi.json');
assert.equal(spec.openapi, '3.0.3');
const docs = await fetch(origin + '/docs');
assert.equal(docs.status, 200);
assert.match(await docs.text(), /SwaggerUIBundle/);

// Always allocate fresh fixtures; repeated runs do not depend on earlier data.
const external = 'endpoint-audit-' + randomUUID();
const tenant = (await call('POST', '/v1/tenants', {name: 'Endpoint audit', external_key: external}, 201)).tenant;
assert.equal(tenant.external_key, external);
assert.equal((await call('GET', `/v1/tenants/${tenant.id}`)).tenant.id, tenant.id);
assert((await call('GET', '/v1/tenants')).tenants.some(t => t.id === tenant.id));
await call('POST', `/v1/tenants/${tenant.id}/provision`);
assert.equal((await call('GET', `/v1/tenants/${tenant.id}`)).tenant.status, 'active');
const table = (await call('POST', `/v1/tenants/${tenant.id}/tables`, {name: 'audit_records'}, 201)).table;
assert((await call('GET', `/v1/tenants/${tenant.id}/tables`)).tables.some(t => t.id === table.id));
const model = (await call('GET', `/v1/tenants/${tenant.id}/data-model`)).data_model;
assert.equal(model.tables.audit_records.id, table.id);
const fields = (await call('GET', `/v1/tables/${table.id}/fields`)).fields;
assert(fields.some(f => f.name === 'object_id'));
assert(fields.some(f => f.name === 'updated_at'));

const jobs = `/v1/tenants/${tenant.id}/index-jobs`;
const job = (await call('POST', jobs, {table_id: table.id, index_type: 'search', columns: ['object_id']}, 201)).index_job;
assert.equal((await call('GET', `/v1/index-jobs/${job.id}`)).index_job.id, job.id);
assert((await call('GET', jobs)).index_jobs.some(j => j.id === job.id));
const logs = await call('GET', `/v1/tenants/${tenant.id}/schema-change-log`);
assert(logs.schema_changes.some(entry => entry.resource_id === table.id));
const migrations = await call('GET', `/v1/tenants/${tenant.id}/schema-migrations`);
assert(migrations.tenant_schema_migrations.length > 0);

await call('GET', `/v1/tenants/${randomUUID()}`, undefined, 404);
await call('GET', '/v1/tenants/not-a-uuid', undefined, 400);
await call('GET', `/v1/index-jobs/${randomUUID()}`, undefined, 404);
await call('GET', '/v1/tenants', undefined, 403, randomUUID(), {Origin: 'http://evil.example'});
console.log(JSON.stringify({passed: true, tenant: tenant.id, table: table.id, job: job.id,
  checks: 'MySQL readiness; registration/catalog persistence; provisioning; assembled model; required fields; nonempty audit/revisions; successful job lookup; missing/invalid resources; origin rejection'}));
