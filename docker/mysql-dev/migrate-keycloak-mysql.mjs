// Local-only migration. Full exports contain private credentials and stay under
// ignored .local/. Never overwrite a populated target or delete the PG volume.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {closeSync, existsSync, mkdirSync, openSync, readFileSync, readdirSync, unlinkSync, writeFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import {resolve, relative} from 'node:path';
import {randomUUID} from 'node:crypto';
import {digest, importAllowed, realmManifest} from '../keycloak/migration-contract.mjs';
import {gatewayIssuer} from '../keycloak/urls.mjs';

const root = fileURLToPath(new URL('../../', import.meta.url));
const local = resolve(root, '.local/mysql-dev');
const privateDir = resolve(local, 'keycloak-migration');
const bootstrap = resolve(root, 'docker/keycloak/setup.compose.yaml');
const sourceCompose = resolve(root, 'docker/keycloak/postgres-source.compose.yaml');
const journalPath = resolve(privateDir, 'state.json');
mkdirSync(privateDir, {recursive: true, mode: 0o700});
const lockPath = resolve(privateDir, 'migration.lock');
let lock;
try {lock = openSync(lockPath, 'wx', 0o600);} catch {throw Error('Another provider migration may be active. Inspect .local/mysql-dev/keycloak-migration/migration.lock before retrying.');}
writeFileSync(lock, String(process.pid));
process.on('exit', () => {closeSync(lock); unlinkSync(lockPath);});
let journal = existsSync(journalPath) ? JSON.parse(readFileSync(journalPath, 'utf8')) : null;
if (journal) assert.equal(journal.version, 1, 'Unknown provider migration journal');
function save(values) {
  journal = {...journal, version: 1, ...values, updatedAt: new Date().toISOString()};
  writeFileSync(journalPath, JSON.stringify(journal, null, 2) + '\n', {mode: 0o600});
}
function docker(args, {input, log} = {}) {
  const result = spawnSync('docker', args, {cwd: root, env: {...process.env, COMPOSE_IGNORE_ORPHANS: 'true'}, input, encoding: 'utf8', windowsHide: true, timeout: 600000, maxBuffer: 20 << 20});
  if (log) writeFileSync(resolve(privateDir, log), (result.stdout ?? '') + (result.stderr ?? ''), {mode: 0o600});
  if (result.status !== 0) throw Error(`Docker ${args[0]} failed${log ? '; private diagnostic log: .local/mysql-dev/keycloak-migration/' + log : ''}`);
  return result.stdout;
}
function inspect(name) {
  const ids = docker(['ps', '-a', '--filter', `name=^${name}$`, '--format', '{{.ID}}']).trim();
  if (!ids) return null;
  const [container] = JSON.parse(docker(['inspect', ids]));
  assert(['fraud-mysql-dev', 'new-fraud-system'].includes(container.Config.Labels?.['com.docker.compose.project']), 'Unexpected provider container ownership');
  return container;
}
const environment = container => Object.fromEntries((container.Config.Env ?? []).map(line => {const at = line.indexOf('='); return [line.slice(0, at), line.slice(at + 1)];}));
const readEnv = path => Object.fromEntries(readFileSync(path, 'utf8').split(/\r?\n/).filter(line => line && !line.startsWith('#')).map(line => {const at = line.indexOf('='); return [line.slice(0, at), line.slice(at + 1)];}));
const provider = readEnv(resolve(root, '.local/auth-provider/keycloak.env'));
assert(/^[a-f0-9]{64}$/.test(provider.KC_DB_PASSWORD), 'Restore the existing provider database credential');
const passwordFile = resolve(local, 'keycloak-db-password.txt');
if (!existsSync(passwordFile)) writeFileSync(passwordFile, provider.KC_DB_PASSWORD, {flag: 'wx', mode: 0o600});
assert.equal(readFileSync(passwordFile, 'utf8'), provider.KC_DB_PASSWORD, 'Provider database credentials differ; no automatic rotation was attempted');

