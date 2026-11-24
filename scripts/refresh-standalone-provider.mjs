#!/usr/bin/env node
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

/**
 * Scheduled Live Refresh Pipeline for the Standalone Screening Provider
 *
 * Runs all live online scrapers (no prepopulated data), builds Version 2 JSON
 * snapshots, stages them into .local/provider-dev/imports, and imports/publishes
 * them into the standalone provider catalog via docker compose `provider-import`.
 *
 * Usage:
 *   node scripts/refresh-standalone-provider.mjs --once
 *   node scripts/refresh-standalone-provider.mjs --interval 360
 *   node scripts/refresh-standalone-provider.mjs --once --skip-import
 *   node scripts/refresh-standalone-provider.mjs --once --strict
 */

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const ROOT_DIR = path.resolve(__dirname, '..');

function parseArgs(argv) {
  const opts = {
    once: false,
    intervalMinutes: Number(process.env.PEP_REFRESH_INTERVAL_MINUTES || 360),
    skipImport: false,
    directImport: false,
    strict: false,
    python: process.env.SCREENING_PYTHON || (process.platform === 'win32' ? 'python' : 'python3'),
  };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === '--once') opts.once = true;
    else if (arg === '--skip-import') opts.skipImport = true;
    else if (arg === '--direct-import') opts.directImport = true;
    else if (arg === '--strict') opts.strict = true;
    else if (arg === '--interval' && argv[i + 1]) {
      opts.intervalMinutes = Number(argv[++i]);
    } else if (arg === '--python' && argv[i + 1]) {
      opts.python = argv[++i];
    }
  }
  if (!Number.isFinite(opts.intervalMinutes) || opts.intervalMinutes < 1) {
    throw new Error('--interval must be a positive number of minutes');
  }
  return opts;
}

function runStep(label, cmd, args, env = {}) {
  console.log(`\n[Refresh] --> ${label}`);
  const res = spawnSync(cmd, args, {
    cwd: ROOT_DIR,
    stdio: 'inherit',
    env: { ...process.env, ...env },
  });
  if (res.error) {
    console.error(`[Refresh] [FAIL] ${label}: ${res.error.message}`);
    return false;
  }
  if (res.status !== 0) {
    console.error(`[Refresh] [FAIL] ${label} exited with code ${res.status}`);
    return false;
  }
  console.log(`[Refresh] [OK] ${label}`);
  return true;
}

const SNAPSHOT_MAPPINGS = [
  { file: 'gh_pep_parliament.json', dataset: 'gh-pep-parliament', label: 'Ghana Parliament PEPs (Live)' },
  { file: 'gh_pep_executive.json', dataset: 'gh-pep-executive', label: 'Ghana Executive & Office Holders (Wikidata Live)' },
  { file: 'gh_pep_soe.json', dataset: 'gh-pep-soe', label: 'Ghana SOE & SIGA PEPs (Live)' },
  { file: 'gh_pep_rca.json', dataset: 'gh-pep-rca', label: 'Ghana PEP Relatives & Associates (Wikidata Live)' },
  { file: 'gh_pep_judiciary.json', dataset: 'gh-pep-judiciary', label: 'Ghana Judiciary Supreme Court PEPs (Live)' },
  { file: 'gh_pep_parties.json', dataset: 'gh-pep-parties', label: 'Ghana Political Party Leadership PEPs (Live)' },
  { file: 'gh_pep_local_gov.json', dataset: 'gh-pep-local-gov', label: 'Ghana Sub-National MMDCE PEPs (Live)' },
];

function importDirectInContainer(strict) {
  const sourceImportBin = process.env.SOURCE_IMPORT_BIN || '/app/source-import';
  let imported = 0;
  for (const item of SNAPSHOT_MAPPINGS) {
    const src = path.join(ROOT_DIR, item.file);
    if (!fs.existsSync(src)) {
      console.warn(`[Refresh] Skipping ${item.dataset}: ${item.file} not produced.`);
      continue;
    }
    const parsed = JSON.parse(fs.readFileSync(src, 'utf8'));
    if (!Array.isArray(parsed.records) || parsed.records.length === 0) {
      console.warn(`[Refresh] Skipping ${item.dataset}: ${item.file} has 0 records.`);
      continue;
    }

    const args = [
      '-file', src,
      '-dataset', item.dataset,
      '-snapshot-reference', `Scheduled live refresh ${new Date().toISOString()}`,
    ];
    let ok = runStep(`Import ${item.dataset} (${parsed.records.length} records)`, sourceImportBin, args);
    if (!ok) {
      const regArgs = [
        '-register',
        '-label', item.label,
        '-file', src,
        '-dataset', item.dataset,
      ];
      ok = runStep(`Register & import ${item.dataset}`, sourceImportBin, regArgs);
    }
    if (ok) imported++;
    else if (strict) throw new Error(`Failed to import ${item.dataset}`);
  }
  return imported;
}

