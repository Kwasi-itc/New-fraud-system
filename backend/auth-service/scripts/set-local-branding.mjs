// Update the persisted local realm; startup realm import does not overwrite it.
// Credentials are read privately and never printed. Accounts are not modified.
import { readFileSync } from 'node:fs';
const root = new URL('../../../', import.meta.url);
const env = Object.fromEntries(readFileSync(new URL('.local/auth-provider/keycloak.env', root), 'utf8').split(/\r?\n/).filter(line => line && !line.startsWith('#')).map(line => {
  const index = line.indexOf('='); return [line.slice(0, index), line.slice(index + 1)];
}));
const request = (url, init = {}) => fetch(url, { ...init, redirect: 'error', signal: AbortSignal.timeout(15000) });
try {
  const response = await request('http://localhost:8090/realms/master/protocol/openid-connect/token', {
    method: 'POST', body: new URLSearchParams({ grant_type: 'password', client_id: 'admin-cli', username: env.KC_BOOTSTRAP_ADMIN_USERNAME, password: env.KC_BOOTSTRAP_ADMIN_PASSWORD }),
  });
  if (!response.ok) throw new Error();
  const headers = { Authorization: `Bearer ${(await response.json()).access_token}`, 'Content-Type': 'application/json' };
  const realmURL = 'http://localhost:8090/admin/realms/fraud';
  const template = JSON.parse(readFileSync(new URL('docker/keycloak/fraud-realm.json', root), 'utf8'));
  const update = await request(realmURL, { method: 'PUT', headers, body: JSON.stringify({ displayName: template.displayName }) });
  if (update.status !== 204) throw new Error();
  const verify = await request(realmURL, { headers });
  if (!verify.ok || (await verify.json()).displayName !== template.displayName) throw new Error();
  const users = await request(`${realmURL}/users/count`, { headers });
  if (!users.ok) throw new Error();
  console.log(`Verified sign-in branding: ${template.displayName}`);
  console.log(`Current application-realm user count: ${await users.json()}`);
} catch {
  console.error('Could not update/verify local realm branding. Check provider readiness and private admin configuration.');
  process.exitCode = 1;
}