if (!existsSync(resolve(local, 'root-password.txt'))) {
  const existingData = docker(['volume', 'ls', '--filter', 'name=^fraud-mysql-dev_mysql_dev_data$', '--format', '{{.Name}}']).trim();
  assert(!existingData, 'Restore the retained MySQL root secret before running setup');
  const setup = spawnSync(process.execPath, [resolve(root, 'docker/mysql-dev/configure-foundation.mjs')], {cwd: root, stdio: 'inherit', windowsHide: true});
  assert.equal(setup.status, 0, 'MySQL foundation setup failed');
}
docker(['compose', '-f', bootstrap, 'up', '-d', '--wait', 'mysql'], {log: 'mysql-start.log'});
docker(['compose', '-f', bootstrap, 'run', '--rm', '--no-deps', 'keycloak-db-init'], {log: 'mysql-provision.log'});
function mysql(sql) {
  const script = 'export MYSQL_PWD="$(cat /run/secrets/mysql_root_password)"; exec mysql --protocol=socket --socket=/var/run/mysqld/mysqld.sock -uroot --batch --skip-column-names';
  return docker(['exec', '-i', 'fraud-mysql-dev-mysql-1', 'sh', '-c', script], {input: sql + '\n'}).trim();
}
function targetRealmCount() {
  const table = mysql("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA='keycloak' AND UPPER(TABLE_NAME)='REALM'");
  if (!table) return 0;
  assert(/^[A-Za-z_]+$/.test(table));
  return Number(mysql('SELECT COUNT(*) FROM `keycloak`.`' + table + '`'));
}
function readExport(directory) {
  const files = readdirSync(directory).filter(name => name.endsWith('-realm.json'));
  assert(files.length > 0, 'No full realm exports were produced');
  return files.map(name => {
    const realm = JSON.parse(readFileSync(resolve(directory, name), 'utf8'));
    assert.equal(name, realm.realm + '-realm.json', 'Unexpected realm export filename');
    assert(Array.isArray(realm.users), 'A user-inclusive CLI export is required');
    return realm;
  });
}
function keycloakCLI(compose, service, name, directory, args, log, readOnly = false) {
  // Full master-realm imports contain the existing administrator. Passing the
  // normal bootstrap-admin env would try to create that same user a second time.
  const command = 'unset KC_BOOTSTRAP_ADMIN_USERNAME KC_BOOTSTRAP_ADMIN_PASSWORD; exec /opt/keycloak/bin/kc.sh "$@"';
  docker(['compose', '-f', compose, 'run', '--rm', '--no-deps', '--name', name, '-v', directory + ':/migration/export' + (readOnly ? ':ro' : ''), '--entrypoint', '/bin/bash', service, '-c', command, '--', ...args], {log});
}
async function verifyAdminAndIssuer() {
  const {issuer, masterToken} = gatewayIssuer();
  const discovery = await fetch(issuer + '/.well-known/openid-configuration', {signal: AbortSignal.timeout(15000)});
  assert.equal(discovery.status, 200, 'Provider discovery');
  assert.equal((await discovery.json()).issuer, issuer, 'The identity issuer must match the public gateway');
  const response = await fetch(masterToken, {method: 'POST', signal: AbortSignal.timeout(15000), body: new URLSearchParams({grant_type: 'password', client_id: 'admin-cli', username: provider.KC_BOOTSTRAP_ADMIN_USERNAME, password: provider.KC_BOOTSTRAP_ADMIN_PASSWORD})});
  assert.equal(response.status, 200, 'Retained provider administrator password must work');
  // Consume the response without logging or retaining the bearer credential.
  await response.arrayBuffer();
}
const candidates = ['fraud-mysql-dev-keycloak-1', 'fraud-mysql-dev-keycloak-postgres-1', 'new-fraud-system-keycloak-1'].map(inspect).filter(Boolean);
const postgresSources = candidates.filter(container => environment(container).KC_DB === 'postgres');
const runningSources = postgresSources.filter(container => container.State.Running);
assert(runningSources.length <= 1, 'Multiple PostgreSQL Keycloak writers detected; reconcile before migration');
const runningMySQL = candidates.filter(container => environment(container).KC_DB === 'mysql' && container.State.Running);
assert(!(runningSources.length && runningMySQL.length), 'Both provider databases have running writers; reconcile the authoritative source before migration');
const source = runningSources[0] ?? (runningMySQL.length ? null : postgresSources[0]);
const count = targetRealmCount();
const oldPostgres = inspect('fraud-mysql-dev-postgres-1') ?? inspect('new-fraud-system-postgres-1');
const retainedPostgresVolume = docker(['volume', 'ls', '--filter', 'name=^new-fraud-system_postgres_data$', '--format', '{{.Name}}']).trim();
if (oldPostgres) {
  assert.equal(oldPostgres.Config.Image, 'postgres:16');
  assert(oldPostgres.Mounts.some(mount => mount.Destination === '/var/lib/postgresql/data' && mount.Type === 'volume' && mount.Name === 'new-fraud-system_postgres_data'), 'Unexpected PostgreSQL data volume; refusing migration');
}
if (!source && retainedPostgresVolume && count === 0) throw Error('A retained PostgreSQL database exists without its Keycloak source container. Restore/start that source before attempting migration.');

