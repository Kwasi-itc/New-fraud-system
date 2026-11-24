#!/usr/bin/env node
// Customer-owned client keys and private_key_jwt assertions. No dependencies or network calls.
import { createPrivateKey, createPublicKey, generateKeyPairSync, randomUUID, sign } from 'node:crypto';
import { mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { parseArgs } from 'node:util';

const defaultDirectory = '.local/client-auth';
const help = `Usage:
  node backend/auth-service/scripts/client-proof.mjs keygen [--dir DIRECTORY] [--kid KEY_ID]
  node backend/auth-service/scripts/client-proof.mjs proof [--dir DIRECTORY] --client-id UUID --token-url URL [--out FILE]

keygen creates a new RSA-3072 key pair and key.json in DIRECTORY (default: ${defaultDirectory}).
Register public.pem and the key ID with your API client; keep private.pem private.
An existing key directory is never overwritten.

proof signs a fresh RS256 assertion, valid for 60 seconds, using that key pair.
Use the registered client UUID and the exact public /oauth/token URL.
The JWT is written to stdout, or to a new mode-0600 file with --out.
Generate a fresh proof for each token exchange; proofs are single-use.
`;

function keyID(value) {
  if (typeof value !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$/.test(value)) {
    throw new Error('Key ID must be 1–100 letters, digits, dots, underscores or hyphens, starting with a letter or digit.');
  }
  return value;
}

export function generateClientKeyPair(directory, kid = `client-${randomUUID()}`) {
  kid = keyID(kid);
  const dir = path.resolve(directory);
  const files = {
    kid,
    public_key_file: path.join(dir, 'public.pem'),
    private_key_file: path.join(dir, 'private.pem'),
    metadata_file: path.join(dir, 'key.json'),
  };
  mkdirSync(path.dirname(dir), { recursive: true, mode: 0o700 });
  // mkdir without recursive is exclusive, including when the directory is empty.
  mkdirSync(dir, { mode: 0o700 });
  try {
    const pair = generateKeyPairSync('rsa', {
      modulusLength: 3072,
      publicKeyEncoding: { type: 'spki', format: 'pem' },
      privateKeyEncoding: { type: 'pkcs8', format: 'pem' },
    });
    writeFileSync(files.private_key_file, pair.privateKey, { mode: 0o600, flag: 'wx' });
    writeFileSync(files.public_key_file, pair.publicKey, { mode: 0o644, flag: 'wx' });
    writeFileSync(files.metadata_file, JSON.stringify({ alg: 'RS256', kid }, null, 2) + '\n', { mode: 0o600, flag: 'wx' });
    return files;
  } catch (error) {
    // This directory was exclusively created by this invocation.
    rmSync(dir, { recursive: true, force: true });
    throw error;
  }
}

export function createClientProof(directory, clientID, tokenURL) {
  if (typeof clientID !== 'string' || !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(clientID) || clientID === '00000000-0000-0000-0000-000000000000') {
    throw new Error('--client-id must be the registered API client UUID.');
  }
  let endpoint;
  try { endpoint = new URL(tokenURL); } catch { throw new Error('--token-url must be the exact public token endpoint URL.'); }
  if (!/^https?:\/\//i.test(tokenURL) || tokenURL !== tokenURL.trim() || !['http:', 'https:'].includes(endpoint.protocol) || endpoint.username || endpoint.password || endpoint.search || endpoint.hash) {
    throw new Error('--token-url requires an absolute HTTP(S) URL without credentials, query or fragment.');
  }

  const dir = path.resolve(directory);
  const metadata = JSON.parse(readFileSync(path.join(dir, 'key.json'), 'utf8'));
  if (!metadata || metadata.alg !== 'RS256') throw new Error('Unsupported key metadata; expected RS256.');
  const kid = keyID(metadata.kid);
  const key = createPrivateKey(readFileSync(path.join(dir, 'private.pem')));
  if (key.asymmetricKeyType !== 'rsa' || key.asymmetricKeyDetails.modulusLength < 2048) {
    throw new Error('The private key must be an RSA key of at least 2048 bits.');
  }
  const publicKey = createPublicKey(readFileSync(path.join(dir, 'public.pem')));
  const encoding = { type: 'spki', format: 'der' };
  if (!createPublicKey(key).export(encoding).equals(publicKey.export(encoding))) {
    throw new Error('public.pem does not match private.pem.');
  }

  const now = Math.floor(Date.now() / 1000);
  const client = clientID.toLowerCase();
  const encode = value => Buffer.from(JSON.stringify(value)).toString('base64url');
  const header = encode({ alg: 'RS256', typ: 'JWT', kid });
  // Preserve the supplied audience exactly: the server compares it to its configured URL.
  const payload = encode({ iss: client, sub: client, aud: tokenURL, jti: randomUUID(), iat: now, exp: now + 60 });
  const input = `${header}.${payload}`;
  return `${input}.${sign('RSA-SHA256', Buffer.from(input), key).toString('base64url')}`;
}

export function main(args) {
  const [command, ...rest] = args;
  if (!command || command === '--help' || command === '-h') { console.log(help); return; }
  if (!['keygen', 'proof'].includes(command)) throw new Error('Choose keygen or proof; use --help for examples.');
  const options = { dir: { type: 'string', default: defaultDirectory }, help: { type: 'boolean', short: 'h' } };
  if (command === 'keygen') options.kid = { type: 'string' };
  else Object.assign(options, { 'client-id': { type: 'string' }, 'token-url': { type: 'string' }, out: { type: 'string' } });
  const { values } = parseArgs({ args: rest, options, strict: true, allowPositionals: false });
  if (values.help) { console.log(help); return; }
  if (command === 'keygen') {
    console.log(JSON.stringify(generateClientKeyPair(values.dir, values.kid), null, 2));
    return;
  }
  const proof = createClientProof(values.dir, values['client-id'], values['token-url']);
  if (values.out !== undefined) {
    writeFileSync(values.out, proof + '\n', { mode: 0o600, flag: 'wx' });
    console.error(`Proof saved to ${path.resolve(values.out)} (valid for 60 seconds).`);
  } else {
    console.log(proof);
  }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try { main(process.argv.slice(2)); }
  catch (error) { console.error(`client-proof: ${error.message}`); process.exitCode = 1; }
}
