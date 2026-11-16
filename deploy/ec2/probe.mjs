import { fileURLToPath } from 'node:url';
import { checkConfiguration } from './configure.mjs';

const root = fileURLToPath(new URL('../../', import.meta.url));
const config = checkConfiguration(root);
const origin = config.EC2_PUBLIC_ORIGIN;
const identityOnly = process.argv.includes('--identity-only');
const waiting = process.argv.includes('--wait');
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));

async function check() {
  const response = await fetch(`${origin}/identity/realms/fraud/.well-known/openid-configuration`, { redirect: 'error', signal: AbortSignal.timeout(10000) });
  if (!response.ok) throw new Error('OIDC discovery is not ready.');
  const metadata = await response.json();
  if (metadata.issuer !== `${origin}/identity/realms/fraud` || !metadata.code_challenge_methods_supported?.includes('S256')) throw new Error('OIDC issuer or PKCE capability does not match the configured deployment.');
  for (const key of ['authorization_endpoint', 'token_endpoint', 'jwks_uri']) {
    if (new URL(metadata[key]).origin !== origin) throw new Error('OIDC endpoints must use the configured HTTPS origin.');
  }
  if (identityOnly) return;
  for (const [route, status] of [['/login', 200], ['/services/auth/readyz', 204], ['/services/data-model/healthz', 200], ['/services/ingestion/healthz', 200], ['/services/decisions/healthz', 200], ['/services/screening/healthz', 200], ['/services/cases/healthz', 200]]) {
    const result = await fetch(origin + route, { redirect: 'manual', signal: AbortSignal.timeout(10000) });
    if (result.status !== status) throw new Error('An application readiness check failed.');
    await result.body?.cancel();
  }
  const login = await fetch(`${origin}/api/auth/start`, { method: 'POST', headers: { Origin: origin }, redirect: 'manual', signal: AbortSignal.timeout(15000) });
  const location = login.headers.get('location');
  if (login.status !== 303 || !location || !login.headers.has('set-cookie')) throw new Error('Login initiation is not ready.');
  const target = new URL(location);
  if (target.origin !== origin || target.searchParams.get('redirect_uri') !== `${origin}/api/auth/callback`) throw new Error('Login redirects do not match the HTTPS deployment.');
}

let lastError;
for (let attempt = 0; attempt < (waiting ? 30 : 1); attempt++) {
  try {
    await check();
    console.log(identityOnly ? 'Verified HTTPS issuer, TLS certificate and PKCE discovery.' : 'Verified HTTPS service readiness and login initiation. Password sign-in is a separate acceptance check.');
    process.exit(0);
  } catch (error) {
    lastError = error;
    if (waiting && attempt < 29) await pause(3000);
  }
}
console.error(lastError?.message === 'fetch failed' ? 'HTTPS probe failed; check DNS, certificate issuance and service readiness.' : lastError?.message);
process.exitCode = 1;