let stoppedSource = false;
try {
  if (source) {
    assert.equal(source.Config.Image, 'quay.io/keycloak/keycloak:26.8.0', 'Migrate using the identical Keycloak version');
    const shouldImport = importAllowed(count, Boolean(journal?.sourceManifest));
    if (source.State.Running) {docker(['stop', source.Id]); stoppedSource = true;}
    if (oldPostgres?.Name === '/new-fraud-system-postgres-1' && oldPostgres.State.Running) docker(['stop', oldPostgres.Id]);
    docker(['compose', '-f', sourceCompose, 'up', '-d', '--wait', 'postgres'], {log: 'postgres-source.log'});
    const run = resolve(privateDir, new Date().toISOString().replaceAll(':', '-') + '-' + randomUUID().slice(0, 8));
    const exported = resolve(run, 'source'); const verified = resolve(run, 'target');
    mkdirSync(exported, {recursive: true, mode: 0o700}); mkdirSync(verified, {recursive: true, mode: 0o700});
    console.log('Exporting all Keycloak realms and password material with the source server stopped.');
    keycloakCLI(sourceCompose, 'keycloak-postgres', 'fraud-mysql-dev-keycloak-migrate-source', exported, ['export', '--dir', '/migration/export', '--users', 'realm_file'], 'postgres-export.log');
    const original = realmManifest(readExport(exported));
    save({phase: 'exported', sourceManifest: original, sourceDigest: digest(original), exportDirectory: relative(root, exported), sourceVolume: 'new-fraud-system_postgres_data'});
    if (shouldImport) {
      assert.equal(targetRealmCount(), 0, 'Target became populated; refusing to overwrite realms');
      console.log('Importing the retained realms into the empty Keycloak MySQL database.');
      keycloakCLI(bootstrap, 'keycloak', 'fraud-mysql-dev-keycloak-migrate-import', exported, ['import', '--dir', '/migration/export', '--override', 'true'], 'mysql-import.log', true);
      save({phase: 'imported'});
    }
    // The MySQL server is not started as a Keycloak application until the
    // imported IDs, credentials, roles, clients and signing keys compare equal.
    keycloakCLI(bootstrap, 'keycloak', 'fraud-mysql-dev-keycloak-migrate-target', verified, ['export', '--dir', '/migration/export', '--users', 'realm_file'], 'mysql-export.log');
    const imported = realmManifest(readExport(verified), original);
    assert.equal(digest(imported), digest(original), 'Imported realm identity/authentication manifest differs; refusing takeover');
    save({phase: 'verified', targetDigest: digest(imported), realms: imported.length, users: imported.reduce((sum, realm) => sum + realm.users.length, 0)});
  } else if (count === 0 && journal?.sourceManifest) {
    throw Error('The MySQL target is empty but a retained-source migration journal exists. Restore the source or replay its private export explicitly; no fresh realm was created.');
  }
  docker(['compose', '-f', bootstrap, 'up', '-d', '--wait', 'keycloak'], {log: 'mysql-keycloak-start.log'});
  // Bootstrap the proxy without its frontend dependency: auth discovery/login
  // configuration must work before the full application can become healthy.
  docker(['compose', '-f', bootstrap, 'up', '-d', '--no-deps', '--wait', 'api-gateway'], {log: 'gateway-start.log'});
  docker(['compose', '-f', bootstrap, 'exec', '-T', 'api-gateway', 'nginx', '-t']);
  docker(['compose', '-f', bootstrap, 'exec', '-T', 'api-gateway', 'nginx', '-s', 'reload']);
  await verifyAdminAndIssuer();
  // Remove only known stopped provider containers, never their data volumes.
  for (const name of ['fraud-mysql-dev-postgres-1', 'new-fraud-system-postgres-1', 'fraud-mysql-dev-keycloak-postgres-1', 'new-fraud-system-keycloak-1', 'new-fraud-system-keycloak-db-init-1']) {
    const container = inspect(name); if (!container) continue;
    if (container.State.Running) docker(['stop', container.Id]);
    docker(['rm', container.Id]);
  }
  save({phase: 'complete', database: 'mysql', address: 'mysql:3306/keycloak', issuer: gatewayIssuer().issuer, postgresVolumeRetained: Boolean(source || oldPostgres || journal?.sourceVolume)});
  console.log('Keycloak is running on MySQL behind the application gateway; PostgreSQL is no longer running.');
} catch (error) {
  // Killing a timed-out Compose CLI does not necessarily stop its one-off
  // container. Release all known export/import processes before any rollback.
  for (const name of ['fraud-mysql-dev-keycloak-migrate-source', 'fraud-mysql-dev-keycloak-migrate-import', 'fraud-mysql-dev-keycloak-migrate-target']) {
    const container = inspect(name);
    if (container?.State.Running) docker(['stop', container.Id]);
  }
  if (source && (stoppedSource || journal?.sourceManifest)) {
    console.error('Keycloak MySQL cutover failed; restoring the PostgreSQL source without deleting either database.');
    try {
      docker(['compose', '-f', bootstrap, 'stop', 'keycloak'], {log: 'rollback-stop.log'});
      // The old named-volume source is retained and remains authoritative on failure.
      docker(['compose', '-f', sourceCompose, 'up', '-d', '--wait', 'keycloak-postgres'], {log: 'rollback-start.log'});
      save({phase: 'rolled_back'});
    } catch {
      throw Error('Provider migration failed and automatic rollback needs attention. Both database stores and private exports were retained; see .local/mysql-dev/keycloak-migration/.');
    }
  }
  // Error text deliberately omits CLI output, exports and credentials.
  throw Error('Keycloak MySQL migration did not complete: ' + error.message);
}
