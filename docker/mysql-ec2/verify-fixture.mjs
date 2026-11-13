// Only the disposable compose.test.yaml stack. No Aurora or application state.
import assert from 'node:assert/strict';
import {randomBytes, createHash, createPublicKey, verify} from 'node:crypto';
import {createRequire} from 'node:module';
const base = 'http://127.0.0.1:18089';
const issuer = base + '/identity/realms/fraud';
const response = await fetch(issuer + '/.well-known/openid-configuration');
assert.equal(response.status, 200);
const discovery = await response.json();
assert.equal(discovery.issuer, issuer);
for (const key of ['authorization_endpoint', 'token_endpoint', 'jwks_uri', 'end_session_endpoint']) assert(discovery[key].startsWith(issuer + '/'));
const keyResponse = await fetch(discovery.jwks_uri);
assert.equal(keyResponse.status, 200);
const signingKeys = (await keyResponse.json()).keys;
const token = await fetch(base + '/identity/realms/master/protocol/openid-connect/token', {method: 'POST', body: new URLSearchParams({grant_type: 'password', client_id: 'admin-cli', username: 'fixture-admin', password: 'isolated-admin-test-password'})});
assert.equal(token.status, 200, 'Fixture administrator sign-in through gateway');
const headers = {Authorization: 'Bearer ' + (await token.json()).access_token, 'Content-Type': 'application/json'};
const clients = await fetch(base + '/identity/admin/realms/fraud/clients?clientId=fraud-auth', {headers});
assert.equal(clients.status, 200);
assert.equal((await clients.json()).length, 1);
console.log('PASS Keycloak/MySQL JDBC verified TLS, gateway discovery, signing keys and administrator login.');

if (process.argv.includes('--browser')) {
  const require = createRequire(new URL('../../frontend/package.json', import.meta.url));
  const {chromium} = require('@playwright/test');
  const username = 'gateway-fixture-' + randomBytes(6).toString('hex');
  const password = randomBytes(32).toString('base64url');
  const users = base + '/identity/admin/realms/fraud/users';
  const created = await fetch(users, {method: 'POST', headers, body: JSON.stringify({username, firstName: 'Gateway', lastName: 'Fixture', email: username + '@example.test', emailVerified: true, enabled: true, credentials: [{type: 'password', value: password, temporary: false}]})});
  assert.equal(created.status, 201);
  const user = created.headers.get('location').split('/').pop();
  let browser;
  try {
    browser = await chromium.launch({headless: true});
    const page = await browser.newPage();
    const verifier = randomBytes(32).toString('base64url');
    const state = randomBytes(16).toString('hex');
    const redirect = base + '/api/auth/callback';
    const auth = new URL(discovery.authorization_endpoint);
    auth.search = new URLSearchParams({client_id: 'fraud-auth', redirect_uri: redirect, response_type: 'code', scope: 'openid', state, code_challenge_method: 'S256', code_challenge: createHash('sha256').update(verifier).digest('base64url')});
    await page.goto(auth.href);
    await page.locator('#username').fill(username);
    await page.locator('#password').fill(password);
    await page.locator('#kc-login').click();
    // This fixture has no frontend. Capture the redirect to its callback; the
    // authorization code is then exchanged directly without printing it.
    await page.waitForURL(redirect + '?**');
    const callback = new URL(page.url());
    assert.equal(callback.searchParams.get('state'), state);
    assert(callback.searchParams.get('code'));
    const exchange = await fetch(discovery.token_endpoint, {method: 'POST', body: new URLSearchParams({grant_type: 'authorization_code', client_id: 'fraud-auth', client_secret: 'isolated-client-test-secret', code: callback.searchParams.get('code'), redirect_uri: redirect, code_verifier: verifier})});
    assert.equal(exchange.status, 200);
    const tokens = await exchange.json();
    const parts = tokens.id_token.split('.');
    const header = JSON.parse(Buffer.from(parts[0], 'base64url'));
    assert.equal(header.alg, 'RS256');
    const jwk = signingKeys.find(key => key.kid === header.kid);
    assert(jwk);
    assert(verify('RSA-SHA256', Buffer.from(parts.slice(0, 2).join('.')), createPublicKey({key: jwk, format: 'jwk'}), Buffer.from(parts[2], 'base64url')));
    const claims = JSON.parse(Buffer.from(parts[1], 'base64url'));
    assert.equal(claims.iss, issuer); assert.equal(claims.sub, user);
    assert.equal(claims.aud, 'fraud-auth');
    const cookies = (await page.context().cookies()).filter(cookie => cookie.name.startsWith('KEYCLOAK_') || cookie.name.startsWith('AUTH_SESSION_ID'));
    assert(cookies.length && cookies.every(cookie => cookie.path.startsWith('/identity/')));
    console.log('PASS browser sign-in, gateway callback, PKCE code exchange, signed ID token, issuer/subject and identity cookie paths.');
  } finally {
    if (browser) await browser.close();
    const removed = await fetch(users + '/' + user, {method: 'DELETE', headers});
    assert.equal(removed.status, 204, 'Temporary fixture user removed');
  }
}
