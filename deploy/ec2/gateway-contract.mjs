import assert from 'node:assert/strict';
import https from 'node:https';
import http from 'node:http';
import { readFileSync, writeFileSync } from 'node:fs';
import { gunzipSync } from 'node:zlib';
import { execFileSync } from 'node:child_process';

const hostname = 'fraud.example.test';
// Process health can precede the fixture CA's first certificate by a moment.
for (let attempt = 0; attempt < 20; attempt++) {
  try {
    const certificate = execFileSync('docker', ['compose', '-f', 'deploy/ec2/compose.test.yml', 'exec', '-T', 'tls-edge', 'cat', '/data/caddy/pki/authorities/local/root.crt'], { stdio: ['ignore', 'pipe', 'pipe'] });
    writeFileSync('.local/ec2-tests/root.crt', certificate);
    break;
  } catch {
    if (attempt === 19) throw new Error('Fixture CA is not ready.');
    await new Promise(resolve => setTimeout(resolve, 200));
  }
}
const ca = readFileSync('.local/ec2-tests/root.crt');
const httpPort = Number(process.env.EC2_TEST_HTTP_PORT ?? 18089);
const httpsPort = Number(process.env.EC2_TEST_HTTPS_PORT ?? 18443);
for (const port of [httpPort, httpsPort]) assert.ok(Number.isInteger(port) && port >= 1024 && port <= 65535);
function request(path, { body, trust = ca, plain = false, headers = {} } = {}) {
  return new Promise((resolve, reject) => {
    const client = plain ? http : https;
    const req = client.request({ hostname: '127.0.0.1', port: plain ? httpPort : httpsPort, servername: hostname, path, method: body === undefined ? 'GET' : 'POST', ca: trust, rejectUnauthorized: true, headers: { Host: hostname, ...headers }, timeout: 10000 }, response => {
      const chunks = [];
      response.on('data', data => chunks.push(data));
      response.on('end', () => {
        let bytes = Buffer.concat(chunks);
        if (response.headers['content-encoding'] === 'gzip') bytes = gunzipSync(bytes);
        resolve({ status: response.statusCode, headers: response.headers, body: bytes.toString('utf8') });
      });
    });
    req.on('error', reject);
    req.on('timeout', () => req.destroy(new Error('Fixture request timed out')));
    req.end(body);
  });
}

await assert.rejects(request('/healthz', { trust: null }), error => ['SELF_SIGNED_CERT_IN_CHAIN', 'UNABLE_TO_GET_ISSUER_CERT_LOCALLY', 'UNABLE_TO_VERIFY_LEAF_SIGNATURE', 'DEPTH_ZERO_SELF_SIGNED_CERT'].includes(error.code));
const redirect = await request('/services/', { plain: true });
assert.equal(redirect.status, 308);
assert.equal(redirect.headers.location, `https://${hostname}/services/`);
console.log('PASS TLS verification and HTTP-to-HTTPS redirect');
for (const name of ['data-model', 'ingestion', 'decisions', 'screening', 'cases', 'auth']) {
  const body = JSON.stringify({ reference: 'synthetic-contract-only' });
  const path = '/echo/a%2Fb?term=one+two&term=a%2Fb';
  const result = await request(`/services/${name}${path}`, { body, headers: { Authorization: 'Bearer synthetic-fixture-token', 'Content-Type': 'application/json', 'X-Forwarded-Proto': 'http', 'X-Forwarded-Host': 'untrusted.invalid', Forwarded: 'host=untrusted.invalid;proto=http' } });
  assert.equal(result.status, 201);
  const echo = JSON.parse(result.body);
  assert.equal(echo.url, path);
  assert.equal(echo.body, body);
  assert.equal(echo.headers.host, hostname);
  assert.equal(echo.headers['x-forwarded-host'], hostname);
  assert.equal(echo.headers['x-forwarded-proto'], 'https');
  assert.equal(echo.headers.authorization, 'Bearer synthetic-fixture-token');
  assert.equal(echo.headers.forwarded, undefined);
  console.log(`PASS HTTPS API forwarding: ${name}`);
}
const identityPath = '/identity/realms/fraud/echo/a%2Fb?term=a%2Fb';
const identity = await request(identityPath, { body: 'synthetic identity contract', headers: { 'X-Forwarded-Port': '9999', 'X-Forwarded-Proto': 'http', 'X-Forwarded-Host': 'untrusted.invalid' } });
assert.equal(identity.status, 201);
const echo = JSON.parse(identity.body);
assert.equal(echo.url, identityPath);
assert.equal(echo.headers.host, hostname);
assert.equal(echo.headers['x-forwarded-proto'], 'https');
assert.equal(echo.headers['x-forwarded-port'], '443');
assert.equal(echo.headers.forwarded, undefined);
assert.equal((await request('/identity/admin/')).status, 404);
assert.equal((await request('/identity/realms/master/')).status, 404);
console.log('PASS HTTPS identity forwarding, context and administration boundaries');
const marker = 'synthetic-sensitive-callback-marker';
assert.equal((await request(`/services/decisions/unavailable?code=${marker}`)).status, 502);
const logs = execFileSync('docker', ['compose', '-f', 'deploy/ec2/compose.test.yml', 'logs', '--no-color', 'api-gateway', 'tls-edge'], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
assert.ok(!logs.includes(marker), 'Callback query value entered gateway diagnostics');
console.log('PASS upstream failure and callback-query log redaction');
const database = execFileSync('docker', ['compose', '-f', 'deploy/ec2/compose.test.yml', 'exec', '-T', '-e', 'PGPASSWORD=synthetic-contract-database-password-only', 'postgres', 'psql', '-X', '-h', '127.0.0.1', '-U', 'fixture', '-d', 'fixture', '-At', '-c', 'SELECT 1'], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
assert.equal(database.trim(), '1');
console.log('PASS PostgreSQL file credential initialization and authenticated TCP connection');
