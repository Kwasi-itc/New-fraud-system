import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const args = process.argv.slice(2);
const usage = 'Usage: node docker/setup-root-tenant-ec2.mjs [--project new-fraud-system]';
let project;
for (let index = 0; index < args.length; index++) {
  const arg = args[index];
  const separator = arg.indexOf('=');
  const flag = separator < 0 ? arg : arg.slice(0, separator);
  if (flag !== '--project' || project !== undefined) throw new Error(usage);
  const value = separator < 0 ? args[++index] : arg.slice(separator + 1);
  if (!value || value.startsWith('--')) throw new Error(usage);
  project = value;
}

project ??= 'new-fraud-system';
if (!/^[a-z0-9][a-z0-9_-]*$/.test(project)) {
  throw new Error('Project name must start with a lowercase letter or number and contain only lowercase letters, numbers, hyphens, or underscores.');
}

function parse(file) {
  if (!existsSync(file)) return {};
  return Object.fromEntries(readFileSync(file, 'utf8').split(/\r?\n/).flatMap(line => {
    const match = line.match(/^([A-Z][A-Z0-9_]*)=(.*)$/);
    if (!match) return [];
    let value = match[2].trim();
    if (/^(['"]).*\1$/.test(value)) value = value.slice(1, -1).replaceAll("\\'", "'");
    return [[match[1], value]];
  }));
}

const projectDirectory = path.join(root, '.local', project);
const composeEnvPath = path.join(projectDirectory, 'compose.env');
const serviceEnvPath = path.join(projectDirectory, 'data-model.env');
const config = parse(composeEnvPath);
const serviceConfig = parse(serviceEnvPath);
if (!existsSync(composeEnvPath)) {
  throw new Error(`Missing ${path.relative(root, composeEnvPath)}. Run docker/setup-postgres-ec2.mjs first.`);
}

const token = serviceConfig.DATA_MODEL_SERVICE_TOKEN || serviceConfig.SERVICE_AUTH_TOKEN || config.DATA_MODEL_SERVICE_TOKEN;
if (!token) {
  throw new Error(`DATA_MODEL_SERVICE_TOKEN is missing from ${path.relative(root, serviceEnvPath)}.`);
}

const port = Number(config.DATA_MODEL_FORWARDED_PORT || '8080');
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  throw new Error('DATA_MODEL_FORWARDED_PORT must be a valid TCP port.');
}

const baseUrl = `http://127.0.0.1:${port}`;
const timeout = 60_000;

async function waitForService() {
  const deadline = Date.now() + 90_000;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${baseUrl}/healthz`, { signal: AbortSignal.timeout(3_000) });
      if (response.ok) return;
      lastError = new Error(`health endpoint returned HTTP ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise(resolve => setTimeout(resolve, 1_000));
  }
  throw new Error(`Data-model-service did not become ready at ${baseUrl}/healthz within 90 seconds: ${lastError?.message ?? 'no response'}`);
}

async function requestJson(method, pathname, body) {
  let response;
  try {
    response = await fetch(`${baseUrl}${pathname}`, {
      method,
      headers: {
        Authorization: `Bearer ${token}`,
        ...(body ? { 'Content-Type': 'application/json' } : {}),
      },
      ...(body ? { body: JSON.stringify(body) } : {}),
      signal: AbortSignal.timeout(timeout),
    });
  } catch (error) {
    throw new Error(`${method} ${pathname} failed to reach data-model-service: ${error.message}`);
  }

  const text = await response.text();
  if (!response.ok) {
    throw new Error(`${method} ${pathname} returned HTTP ${response.status}: ${text.slice(0, 1000) || '(empty response)'}`);
  }

  try {
    return JSON.parse(text);
  } catch (error) {
    throw new Error(`${method} ${pathname} returned invalid JSON: ${error.message}`);
  }
}

function tenantFrom(response, operation) {
  if (!response?.tenant || typeof response.tenant.id !== 'string' || typeof response.tenant.status !== 'string') {
    throw new Error(`${operation} returned an invalid tenant response.`);
  }
  return response.tenant;
}

await waitForService();

const list = await requestJson('GET', '/v1/tenants');
if (!Array.isArray(list?.tenants)) {
  throw new Error('GET /v1/tenants returned an invalid tenant list.');
}

const roots = list.tenants.filter(tenant => tenant.external_key === 'root');
if (roots.length > 1) {
  throw new Error(`Found ${roots.length} tenants with external_key "root"; resolve the duplicate records before continuing.`);
}

let tenant;
if (roots.length === 1) {
  tenant = roots[0];
  if (typeof tenant.id !== 'string' || typeof tenant.status !== 'string') {
    throw new Error('The existing root tenant record is invalid.');
  }
  console.log(`Using existing root tenant ${tenant.id} (${tenant.status}).`);
} else {
  tenant = tenantFrom(await requestJson('POST', '/v1/tenants', {
    name: 'Root',
    external_key: 'root',
  }), 'POST /v1/tenants');
  console.log(`Created root tenant ${tenant.id} (${tenant.status}).`);
}

if (tenant.status === 'pending') {
  tenant = tenantFrom(
    await requestJson('POST', `/v1/tenants/${encodeURIComponent(tenant.id)}/provision`),
    'tenant provision',
  );
}

if (tenant.status !== 'active') {
  throw new Error(`Root tenant ${tenant.id} has unexpected status "${tenant.status}" after provisioning.`);
}

console.log(`Root tenant ready: ${tenant.id}`);