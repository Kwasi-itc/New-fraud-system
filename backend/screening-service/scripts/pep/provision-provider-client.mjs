#!/usr/bin/env node
import { spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

/**
 * One-command client & API key provisioning for the Standalone Screening Provider.
 *
 * Creates a machine client, issues a bearer token to .local/provider-dev/<key-file>,
 * and grants access to available datasets so you can immediately test in /docs or
 * connect an external product.
 *
 * Usage:
 *   node backend/screening-service/scripts/pep/provision-provider-client.mjs --name "Swagger Test"
 *   node backend/screening-service/scripts/pep/provision-provider-client.mjs --name "Core Banking" --key-file core-banking.key.txt
 */

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const REPO_ROOT = path.resolve(__dirname, '..', '..', '..', '..');

const DEFAULT_DATASETS = [
  'gh-pep-parliament',
  'gh-pep-executive',
  'gh-pep-soe',
  'gh-pep-rca',
  'gh-pep-judiciary',
  'gh-pep-parties',
  'gh-pep-local-gov',
  'ofac-sdn',
  'un-consolidated',
  'uksl',
  'eu-fsf',
  'standalone-demo',
];

function parseArgs(argv) {
  const opts = {
    name: 'Docs & Integration Client',
    keyFile: `client-${Date.now()}.key.txt`,
    ttl: '8760h',
    rpm: 6000,
    datasets: DEFAULT_DATASETS,
  };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === '--name' && argv[i + 1]) opts.name = argv[++i];
    else if (arg === '--key-file' && argv[i + 1]) opts.keyFile = path.basename(argv[++i]);
    else if (arg === '--ttl' && argv[i + 1]) opts.ttl = argv[++i];
    else if (arg === '--rpm' && argv[i + 1]) opts.rpm = Number(argv[++i]);
    else if (arg === '--datasets' && argv[i + 1]) {
      opts.datasets = argv[++i].split(',').map(s => s.trim()).filter(Boolean);
    }
  }
  return opts;
}

function runAdmin(composeBase, adminArgs, allowFailure = false) {
  const res = spawnSync('docker', [...composeBase, 'run', '--rm', 'provider-admin', ...adminArgs], {
    cwd: REPO_ROOT,
    encoding: 'utf8',
  });
  if (res.error) throw res.error;
  if (res.status !== 0) {
    if (allowFailure) return null;
    throw new Error((res.stderr || res.stdout || `provider-admin exited ${res.status}`).trim());
  }
  return (res.stdout || '').trim();
}

function main() {
  const opts = parseArgs(process.argv.slice(2));
  const secretDir = path.join(REPO_ROOT, '.local', 'provider-dev');
  const envFile = path.join(secretDir, 'compose.env');
  if (!fs.existsSync(envFile)) {
    throw new Error('Standalone provider is not initialized. Run: node docker/setup-provider.mjs');
  }

  const hostKeyPath = path.join(secretDir, opts.keyFile);
  if (fs.existsSync(hostKeyPath)) {
    throw new Error(`Key file already exists: ${hostKeyPath}. Pass --key-file <new-name.txt>.`);
  }

  const composeBase = [
    'compose', '--env-file', envFile, '-p', 'screening-provider-dev',
    '-f', path.join(REPO_ROOT, 'docker-compose.provider.yml'),
  ];

  const namespace = crypto.randomUUID();
  console.log(`[Provision] Creating client "${opts.name}" (namespace: ${namespace})...`);
  const createdRaw = runAdmin(composeBase, [
    '-action', 'create-client',
    '-name', opts.name,
    '-namespace', namespace,
    '-operations', 'search,enrich,catalog,freshness,delta',
    '-requests-per-minute', String(opts.rpm),
    '-ttl', opts.ttl,
  ]);
  const created = JSON.parse(createdRaw);
  const clientId = created.client_id;

  console.log(`[Provision] Issuing key to .local/provider-dev/${opts.keyFile}...`);
  runAdmin(composeBase, [
    '-action', 'issue-key',
    '-client', clientId,
    '-ttl', opts.ttl,
    '-key-file', `/output/${opts.keyFile}`,
  ]);

  const granted = [];
  const skipped = [];
  for (const ds of opts.datasets) {
    const out = runAdmin(composeBase, [
      '-action', 'grant-dataset',
      '-client', clientId,
      '-scope', 'global',
      '-dataset', ds,
    ], true);
    if (out !== null) granted.push(ds);
    else skipped.push(ds);
  }

  console.log('\n========================================================================');
  console.log('  Standalone Provider Client Ready');
  console.log('========================================================================');
  console.log(`Client ID:    ${clientId}`);
  console.log(`Namespace:    ${namespace}`);
  console.log(`Key File:     ${hostKeyPath}`);
  console.log(`Granted (${granted.length}): ${granted.join(', ') || '(none yet — run refresh/import first)'}`);
  if (skipped.length > 0) {
    console.log(`Skipped (${skipped.length}): ${skipped.join(', ')} (not yet registered/imported)`);
  }
  console.log('\nTo test in Swagger UI (http://localhost:8091/docs):');
  console.log(`  1. Copy the token from: ${hostKeyPath}`);
  console.log('  2. Click "Authorize" in Swagger UI and paste the token.');
  console.log('========================================================================\n');
}

try {
  main();
} catch (err) {
  console.error(`[Provision] ERROR: ${err.message}`);
  process.exit(1);
}
