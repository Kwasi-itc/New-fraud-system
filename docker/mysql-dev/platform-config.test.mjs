// Validates the resolved model without connecting to the Docker daemon.
import assert from 'node:assert/strict';
import test from 'node:test';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
const root = fileURLToPath(new URL('../../', import.meta.url));
test('one project publishes only the gateway for frontend, fraud APIs and identity', () => {
  const result = spawnSync('docker', ['compose', '-f', 'docker/compose.yaml', 'config', '--format', 'json'], {cwd: root, encoding: 'utf8', windowsHide: true});
  assert.equal(result.status, 0, 'Compose model must validate; no secret-bearing config is printed');
  const config = JSON.parse(result.stdout);
  assert.equal(config.name, 'fraud-mysql-dev');
  const published = Object.entries(config.services).flatMap(([service, value]) => (value.ports ?? []).map(port => ({service, host: port.host_ip, port: Number(port.published)})));
  assert.deepEqual(published, [{service: 'api-gateway', host: '127.0.0.1', port: 8088}]);
  assert.equal(config.services.keycloak.environment.KC_HOSTNAME, 'http://localhost:8088/identity');
  assert.equal(config.services.keycloak.environment.KC_HTTP_RELATIVE_PATH, '/identity');
  assert.equal(config.services.keycloak.environment.KC_HTTP_MANAGEMENT_RELATIVE_PATH, '/');
  assert.equal(config.services.keycloak.environment.KC_PROXY_HEADERS, 'xforwarded');
  assert.equal(config.services.postgres, undefined, 'No PostgreSQL container in the normal platform');
  assert.equal(config.volumes.postgres_data, undefined, 'The offline backup is outside normal runtime resources');
  assert.equal(config.services.keycloak.environment.KC_DB, 'mysql');
  assert(config.services.keycloak.environment.KC_DB_URL.startsWith('jdbc:mysql://mysql:3306/keycloak'));
  assert.equal(config.services.keycloak.environment.KC_DB_POOL_MAX_SIZE, '5');
  assert.equal(config.services['keycloak-db-init'].image, 'mysql:8.4.8');
  assert(config.services['mysql-reference'].volumes.some(volume => volume.source === 'mysql_dev_reference_data' && volume.target === '/var/lib/mysql'));
  for (const [name, service] of Object.entries(config.services)) {
    if (name.startsWith('migrate-')) continue;
    assert(!Object.keys(service.depends_on ?? {}).some(dependency => dependency.startsWith('migrate-')), `${name}: runtime must not replay owner installers`);
  }
  assert.equal(config.services.frontend.environment.APP_ORIGIN, 'http://localhost:8088');
});
