import test from 'node:test';
import assert from 'node:assert/strict';
import { chmodSync, chownSync, copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { applyContainerKeyPermissions, provisionPlatformKeys, requireContainerKeyGroup, validatePlatformKeys } from './platform-keys.mjs';
test('per-service identities preserve private keys and deny service-to-human trust', () => {
  const dir = mkdtempSync(path.join(os.tmpdir(), 'fraud-platform-keys-'));
  const first = provisionPlatformKeys(dir, { tokenEndpointURL: 'https://fraud.test/oauth/token' });
  const before = readFileSync(path.join(dir, 'screening-case-delivery-worker-private.pem'), 'utf8');
  const second = provisionPlatformKeys(dir, { tokenEndpointURL: 'https://fraud.test/oauth/token' });
  assert.equal(before, readFileSync(path.join(dir, 'screening-case-delivery-worker-private.pem'), 'utf8'));
  assert.equal(statSync(path.join(dir, 'screening-case-delivery-worker-private.pem')).mode & 0o777, 0o600);
  assert.deepEqual(first.keys, second.keys);
  const platform = first.keys.find(key => key.principal_types.includes('client'));
  assert.deepEqual(platform.audiences, ['urn:fraud:platform-api', 'urn:fraud:system']);
  assert.doesNotMatch(readFileSync(path.join(dir, 'platform.env'), 'utf8'), /^AUTH_CLIENT_AUDIENCES=/m);
  const serviceKeys = first.keys.filter(key => key.principal_types.includes('service'));
  assert.equal(new Set(serviceKeys.map(key => key.public_key_pem)).size, serviceKeys.length);
  assert.ok(serviceKeys.every(key => key.principal_types.length === 1));
  assert.ok(serviceKeys.every(key => !key.audiences.includes('urn:fraud:system')));
  const delivery = serviceKeys.find(key => key.service_id === 'screening-case-delivery-worker');
  assert.deepEqual(delivery.capabilities, ['cases:events-write']);
  const ringFile=path.join(dir,'public-keys.json');
  const historical={...serviceKeys[0],kid:'previous-rotation-key'};
  writeFileSync(ringFile,JSON.stringify({keys:[...second.keys,historical]}));
  const rotated=provisionPlatformKeys(dir,{tokenEndpointURL:'https://fraud.test/oauth/token'});
  assert.ok(rotated.keys.some(key=>key.kid===historical.kid));
  validatePlatformKeys(dir);
  applyContainerKeyPermissions(dir, rotated.identities, process.getgid());
  assert.equal(statSync(ringFile).mode & 0o777, 0o644);
  for (const name of ['platform-private.pem', ...Object.keys(rotated.identities).map(id => `${id}-private.pem`)]) {
    const stat = statSync(path.join(dir, name));
    assert.equal(stat.mode & 0o777, 0o640);
    assert.equal(stat.gid, process.getgid());
  }
  const again = provisionPlatformKeys(dir, { tokenEndpointURL: 'https://fraud.test/oauth/token' });
  applyContainerKeyPermissions(dir, again.identities, process.getgid());
  assert.equal(statSync(ringFile).mode & 0o777, 0o644);
  writeFileSync(path.join(dir,'screening-service-private.pem'),readFileSync(path.join(dir,'ingestion-service-private.pem')));
  assert.throws(()=>validatePlatformKeys(dir), /do not match/);
  assert.throws(()=>provisionPlatformKeys(dir,{tokenEndpointURL:'https://fraud.test/oauth/token'}), /without a new key ID/);
});

test('key permissions reject a setup user outside the container group', { skip: process.getuid?.() === 0 }, () => {
  const missingGroup = Math.max(...process.getgroups(), process.getgid()) + 1;
  assert.throws(() => requireContainerKeyGroup(missingGroup), /must belong to host group/);
});

test('container UID can read keys owned by a different setup user', { skip: process.getuid?.() !== 0 }, (t) => {
  const dir = mkdtempSync(path.join(os.tmpdir(), 'fraud-ec2-key-permissions-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  chmodSync(dir, 0o755);
  for (const name of ['platform-private.pem', 'case-manager-service-private.pem', 'public-keys.json']) {
    const file = path.join(dir, name);
    writeFileSync(file, 'test', { mode: 0o600 });
    chownSync(file, 1000, 1000);
  }
  const modulePath = fileURLToPath(new URL('./platform-keys.mjs', import.meta.url));
  const setup = spawnSync(process.execPath, ['--input-type=module', '-e',
    `import { applyContainerKeyPermissions } from ${JSON.stringify(modulePath)}; applyContainerKeyPermissions(process.argv[1], {'case-manager-service': {}});`,
    dir], { uid: 1000, gid: 65532, encoding: 'utf8' });
  assert.equal(setup.status, 0, setup.stderr);
  const check = spawnSync(process.execPath, ['-e',
    `const fs = require('node:fs'); for (const name of ['platform-private.pem', 'case-manager-service-private.pem', 'public-keys.json']) fs.readFileSync(require('node:path').join(process.argv[1], name));`,
    dir], { uid: 65532, gid: 65532, encoding: 'utf8' });
  assert.equal(check.status, 0, check.stderr);
});

test('EC2 setup reapplies key permissions and recreates the stack', {
  skip: process.getuid?.() !== 0 && !process.getgroups?.().includes(65532),
}, (t) => {
  const root = mkdtempSync(path.join(os.tmpdir(), 'fraud-ec2-setup-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const repo = fileURLToPath(new URL('../../../', import.meta.url));
  for (const file of [
    'docker/setup-postgres-ec2.mjs',
    'backend/auth-service/scripts/platform-keys.mjs',
    'backend/platform-auth/services.json',
    'backend/platform-auth/capabilities.json',
  ]) {
    mkdirSync(path.dirname(path.join(root, file)), { recursive: true });
    copyFileSync(path.join(repo, file), path.join(root, file));
  }
  const identityDir = path.join(root, '.local', 'auth-provider');
  provisionPlatformKeys(identityDir, { tokenEndpointURL: 'https://example.test/oauth/token' });
  for (const name of ['auth.env', 'keycloak.env', 'database.env']) {
    writeFileSync(path.join(identityDir, name), '', { mode: 0o600 });
  }
  writeFileSync(path.join(root, '.env'), 'DATABASE_URL=postgres://test:test@rds.example.test/fraud\nNEXT_PUBLIC_DATA_MODEL_TENANT_ID=11111111-1111-4111-8111-111111111111\n', { mode: 0o600 });
  const bin = path.join(root, 'bin');
  mkdirSync(bin);
  writeFileSync(path.join(bin, 'docker'), `#!/bin/sh
printf '%s\\n' "$*" >> "$DOCKER_CALLS"
case "$*" in
  *" ps -a -q "*) echo fake-container ;;
  *"inspect --format "*) echo exited:0 ;;
esac
exit 0
`, { mode: 0o755 });
  const calls = path.join(root, 'docker-calls');
  const env = { ...process.env, PATH: `${bin}${path.delimiter}${process.env.PATH}`, DOCKER_CALLS: calls };
  const target = path.join(root, '.local', 'new-fraud-system');
  for (let i = 0; i < 2; i++) {
    const result = spawnSync(process.execPath, [path.join(root, 'docker/setup-postgres-ec2.mjs'), '--prepare-only'], { cwd: root, env, encoding: 'utf8' });
    assert.equal(result.status, 0, result.stderr);
    assert.equal(statSync(path.join(target, 'public-keys.json')).mode & 0o777, 0o644);
    const privateKey = statSync(path.join(target, 'case-manager-service-private.pem'));
    assert.equal(privateKey.mode & 0o777, 0o640);
    assert.equal(privateKey.gid, 65532);
    assert.match(result.stdout, /containers must be recreated/);
  }
  const result = spawnSync(process.execPath, [path.join(root, 'docker/setup-postgres-ec2.mjs'), '--skip-build'], { cwd: root, env, encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  assert.match(readFileSync(calls, 'utf8'), /up -d --wait --wait-timeout 300 --force-recreate/);
});
