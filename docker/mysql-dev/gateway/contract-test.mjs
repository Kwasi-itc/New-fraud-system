// Run only against gateway/compose.test.yaml's isolated fixture network.
import assert from 'node:assert/strict';
const port = Number(process.env.MYSQL_GATEWAY_TEST_PORT || 18088);
assert(Number.isInteger(port) && port >= 1024 && port <= 65535);
const base = `http://127.0.0.1:${port}`;
const identityPath = '/identity/realms/fraud/echo/a%2Fb?term=a%2Fb&term=two+words';
const identity = await fetch(base + identityPath, {
  method: 'POST', body: 'identity fixture', signal: AbortSignal.timeout(10000),
  headers: {'X-Forwarded-For': 'untrusted', 'X-Forwarded-Host': 'untrusted.invalid', 'X-Forwarded-Port': '9999', 'X-Forwarded-Proto': 'https', 'X-Forwarded-Prefix': '/untrusted', Forwarded: 'host=untrusted.invalid;proto=https'},
});
assert.equal(identity.status, 201);
const identityEcho = await identity.json();
assert.equal(identityEcho.url, identityPath, 'Actual identity context and escaped URI retained');
assert.equal(identityEcho.method, 'POST');
assert.equal(identityEcho.body, 'identity fixture');
assert.equal(identityEcho.headers.host, `127.0.0.1:${port}`);
assert.equal(identityEcho.headers['x-forwarded-host'], `127.0.0.1:${port}`);
assert.equal(identityEcho.headers['x-forwarded-port'], String(port));
assert.equal(identityEcho.headers['x-forwarded-proto'], 'http');
assert.notEqual(identityEcho.headers['x-forwarded-for'], 'untrusted');
assert.equal(identityEcho.headers.forwarded, undefined);
assert.equal(identityEcho.headers['x-forwarded-prefix'], undefined);
const identityRedirect = await fetch(base + '/identity/redirect', {redirect: 'manual'});
assert.equal(identityRedirect.status, 303);
assert.equal(identityRedirect.headers.get('location'), '/identity/next?from=redirect');
assert.equal(identityRedirect.headers.get('set-cookie'), 'identity=value; Path=/identity/realms/fraud/; HttpOnly');
console.log('PASS identity contract: context/URI, trusted proxy headers, redirects and cookie scope');
for (const [name, upstreamPort] of [['data-model', 8080], ['ingestion', 8081], ['decisions', 8082], ['screening', 8085], ['cases', 8086], ['auth', 8087]]) {
  const path = '/echo/a%2Fb%20c?term=a%2Fb&term=two+words&next=%2Fdocs';
  const body = JSON.stringify({url: 'http://localhost:8087', reference: '/openapi.json', nested: {tenant: 'fixture'}});
  const response = await fetch(`${base}/services/${name}${path}`, {
    method: 'POST', body, signal: AbortSignal.timeout(10000),
    headers: {'Content-Type': 'application/json', Authorization: 'Bearer fixture-only-token', Cookie: 'fixture_session=value', 'X-Session': 'fixture-session', 'X-Forwarded-For': 'untrusted', 'X-Forwarded-Prefix': '/untrusted'},
  });
  assert.equal(response.status, 201);
  assert.equal(response.headers.get('x-fixture'), 'upstream');
  assert.equal(response.headers.get('content-encoding'), 'gzip');
  const echo = await response.json();
  assert.equal(echo.port, upstreamPort);
  assert.equal(echo.method, 'POST');
  assert.equal(echo.url, path, `${name}: encoded path and repeated query parameters`);
  assert.equal(echo.body, body, `${name}: business JSON never rewritten`);
  assert.equal(echo.headers.authorization, 'Bearer fixture-only-token');
  assert.equal(echo.headers.cookie, 'fixture_session=value');
  assert.equal(echo.headers['x-session'], 'fixture-session');
  assert.equal(echo.headers.host, `127.0.0.1:${port}`);
  assert.equal(echo.headers['x-forwarded-prefix'], `/services/${name}`);
  assert.notEqual(echo.headers['x-forwarded-for'], 'untrusted');
  const redirect = await fetch(`${base}/services/${name}/redirect`, {redirect: 'manual', signal: AbortSignal.timeout(10000)});
  assert.equal(redirect.status, 307);
  assert.equal(redirect.headers.get('location'), `/services/${name}/next?from=redirect`);
  assert.equal(redirect.headers.get('set-cookie'), `fixture=value; Path=/services/${name}/; HttpOnly`);
  const unavailable = await fetch(`${base}/services/${name}/unavailable`, {signal: AbortSignal.timeout(10000)});
  assert.equal(unavailable.status, 502);
  console.log(`PASS contract ${name}: method/body, escaped paths/query, credentials, compression, redirects/cookies, upstream failure`);
}
