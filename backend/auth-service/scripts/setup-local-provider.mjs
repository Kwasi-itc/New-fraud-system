// Generates private development configuration once. Never prints credentials,
// resets persisted Keycloak accounts or changes application JWT trust.
import { randomBytes } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('../../../', import.meta.url));
const directory = path.join(root, '.local', 'auth-provider');
const secret = () => randomBytes(32).toString('hex');
mkdirSync(directory, { recursive: true });
const keycloakPath = path.join(directory, 'keycloak.env');
const authPath = path.join(directory, 'auth.env');
if (existsSync(keycloakPath) !== existsSync(authPath)) {
  throw new Error('Incomplete local provider configuration. Restore the matching files; do not silently rotate a persisted provider secret.');
}
if (!existsSync(keycloakPath)) {
  const clientSecret = secret();
  writeFileSync(keycloakPath, [
    'KC_BOOTSTRAP_ADMIN_USERNAME=local-admin',
    `KC_BOOTSTRAP_ADMIN_PASSWORD=${secret()}`,
    `KC_DB_PASSWORD=${secret()}`,
    `FRAUD_OIDC_CLIENT_SECRET=${clientSecret}`,
    'FRAUD_AUTH_REDIRECT_URI=http://localhost:3000/api/auth/callback',
    '',
  ].join('\n'), { flag: 'wx', mode: 0o600 });
  writeFileSync(authPath, [
    'AUTH_ENV=development',
    'OIDC_ISSUER_URL=http://localhost:8090/realms/fraud',
    'OIDC_BACKCHANNEL_ORIGIN=http://keycloak:8080',
    'OIDC_CLIENT_ID=fraud-auth',
    `OIDC_CLIENT_SECRET=${clientSecret}`,
    'OIDC_REDIRECT_URL=http://localhost:3000/api/auth/callback',
    '',
  ].join('\n'), { flag: 'wx', mode: 0o600 });
}
const dbPath = path.join(directory, 'database.env');
const password = readFileSync(keycloakPath, 'utf8').match(/^KC_DB_PASSWORD=([a-f0-9]{64})$/m)?.[1];
if (!password) throw new Error('Local provider database credential missing or invalid; restore the original configuration.');
if (!existsSync(dbPath)) {
  writeFileSync(dbPath, `KC_DB_PASSWORD=${password}\n`, { flag: 'wx', mode: 0o600 });
} else if (readFileSync(dbPath, 'utf8') !== `KC_DB_PASSWORD=${password}\n`) {
  throw new Error('Provider and database provisioning credentials differ; reconcile explicitly.');
}
const envPath = path.join(root, '.env');
let env = existsSync(envPath) ? readFileSync(envPath, 'utf8') : '';
const match = env.match(/^COMPOSE_PROFILES=(.*)$/m);
const profiles = (match?.[1] ?? '').trim().replace(/^["']|["']$/g, '').split(',').filter(Boolean);
if (!profiles.includes('local-idp')) {
  profiles.push('local-idp');
  const setting = `COMPOSE_PROFILES=${profiles.join(',')}`;
  env = match ? env.replace(/^COMPOSE_PROFILES=.*$/m, setting) : `${env.trimEnd()}\n${setting}\n`;
  writeFileSync(envPath, env, { mode: 0o600 });
}
console.log('Local provider configuration ready in .local/auth-provider; local-idp profile enabled. Existing credentials preserved.');
