// Reuse PostgreSQL and existing identity material. Never delete a volume or copy MySQL data.
import { randomBytes } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, writeFileSync, createWriteStream } from 'node:fs';
import { spawn, spawnSync } from 'node:child_process';
import { pipeline } from 'node:stream/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const args = process.argv.slice(2);
const portIndex = args.indexOf('--port');
const requestedPort = portIndex >= 0 ? args[portIndex + 1] : undefined;
if ((portIndex >= 0 && requestedPort === undefined) || args.some((arg, i) => !['--port', '--prepare-only', '--skip-build'].includes(arg) && !(portIndex >= 0 && i === portIndex + 1))) {
  throw new Error('Usage: node docker/setup-postgres.mjs [--port 8088] [--prepare-only] [--skip-build]');
}
function parse(file) {
  if (!existsSync(file)) return {};
  return Object.fromEntries(readFileSync(file, 'utf8').split(/\r?\n/).flatMap(line => {
    const match = line.match(/^([A-Z][A-Z0-9_]*)=(.*)$/);
    if (!match) return [];
    let value = match[2].trim();
    if (/^(['"]).*\1$/.test(value)) value = value.slice(1, -1);
    return [[match[1], value]];
  }));
}
const directory = path.join(root, '.local/postgres-dev');
const envFile = path.join(directory, 'compose.env');
const config = { ...parse(path.join(root, '.env')), ...parse(envFile) };
const port = Number(requestedPort ?? config.POSTGRES_GATEWAY_PORT ?? 8088);
if (!Number.isInteger(port) || port < 1024 || port > 65535) throw new Error('Port must be between 1024 and 65535.');
for (const file of ['auth.env', 'platform.env', 'proxy.env', 'keycloak.env', 'database.env', 'data-model.env', 'platform-private.pem', 'platform-public.json']) {
  if (!existsSync(path.join(root, '.local/auth-provider', file))) throw new Error(`Missing existing identity material: ${file}. Follow backend/auth-service/PLATFORM_AUTH.md and run the local provider/platform setup first.`);
}
for (const key of ['CASE_SERVICE_TENANT_IDS', 'CASE_USER_JWT_KEYS_FILE', 'CASE_USER_JWT_ISSUER']) {
  if (!config[key]) throw new Error(`Configure ${key} in .env before starting PostgreSQL.`);
}
for (const key of ['SERVICE_AUTH_TOKEN', 'CASE_SERVICE_AUTH_TOKEN', 'WORKFLOW_AUTHORING_TOKEN', 'SCREENING_AUTHORING_TOKEN', 'SCREENING_INTAKE_TOKEN', 'SCREENING_CALLBACK_TOKEN', 'SCREENING_OPERATOR_TOKEN']) {
  config[key] ||= randomBytes(32).toString('hex');
}
const profiles = new Set((config.COMPOSE_PROFILES ?? '').split(',').filter(Boolean));
profiles.add('local-idp'); profiles.add('screening-worker');
config.COMPOSE_PROFILES = [...profiles].join(',');
config.COMPOSE_PROJECT_NAME = 'new-fraud-system';
config.POSTGRES_GATEWAY_PORT = String(port);
mkdirSync(directory, { recursive: true });
const quote = value => `'${value.replaceAll("'", "\\'")}'`;
writeFileSync(envFile, Object.entries(config).map(([key, value]) => `${key}=${quote(value)}`).join('\n') + '\n', { mode: 0o600 });
console.log('PostgreSQL gateway configuration ready in ignored .local/postgres-dev. Existing identity material preserved.');
const compose = ['compose', '--env-file', envFile, '-p', 'new-fraud-system', '-f', 'docker-compose.yml', '-f', 'docker-compose.gateway.yml'];
function docker(extra, options = {}) {
  const result = spawnSync('docker', [...compose, ...extra], { cwd: root, stdio: 'inherit', ...options });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error('Docker command failed; no data volumes were removed.');
  return result;
}
docker(['config', '--quiet']);
if (args.includes('--prepare-only')) process.exit(0);
docker(['up', '-d', '--wait', 'postgres']);
// Back up the PostgreSQL application database before additive service migrations.
const backup = path.join(directory, `before-start-${new Date().toISOString().replaceAll(/[:.]/g, '-')}.dump`);
const dump = spawn('docker', [...compose, 'exec', '-T', 'postgres', 'pg_dump', '-U', 'fraud', '-d', 'fraud', '-Fc'], { cwd: root, stdio: ['ignore', 'pipe', 'inherit'] });
const completion = new Promise((resolve, reject) => {
  dump.once('error', reject);
  dump.once('exit', code => code === 0 ? resolve() : reject(new Error('PostgreSQL backup failed. Startup halted before migrations.')));
});
await Promise.all([pipeline(dump.stdout, createWriteStream(backup, { mode: 0o600, flags: 'wx' })), completion]);
console.log('Private PostgreSQL backup saved before service migrations.');
if (!args.includes('--skip-build')) docker(['build', '--parallel'], { env: { ...process.env, COMPOSE_PARALLEL_LIMIT: '2' } });
docker(['up', '-d', '--wait', '--wait-timeout', '300']);
// Import does not update an existing realm. Change only this client's redirect allowlist.
const identityScript = `set -eu
admin=/opt/keycloak/bin/kcadm.sh
cfg=/tmp/postgres-gateway-kcadm.config
trap 'rm -f "$cfg"' EXIT
"$admin" config credentials --config "$cfg" --server http://localhost:8080/identity --realm master --user "$KC_BOOTSTRAP_ADMIN_USERNAME" --password "$KC_BOOTSTRAP_ADMIN_PASSWORD" >/dev/null
client=$("$admin" get clients --config "$cfg" -r fraud -q clientId=fraud-auth --fields id --format csv --noquotes)
test -n "$client"
"$admin" update "clients/$client" --config "$cfg" -r fraud -s 'redirectUris=["http://localhost:${port}/api/auth/callback"]'
`;
docker(['exec', '-T', 'keycloak', '/bin/bash', '-ec', identityScript]);
console.log(`PostgreSQL application: http://localhost:${port}\nAPI documentation: http://localhost:${port}/services/\nMySQL containers and volumes remain separate.`);
