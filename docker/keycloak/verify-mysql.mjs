// Read-only runtime/privilege checks. No credentials or realm exports are printed.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
const root = fileURLToPath(new URL('../../', import.meta.url));
function docker(args, input) {
  const result = spawnSync('docker', args, {cwd: root, encoding: 'utf8', input, windowsHide: true, timeout: 60000});
  assert.equal(result.status, 0, 'Docker verification failed; private output omitted');
  return result.stdout;
}
const [container] = JSON.parse(docker(['inspect', 'fraud-mysql-dev-keycloak-1']));
const env = Object.fromEntries(container.Config.Env.map(line => {const at = line.indexOf('='); return [line.slice(0, at), line.slice(at + 1)];}));
assert.equal(container.State.Health.Status, 'healthy');
assert.equal(env.KC_DB, 'mysql');
assert(env.KC_DB_URL.startsWith('jdbc:mysql://mysql:3306/keycloak'));
assert.equal(env.KC_DB_POOL_MAX_SIZE, '5');
assert.equal(env.KC_HTTP_RELATIVE_PATH, '/identity');
assert.equal(container.HostConfig.PortBindings && Object.keys(container.HostConfig.PortBindings).length || 0, 0, 'Keycloak has no published host ports');
const journal = JSON.parse(readFileSync(resolve(root, '.local/mysql-dev/keycloak-migration/state.json'), 'utf8'));
assert.equal(journal.phase, 'complete');
if (journal.sourceManifest) assert.equal(journal.sourceDigest, journal.targetDigest, 'Imported identity material verified');
const script = 'export MYSQL_PWD="$(cat /run/secrets/mysql_root_password)"; exec mysql --protocol=socket -uroot --batch --skip-column-names';
const values = docker(['exec', '-i', 'fraud-mysql-dev-mysql-1', 'sh', '-c', script],
  "SELECT COUNT(*) FROM keycloak.REALM; SELECT COUNT(*) FROM keycloak.USER_ENTITY; SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE USER='fraud_keycloak' AND DB='keycloak'; SELECT DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='keycloak';\n").trim().split(/\r?\n/);
if (journal.sourceManifest) {
  assert.equal(Number(values[0]), journal.realms);
  assert.equal(Number(values[1]), journal.users);
}
assert(Number(values[2]) > 0, 'Running Keycloak must have an active MySQL connection');
assert.equal(values[3], 'utf8mb4_unicode_ci');
const deniedRead = `set -eu
export MYSQL_PWD="$(cat /run/secrets/keycloak_mysql_password)"
mysql --protocol=socket --socket=/target-socket/mysqld.sock -ufraud_keycloak --database=keycloak -e 'SELECT COUNT(*) FROM REALM' >/dev/null
if mysql --protocol=socket --socket=/target-socket/mysqld.sock -ufraud_keycloak -e 'SELECT id FROM fraud.data_model_tenants LIMIT 0' >/dev/null 2>/tmp/denied; then
  echo 'Unexpected cross-database access' >&2; exit 1
fi
grep -q 'ERROR 1142' /tmp/denied
`;
docker(['compose', '-f', 'docker/keycloak/setup.compose.yaml', 'run', '--rm', '--no-deps', '--entrypoint', '/bin/sh', 'keycloak-db-init', '-c', deniedRead]);
const projects = JSON.parse(docker(['compose', 'ls', '--format', 'json']));
assert(projects.some(project => project.Name === 'fraud-mysql-dev'));
const ids = docker(['ps', '--filter', 'label=com.docker.compose.project=fraud-mysql-dev', '--format', '{{.ID}}']).trim().split(/\r?\n/).filter(Boolean);
const runtime = JSON.parse(docker(['inspect', ...ids]));
assert(!runtime.some(value => value.Config.Image.startsWith('postgres:')), 'No running PostgreSQL container');
console.log(`PASS Keycloak MySQL: ${values[0]} realms, ${values[1]} users; identities verified, live MySQL connection, fraud-table access denied, no PostgreSQL runtime`);
