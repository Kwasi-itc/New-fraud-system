import assert from 'node:assert/strict';
import test from 'node:test';
import {mkdtempSync, mkdirSync, readFileSync, writeFileSync, rmSync, statSync, symlinkSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {options} from '../setup.mjs';
import {prepareApplicationFiles, prepareFoundation, prepareMountPermissions, readEnv, writeEnv} from './setup-files.mjs';
import {installServices, prepareTenant} from './install-services.mjs';
import {setupPlatform} from './platform.mjs';

function fixture(t) {
  const root = mkdtempSync(resolve(tmpdir(), 'fraud-portable-'));
  t.after(() => rmSync(root, {recursive: true, force: true}));
  prepareFoundation(root);
  return {root, local: resolve(root, '.local/mysql-dev')};
}
test('portable options validate input and preserve paths containing spaces', t => {
  const {root} = fixture(t);
  const file = resolve(root, 'users for mac.json'); writeFileSync(file, '{}');
  const parsed = options(['--port', '8099', '--install', '--skip-build', '--users-file', file, '--create-missing-users']);
  assert.equal(parsed.port, 8099); assert(parsed.install && parsed.skipBuild && parsed.createMissingUsers); assert.equal(parsed.usersFile, file);
  for (const args of [['--port', '80'], ['--port', '65536'], ['--port', '12x'], ['--create-missing-users'], ['--unknown']]) assert.throws(() => options(args));
});
test('foundation and application setup retain credentials and saved port settings', t => {
  const {root, local} = fixture(t);
  writeEnv(resolve(local, 'compose.env'), {MYSQL_DEV_PORT: 3309});
  prepareApplicationFiles(root);
  const saved = readFileSync(resolve(local, 'root-password.txt'));
  const token = readFileSync(resolve(local, 'decision-runtime-token.txt'));
  prepareFoundation(root, true); prepareApplicationFiles(root);
  assert.deepEqual(readFileSync(resolve(local, 'root-password.txt')), saved);
  assert.deepEqual(readFileSync(resolve(local, 'decision-runtime-token.txt')), token);
  assert.equal(readEnv(resolve(local, 'compose.env')).MYSQL_DEV_PORT, '3309');
  writeFileSync(resolve(local, 'root-password.txt'), 'invalid');
  assert.throws(() => prepareFoundation(root), /Invalid retained credential/);
  assert.equal(readFileSync(resolve(local, 'root-password.txt'), 'utf8'), 'invalid');
});
test('a populated volume never receives replacement foundation credentials', t => {
  const root = mkdtempSync(resolve(tmpdir(), 'fraud-retained-'));
  t.after(() => rmSync(root, {recursive: true, force: true}));
  assert.throws(() => prepareFoundation(root, true), /Restore root-password/);
});
test('POSIX secret bind mounts remain readable inside a private host directory', {skip: process.platform === 'win32'}, t => {
  const {root, local} = fixture(t);
  mkdirSync(resolve(root, 'docker'), {recursive: true});
  writeFileSync(resolve(root, 'docker/compose.yaml'), 'secrets:\n  root: {file: ../.local/mysql-dev/root-password.txt}\n');
  writeFileSync(resolve(root, 'docker/provider.compose.yaml'), '');
  const plan = resolve(local, 'plans/data_model.json'); writeFileSync(plan, '{}', {mode: 0o600});
  prepareMountPermissions(root);
  assert.equal(statSync(local).mode & 0o777, 0o700);
  assert.equal(statSync(resolve(local, 'root-password.txt')).mode & 0o777, 0o644);
  assert.equal(statSync(plan).mode & 0o777, 0o600, 'Private owner artifacts retain their mode');
  const link = resolve(local, 'auth-runtime'); symlinkSync(tmpdir(), link);
  assert.throws(() => prepareMountPermissions(root), /symlink/);
});

function tenantAPI() {
  const calls = [];
  const tenant = '12345678-1234-4234-8234-123456789abc';
  const request = async (url, init = {}) => {
    const path = new URL(url).pathname.replace('/services/data-model/v1', '');
    const method = init.method || 'GET'; calls.push({path, method, body: init.body && JSON.parse(init.body)});
    const payload = path.endsWith('/readyz') ? {} : path === '/tenants' ? {tenants: [{id: tenant, external_key: 'local-ingestion-development'}]} : path.endsWith('/tables') ? {tables: [{id: 'table', name: 'transactions'}]} : path.endsWith('/fields') ? {fields: [{name: 'amount'}, {name: 'currency'}]} : {tenant: {id: tenant, status: 'active', external_key: 'local-ingestion-development'}};
    return {ok: true, status: 200, json: async () => payload};
  };
  return {request, calls, tenant};
}
test('tenant bootstrap resumes saved scope and leaves existing fields alone', async t => {
  const {root, local} = fixture(t); const api = tenantAPI();
  writeEnv(resolve(local, 'ingestion.env'), {INGESTION_TENANT_ID: api.tenant, INGESTION_OBJECT_TYPE: 'transactions'});
  const scope = await prepareTenant({root, local, origin: 'http://localhost:8088', request: api.request});
  assert.equal(scope.tenant, api.tenant); assert(api.calls.every(call => call.method === 'GET'));
  assert(!api.calls.some(call => call.path === '/tenants'));
});
test('owner setup uses the canonical Compose services in dependency order and stops on failure', async t => {
  const {root, local} = fixture(t); const api = tenantAPI(); const commands = []; const env = {};
  const context = {root, local, env, origin: 'http://localhost:8088', request: api.request, permissions() {},
    compose: (...args) => commands.push(args), runtime: (...args) => commands.push(args), node: (...args) => commands.push(args),
    run: () => JSON.stringify([{IPAM: {Config: [{Subnet: '172.20.0.0/16'}]}}]),
  };
  await installServices(context, {skipBuild: true});
  const migrations = commands.filter(args => args[0] === 'run' && args[3]?.startsWith('migrate-')).map(args => args[3]);
  assert.deepEqual(migrations, ['migrate-data-model', 'migrate-ingestion', 'migrate-decisions', 'migrate-auth', 'migrate-case', 'migrate-screening', 'migrate-decisions']);
  assert.equal(env.DECISION_TENANT_ID, api.tenant); assert.equal(env.INGESTION_TENANT_ID, api.tenant);
  assert(!commands.some(args => args.includes('powershell')));
  const failed = [];
  context.compose = (...args) => {failed.push(args); if (args.includes('migrate-ingestion') && args[0] === 'run') throw Error('owner failed');};
  await assert.rejects(installServices(context, {skipBuild: true}), /owner failed/);
  assert(!failed.some(args => args[0] === 'run' && args.includes('migrate-decisions')));
});
test('an installed platform restarts without rebuilding backends or replaying migrations', async t => {
  const {root, local} = fixture(t);
  for (const owner of ['auth', 'case', 'screening', 'ingestion', 'decision']) {mkdirSync(resolve(local, owner + '-runtime')); writeFileSync(resolve(local, owner + '-runtime/config.json'), '{}');}
  writeFileSync(resolve(local, 'frontend.env'), ''); writeFileSync(resolve(local, 'plans/data_model.json'), '{}');
  const commands = [];
  const run = (program, args) => {
    commands.push([program, ...args]);
    if (args[0] === 'info') return 'linux';
    if (args.includes('version')) return '2.39.0';
    if (args.some(arg => arg.endsWith('check-runtime-network.mjs'))) return 'true';
    if (args.includes('name=^fraud-mysql-dev-mysql-reference-1$')) return 'reference-id';
    if (args[0] === 'inspect') return JSON.stringify([{Destination: '/var/lib/mysql', Type: 'volume', Name: 'fraud-mysql-dev_mysql_dev_reference_data'}]);
    return '';
  };
  // Permission handling reads checked-in source declarations even in a fixture.
  mkdirSync(resolve(root, 'docker')); for (const file of ['compose.yaml', 'provider.compose.yaml']) writeFileSync(resolve(root, 'docker', file), '');
  await setupPlatform({port: 8088, skipBuild: true}, {root, run});
  assert(!commands.some(command => command.includes('build') || command.includes('run') || command.includes('powershell')));
  assert(commands.some(command => command.some(arg => arg.endsWith('verify-gateway.mjs'))));
  const start = commands.findLast(command => command.includes('up'));
  assert(!start.includes('install')); assert(start.includes('--no-build'));
});
test('first-install orchestration stops writers before network reconciliation and uses no PowerShell', async t => {
  const {root, local} = fixture(t); const commands = []; const api = tenantAPI();
  mkdirSync(resolve(root, 'docker'));
  const repository = fileURLToPath(new URL('../../', import.meta.url));
  for (const file of ['compose.yaml', 'provider.compose.yaml']) writeFileSync(resolve(root, 'docker', file), readFileSync(resolve(repository, 'docker', file)));
  const run = (program, args) => {
    commands.push([program, ...args]);
    if (args[0] === 'info') return 'linux';
    if (args.includes('version')) return '2.24.4';
    if (args.some(arg => arg.endsWith('check-runtime-network.mjs'))) return 'false';
    if (args[0] === 'network') return JSON.stringify([{IPAM: {Config: [{Subnet: '172.20.0.0/16'}]}}]);
    if (args.includes('{{.ID}} {{.Label "com.docker.compose.service"}}')) return 'old-auth auth-service';
    for (const owner of ['auth', 'case', 'screening']) {
      if (args.some(arg => arg.replaceAll('\\', '/').endsWith(`${owner === 'case' ? 'case-manager' : owner}-service/scripts/prepare-mysql.mjs`))) {
        mkdirSync(resolve(local, owner + '-runtime'), {recursive: true}); writeFileSync(resolve(local, owner + '-runtime/config.json'), '{}');
      }
    }
    if (args.some(arg => arg.replaceAll('\\', '/').endsWith('frontend/scripts/prepare-mysql.mjs'))) writeFileSync(resolve(local, 'frontend.env'), '');
    return '';
  };
  await setupPlatform({port: 8088, skipBuild: false}, {root, run, request: api.request});
  const stopped = commands.findIndex(command => command.includes('old-auth'));
  const reconciled = commands.findIndex(command => command.some(arg => arg.replaceAll('\\', '/').endsWith('auth-service/scripts/prepare-mysql.mjs')));
  assert(stopped >= 0 && reconciled > stopped);
  assert(commands.some(command => command.includes('build') && command.includes('frontend')));
  assert(commands.some(command => command.includes('run') && command.includes('migrate-screening')));
  assert(!commands.some(command => command.includes('powershell') || command.some(arg => arg.endsWith('.ps1'))));
});
