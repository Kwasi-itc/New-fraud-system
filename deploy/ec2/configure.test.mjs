import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync, copyFileSync, rmSync, existsSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { configure, checkConfiguration, readEnv, serializeEnv, identityFiles, renderGateway, validateDomain } from './configure.mjs';

const repository = fileURLToPath(new URL('../../', import.meta.url));
const temporary = path.join(repository, '.local/ec2-tests/unit');
const tenant = '11111111-1111-4111-8111-111111111111';
const secondTenant = '22222222-2222-4222-8222-222222222222';
const options = { domain: 'fraud.example.test', email: 'ops@example.test', caseTenants: tenant };

function fixture(t) {
  mkdirSync(temporary, { recursive: true });
  const root = mkdtempSync(path.join(temporary, 'case-'));
  t.after(() => {
    if (!path.resolve(root).startsWith(path.resolve(temporary) + path.sep)) throw new Error('Invalid fixture cleanup path.');
    rmSync(root, { recursive: true, force: true });
  });
  for (const file of ['docker/gateway/nginx.conf', 'docker/gateway/service.yaml', 'docker-compose.yml', 'docker-compose.gateway.yml', 'deploy/ec2/compose.yml']) {
    mkdirSync(path.dirname(path.join(root, file)), { recursive: true });
    copyFileSync(path.join(repository, file), path.join(root, file));
  }
  return root;
}

test('untrusted origin input cannot become nginx, Caddy or Compose syntax', () => {
  for (const domain of ['localhost', 'fraud.localhost', '127.0.0.1', 'https://fraud.example.test', 'fraud.example.test:443', 'fraud.example.test/path', 'fraud.example.test\nserver {}', '${TOKEN}.example.test', '-bad.example.test', 'FRAUD.example.test']) assert.throws(() => validateDomain(domain));
  assert.equal(validateDomain(options.domain), options.domain);
});

test('EC2 instance template requires IMDSv2, encrypted retained storage and narrow public ports', () => {
  const model = JSON.parse(readFileSync(path.join(repository, 'deploy/ec2/instance.json'), 'utf8'));
  const instance = model.Resources.Instance.Properties;
  assert.equal(instance.MetadataOptions.HttpTokens, 'required');
  assert.equal(instance.MetadataOptions.HttpPutResponseHopLimit, 1);
  assert.equal(instance.BlockDeviceMappings[0].Ebs.Encrypted, true);
  assert.equal(instance.BlockDeviceMappings[0].Ebs.DeleteOnTermination, false);
  const rules = model.Resources.SecurityGroup.Properties.SecurityGroupIngress;
  assert.deepEqual(rules.map(rule => rule.FromPort).sort((a, b) => a - b), [22, 80, 443]);
  assert.deepEqual(rules.find(rule => rule.FromPort === 22).CidrIp, { Ref: 'AdminCidr' });
  assert.ok(model.Rules.RestrictSSH);
  const cidr = new RegExp(`^${model.Parameters.AdminCidr.AllowedPattern}$`);
  assert.equal(cidr.test('203.0.113.10/32'), true);
  assert.equal(cidr.test('203.0.113.10/0'), false);
});

test('first setup creates matching production credentials without changing root .env', t => {
  const root = fixture(t);
  writeFileSync(path.join(root, '.env'), 'PRESERVE_EXISTING_ENV=true\n');
  const result = configure({ root, ...options });
  assert.equal(result.EC2_PUBLIC_ORIGIN, 'https://fraud.example.test');
  assert.equal(readFileSync(path.join(root, '.env'), 'utf8'), 'PRESERVE_EXISTING_ENV=true\n');
  assert.equal(checkConfiguration(root).CASE_SERVICE_TENANT_IDS, tenant);
  const auth = readEnv(path.join(root, '.local/auth-provider/auth.env'));
  const provider = readEnv(path.join(root, '.local/auth-provider/keycloak.env'));
  assert.equal(auth.AUTH_ENV, 'production');
  assert.equal(auth.OIDC_CLIENT_SECRET, provider.FRAUD_OIDC_CLIENT_SECRET);
  assert.equal(auth.OIDC_ISSUER_URL, 'https://fraud.example.test/identity/realms/fraud');
  assert.equal(new URL(result.EC2_DATABASE_URL).searchParams.get('pool_max_conns'), '4');
  assert.equal(result.CASE_SESSION_COOKIE, '__Host-fraud_session');
  assert.equal(readFileSync(path.join(root, '.local/ec2/postgres-password.txt'), 'utf8').trim(), result.EC2_POSTGRES_PASSWORD);
  if (process.platform !== 'win32') {
    assert.equal(statSync(path.join(root, '.local/ec2/compose.env')).mode & 0o777, 0o600);
    assert.equal(statSync(path.join(root, '.local/auth-provider/platform-private.pem')).mode & 0o777, 0o600);
    assert.equal(statSync(path.join(root, '.local/ec2/nginx.conf')).mode & 0o777, 0o644);
  }
});

test('retry and tenant allowlist update preserve every credential and key', t => {
  const root = fixture(t);
  const before = configure({ root, ...options });
  const snapshot = Object.fromEntries(identityFiles.map(file => [file, readFileSync(path.join(root, '.local/auth-provider', file), 'utf8')]));
  const after = configure({ root, caseTenants: `${secondTenant},${tenant},${tenant}` });
  assert.equal(after.CASE_SERVICE_TENANT_IDS, `${tenant},${secondTenant}`);
  for (const key of Object.keys(before).filter(key => /TOKEN|PASSWORD|DATABASE_URL/.test(key))) assert.equal(after[key], before[key]);
  for (const file of identityFiles) assert.equal(readFileSync(path.join(root, '.local/auth-provider', file), 'utf8'), snapshot[file]);
});

