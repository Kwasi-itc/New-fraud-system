import { randomBytes, generateKeyPairSync, createPublicKey } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, writeFileSync, chmodSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const project = 'fraud-ec2';
export const defaultVolume = 'fraud-ec2_postgres_data';
export const identityFiles = ['auth.env', 'keycloak.env', 'database.env', 'platform.env', 'proxy.env', 'data-model.env', 'invitations.env', 'platform-private.pem', 'platform-public.json'];
const secret = () => randomBytes(32).toString('hex');
const integrationKeys = ['SERVICE_AUTH_TOKEN', 'CASE_SERVICE_AUTH_TOKEN', 'WORKFLOW_AUTHORING_TOKEN', 'SCREENING_AUTHORING_TOKEN', 'SCREENING_INTAKE_TOKEN', 'SCREENING_CALLBACK_TOKEN', 'SCREENING_OPERATOR_TOKEN'];
const read = file => readFileSync(file, 'utf8');

export function readEnv(file) {
  if (!existsSync(file)) return {};
  const result = {};
  for (const line of read(file).split(/\r?\n/)) {
    if (!line.trim() || line.startsWith('#')) continue;
    const match = line.match(/^([A-Z][A-Z0-9_]*)=(.*)$/);
    if (!match || Object.hasOwn(result, match[1])) throw new Error('Invalid or duplicate deployment configuration key.');
    let value = match[2];
    if (value.startsWith("'") && value.endsWith("'")) value = value.slice(1, -1).replaceAll("\\'", "'");
    result[match[1]] = value;
  }
  return result;
}

export function serializeEnv(values) {
  return Object.entries(values).map(([key, value]) => {
    if (/[\r\n\0]/.test(value)) throw new Error('Configuration values must be single-line.');
    return `${key}='${value.replaceAll("'", "\\'")}'`;
  }).join('\n') + '\n';
}

export function validateDomain(domain) {
  if (typeof domain !== 'string' || domain.length > 253 || domain !== domain.toLowerCase() || !domain.includes('.') || /^\d+\.\d+\.\d+\.\d+$/.test(domain) || domain.endsWith('.localhost') || !domain.split('.').every(label => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label))) {
    throw new Error('Use a lowercase DNS hostname without scheme, port, path or IP address.');
  }
  return domain;
}

export function tenantList(value) {
  const ids = [...new Set((value ?? '').split(',').map(id => id.trim().toLowerCase()))];
  if (!ids.length || ids.some(id => !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(id) || id === '00000000-0000-0000-0000-000000000000')) throw new Error('Supply existing case integration tenant UUIDs with --case-tenants.');
  return ids.sort().join(',');
}

export function renderGateway(base, domain) {
  validateDomain(domain);
  for (const marker of ['proxy_set_header Host $http_host;', 'proxy_set_header X-Forwarded-Proto $scheme;', 'proxy_set_header X-Forwarded-Port $forwarded_port;']) {
    if (!base.includes(marker)) throw new Error('Gateway template changed; review HTTPS forwarding before deployment.');
  }
  return base
    .replace('proxy_set_header Connection "";', 'proxy_set_header Connection "";\n        proxy_set_header Forwarded "";')
    .replaceAll('proxy_set_header Host $http_host;', `proxy_set_header Host ${domain};`)
    .replaceAll('proxy_set_header X-Forwarded-Host $http_host;', `proxy_set_header X-Forwarded-Host ${domain};`)
    .replaceAll('proxy_set_header X-Forwarded-Proto $scheme;', 'proxy_set_header X-Forwarded-Proto https;')
    .replaceAll('proxy_set_header X-Forwarded-Port $forwarded_port;', 'proxy_set_header X-Forwarded-Port 443;')
    // Stock nginx diagnostic errors can include entire OAuth callback URLs.
    // Sanitized access logs still record upstream status and duration.
    .replace('error_log /dev/stderr warn;', 'error_log /dev/stderr crit;');
}

