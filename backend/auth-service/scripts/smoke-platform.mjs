// Local Docker acceptance. Creates two temporary, explicitly provisioned accounts
// in an existing tenant, tests real login/permissions/revocation, then removes
// exactly those accounts. Their audit entries are retained. No case data changes.
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../../../', import.meta.url));
const env = name => Object.fromEntries(readFileSync(path.join(root, '.local/auth-provider', name), 'utf8').split(/\r?\n/).filter(l => l && !l.startsWith('#')).map(l => { const n = l.indexOf('='); return [l.slice(0,n),l.slice(n+1)]; }));
const provider = env('auth.env'), admin = env('keycloak.env');
assert.equal(provider.OIDC_ISSUER_URL, 'http://localhost:8090/realms/fraud');
assert.equal(provider.OIDC_REDIRECT_URL, 'http://localhost:3000/api/auth/callback');
const docker = (args, input) => execFileSync('docker', ['compose', ...args], { cwd: root, input, encoding: 'utf8', stdio: ['pipe','pipe','pipe'], timeout: 30000, windowsHide: true }).trim();
const sql = query => docker(['exec','-T','postgres','psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U','fraud','-d','fraud'], query);
const uuid = value => /^[0-9a-f]{8}-[0-9a-f-]{27}$/.test(value);
const request = (url, init = {}) => fetch(url, { ...init, signal: AbortSignal.timeout(15000), redirect: 'error' });
const { chromium } = createRequire(path.join(root, 'frontend/package.json'))('@playwright/test');
const accounts = []; let browser, headers, stage = 'tenant readiness';
try {
  const tenant = sql("SELECT id FROM core.tenants WHERE status='active' ORDER BY id LIMIT 1;");
  assert.ok(uuid(tenant), 'An existing active Data Model tenant is required');
  stage = 'provider admin authentication';
  const response = await request('http://localhost:8090/realms/master/protocol/openid-connect/token', { method: 'POST', body: new URLSearchParams({ grant_type: 'password', client_id: 'admin-cli', username: admin.KC_BOOTSTRAP_ADMIN_USERNAME, password: admin.KC_BOOTSTRAP_ADMIN_PASSWORD }) });
  assert.equal(response.status, 200);
  headers = { Authorization: `Bearer ${(await response.json()).access_token}`, 'Content-Type': 'application/json' };
  for (const role of ['admin','investigator']) {
    stage = `${role} provisioning`;
    const account = { username: `platform-smoke-${role}-${randomBytes(8).toString('hex')}`, password: randomBytes(32).toString('base64url') };
    const created = await request('http://localhost:8090/admin/realms/fraud/users', { method: 'POST', headers, body: JSON.stringify({ username: account.username, firstName: 'Platform', lastName: 'Smoke', email: `${account.username}@example.test`, emailVerified: true, enabled: true, credentials: [{ type: 'password', value: account.password, temporary: false }] }) });
    assert.equal(created.status, 201); account.providerID = created.headers.get('location').split('/').pop(); accounts.push(account);
    const output = docker(['exec','-T','auth-service','/app/admin','-action','provision','-tenant',tenant,'-issuer',provider.OIDC_ISSUER_URL,'-subject',account.providerID,'-name',account.username,'-roles',role === 'admin' ? 'tenant_admin,case_manager_admin' : 'case_investigator']);
    account.userID = output.match(/Platform user: ([0-9a-f-]+)/)?.[1]; assert.ok(uuid(account.userID));
  }
  browser = await chromium.launch({ headless: true, ...(process.env.PLAYWRIGHT_BROWSER_PATH ? { executablePath: process.env.PLAYWRIGHT_BROWSER_PATH } : {}) });
  for (const account of accounts) {
    stage = 'real application login';
    account.context = await browser.newContext(); account.page = await account.context.newPage();
    await account.page.goto('http://localhost:3000/login');
    await account.page.getByRole('button', { name: 'Continue to sign in' }).click();
    await account.page.locator('#username').fill(account.username); await account.page.locator('#password').fill(account.password); await account.page.locator('#kc-login').click();
    await account.page.waitForURL('http://localhost:3000/my-account', { timeout: 30000 });
    const cookie = (await account.context.cookies()).find(c => c.name === '__Host-fraud_session');
    assert.ok(cookie && cookie.httpOnly && cookie.secure && cookie.sameSite === 'Lax');
    assert.equal(cookie.value.length, 43); assert.ok(!(await account.page.evaluate(() => document.cookie)).includes(cookie.value));
    assert.equal((await account.context.request.get(`http://localhost:3000/api/cases/tenants/${tenant}/case-session`)).status(), 200);
  }
  const [manager, investigator] = accounts;
  stage = 'self-service session permissions';
  const extraContext = await browser.newContext();
  const extraPage = await extraContext.newPage();
  await extraPage.goto('http://localhost:3000/login');
  await extraPage.getByRole('button', { name: 'Continue to sign in' }).click();
  await extraPage.locator('#username').fill(manager.username); await extraPage.locator('#password').fill(manager.password); await extraPage.locator('#kc-login').click();
  await extraPage.waitForURL('http://localhost:3000/my-account', { timeout: 30000 });
  const inventory = await extraContext.request.get('http://localhost:3000/api/auth/sessions');
  assert.equal(inventory.status(), 200);
  const sessions = await inventory.json(); assert.equal(sessions.items.length, 2);
  const extraID = sessions.items.find(session => session.current)?.id; assert.ok(uuid(extraID));
  assert.equal((await investigator.context.request.post('http://localhost:3000/api/auth/sessions', { headers: { Origin: 'http://localhost:3000' }, data: { target: extraID, all: false } })).status(), 403);
  assert.equal((await manager.context.request.post('http://localhost:3000/api/auth/sessions', { headers: { Origin: 'http://localhost:3000' }, data: { target: extraID, all: false } })).status(), 200);
  assert.equal((await extraContext.request.get(`http://localhost:3000/api/cases/tenants/${tenant}/case-session`)).status(), 401);
  await extraContext.close();
  stage = 'permission checks and tenant suspension';
  assert.equal((await investigator.context.request.get('http://localhost:3000/api/auth/members')).status(), 403);
  const target = manager.page.locator('fieldset').filter({ has: manager.page.getByText(investigator.username, { exact: true }) });
  await target.getByLabel('Account active in this tenant').uncheck();
  manager.page.once('dialog', dialog => dialog.accept()); await target.getByRole('button', { name: 'Save access' }).click();
  await target.getByRole('button', { name: 'Save access' }).waitFor();
  // Wait for the committed mutation, not merely the button's initial visibility.
  for (let i = 0; i < 50; i++) {
    if (sql(`SELECT active FROM identity.memberships WHERE user_id='${investigator.userID}';`) === 'f') break;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  assert.equal(sql(`SELECT active FROM identity.memberships WHERE user_id='${investigator.userID}';`), 'f');
  assert.equal((await investigator.context.request.get(`http://localhost:3000/api/cases/tenants/${tenant}/case-session`)).status(), 401);
  await investigator.page.goto('http://localhost:3000/my-account'); await investigator.page.waitForURL('http://localhost:3000/login');
  stage = 'revoke-all logout';
  const oldCookies = await manager.context.cookies();
  manager.page.once('dialog', dialog => dialog.accept());
  await manager.page.getByRole('button', { name: 'Sign out all sessions in this tenant', exact: true }).click(); await manager.page.waitForURL('http://localhost:3000/login');
  assert.ok(!(await manager.context.cookies()).some(cookie => cookie.name === '__Host-fraud_session'), 'Logout must clear the host-prefixed cookie');
  await manager.context.addCookies(oldCookies);
  assert.equal((await manager.context.request.get(`http://localhost:3000/api/cases/tenants/${tenant}/case-session`)).status(), 401);
  console.log('PASS: real OIDC callback, secure sessions, Case access, role enforcement, session inventory, cross-account denial, revoke-one, tenant suspension, revoke-all cookie clearing and logged-out replay denial.');
} catch {
  console.error(`Platform acceptance failed at ${stage}; sensitive response details suppressed.`); process.exitCode = 1;
} finally {
  if (browser) await browser.close();
  for (const account of accounts.reverse()) {
    if (uuid(account.userID)) {
      try { sql(`BEGIN; DELETE FROM identity.sessions WHERE user_id='${account.userID}'; DELETE FROM identity.memberships WHERE user_id='${account.userID}'; DELETE FROM identity.external_identities WHERE user_id='${account.userID}'; DELETE FROM identity.users WHERE id='${account.userID}'; COMMIT;`); }
      catch { console.error('Temporary platform account cleanup failed; inspect platform-smoke accounts.'); process.exitCode = 1; }
    }
    if (account.providerID && headers) {
      const deleted = await request(`http://localhost:8090/admin/realms/fraud/users/${encodeURIComponent(account.providerID)}`, { method: 'DELETE', headers });
      if (deleted.status !== 204) { console.error('Temporary provider account cleanup failed.'); process.exitCode = 1; }
    }
  }
}
