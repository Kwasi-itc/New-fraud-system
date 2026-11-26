import { createPrivateKey, createPublicKey, generateKeyPairSync, randomBytes } from 'node:crypto';
import { chmodSync, chownSync, existsSync, lstatSync, mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const profiles = JSON.parse(readFileSync(fileURLToPath(new URL('../../platform-auth/services.json', import.meta.url)), 'utf8'));
const catalog = JSON.parse(readFileSync(fileURLToPath(new URL('../../platform-auth/capabilities.json', import.meta.url)), 'utf8'));
const apiAudiences = ['urn:fraud:platform-api', 'urn:fraud:system'];
export const EC2_CONTAINER_GROUP = 65532;

export function requireContainerKeyGroup(gid = EC2_CONTAINER_GROUP) {
  if (typeof process.getuid !== 'function' || typeof process.getgroups !== 'function') {
    throw new Error('EC2 key permissions require a Unix host with container group membership.');
  }
  if (process.getuid() !== 0 && !process.getgroups().includes(gid)) {
    throw new Error(`The setup user must belong to host group ${gid} to grant containers access to private keys; add the user to that group and start a new login session.`);
  }
}

export function applyContainerKeyPermissions(directory, identities, gid = EC2_CONTAINER_GROUP) {
  requireContainerKeyGroup(gid);
  const privateFiles = ['platform-private.pem', ...Object.keys(identities).map(id => `${id}-private.pem`)];
  for (const name of privateFiles) {
    const file = path.join(directory, name);
    try {
      const stat = lstatSync(file);
      if (!stat.isFile()) throw new Error('not a regular file');
      if (stat.gid !== gid) chownSync(file, stat.uid, gid);
      chmodSync(file, 0o640);
    } catch (error) {
      throw new Error(`Unable to set container permissions on ${file}; ensure the setup user owns the file and belongs to group ${gid}.`, { cause: error });
    }
  }
  const publicFile = path.join(directory, 'public-keys.json');
  try {
    if (!lstatSync(publicFile).isFile()) throw new Error('not a regular file');
    chmodSync(publicFile, 0o644);
  } catch (error) {
    throw new Error(`Unable to set public-key ring permissions on ${publicFile}.`, { cause: error });
  }
}
function parseEnv(file) {
  return Object.fromEntries(readFileSync(file, 'utf8').split(/\r?\n/).filter(line => line && !line.startsWith('#')).map(line => { const i = line.indexOf('='); let value=line.slice(i+1);if ((value.startsWith("'") && value.endsWith("'")) || (value.startsWith('"') && value.endsWith('"'))) value=value.slice(1,-1);return [line.slice(0,i),value]; }));
}
function atomicWrite(file, data) {
  const temporary = `${file}.${randomBytes(6).toString('hex')}.tmp`;
  writeFileSync(temporary, data, { mode: 0o600, flag: 'wx' }); renameSync(temporary, file);
}
function privateKey(file) {
  const key = createPrivateKey(readFileSync(file));
  if (key.asymmetricKeyType !== 'rsa' || key.asymmetricKeyDetails.modulusLength < 2048) throw new Error('Invalid RSA signing key.');
  return key;
}
function createKey(file) {
  const { privateKey } = generateKeyPairSync('rsa', { modulusLength: 3072, privateKeyEncoding: { type: 'pkcs8', format: 'pem' }, publicKeyEncoding: { type: 'spki', format: 'pem' } });
  writeFileSync(file, privateKey, { mode: 0o600, flag: 'wx' });
}
export function provisionPlatformKeys(directory, { tokenEndpointURL, issuer = 'urn:fraud:identity' } = {}) {
  const endpoint = new URL(tokenEndpointURL);
  if (!['https:', 'http:'].includes(endpoint.protocol) || endpoint.username || endpoint.password || endpoint.search || endpoint.hash) throw new Error('A fixed public token endpoint URL is required.');
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  const platformFile = path.join(directory, 'platform-private.pem'), envFile = path.join(directory, 'platform.env');
  if (existsSync(platformFile) !== existsSync(envFile)) throw new Error('Incomplete platform key identity; restore matching private key and environment files.');
  if (!existsSync(platformFile)) {
    createKey(platformFile);
    writeFileSync(envFile, `PLATFORM_JWT_KEY_ID=platform-${randomBytes(8).toString('hex')}\n`, { mode: 0o600, flag: 'wx' });
  }
  const old = parseEnv(envFile), platformID = old.PLATFORM_JWT_KEY_ID;
  if (!platformID) throw new Error('Existing platform key ID is missing.');
  const publicKey = createPublicKey(privateKey(platformFile)).export({ type: 'spki', format: 'pem' });
  const keys = [{ kid: platformID, public_key_pem: publicKey, issuer, principal_types: ['user','client'], audiences: apiAudiences }];
  const ringFile = path.join(directory, 'public-keys.json');
  const retained = existsSync(ringFile) ? JSON.parse(readFileSync(ringFile, 'utf8')).keys : [];
  if (!Array.isArray(retained) || new Set(retained.map(key => key.kid)).size !== retained.length) throw new Error('Invalid or duplicate trusted key IDs.');
  const generated = {};
  for (const profile of profiles) {
    for (const capability of profile.capabilities) if (!catalog.some(item => item.name === capability && item.service)) throw new Error(`Invalid service policy: ${profile.id}`);
    const filename = `${profile.id}-private.pem`, keyFile = path.join(directory, filename), settingsFile = path.join(directory, `${profile.id}.env`);
    if (existsSync(keyFile) !== existsSync(settingsFile)) throw new Error(`Incomplete signing identity: ${profile.id}`);
    if (!existsSync(keyFile)) {
      createKey(keyFile);
      writeFileSync(settingsFile, `PLATFORM_AUTH_SIGNING_KEY_ID=${profile.id}-${randomBytes(8).toString('hex')}\n`, { mode: 0o600, flag: 'wx' });
    }
    const previous = parseEnv(settingsFile), kid = previous.PLATFORM_AUTH_SIGNING_KEY_ID;
    if (!kid) throw new Error(`Missing signing key ID for ${profile.id}`);
    const pub = createPublicKey(privateKey(keyFile)).export({ type: 'spki', format: 'pem' });
    keys.push({ kid, public_key_pem: pub, issuer: `urn:fraud:service:${profile.id}`, principal_types: ['service'], service_id: profile.id, capabilities: profile.capabilities, audiences: profile.audiences });
    const receiver = profile.id.replace(/-worker$/, '-service');
    generated[profile.id] = {
      PLATFORM_AUTH_KEYS_FILE: '/run/secrets/public-keys.json',
      PLATFORM_AUTH_AUDIENCE: `urn:fraud:${receiver}`,
      PLATFORM_AUTH_SIGNING_KEY_FILE: `/run/secrets/${filename}`,
      PLATFORM_AUTH_SIGNING_KEY_ID: kid,
      PLATFORM_AUTH_SERVICE_ID: profile.id,
    };
    atomicWrite(settingsFile, Object.entries(generated[profile.id]).map(([k,v]) => `${k}=${v}`).join('\n') + '\n');
  }
  for (const key of keys) {
    const previous = retained.find(old => old.kid === key.kid);
    if (previous && (previous.public_key_pem !== key.public_key_pem || previous.issuer !== key.issuer || previous.service_id !== key.service_id || JSON.stringify(previous.principal_types) !== JSON.stringify(key.principal_types))) {
      throw new Error(`Signing identity changed without a new key ID: ${key.kid}`);
    }
  }
  for (const old of retained) if (!keys.some(key => key.kid === old.kid)) keys.push(old);
  atomicWrite(envFile, Object.entries({
    PLATFORM_JWT_KEY_ID: platformID, PLATFORM_JWT_ISSUER: issuer,
    PLATFORM_JWT_AUDIENCE: 'urn:fraud:platform-api',
    PLATFORM_JWT_PRIVATE_KEY_FILE: '/run/secrets/platform-private.pem',
    PLATFORM_AUTH_KEYS_FILE: '/run/secrets/public-keys.json',
    PLATFORM_AUTH_AUDIENCE: 'urn:fraud:auth-service',
    AUTH_TOKEN_ENDPOINT_URL: endpoint.href,
  }).map(([k,v]) => `${k}=${v}`).join('\n') + '\n');
  atomicWrite(path.join(directory, 'public-keys.json'), JSON.stringify({ keys }, null, 2) + '\n');
  // Retain the old public filename for configuration paths during the clean
  // rebuild. Its contents are the new strict key ring, not a legacy verifier map.
  atomicWrite(path.join(directory, 'platform-public.json'), JSON.stringify({ keys }, null, 2) + '\n');
  return { keys, identities: generated };
}


// Read-only deployment preflight. Historical public keys may overlap during
// rotation; every active signer must match its own current policy and private key.
export function validatePlatformKeys(directory) {
  const ring = JSON.parse(readFileSync(path.join(directory, 'public-keys.json'), 'utf8'));
  if (!Array.isArray(ring.keys) || new Set(ring.keys.map(k => k.kid)).size !== ring.keys.length) throw new Error('Invalid trusted key ring.');
  const platform = parseEnv(path.join(directory, 'platform.env'));
  const check = (kid, file, service) => {
    const entry = ring.keys.find(k => k.kid === kid);
    const pub = createPublicKey(privateKey(path.join(directory, file))).export({ type: 'spki', format: 'pem' });
    if (!entry || entry.public_key_pem !== pub || entry.service_id !== service) throw new Error('Signing keys do not match their trusted identities.');
    return entry;
  };
  const human = check(platform.PLATFORM_JWT_KEY_ID, 'platform-private.pem', undefined);
  if (human.issuer !== platform.PLATFORM_JWT_ISSUER || JSON.stringify(human.principal_types) !== JSON.stringify(['user','client']) || JSON.stringify(human.audiences) !== JSON.stringify(apiAudiences)) throw new Error('Platform signing policy disagrees.');
  for (const profile of profiles) {
    const env = parseEnv(path.join(directory, `${profile.id}.env`));
    const key = check(env.PLATFORM_AUTH_SIGNING_KEY_ID, `${profile.id}-private.pem`, profile.id);
    if (env.PLATFORM_AUTH_SERVICE_ID !== profile.id || key.issuer !== `urn:fraud:service:${profile.id}` || JSON.stringify(key.principal_types) !== '["service"]' || JSON.stringify(key.capabilities) !== JSON.stringify(profile.capabilities) || JSON.stringify(key.audiences) !== JSON.stringify(profile.audiences)) throw new Error('Service signing policy disagrees.');
  }
  return ring;
}
