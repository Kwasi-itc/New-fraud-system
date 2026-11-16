import { randomBytes } from 'node:crypto';
import { copyFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const args = process.argv.slice(2);
const usage = 'Usage: node docker/setup-ec2.mjs [--project new-fraud-system] [--port 8088] [--env-file .env] [--prepare-only] [--skip-build]';

const valueFlags = new Set(['--port', '--project', '--env-file']);
const booleanFlags = new Set(['--prepare-only', '--skip-build']);
const parsedArgs = {};

for (let i = 0; i < args.length; i++) {
  const arg = args[i];
  const eqIndex = arg.indexOf('=');
  const flag = eqIndex >= 0 ? arg.slice(0, eqIndex) : arg;

  if (booleanFlags.has(flag) && eqIndex === -1) {
    parsedArgs[flag] = true;
  } else if (valueFlags.has(flag)) {
    const value = eqIndex >= 0 ? arg.slice(eqIndex + 1) : args[++i];
    if (!value || (eqIndex === -1 && value.startsWith('--'))) {
      throw new Error(`Missing value for ${flag}.\n${usage}`);
    }
    parsedArgs[flag] = value;
  } else {
    throw new Error(usage);
  }
}

const requestedPort = parsedArgs['--port'];
const requestedProject = parsedArgs['--project'];
const requestedEnvFile = parsedArgs['--env-file'];

const sourceEnvPath = requestedEnvFile
  ? path.resolve(process.cwd(), requestedEnvFile)
  : path.join(root, '.env');

if (requestedEnvFile && !existsSync(sourceEnvPath)) {
  throw new Error(`Environment file not found: ${sourceEnvPath}`);
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

const baseEnv = parse(sourceEnvPath);
const project = requestedProject ?? baseEnv.COMPOSE_PROJECT_NAME ?? 'new-fraud-system';
if (!/^[a-z0-9][a-z0-9_-]*$/.test(project)) {
  throw new Error('Project name must start with a lowercase letter or number and contain only lowercase letters, numbers, hyphens, or underscores.');
}

const directory = path.join(root, '.local', project);
const envFile = path.join(directory, 'compose.env');
const existingComposeEnv = parse(envFile);

// When --env-file is explicitly supplied, let its values update existing compose.env keys
// while preserving previously generated tokens that are not defined in the source .env file.
const config = requestedEnvFile
  ? { ...existingComposeEnv, ...baseEnv }
  : { ...baseEnv, ...existingComposeEnv };

const port = Number(requestedPort ?? config.POSTGRES_GATEWAY_PORT ?? 8088);
if (!Number.isInteger(port) || port < 1024 || port > 65535) {
  throw new Error('Port must be between 1024 and 65535.');
}

mkdirSync(directory, { recursive: true });

// Ensure project-scoped auth/config files exist in .local/<project>/ (seed from .local/auth-provider if needed).
const legacyAuthDir = path.join(root, '.local/auth-provider');
const requiredAuthFiles = [
  'auth.env',
  'platform.env',
  'proxy.env',
  'keycloak.env',
  'database.env',
  'data-model.env',
  'platform-private.pem',
  'platform-public.json',
];

for (const file of requiredAuthFiles) {
  const target = path.join(directory, file);
  const seed = path.join(legacyAuthDir, file);
  if (!existsSync(target) && existsSync(seed)) {
    copyFileSync(seed, target);
  }
  if (!existsSync(target)) {
    throw new Error(`Missing identity material: .local/${project}/${file}. Follow backend/auth-service/PLATFORM_AUTH.md and run node backend/auth-service/scripts/setup-local-provider.mjs first.`);
  }
}

const optionalAuthFiles = ['invitations.env'];
for (const file of optionalAuthFiles) {
  const target = path.join(directory, file);
  const seed = path.join(legacyAuthDir, file);
  if (!existsSync(target) && existsSync(seed)) {
    copyFileSync(seed, target);
  }
}

// Scope CASE_USER_JWT_KEYS_FILE to this project's directory unless a custom path was set.
const defaultProjectKeys = `./.local/${project}/platform-public.json`;
if (!config.CASE_USER_JWT_KEYS_FILE || config.CASE_USER_JWT_KEYS_FILE.includes('.local/auth-provider/')) {
  config.CASE_USER_JWT_KEYS_FILE = defaultProjectKeys;
}

for (const key of ['CASE_SERVICE_TENANT_IDS', 'CASE_USER_JWT_KEYS_FILE', 'CASE_USER_JWT_ISSUER']) {
  if (!config[key]) {
    throw new Error(`Configure ${key} in ${path.relative(root, sourceEnvPath)} or .local/${project}/compose.env before starting the system.`);
  }
}

if (!config.DATABASE_URL) {
  throw new Error(`Configure RDS DATABASE_URL in ${path.relative(root, sourceEnvPath)} or .local/${project}/compose.env before starting the system.`);
}

// Derive RDS_HOST / RDS_PORT / RDS_DATABASE / RDS_USER / RDS_PASSWORD / RDS_SSLMODE from DATABASE_URL if not explicitly set.
let parsedDb;
try {
  parsedDb = new URL(config.DATABASE_URL);
} catch {
  throw new Error('DATABASE_URL must be a valid PostgreSQL connection URL (e.g., postgres://user:pass@rds-host:5432/dbname?sslmode=require).');
}
if (parsedDb.hostname === 'postgres') {
  throw new Error('DATABASE_URL still points to the local Docker "postgres" host. Set it to your RDS endpoint.');
}
config.RDS_HOST ||= parsedDb.hostname;
config.RDS_PORT ||= parsedDb.port || '5432';
config.RDS_DATABASE ||= parsedDb.pathname.replace(/^\//, '');
config.RDS_USER ||= decodeURIComponent(parsedDb.username);
config.RDS_PASSWORD ||= decodeURIComponent(parsedDb.password);
config.RDS_SSLMODE ||= parsedDb.searchParams.get('sslmode') || 'require';

for (const key of ['SERVICE_AUTH_TOKEN', 'CASE_SERVICE_AUTH_TOKEN', 'WORKFLOW_AUTHORING_TOKEN', 'SCREENING_AUTHORING_TOKEN', 'SCREENING_INTAKE_TOKEN', 'SCREENING_CALLBACK_TOKEN', 'SCREENING_OPERATOR_TOKEN']) {
  config[key] ||= randomBytes(32).toString('hex');
}

const profiles = new Set((config.COMPOSE_PROFILES ?? '').split(',').filter(Boolean));
profiles.add('local-idp');
profiles.add('screening-worker');
config.COMPOSE_PROFILES = [...profiles].join(',');
config.COMPOSE_PROJECT_NAME = project;
config.POSTGRES_GATEWAY_PORT = String(port);

const quote = value => `'${String(value).replaceAll("'", "\\'")}'`;
writeFileSync(envFile, Object.entries(config).map(([key, value]) => `${key}=${quote(value)}`).join('\n') + '\n', { mode: 0o600 });
console.log(`EC2 gateway configuration ready in ignored .local/${project} (source: ${path.relative(root, sourceEnvPath)}).`);

const composeFiles = ['-f', 'docker-compose.ec2.yml', '-f', 'docker-compose.gateway.yml'];
if (existsSync(path.join(root, 'docker-compose.gateway.ec2.yml'))) {
  composeFiles.push('-f', 'docker-compose.gateway.ec2.yml');
}

const compose = ['compose', '--env-file', envFile, '-p', project, ...composeFiles];
function docker(extra, options = {}) {
  const result = spawnSync('docker', [...compose, ...extra], { cwd: root, stdio: 'inherit', ...options });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error('Docker command failed; no data volumes were removed.');
  return result;
}

docker(['config', '--quiet']);
if (parsedArgs['--prepare-only']) process.exit(0);

if (!parsedArgs['--skip-build']) {
  docker(['build', '--parallel'], { env: { ...process.env, COMPOSE_PARALLEL_LIMIT: '2' } });
}
docker(['up', '-d', '--wait', '--wait-timeout', '300']);

const publicOrigin = config.GATEWAY_PUBLIC_ORIGIN || `http://localhost:${port}`;
// Import does not update an existing realm. Change only this client's redirect allowlist.
const identityScript = `set -eu
admin=/opt/keycloak/bin/kcadm.sh
cfg=/tmp/postgres-gateway-kcadm.config
trap 'rm -f "$cfg"' EXIT
"$admin" config credentials --config "$cfg" --server http://localhost:8080/identity --realm master --user "$KC_BOOTSTRAP_ADMIN_USERNAME" --password "$KC_BOOTSTRAP_ADMIN_PASSWORD" >/dev/null
client=$("$admin" get clients --config "$cfg" -r fraud -q clientId=fraud-auth --fields id --format csv --noquotes)
test -n "$client"
"$admin" update "clients/$client" --config "$cfg" -r fraud -s 'redirectUris=["${publicOrigin}/api/auth/callback"]'
`;
docker(['exec', '-T', 'keycloak', '/bin/bash', '-ec', identityScript]);

console.log(`Project (${project}) application: ${publicOrigin}\nAPI documentation: ${publicOrigin}/services/`);