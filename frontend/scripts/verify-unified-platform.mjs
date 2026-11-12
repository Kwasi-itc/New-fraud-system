// Browser verification with the retained synthetic demo account. Never prints
// its password/tokens and never changes application objects or memberships.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import {fileURLToPath} from 'node:url';
import {resolve} from 'node:path';
import {localOrigin} from './mysql-origin.mjs';
const root = fileURLToPath(new URL('../../', import.meta.url));
const local = resolve(root, '.local/mysql-dev/novapay-demo');
const login = JSON.parse(readFileSync(resolve(local, 'login.json'), 'utf8'));
const state = JSON.parse(readFileSync(resolve(local, 'state.json'), 'utf8'));
assert.equal(login.temporary, false, 'Complete the demo account bootstrap before this check');
const origin = localOrigin(process.env.FRAUD_APP_ORIGIN || 'http://localhost:' + (process.env.MYSQL_GATEWAY_PORT || 8088));
const require = createRequire(resolve(root, 'frontend/package.json'));
const {chromium} = require('@playwright/test');
const browser = await chromium.launch({headless: true});
const context = await browser.newContext();
const page = await context.newPage();
page.setDefaultTimeout(30000);
try {
  await page.goto(origin + '/login');
  await page.getByRole('button', {name: 'Continue to sign in'}).click();
  await page.waitForURL(origin + '/identity/**');
  assert.equal(new URL(page.url()).origin, origin, 'Identity uses the same public port');
  assert(new URL(page.url()).pathname.startsWith('/identity/realms/fraud/'));
  assert.equal(new URL(page.url()).searchParams.get('redirect_uri'), origin + '/api/auth/callback');
  await page.locator('#username').fill(login.username);
  await page.locator('#password').fill(login.password);
  await page.locator('#kc-login').click();
  await page.waitForURL(origin + '/my-account');
  const providerCookies = (await context.cookies()).filter(value => value.name.startsWith('KEYCLOAK_') || value.name.startsWith('AUTH_SESSION_ID'));
  assert(providerCookies.length > 0 && providerCookies.every(value => value.path.startsWith('/identity/')), 'Provider cookies retain the identity context');
  const cookie = (await context.cookies()).find(value => value.name === '__Host-fraud_session');
  assert(cookie?.secure && cookie.httpOnly && cookie.sameSite === 'Lax' && cookie.path === '/');
  console.log('PASS unified login: retained account, callback on gateway, secure session cookie');
  for (const [service, path] of [
    ['data-model', `/tenants/${state.tenant}/tables`],
    ['decisions', `/tenants/${state.tenant}/scenarios`],
    ['cases', `/tenants/${state.tenant}/cases`],
    ['ingestion', `/tenants/${state.tenant}/records/transactions`],
  ]) {
    const response = await context.request.get(origin + '/api/' + service + path);
    assert.equal(response.status(), 200, `${service}: authenticated frontend contract`);
    if (service === 'data-model') assert.equal((await response.json()).tables.length, 3, 'Retained demo models');
    console.log(`PASS unified frontend API: ${service}`);
  }
  for (const path of ['/cases', '/detection', '/your-data', '/screening-search']) {
    const response = await page.goto(origin + path);
    assert.equal(response.status(), 200, path + ': frontend page must not be shadowed by a raw API route');
    assert(new URL(page.url()).origin === origin);
    console.log(`PASS unified frontend page: ${path}`);
  }
  const denied = await context.request.post(origin + '/api/auth/logout', {headers: {Origin: 'http://untrusted.invalid'}});
  assert.equal(denied.status(), 403, 'Gateway preserves frontend CSRF validation');
} catch (error) {
  console.error(String(error.message).replaceAll(login.password, '[private]'));
  process.exitCode = 1;
} finally {
  try {
    const logout = await context.request.post(origin + '/api/auth/logout', {headers: {Origin: origin}, maxRedirects: 0});
    assert.equal(logout.status(), 303, 'Session cleanup');
  } finally {
    await context.close();
    await browser.close();
  }
}
