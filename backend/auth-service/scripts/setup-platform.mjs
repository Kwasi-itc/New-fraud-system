import { randomBytes, generateKeyPairSync } from 'node:crypto';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
const root = fileURLToPath(new URL('../../../', import.meta.url));
const dir = path.join(root, '.local/auth-provider');
if (!existsSync(path.join(dir, 'auth.env'))) throw new Error('Run setup-local-provider.mjs first');
const paths = ['platform.env', 'proxy.env', 'platform-private.pem', 'platform-public.json'].map(p => path.join(dir, p));
const existing = paths.filter(existsSync).length;
if (existing && existing !== paths.length) throw new Error('Incomplete platform secrets; restore the matching original files');
if (!existing) {
  const { publicKey, privateKey } = generateKeyPairSync('rsa', { modulusLength: 3072, publicKeyEncoding: { type: 'spki', format: 'pem' }, privateKeyEncoding: { type: 'pkcs8', format: 'pem' } });
  const token = randomBytes(32).toString('hex'), kid = `platform-${randomBytes(8).toString('hex')}`;
  const values = [`AUTH_PROXY_TOKEN=${token}`, `PLATFORM_JWT_KEY_ID=${kid}`, 'PLATFORM_JWT_ISSUER=urn:fraud:identity', 'PLATFORM_JWT_AUDIENCE=case-manager', 'PLATFORM_JWT_PRIVATE_KEY_FILE=/run/secrets/platform-private.pem', ''];
  for (const [file, value] of [[paths[0], values.join('\n')], [paths[1], `AUTH_PROXY_TOKEN=${token}\n`], [paths[2], privateKey], [paths[3], JSON.stringify({ [kid]: publicKey }, null, 2)]]) writeFileSync(file, value, { flag: 'wx', mode: 0o600 });
}
const envPath = path.join(root, '.env'); let env = readFileSync(envPath, 'utf8');
// Private owner integration credential: never mounted into the frontend.
const dataModelEnv = path.join(dir, 'data-model.env');
const invitationEnv = path.join(dir, 'invitations.env');
if (!existsSync(invitationEnv)) writeFileSync(invitationEnv, `AUTH_INVITATIONS_ENABLED=false\nAUTH_INVITATION_KEY=${randomBytes(32).toString('base64')}\nAUTH_APP_ORIGIN=http://localhost:3000\nAUTH_SMTP_ADDR=mailpit:1025\nAUTH_SMTP_FROM=access@example.test\nAUTH_SMTP_LOCAL_CAPTURE=true\n`, { flag: 'wx', mode: 0o600 });
if (!existsSync(dataModelEnv)) writeFileSync(dataModelEnv, `DATA_MODEL_SERVICE_TOKEN=${randomBytes(32).toString('hex')}\n`, { flag: 'wx', mode: 0o600 });
for (const [key, value] of Object.entries({ CASE_USER_JWT_KEYS_FILE: './.local/auth-provider/platform-public.json', CASE_USER_JWT_ISSUER: 'urn:fraud:identity', CASE_USER_JWT_AUDIENCE: 'case-manager', CASE_SESSION_COOKIE: '__Host-fraud_session' })) {
  const pattern = new RegExp(`^${key}=.*$`, 'm'); env = pattern.test(env) ? env.replace(pattern, `${key}=${value}`) : `${env.trimEnd()}\n${key}=${value}\n`;
}
writeFileSync(envPath, env, { mode: 0o600 });
console.log('Platform keys and private Data Model integration credential ready. Apply auth migrations, explicitly grant platform roles, then rebuild auth, Data Model, ingestion/decision APIs and workers, investigator consumers and frontend together.');