test('origin or volume changes require explicit migration and leave state intact', t => {
  const root = fixture(t);
  configure({ root, ...options });
  const file = path.join(root, '.local/ec2/compose.env');
  const before = readFileSync(file, 'utf8');
  assert.throws(() => configure({ root, domain: 'other.example.test' }), /explicit migration/);
  assert.throws(() => configure({ root, volume: 'another-volume' }), /explicit migration/);
  assert.equal(readFileSync(file, 'utf8'), before);
});

test('partial identity configuration is never silently regenerated', t => {
  const root = fixture(t);
  mkdirSync(path.join(root, '.local/auth-provider'), { recursive: true });
  const file = path.join(root, '.local/auth-provider/auth.env');
  writeFileSync(file, 'AUTH_ENV=development\n');
  assert.throws(() => configure({ root, ...options }), /Incomplete deployment state/);
  assert.equal(readFileSync(file, 'utf8'), 'AUTH_ENV=development\n');
  assert.equal(existsSync(path.join(root, '.local/ec2/compose.env')), false);
});

test('missing integration credentials fail rather than rotating active secrets', t => {
  const root = fixture(t);
  configure({ root, ...options });
  const file = path.join(root, '.local/ec2/compose.env');
  const values = readEnv(file);
  delete values.SCREENING_OPERATOR_TOKEN;
  writeFileSync(file, serializeEnv(values));
  assert.throws(() => configure({ root }), /credentials are missing/);
  assert.equal(readEnv(file).SCREENING_OPERATOR_TOKEN, undefined);
});

test('HTTPS forwarding is fixed to configured origin rather than client-supplied proxy headers', () => {
  const base = readFileSync(path.join(repository, 'docker/gateway/nginx.conf'), 'utf8');
  const result = renderGateway(base, options.domain);
  assert.ok(result.includes('proxy_set_header Host fraud.example.test;'));
  assert.ok(result.includes('proxy_set_header X-Forwarded-Proto https;'));
  assert.ok(result.includes('proxy_set_header X-Forwarded-Port 443;'));
  assert.ok(!result.includes('proxy_set_header Host $http_host;'));
  assert.ok(!result.includes('proxy_set_header X-Forwarded-Proto $scheme;'));
  assert.ok(result.includes('error_log /dev/stderr crit;'));
  assert.throws(() => renderGateway(base.replaceAll('proxy_set_header Host $http_host;', ''), options.domain), /template changed/);
});

test('merged EC2 Compose hides database/APIs, keeps PostgreSQL ownership and has no dependency cycle', { skip: process.env.EC2_COMPOSE_TESTS !== 'true' }, t => {
  const root = fixture(t);
  const values = configure({ root, ...options });
  const result = spawnSync('docker', ['compose', '--project-directory', root, '--env-file', path.join(root, '.local/ec2/compose.env'), '-p', 'fraud-ec2', '-f', path.join(root, 'docker-compose.yml'), '-f', path.join(root, 'docker-compose.gateway.yml'), '-f', path.join(root, 'deploy/ec2/compose.yml'), 'config', '--format', 'json'], { encoding: 'utf8', maxBuffer: 5 * 1024 * 1024 });
  assert.equal(result.status, 0, 'EC2 Compose rendering failed');
  const model = JSON.parse(result.stdout);
  for (const [name, service] of Object.entries(model.services)) if (name !== 'tls-edge') assert.equal(service.ports?.length ?? 0, 0, `${name} publishes a private port`);
  assert.deepEqual(model.services['tls-edge'].ports.map(port => port.published).sort(), ['443', '80']);
  assert.equal(model.volumes.postgres_data.name, 'fraud-ec2_postgres_data');
  assert.equal(model.services.postgres.environment.POSTGRES_PASSWORD, undefined);
  assert.equal(model.services.postgres.environment.POSTGRES_PASSWORD_FILE, '/run/secrets/postgres-password');
  for (const service of Object.values(model.services)) if (service.environment?.DATABASE_URL) assert.equal(service.environment.DATABASE_URL, values.EC2_DATABASE_URL);
  const auth = model.services['auth-service'].environment;
  assert.equal(auth.AUTH_ENV, 'production');
  assert.equal(auth.OIDC_BACKCHANNEL_ORIGIN, '');
  assert.equal(auth.OIDC_ISSUER_URL, 'https://fraud.example.test/identity/realms/fraud');
  assert.equal(model.services.frontend.environment.APP_ORIGIN, values.EC2_PUBLIC_ORIGIN);
  assert.deepEqual(model.services.keycloak.command, ['start', '--import-realm']);
  assert.equal(model.services.keycloak.environment.KC_HOSTNAME, `${values.EC2_PUBLIC_ORIGIN}/identity`);
  assert.equal(model.services['case-manager-service'].environment.SERVICE_AUTH_TOKEN, values.CASE_SERVICE_AUTH_TOKEN);
  assert.equal(model.services['tls-edge'].networks.default.aliases[0], options.domain);
  const visiting = new Set(), visited = new Set();
  function visit(name) {
    assert.ok(!visiting.has(name), 'EC2 startup has a dependency cycle');
    if (visited.has(name)) return;
    visiting.add(name);
    for (const next of Object.keys(model.services[name].depends_on ?? {})) visit(next);
    visiting.delete(name); visited.add(name);
  }
  for (const name of Object.keys(model.services)) visit(name);
});
