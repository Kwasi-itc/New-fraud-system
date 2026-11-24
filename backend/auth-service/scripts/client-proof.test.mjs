import test from 'node:test';
import assert from 'node:assert/strict';
import { createPrivateKey, createPublicKey, generateKeyPairSync, randomUUID, verify } from 'node:crypto';
import { mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { createClientProof, generateClientKeyPair } from './client-proof.mjs';

const script = fileURLToPath(new URL('./client-proof.mjs', import.meta.url));
const endpoint = 'http://localhost:8088/oauth/token';
function fixture(t) {
  const root = mkdtempSync(path.join(os.tmpdir(), 'fraud-client-proof-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  return { root, dir: path.join(root, 'keys') };
}
function decode(raw) {
  const [header, payload, signature] = raw.trim().split('.');
  return { header: JSON.parse(Buffer.from(header, 'base64url')), payload: JSON.parse(Buffer.from(payload, 'base64url')), input: `${header}.${payload}`, signature: Buffer.from(signature, 'base64url') };
}

test('keygen creates a matching RSA-3072 pair without exposing or overwriting private material', t => {
  const { dir } = fixture(t);
  const result = spawnSync(process.execPath, [script, 'keygen', '--dir', dir, '--kid', 'customer-1'], { encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  assert.doesNotMatch(result.stdout, /-----BEGIN (?:RSA )?PRIVATE KEY-----/);
  const files = JSON.parse(result.stdout);
  const privatePEM = readFileSync(files.private_key_file);
  const privateKey = createPrivateKey(privatePEM);
  assert.equal(privateKey.asymmetricKeyDetails.modulusLength, 3072);
  assert.equal(createPublicKey(privateKey).export({ type: 'spki', format: 'pem' }), readFileSync(files.public_key_file, 'utf8'));
  assert.deepEqual(JSON.parse(readFileSync(files.metadata_file)), { alg: 'RS256', kid: 'customer-1' });
  if (process.platform !== 'win32') {
    assert.equal(statSync(dir).mode & 0o777, 0o700);
    assert.equal(statSync(files.private_key_file).mode & 0o777, 0o600);
  }
  const again = spawnSync(process.execPath, [script, 'keygen', '--dir', dir], { encoding: 'utf8' });
  assert.notEqual(again.status, 0);
  assert.deepEqual(readFileSync(files.private_key_file), privatePEM);
});

test('proof carries the exact client/audience, a 60-second lifetime and a fresh signed jti', t => {
  const { dir } = fixture(t);
  const files = generateClientKeyPair(dir, 'customer-2');
  const client = randomUUID();
  const before = Math.floor(Date.now() / 1000);
  const first = createClientProof(dir, client.toUpperCase(), endpoint);
  const second = createClientProof(dir, client, endpoint);
  const after = Math.floor(Date.now() / 1000);
  const proof = decode(first);
  assert.deepEqual(proof.header, { alg: 'RS256', typ: 'JWT', kid: 'customer-2' });
  assert.deepEqual(Object.keys(proof.payload).sort(), ['aud', 'exp', 'iat', 'iss', 'jti', 'sub']);
  assert.equal(proof.payload.iss, client);
  assert.equal(proof.payload.sub, client);
  assert.equal(proof.payload.aud, endpoint);
  assert.equal(proof.payload.exp - proof.payload.iat, 60);
  assert(proof.payload.iat >= before && proof.payload.iat <= after);
  assert.notEqual(proof.payload.jti, decode(second).payload.jti);
  assert(verify('RSA-SHA256', Buffer.from(proof.input), readFileSync(files.public_key_file), proof.signature));
  assert(!verify('RSA-SHA256', Buffer.from(proof.input + 'tampered'), readFileSync(files.public_key_file), proof.signature));
});

test('proof file output stays private and cannot replace the key or an existing proof', t => {
  const { root, dir } = fixture(t);
  const files = generateClientKeyPair(dir);
  const output = path.join(root, 'proof.jwt');
  const args = [script, 'proof', '--dir', dir, '--client-id', randomUUID(), '--token-url', endpoint, '--out', output];
  const result = spawnSync(process.execPath, args, { encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout, '');
  assert.equal(decode(readFileSync(output, 'utf8')).payload.aud, endpoint);
  if (process.platform !== 'win32') assert.equal(statSync(output).mode & 0o777, 0o600);
  const first = readFileSync(output);
  assert.notEqual(spawnSync(process.execPath, args, { encoding: 'utf8' }).status, 0);
  assert.deepEqual(readFileSync(output), first);
  const privatePEM = readFileSync(files.private_key_file);
  args[args.length - 1] = files.private_key_file;
  assert.notEqual(spawnSync(process.execPath, args, { encoding: 'utf8' }).status, 0);
  assert.deepEqual(readFileSync(files.private_key_file), privatePEM);
});

test('invalid claims, unsupported options and mismatched or weak keys fail closed', t => {
  const { dir } = fixture(t);
  const files = generateClientKeyPair(dir);
  for (const id of [undefined, 'customer', '00000000-0000-0000-0000-000000000000']) {
    assert.throws(() => createClientProof(dir, id, endpoint), /registered API client UUID/);
  }
  for (const url of [undefined, 'not-a-url', 'ftp://auth.test/oauth/token', 'https://user:password@auth.test/oauth/token', 'https://auth.test/oauth/token?scope=all', 'https://auth.test/oauth/token#fragment']) {
    assert.throws(() => createClientProof(dir, randomUUID(), url), /token-url/);
  }
  const result = spawnSync(process.execPath, [script, 'proof', '--dir', dir, '--client-id', randomUUID(), '--token-url', endpoint, '--ttl', '3600'], { encoding: 'utf8' });
  assert.notEqual(result.status, 0);
  assert.equal(result.stdout, '');
  const other = generateKeyPairSync('rsa', { modulusLength: 2048, publicKeyEncoding: { type: 'spki', format: 'pem' }, privateKeyEncoding: { type: 'pkcs8', format: 'pem' } });
  writeFileSync(files.public_key_file, other.publicKey);
  assert.throws(() => createClientProof(dir, randomUUID(), endpoint), /does not match/);
  const weak = generateKeyPairSync('rsa', { modulusLength: 1024, privateKeyEncoding: { type: 'pkcs8', format: 'pem' }, publicKeyEncoding: { type: 'spki', format: 'pem' } });
  writeFileSync(files.private_key_file, weak.privateKey);
  assert.throws(() => createClientProof(dir, randomUUID(), endpoint), /at least 2048 bits/);
});