function validateExisting(root, values) {
  const dir = path.join(root, '.local/auth-provider');
  const auth = readEnv(path.join(dir, 'auth.env'));
  const platform = readEnv(path.join(dir, 'platform.env'));
  const provider = readEnv(path.join(dir, 'keycloak.env'));
  const db = readEnv(path.join(dir, 'database.env'));
  const proxy = readEnv(path.join(dir, 'proxy.env'));
  const tokens = integrationKeys.map(key => values[key]);
  if (tokens.some(value => typeof value !== 'string' || value.length < 32) || new Set(tokens).size !== tokens.length) throw new Error('Integration credentials are missing or overlap; restore matching deployment configuration.');
  if (auth.AUTH_ENV !== 'production' || auth.OIDC_ISSUER_URL !== `${values.EC2_PUBLIC_ORIGIN}/identity/realms/fraud` || auth.OIDC_REDIRECT_URL !== `${values.EC2_PUBLIC_ORIGIN}/api/auth/callback`) throw new Error('Existing identity material belongs to another origin/environment. Use a separate checkout or an explicit identity migration.');
  if (!auth.OIDC_CLIENT_SECRET || auth.OIDC_CLIENT_SECRET !== provider.FRAUD_OIDC_CLIENT_SECRET || db.KC_DB_PASSWORD !== provider.KC_DB_PASSWORD || platform.AUTH_PROXY_TOKEN !== proxy.AUTH_PROXY_TOKEN) throw new Error('Existing identity credentials disagree; restore matching configuration.');
  const publicKeys = JSON.parse(read(path.join(dir, 'platform-public.json')));
  const actual = createPublicKey(read(path.join(dir, 'platform-private.pem'))).export({ type: 'spki', format: 'pem' });
  if (publicKeys[platform.PLATFORM_JWT_KEY_ID] !== actual) throw new Error('Platform signing keys do not match.');
  if (read(path.join(root, '.local/ec2/postgres-password.txt')).trim() !== values.EC2_POSTGRES_PASSWORD) throw new Error('PostgreSQL credentials disagree; no password will be regenerated.');
  const connection = new URL(values.EC2_DATABASE_URL);
  if (connection.protocol !== 'postgres:' || connection.hostname !== 'postgres' || connection.pathname !== '/fraud' || decodeURIComponent(connection.password) !== values.EC2_POSTGRES_PASSWORD) throw new Error('Expected the shared PostgreSQL deployment connection.');
}

export function checkConfiguration(root) {
  const values = readEnv(path.join(root, '.local/ec2/compose.env'));
  validateDomain(values.EC2_DOMAIN);
  if (values.EC2_PUBLIC_ORIGIN !== `https://${values.EC2_DOMAIN}` || values.COMPOSE_PROJECT_NAME !== project || values.FRAUD_INTERNAL_NETWORK !== `${project}_internal`) throw new Error('Deployment origin/project/network is inconsistent.');
  tenantList(values.CASE_SERVICE_TENANT_IDS);
  const dir = path.join(root, '.local/auth-provider');
  if (identityFiles.some(file => !existsSync(path.join(dir, file)))) throw new Error('Incomplete identity material; restore the original matching files.');
  validateExisting(root, values);
  return values;
}

