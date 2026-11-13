import {spawnSync} from 'node:child_process';
import {existsSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {prepareApplicationFiles, prepareMountPermissions} from './setup-files.mjs';
import {installServices} from './install-services.mjs';

export const runtimeServices = ['api-gateway', 'frontend', 'data-model-service', 'data-model-worker', 'data-model-author', 'ingestion-service', 'ingestion-worker', 'decision-engine-service', 'decision-engine-worker', 'decision-local-providers', 'auth-service', 'auth-worker', 'case-manager-service', 'case-manager-worker', 'screening-service', 'screening-worker'];
export function runner(root, env) {
  return (program, args, capture = false) => {
    const result = spawnSync(program, args, {cwd: root, env, windowsHide: true, encoding: 'utf8', stdio: capture ? ['ignore', 'pipe', 'pipe'] : 'inherit', maxBuffer: 20 << 20});
    if (result.error || result.status !== 0) throw Error(`${program} ${args[0] ?? ''} failed${capture ? '; private output omitted' : '; see output above'}`);
    return result.stdout?.trim() ?? '';
  };
}
export async function setupPlatform(options, dependencies = {}) {
  const root = dependencies.root ?? fileURLToPath(new URL('../../', import.meta.url));
  const local = resolve(root, '.local/mysql-dev');
  const env = {...process.env, MYSQL_GATEWAY_PORT: String(options.port), FRAUD_APP_ORIGIN: `http://localhost:${options.port}`, FRAUD_UNIFIED_PLATFORM: '1', COMPOSE_IGNORE_ORPHANS: 'true'};
  // Keep the EC2 profile opt-in separate from this local Docker Desktop setup.
  if (env.EC2_MYSQL_HTTP_TEST) throw Error('Unset EC2_MYSQL_HTTP_TEST before starting the local platform');
  const run = dependencies.run ?? runner(root, env);
  const node = (script, ...args) => run(process.execPath, [resolve(root, script), ...args]);
  const compose = (...args) => run('docker', ['compose', '--profile', 'install', '-f', resolve(root, 'docker/compose.yaml'), ...args]);
  const runtime = (...args) => run('docker', ['compose', '-f', resolve(root, 'docker/compose.yaml'), ...args]);
  const context = {root, local, env, run, node, compose, runtime, origin: env.FRAUD_APP_ORIGIN, request: dependencies.request ?? fetch, permissions: () => prepareMountPermissions(root)};
  if (run('docker', ['info', '--format', '{{.OSType}}'], true) !== 'linux') throw Error('Docker must use Linux containers');
  const version = run('docker', ['compose', 'version', '--short'], true).replace(/^v/, '').split('.').map(value => Number.parseInt(value, 10));
  if (!(version[0] > 2 || version[0] === 2 && (version[1] > 24 || version[1] === 24 && version[2] >= 4))) throw Error('Docker Compose 2.24.4 or newer is required');
  node('docker/mysql-dev/configure-foundation.mjs');
  node('backend/auth-service/scripts/setup-local-provider.mjs');
  node('docker/mysql-dev/consolidate-provider.mjs');
  node('docker/keycloak/configure-gateway-issuer.mjs');

  const required = ['auth-runtime/config.json', 'case-runtime/config.json', 'screening-runtime/config.json', 'ingestion-runtime/config.json', 'decision-runtime/config.json', 'frontend.env', 'plans/data_model.json'];
  const missing = required.some(file => !existsSync(resolve(local, file)));
  const aligned = run(process.execPath, [resolve(root, 'docker/mysql-dev/check-runtime-network.mjs')], true) === 'true';
  const reference = run('docker', ['ps', '-a', '--filter', 'name=^fraud-mysql-dev-mysql-reference-1$', '--format', '{{.ID}}'], true);
  const mounts = reference ? JSON.parse(run('docker', ['inspect', reference, '--format', '{{json .Mounts}}'], true)) : [];
  const persistent = mounts.some(mount => mount.Destination === '/var/lib/mysql' && mount.Type === 'volume' && mount.Name === 'fraud-mysql-dev_mysql_dev_reference_data');
  const install = options.install || missing || !aligned || !persistent;

  if (install) {
    console.log('Installing owner schemas and runtime images. This can take several minutes on first setup.');
    const running = run('docker', ['ps', '--filter', 'label=com.docker.compose.project=fraud-mysql-dev', '--format', '{{.ID}} {{.Label "com.docker.compose.service"}}'], true);
    const writers = running.split(/\r?\n/).map(line => line.split(' ')).filter(([, service]) => runtimeServices.includes(service)).map(([id]) => id);
    if (writers.length) run('docker', ['stop', ...writers]);
    prepareApplicationFiles(root);
    node('backend/auth-service/scripts/prepare-mysql.mjs', '--reconcile-network');
    node('backend/case-manager-service/scripts/prepare-mysql.mjs', '--reconcile-network');
    node('backend/screening-service/scripts/prepare-mysql.mjs', '--reconcile-network');
  }
  node('frontend/scripts/configure-mysql-login.mjs');
  node('frontend/scripts/prepare-mysql.mjs');
  context.permissions();
  runtime('config', '--quiet');
  const legacy = run('docker', ['ps', '-a', '--filter', 'label=com.docker.compose.project=fraud-mysql-gateway', '--format', '{{.ID}}'], true);
  if (legacy) run('docker', ['compose', '-p', 'fraud-mysql-gateway', '-f', resolve(root, 'compose.mysql-gateway.yaml'), 'down']);
  runtime('stop', ...runtimeServices);
  // The bootstrap gateway has no frontend dependency and must remain available
  // while owner HTTP setup creates the initial ingestion tenant and table.
  run('docker', ['compose', '-f', resolve(root, 'docker/keycloak/setup.compose.yaml'), 'up', '-d', '--no-deps', '--wait', 'api-gateway']);
  runtime('up', '-d', '--no-build', '--wait', 'mysql', 'mysql-reference', 'keycloak', 'auth-mail');
  if (install) await installServices(context, options);
  else if (!options.skipBuild) runtime('build', 'frontend');
  context.permissions();
  // Exclude the install profile: restarts never replay owner migration jobs.
  runtime('up', '-d', '--no-build', '--wait');
  if (options.usersFile) {
    node('backend/auth-service/scripts/setup-mysql-users.mjs', '--file', options.usersFile, '--data-model-url', `${context.origin}/services/data-model`, '--apply', ...(options.createMissingUsers ? ['--create-missing'] : []));
  }
  node('docker/mysql-dev/verify-gateway.mjs', '--port', String(options.port));
  console.log(`Platform ready: ${context.origin}/ ; API references: ${context.origin}/services/`);
  console.log(`Docker project: fraud-mysql-dev. Only port ${options.port} is published; Keycloak: ${context.origin}/identity/`);
}
