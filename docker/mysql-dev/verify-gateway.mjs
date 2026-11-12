// Read-only checks against the running local platform; no tokens or fixtures.
// Run: node docker/mysql-dev/verify-gateway.mjs [--port 8088]
import assert from 'node:assert/strict';
import {parseArgs} from 'node:util';

const {values} = parseArgs({options: {port: {type: 'string'}, 'compare-direct': {type: 'boolean', default: false}}});
const port = Number(values.port ?? process.env.MYSQL_GATEWAY_PORT ?? 8088);
assert(Number.isInteger(port) && port >= 1024 && port <= 65535, 'Invalid gateway port');
const base = `http://127.0.0.1:${port}`;
const services = [
  ['data-model', 8080, 'json'],
  ['ingestion', 8081, 'yaml'],
  ['decisions', 8082, 'yaml'],
  ['screening', 8085, 'json'],
  ['cases', 8086, 'json'],
  ['auth', 8087, 'json'],
];

async function request(url, options) {
  const response = await fetch(url, {signal: AbortSignal.timeout(15000), ...options});
  return {status: response.status, headers: response.headers, body: await response.text()};
}

const live = await request(`${base}/healthz`);
assert.equal(live.status, 200);
assert.equal(JSON.parse(live.body).component, 'api-gateway');
const discovery = await request(`${base}/identity/realms/fraud/.well-known/openid-configuration`);
assert.equal(discovery.status, 200, 'Keycloak discovery through gateway');
const metadata = JSON.parse(discovery.body);
const issuer = `http://localhost:${port}/identity/realms/fraud`;
assert.equal(metadata.issuer, issuer);
for (const key of ['authorization_endpoint', 'token_endpoint', 'jwks_uri', 'end_session_endpoint']) assert(metadata[key].startsWith(issuer + '/'), 'Provider endpoint stays on the gateway: ' + key);
assert.equal((await request(metadata.jwks_uri)).status, 200, 'Provider signing keys through gateway');
const identityRedirect = await request(`${base}/identity`, {redirect: 'manual'});
assert.equal(identityRedirect.status, 308);
assert.equal(identityRedirect.headers.get('location'), '/identity/');
const index = await request(`${base}/services/`);
assert.equal(index.status, 200);
assert.equal((await request(`${base}/services/unknown/healthz`)).status, 404);
const frontend = await request(`${base}/login`);
assert.equal(frontend.status, 200, 'Frontend login page');
assert(frontend.body.includes('Continue to sign in'), 'Frontend login UI');
const asset = frontend.body.match(/src="(\/_next\/static\/[^"?]+\.js(?:\?[^"\s]*)?)"/);
assert(asset, 'Frontend JavaScript asset reference');
assert.equal((await request(`${base}${asset[1]}`)).status, 200, 'Frontend assets routed');
assert.equal((await request(`${base}/api/auth/start`, {method: 'POST', headers: {Origin: 'http://untrusted.invalid'}})).status, 403, 'Frontend origin checks remain active');
let failures = 0;
for (const [name, directPort, format] of services) {
  try {
    const prefix = `/services/${name}`;
    const direct = `http://127.0.0.1:${directPort}`;
    assert(index.body.includes(`href="${prefix}/docs"`), `${name}: index link`);
    const redirect = await request(`${base}${prefix}?probe=1`, {redirect: 'manual'});
    assert.equal(redirect.status, 308);
    assert.equal(redirect.headers.get('location'), `${prefix}/?probe=1`);
    for (const path of ['/healthz', '/readyz']) {
      // Owner readiness may acquire a nonblocking database lock; avoid making
      // the two entry points compete with each other during this check.
      const result = await request(`${base}${prefix}${path}?gateway_probe=1`);
      assert(result.status >= 200 && result.status < 300, `${name}${path}: ${result.status}`);
      if (values['compare-direct']) {
        const original = await request(`${direct}${path}?gateway_probe=1`);
        assert.equal(result.status, original.status, `${name}${path}: owner status preserved`);
        assert.equal(result.body, original.body, `${name}${path}: owner body preserved`);
      }
    }
    const docs = await request(`${base}${prefix}/docs`);
    assert.equal(docs.status, 200);
    assert(docs.body.includes(`${prefix}/openapi.${format}`), `${name}: prefixed Swagger spec`);
    assert(!/['"]\/openapi\.(?:json|yaml)['"]/.test(docs.body), `${name}: root spec URL`);
    const specPath = `/openapi.${format}`;
    const spec = await request(`${base}${prefix}${specPath}`);
    const original = values['compare-direct'] ? await request(`${direct}${specPath}`) : null;
    assert.equal(spec.status, 200);
    if (original) assert.equal(original.status, 200);
    if (format === 'json') {
      const parsed = JSON.parse(spec.body);
      assert.deepEqual(parsed.servers.map(server => server.url), [prefix], `${name}: Swagger execution server`);
      assert(Object.keys(parsed.paths).length > 0, `${name}: documented API paths`);
      if (original) {
        const source = JSON.parse(original.body);
        assert.deepEqual(parsed.paths, source.paths, `${name}: API path contracts preserved`);
        assert.deepEqual(parsed.components, source.components, `${name}: authentication contracts preserved`);
      }
    } else {
      assert(spec.body.includes('openapi: 3.0.3'));
      assert(spec.body.includes(`url: ${prefix}`), `${name}: Swagger execution server`);
      if (original) assert.equal(spec.body.slice(spec.body.indexOf('\npaths:')), original.body.slice(original.body.indexOf('\npaths:')), `${name}: API paths preserved`);
    }
    if (!['auth', 'cases', 'data-model'].includes(name)) {
      const redoc = await request(`${base}${prefix}/redoc`);
      assert.equal(redoc.status, 200);
      assert(redoc.body.includes(`${prefix}/openapi.${format}`), `${name}: prefixed Redoc spec`);
    }
    // Missing/invalid credentials must be rejected by the owners through either
    // entry point. The proxy must not manufacture a privileged actor/session.
    if (['auth', 'cases', 'screening'].includes(name)) {
      const path = name === 'auth' ? '/internal/session' : `/v1/tenants/00000000-0000-0000-0000-000000000001/${name === 'cases' ? 'cases' : 'screenings'}`;
      for (const authorization of [undefined, 'Bearer gateway-invalid-test-token']) {
        const options = {headers: authorization ? {Authorization: authorization} : {}};
        if (name === 'auth') {
          options.method = 'POST';
          options.headers['Content-Type'] = 'application/json';
          options.body = '{"session":"invalid-gateway-test-session"}';
        }
        const viaGateway = await request(`${base}${prefix}${path}`, options);
        assert([401, 403].includes(viaGateway.status), `${name}: unauthenticated request rejected`);
        if (values['compare-direct']) {
          const viaDirect = await request(`${direct}${path}`, options);
          assert.equal(viaGateway.status, viaDirect.status, `${name}: authentication status preserved`);
          assert.equal(viaGateway.body, viaDirect.body, `${name}: error response preserved`);
        }
      }
    }
    console.log(`PASS ${name}: routing, readiness, docs and owner contracts`);
  } catch (error) {
    failures++;
    console.error(`FAIL ${name}: ${error.message}`);
  }
}
if (failures) process.exitCode = 1;
else console.log(`Platform verified: ${base}/ (frontend and six APIs; read-only probes)`);