function importIntoStandaloneProvider(strict, directImport) {
  if (directImport) {
    return importDirectInContainer(strict);
  }
  const envFile = path.join(ROOT_DIR, '.local', 'provider-dev', 'compose.env');
  if (!fs.existsSync(envFile)) {
    throw new Error('Standalone provider is not initialized (.local/provider-dev/compose.env missing). Run: node docker/setup-provider.mjs');
  }

  const importDir = path.join(ROOT_DIR, '.local', 'provider-dev', 'imports');
  fs.mkdirSync(importDir, { recursive: true });

  const composeBase = [
    'compose',
    '--env-file', envFile,
    '-p', 'screening-provider-dev',
    '-f', path.join(ROOT_DIR, 'docker-compose.provider.yml'),
  ];

  let imported = 0;
  for (const item of SNAPSHOT_MAPPINGS) {
    const src = path.join(ROOT_DIR, item.file);
    if (!fs.existsSync(src)) {
      console.warn(`[Refresh] Skipping ${item.dataset}: ${item.file} not produced.`);
      continue;
    }
    const parsed = JSON.parse(fs.readFileSync(src, 'utf8'));
    if (!Array.isArray(parsed.records) || parsed.records.length === 0) {
      console.warn(`[Refresh] Skipping ${item.dataset}: ${item.file} has 0 records.`);
      continue;
    }

    const dest = path.join(importDir, item.file);
    fs.copyFileSync(src, dest);

    // Try publishing against an existing dataset first; if not registered yet, register it.
    const args = [
      ...composeBase,
      'run', '--rm', 'provider-import',
      '-file', `/input/${item.file}`,
      '-dataset', item.dataset,
      '-snapshot-reference', `Scheduled live refresh ${new Date().toISOString()}`,
    ];
    let ok = runStep(`Import ${item.dataset} (${parsed.records.length} records)`, 'docker', args, {
      PROVIDER_IMPORT_DIR: importDir,
    });
    if (!ok) {
      const regArgs = [
        ...composeBase,
        'run', '--rm', 'provider-import',
        '-register',
        '-label', item.label,
        '-file', `/input/${item.file}`,
        '-dataset', item.dataset,
      ];
      ok = runStep(`Register & import ${item.dataset}`, 'docker', regArgs, {
        PROVIDER_IMPORT_DIR: importDir,
      });
    }
    if (ok) imported++;
    else if (strict) throw new Error(`Failed to import ${item.dataset}`);
  }
  return imported;
}

async function runCycle(opts) {
  const started = new Date().toISOString();
  console.log(`\n========================================================================`);
  console.log(`[Refresh] Starting live standalone provider refresh at ${started}`);
  console.log(`========================================================================`);

  const fetchSteps = [
    ['Parliament MPs (parliament.gh)', 'node', ['scripts/crawl-all-parliament-mps.mjs']],
    ['Wikidata Ghana PEPs & RCAs', 'node', ['scripts/sync-wikidata-ghana.mjs']],
    ['Judiciary Supreme Court (live)', opts.python, ['scripts/scrape_judiciary_live.py']],
    ['Political Parties Leadership (live)', opts.python, ['scripts/scrape_political_parties_live.py']],
    ['SIGA / SOE Leadership (live)', opts.python, ['scripts/scrape_siga_scrapling.py']],
    ['Districts MMDCEs (live)', 'node', ['scripts/scrape-live-mmdces.mjs']],
  ];

  let failures = 0;
  for (const [label, cmd, args] of fetchSteps) {
    const ok = runStep(label, cmd, args);
    if (!ok) {
      failures++;
      if (opts.strict) {
        throw new Error(`Strict mode aborted on failed step: ${label}`);
      }
    }
  }

  runStep('Build unified PEP snapshots', 'node', ['scripts/build-unified-ghana-peps.mjs']);
  runStep('Build sub-national MMDCE snapshot', 'node', ['scripts/scrape-and-build-subnational-peps.mjs']);

  if (!opts.skipImport) {
    const imported = importIntoStandaloneProvider(opts.strict, opts.directImport);
    console.log(`\n[Refresh] Imported ${imported} live dataset(s) into standalone provider.`);
  } else {
    console.log('\n[Refresh] --skip-import set; skipped Docker provider-import.');
  }

  console.log(`[Refresh] Cycle finished at ${new Date().toISOString()} (fetch failures: ${failures}).`);
}

async function main() {
  const opts = parseArgs(process.argv.slice(2));
  if (opts.once) {
    await runCycle(opts);
    return;
  }
  console.log(`[Refresh] Running in scheduled loop mode every ${opts.intervalMinutes} minute(s).`);
  while (true) {
    try {
      await runCycle(opts);
    } catch (err) {
      console.error(`[Refresh] Cycle error: ${err.message}`);
    }
    await new Promise(resolve => setTimeout(resolve, opts.intervalMinutes * 60_000));
  }
}

main().catch(err => {
  console.error(`[Refresh] Fatal error: ${err.message}`);
  process.exit(1);
});