export function configure({ root, domain, email, caseTenants, volume } = {}) {
  const dir = path.join(root, '.local/auth-provider');
  const state = path.join(root, '.local/ec2');
  const previous = readEnv(path.join(state, 'compose.env'));
  domain = validateDomain(domain ?? previous.EC2_DOMAIN);
  email ??= previous.EC2_ACME_EMAIL;
  if (typeof email !== 'string' || !/^[a-zA-Z0-9._+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/.test(email)) throw new Error('Supply a certificate contact email with --email.');
  const tenants = tenantList(caseTenants ?? previous.CASE_SERVICE_TENANT_IDS);
  volume ??= previous.EC2_POSTGRES_VOLUME ?? defaultVolume;
  if (!/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,99}$/.test(volume)) throw new Error('Invalid PostgreSQL volume name.');
  const present = identityFiles.filter(file => existsSync(path.join(dir, file))).length;
  if ((present && present !== identityFiles.length) || (present > 0) !== (Object.keys(previous).length > 0)) throw new Error('Incomplete deployment state; restore identity files and deployment configuration together.');
  if (present && (previous.EC2_DOMAIN !== domain || previous.EC2_POSTGRES_VOLUME !== volume)) throw new Error('Origin or data-volume change requires an explicit migration; configuration was not changed.');
  if (present) checkConfiguration(root);

  const values = {
    ...previous,
    EC2_DOMAIN: domain, EC2_PUBLIC_ORIGIN: `https://${domain}`, EC2_ACME_EMAIL: email,
    EC2_POSTGRES_VOLUME: volume, COMPOSE_PROJECT_NAME: project, FRAUD_INTERNAL_NETWORK: `${project}_internal`,
    COMPOSE_PROFILES: 'local-idp,screening-worker', CASE_SERVICE_TENANT_IDS: tenants,
    CASE_USER_JWT_KEYS_FILE: './.local/auth-provider/platform-public.json', CASE_USER_JWT_ISSUER: 'urn:fraud:identity',
    CASE_USER_JWT_AUDIENCE: 'case-manager', CASE_SESSION_COOKIE: '__Host-fraud_session',
    CASE_DB_POOL_MAX_CONNS: '4', DECISION_DATABASE_MAX_CONNS: '4', DECISION_WORKER_DATABASE_MAX_CONNS: '4',
    INGESTION_DATABASE_MAX_CONNS: '4', INGESTION_WORKER_DATABASE_MAX_CONNS: '4',
  };
  for (const key of ['EC2_POSTGRES_PASSWORD', ...integrationKeys]) values[key] ||= secret();
  values.EC2_DATABASE_URL ||= `postgres://fraud:${values.EC2_POSTGRES_PASSWORD}@postgres:5432/fraud?sslmode=disable&pool_max_conns=4&pool_min_conns=0`;
  const generatedGateway = renderGateway(read(path.join(root, 'docker/gateway/nginx.conf')), domain);
  mkdirSync(state, { recursive: true, mode: 0o700 });
  chmodSync(state, 0o700);
  if (!present) {
    mkdirSync(dir, { recursive: true, mode: 0o700 });
    chmodSync(dir, 0o700);
    const client = secret(), providerPassword = secret(), proxy = secret();
    const keyID = `platform-${randomBytes(8).toString('hex')}`;
    const keys = generateKeyPairSync('rsa', { modulusLength: 3072, publicKeyEncoding: { type: 'spki', format: 'pem' }, privateKeyEncoding: { type: 'pkcs8', format: 'pem' } });
    const files = {
      'auth.env': serializeEnv({ AUTH_ENV: 'production', OIDC_ISSUER_URL: `${values.EC2_PUBLIC_ORIGIN}/identity/realms/fraud`, OIDC_CLIENT_ID: 'fraud-auth', OIDC_CLIENT_SECRET: client, OIDC_REDIRECT_URL: `${values.EC2_PUBLIC_ORIGIN}/api/auth/callback` }),
      'keycloak.env': serializeEnv({ KC_BOOTSTRAP_ADMIN_USERNAME: 'ec2-admin', KC_BOOTSTRAP_ADMIN_PASSWORD: secret(), KC_DB_PASSWORD: providerPassword, FRAUD_OIDC_CLIENT_SECRET: client, FRAUD_AUTH_REDIRECT_URI: `${values.EC2_PUBLIC_ORIGIN}/api/auth/callback` }),
      'database.env': serializeEnv({ KC_DB_PASSWORD: providerPassword }),
      'platform.env': serializeEnv({ AUTH_PROXY_TOKEN: proxy, PLATFORM_JWT_KEY_ID: keyID, PLATFORM_JWT_ISSUER: 'urn:fraud:identity', PLATFORM_JWT_AUDIENCE: 'case-manager', PLATFORM_JWT_PRIVATE_KEY_FILE: '/run/secrets/platform-private.pem' }),
      'proxy.env': serializeEnv({ AUTH_PROXY_TOKEN: proxy }),
      'data-model.env': serializeEnv({ DATA_MODEL_SERVICE_TOKEN: secret() }),
      'invitations.env': serializeEnv({ AUTH_INVITATIONS_ENABLED: 'false', AUTH_INVITATION_KEY: randomBytes(32).toString('base64'), AUTH_APP_ORIGIN: values.EC2_PUBLIC_ORIGIN }),
      'platform-private.pem': keys.privateKey,
      'platform-public.json': JSON.stringify({ [keyID]: keys.publicKey }, null, 2) + '\n',
    };
    for (const [file, content] of Object.entries(files)) writeFileSync(path.join(dir, file), content, { flag: 'wx', mode: 0o600 });
    writeFileSync(path.join(state, 'postgres-password.txt'), values.EC2_POSTGRES_PASSWORD + '\n', { flag: 'wx', mode: 0o600 });
  }
  writeFileSync(path.join(state, 'compose.env'), serializeEnv(values), { mode: 0o600 });
  // This domain-only file must be readable by nginx's unprivileged container UID.
  writeFileSync(path.join(state, 'nginx.conf'), generatedGateway, { mode: 0o644 });
  chmodSync(path.join(state, 'nginx.conf'), 0o644);
  chmodSync(path.join(state, 'compose.env'), 0o600);
  return checkConfiguration(root);
}

const root = fileURLToPath(new URL('../../', import.meta.url));
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const options = {};
    for (let i = 2; i < process.argv.length; i++) {
      const key = process.argv[i];
      if (key === '--check') options.check = true;
      else if (['--domain', '--email', '--case-tenants', '--volume'].includes(key) && process.argv[i + 1] && !process.argv[i + 1].startsWith('--')) options[key.slice(2)] = process.argv[++i];
      else throw new Error('Usage: configure [--domain DNS_NAME --email EMAIL --case-tenants UUIDS] [--volume VOLUME] or --check');
    }
    const values = options.check ? checkConfiguration(root) : configure({ root, domain: options.domain, email: options.email, caseTenants: options['case-tenants'], volume: options.volume });
    console.log(`EC2 configuration ready for ${values.EC2_PUBLIC_ORIGIN}. Private files remain under .local/.`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
