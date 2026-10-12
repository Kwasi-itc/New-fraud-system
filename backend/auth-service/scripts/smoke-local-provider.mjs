// Opt-in local acceptance: creates and deletes one temporary Keycloak user.
// No passwords, authorization codes or tokens are printed or written to disk.
import assert from 'node:assert/strict';
import { createHash, createPublicKey, randomBytes, verify } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../../../', import.meta.url));
const parse = file => Object.fromEntries(readFileSync(file, 'utf8').split(/\r?\n/).filter(line => line && !line.startsWith('#')).map(line => {
  const i = line.indexOf('='); return [line.slice(0, i), line.slice(i + 1)];
}));
const local = parse(path.join(root, '.local/auth-provider/keycloak.env'));
const auth = parse(path.join(root, '.local/auth-provider/auth.env'));
const issuer = new URL(auth.OIDC_ISSUER_URL);
assert.equal(issuer.hostname, 'localhost', 'Smoke test is only for the local provider');
assert.equal(issuer.pathname, '/realms/fraud');
assert.equal(auth.AUTH_ENV, 'development');
const origin = issuer.origin;
const request = (url, init = {}) => fetch(url, { ...init, redirect: 'error', signal: AbortSignal.timeout(15000) });
const form = body => ({ method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: new URLSearchParams(body) });
const requireFrontend = createRequire(path.join(root, 'frontend/package.json'));
const { chromium } = requireFrontend('@playwright/test');
let userID, browser, adminHeaders;
let stage = 'discovery';
try {
  const metadataResponse = await request(`${issuer}/.well-known/openid-configuration`);
  assert.equal(metadataResponse.status, 200);
  const metadata = await metadataResponse.json();
  assert.equal(metadata.issuer, issuer.toString());
  stage = 'bootstrap admin authentication';
  const adminResponse = await request(`${origin}/realms/master/protocol/openid-connect/token`, form({
    grant_type: 'password', client_id: 'admin-cli', username: local.KC_BOOTSTRAP_ADMIN_USERNAME,
    password: local.KC_BOOTSTRAP_ADMIN_PASSWORD,
  }));
  assert.equal(adminResponse.status, 200, 'Local bootstrap admin could not authenticate');
  const adminToken = await adminResponse.json();
  adminHeaders = { Authorization: `Bearer ${adminToken.access_token}`, 'Content-Type': 'application/json' };
  const suffix = randomBytes(8).toString('hex');
  stage = 'temporary user creation';
  const username = `oidc-smoke-${suffix}`;
  const password = randomBytes(32).toString('base64url');
  const create = await request(`${origin}/admin/realms/fraud/users`, {
    method: 'POST', headers: adminHeaders,
    body: JSON.stringify({ username, enabled: true, firstName: 'OIDC', lastName: 'Smoke',
      email: `${username}@example.test`, emailVerified: true,
      credentials: [{ type: 'password', value: password, temporary: false }] }),
  });
  assert.equal(create.status, 201, 'Temporary test user creation failed');
  userID = create.headers.get('location')?.split('/').pop();
  assert.ok(userID);
  const nonce = randomBytes(32).toString('base64url');
  const state = randomBytes(32).toString('base64url');
  const verifier = randomBytes(32).toString('base64url');
  const authorization = new URL(metadata.authorization_endpoint);
  authorization.search = new URLSearchParams({ client_id: auth.OIDC_CLIENT_ID, response_type: 'code',
    redirect_uri: auth.OIDC_REDIRECT_URL, scope: 'openid profile email', state, nonce,
    code_challenge_method: 'S256', code_challenge: createHash('sha256').update(verifier).digest('base64url') }).toString();
  stage = 'browser login';
  browser = await chromium.launch({ headless: true,
    ...(process.env.PLAYWRIGHT_BROWSER_PATH ? { executablePath: process.env.PLAYWRIGHT_BROWSER_PATH } : {}) });
  const page = await browser.newPage();
  // This tests the provider boundary only. Application session acceptance is
  // exercised separately by smoke-platform.mjs.
  await page.route(`${auth.OIDC_REDIRECT_URL}**`, route => route.fulfill({ status: 200, contentType: 'text/plain', body: 'Provider callback captured for test' }));
  await page.goto(authorization.toString());
  await page.locator('#username').fill(username);
  await page.locator('#password').fill(password);
  await page.locator('#kc-login').click();
  await page.waitForURL(url => url.href.startsWith(`${auth.OIDC_REDIRECT_URL}?`), { timeout: 30000 });
  const callback = new URL(page.url());
  assert.equal(callback.searchParams.get('state'), state);
  assert.equal(callback.searchParams.get('error'), null);
  const code = callback.searchParams.get('code');
  stage = 'code exchange';
  assert.ok(code);
  const exchangeBody = { grant_type: 'authorization_code', client_id: auth.OIDC_CLIENT_ID,
    client_secret: auth.OIDC_CLIENT_SECRET, redirect_uri: auth.OIDC_REDIRECT_URL, code, code_verifier: verifier };
  const exchange = await request(metadata.token_endpoint, form(exchangeBody));
  assert.equal(exchange.status, 200, 'PKCE code exchange failed');
  const tokens = await exchange.json();
  stage = 'token verification';
  const [header, payload, signature] = tokens.id_token.split('.');
  const jwtHeader = JSON.parse(Buffer.from(header, 'base64url'));
  const claims = JSON.parse(Buffer.from(payload, 'base64url'));
  const keysResponse = await request(metadata.jwks_uri);
  assert.equal(keysResponse.status, 200);
  const keys = await keysResponse.json();
  const jwk = keys.keys.find(key => key.kid === jwtHeader.kid);
  assert.equal(jwtHeader.alg, 'RS256');
  assert.ok(jwk);
  assert.ok(verify('RSA-SHA256', Buffer.from(`${header}.${payload}`), createPublicKey({ key: jwk, format: 'jwk' }), Buffer.from(signature, 'base64url')));
  assert.equal(claims.iss, auth.OIDC_ISSUER_URL);
  assert.equal(claims.sub, userID);
  assert.equal(claims.nonce, nonce);
  assert.ok([claims.aud].flat().includes(auth.OIDC_CLIENT_ID));
  assert.ok(claims.exp > Date.now() / 1000);
  stage = 'replay and grant restrictions';
  const replay = await request(metadata.token_endpoint, form(exchangeBody));
  assert.equal(replay.status, 400, 'Authorization code replay must fail');
  const passwordGrant = await request(metadata.token_endpoint, form({ grant_type: 'password',
    client_id: auth.OIDC_CLIENT_ID, client_secret: auth.OIDC_CLIENT_SECRET, username, password }));
  assert.equal(passwordGrant.status, 400, 'Application password grant must be disabled');
  console.log('PASS: real Keycloak login, PKCE exchange, signature/issuer/audience/nonce, code replay rejection and disabled application password grant. Application session integration is not part of this test.');
} catch {
  console.error(`Local OIDC smoke test failed at ${stage}. Check provider readiness, private configuration and installed Playwright Chromium. Sensitive response details were suppressed.`);
  process.exitCode = 1;
} finally {
  if (browser) await browser.close();
  if (userID && adminHeaders) {
    const cleanup = await request(`${origin}/admin/realms/fraud/users/${encodeURIComponent(userID)}`, { method: 'DELETE', headers: adminHeaders });
    if (cleanup.status !== 204) { console.error('Temporary provider test user cleanup failed; remove the oidc-smoke user in the local admin console.'); process.exitCode = 1; }
  }
}
