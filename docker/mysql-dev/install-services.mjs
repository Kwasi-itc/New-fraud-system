import {resolve} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';
import {readEnv, writeEnv} from './setup-files.mjs';

export async function prepareTenant(context) {
  const {local, origin, request} = context;
  const saved = readEnv(resolve(local, 'ingestion.env'));
  const object = saved.INGESTION_OBJECT_TYPE || 'transactions';
  if (!/^[a-z][a-z0-9_]{0,62}$/.test(object)) throw Error('Invalid retained ingestion object type');
  const uuid = /^(?!00000000-0000-0000-0000-000000000000$)[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i;
  async function model(path, method = 'GET', body) {
    // Local owner commands derive stable operation keys from the same command;
    // only their explicit retryable 503 response is retried here.
    for (let attempt = 0; attempt < 8; attempt++) {
      const response = await request(origin + '/services/data-model/v1' + path, {method, redirect: 'error', signal: AbortSignal.timeout(90000), headers: {'Content-Type': 'application/json'}, body: body === undefined ? undefined : JSON.stringify(body)});
      if (response.status === 503 && attempt < 7) {await delay(2000); continue;}
      if (!response.ok) throw Error(`Data-model ${method} ${path} returned ${response.status}; private response omitted`);
      return response.json();
    }
  }
  let tenant = saved.INGESTION_TENANT_ID;
  if (tenant && !uuid.test(tenant)) throw Error('Invalid retained ingestion tenant ID');
  if (!tenant) {
    const candidates = (await model('/tenants')).tenants.filter(item => item.external_key === 'local-ingestion-development');
    if (candidates.length > 1) throw Error('Ambiguous local ingestion tenant');
    tenant = (candidates[0] ?? (await model('/tenants', 'POST', {name: 'Local ingestion development', external_key: 'local-ingestion-development'})).tenant).id;
    if (!uuid.test(tenant)) throw Error('Data-model returned an invalid tenant ID');
  }
  // Persist the returned identity before provisioning so a retry resumes it.
  writeEnv(resolve(local, 'ingestion.env'), {INGESTION_TENANT_ID: tenant, INGESTION_OBJECT_TYPE: object});
  const state = (await model('/tenants/' + tenant)).tenant;
  if (state.status === 'pending') await model(`/tenants/${tenant}/provision`, 'POST', {});
  else if (state.status !== 'active') throw Error('Ingestion tenant must be pending or active');
  const tables = (await model(`/tenants/${tenant}/tables`)).tables.filter(table => table.name === object && !table.archived);
  if (tables.length > 1) throw Error('Ambiguous local ingestion table');
  const created = tables.length === 0;
  const table = tables[0] ?? (await model(`/tenants/${tenant}/tables`, 'POST', {name: object})).table;
  if (created || state.external_key === 'local-ingestion-development') {
    const path = `/tables/${table.id}/fields`;
    const fields = (await model(path)).fields;
    for (const [name, data_type] of [['amount', 'float'], ['currency', 'string']]) {
      if (!fields.some(field => field.name === name && !field.archived)) await model(path, 'POST', {name, data_type, nullable: true});
    }
  }
  return {tenant, object};
}

export async function installServices(context, options) {
  const {compose, runtime, node, run, local, env} = context;
  compose('build', 'migrate-data-model', 'migrate-ingestion', 'migrate-decisions', 'migrate-auth', 'migrate-case', 'migrate-screening');
  node('frontend/scripts/configure-mysql-storage.mjs');
  const migrate = service => {console.log('Installing ' + service); compose('run', '--rm', '--no-deps', service); context.permissions();};
  migrate('migrate-data-model');
  runtime('up', '-d', '--no-deps', '--wait', 'data-model-author', 'data-model-worker', 'data-model-service');
  let ready = false;
  for (let attempt = 0; attempt < 60; attempt++) {
    try {
      const response = await context.request(context.origin + '/services/data-model/readyz', {signal: AbortSignal.timeout(5000)});
      if (response.ok) {ready = true; break;}
    } catch { /* The owner may still be starting or DNS may be refreshing. */ }
    await delay(2000);
  }
  if (!ready) throw Error('Data-model did not become ready; inspect its container logs and rerun setup');
  const scope = await prepareTenant(context);
  env.INGESTION_TENANT_ID = scope.tenant;
  env.INGESTION_OBJECT_TYPE = scope.object;
  migrate('migrate-ingestion');
  const [network] = JSON.parse(run('docker', ['network', 'inspect', 'fraud-mysql-dev_default'], true));
  const subnets = network.IPAM.Config.map(entry => entry.Subnet).filter(value => /^\d+\.\d+\.\d+\.\d+\/\d+$/.test(value));
  if (subnets.length !== 1) throw Error('Exactly one Docker IPv4 subnet is required');
  env.DECISION_TENANT_ID = scope.tenant;
  env.DECISION_MYSQL_SUBNET = subnets[0];
  writeEnv(resolve(local, 'decision.env'), {DECISION_TENANT_ID: scope.tenant, DECISION_MYSQL_SUBNET: subnets[0]});
  migrate('migrate-decisions');
  migrate('migrate-auth');
  migrate('migrate-case');
  migrate('migrate-screening');
  compose('run', '--rm', '--no-deps', 'migrate-decisions', '/app/install-decisions', '--grant-local-browser');
  runtime('run', '--rm', '--no-deps', 'data-model-author', '/app/mysql-local-author', '--sync-local-consumers');
  if (!options.skipBuild) runtime('build', 'frontend');
}
