#!/usr/bin/env node
import { spawnSync } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

/**
 * Configurable Scheduled Live Refresh Pipeline & Control Server
 * for the Standalone Screening Provider.
 *
 * Usage:
 *   node refresh-standalone-provider.mjs --once
 *   node refresh-standalone-provider.mjs --direct-import
 *   node refresh-standalone-provider.mjs --interval 360 --control-port 8092
 */

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const PEP_DIR = __dirname;
const WORK_DIR = path.resolve(process.env.PEP_WORK_DIR || process.cwd());
const REPO_ROOT = path.resolve(__dirname, '..', '..', '..', '..');
const DEFAULT_CONFIG_PATH = path.join(PEP_DIR, 'refresh-config.json');
const RUNTIME_CONFIG_PATH = process.env.PEP_CONFIG_PATH || path.join(WORK_DIR, 'refresh-config.json');

const SOURCE_DEFS = [
  {
    key: 'parliament',
    label: 'Parliament MPs (parliament.gh)',
    cmd: () => 'node',
    args: () => [path.join(PEP_DIR, 'crawl-all-parliament-mps.mjs')],
    builds: ['unified'],
  },
  {
    key: 'wikidata',
    label: 'Wikidata Ghana PEPs & RCAs',
    cmd: () => 'node',
    args: () => [path.join(PEP_DIR, 'sync-wikidata-ghana.mjs')],
    builds: ['unified'],
  },
  {
    key: 'judiciary',
    label: 'Judiciary Supreme Court (live)',
    cmd: (opts) => opts.python,
    args: () => [path.join(PEP_DIR, 'scrape_judiciary_live.py')],
    builds: [],
  },
  {
    key: 'parties',
    label: 'Political Parties Leadership (live)',
    cmd: (opts) => opts.python,
    args: () => [path.join(PEP_DIR, 'scrape_political_parties_live.py')],
    builds: [],
  },
  {
    key: 'siga',
    label: 'SIGA / SOE Leadership (live)',
    cmd: (opts) => opts.python,
    args: () => [path.join(PEP_DIR, 'scrape_siga_scrapling.py')],
    builds: ['unified'],
  },
  {
    key: 'mmdce',
    label: 'Districts MMDCEs (live)',
    cmd: () => 'node',
    args: () => [path.join(PEP_DIR, 'scrape-live-mmdces.mjs')],
    builds: ['subnational'],
  },
  {
    key: 'worldbank',
    label: 'World Bank Debarred Firms & Individuals (live)',
    cmd: () => 'node',
    args: () => [path.join(PEP_DIR, 'crawl-worldbank-debarred.mjs')],
    builds: [],
  },
  {
    key: 'traditional',
    label: 'Ghana Traditional Rulers (Wikidata Live)',
    cmd: () => 'node',
    args: () => [path.join(PEP_DIR, 'crawl-extended-peps.mjs'), '--only', 'traditional'],
    builds: [],
  },
  {
    key: 'security',
    label: 'Ghana Senior Security Officials (Wikidata Live)',
    cmd: () => 'node',
    args: () => [path.join(PEP_DIR, 'crawl-extended-peps.mjs'), '--only', 'security'],
    builds: [],
  },
  {
    key: 'foreign_heads',
    label: 'Global Foreign Heads of State & Gov (Wikidata Live)',
    cmd: () => 'node',
    args: () => [path.join(PEP_DIR, 'crawl-extended-peps.mjs'), '--only', 'foreign_heads'],
    builds: [],
  },
];

const XML_SOURCES = ['un-consolidated', 'ofac-sdn', 'uksl'];

const SNAPSHOT_MAPPINGS = [
  { file: 'gh_pep_parliament.json', dataset: 'gh-pep-parliament', label: 'Ghana Parliament PEPs (Live)' },
  { file: 'gh_pep_executive.json', dataset: 'gh-pep-executive', label: 'Ghana Executive & Office Holders (Wikidata Live)' },
  { file: 'gh_pep_soe.json', dataset: 'gh-pep-soe', label: 'Ghana SOE & SIGA PEPs (Live)' },
  { file: 'gh_pep_rca.json', dataset: 'gh-pep-rca', label: 'Ghana PEP Relatives & Associates (Wikidata Live)' },
  { file: 'gh_pep_judiciary.json', dataset: 'gh-pep-judiciary', label: 'Ghana Judiciary Supreme Court PEPs (Live)' },
  { file: 'gh_pep_parties.json', dataset: 'gh-pep-parties', label: 'Ghana Political Party Leadership PEPs (Live)' },
  { file: 'gh_pep_local_gov.json', dataset: 'gh-pep-local-gov', label: 'Ghana Sub-National MMDCE PEPs (Live)' },
  { file: 'worldbank_fixture.json', dataset: 'worldbank-debarred', label: 'World Bank Ineligible Firms & Individuals', category: 'regulatory_warning' },
  { file: 'gh_pep_traditional.json', dataset: 'gh-pep-traditional', label: 'Ghana Traditional Rulers (Wikidata Live)' },
  { file: 'gh_pep_security.json', dataset: 'gh-pep-security', label: 'Ghana Senior Security Officials (Wikidata Live)' },
  { file: 'global_pep_heads.json', dataset: 'global-pep-heads', label: 'Global Foreign Heads of State & Government (Wikidata Live)' },
];

function parseArgs(argv) {
  const opts = {
    once: false,
    intervalMinutes: process.env.PEP_REFRESH_INTERVAL_MINUTES ? Number(process.env.PEP_REFRESH_INTERVAL_MINUTES) : null,
    controlPort: Number(process.env.PEP_CONTROL_PORT || 0),
    controlHost: process.env.PEP_CONTROL_HOST || '127.0.0.1',
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
    else if (arg === '--interval' && argv[i + 1]) opts.intervalMinutes = Number(argv[++i]);
    else if (arg === '--control-port' && argv[i + 1]) opts.controlPort = Number(argv[++i]);
    else if (arg === '--python' && argv[i + 1]) opts.python = argv[++i];
  }
  if (opts.intervalMinutes !== null && (!Number.isFinite(opts.intervalMinutes) || opts.intervalMinutes < 1)) {
    throw new Error('--interval must be a positive number of minutes');
  }
  return opts;
}

function loadConfig(defaultOverrideMinutes) {
  const base = JSON.parse(fs.readFileSync(DEFAULT_CONFIG_PATH, 'utf8'));
  const config = { sources: { ...base.sources } };
  if (fs.existsSync(RUNTIME_CONFIG_PATH)) {
    const saved = JSON.parse(fs.readFileSync(RUNTIME_CONFIG_PATH, 'utf8'));
    for (const [k, v] of Object.entries(saved.sources || {})) {
      if (config.sources[k]) {
        config.sources[k] = { ...config.sources[k], ...v };
      }
    }
  }
  for (const def of SOURCE_DEFS) {
    const envInterval = process.env[`PEP_REFRESH_${def.key.toUpperCase()}_MINUTES`];
    if (envInterval && Number.isFinite(Number(envInterval)) && Number(envInterval) >= 1) {
      config.sources[def.key].interval_minutes = Number(envInterval);
    } else if (defaultOverrideMinutes) {
      config.sources[def.key].interval_minutes = defaultOverrideMinutes;
    }
    const envEnabled = process.env[`PEP_REFRESH_${def.key.toUpperCase()}_ENABLED`];
    if (envEnabled === 'true' || envEnabled === 'false') {
      config.sources[def.key].enabled = envEnabled === 'true';
    }
  }
  for (const xmlKey of XML_SOURCES) {
    const envKey = xmlKey.toUpperCase().replace(/-/g, '_');
    const envInterval = process.env[`PEP_REFRESH_${envKey}_MINUTES`];
    if (envInterval && Number.isFinite(Number(envInterval)) && Number(envInterval) >= 15) {
      config.sources[xmlKey].interval_minutes = Number(envInterval);
    }
    const envEnabled = process.env[`PEP_REFRESH_${envKey}_ENABLED`];
    if (envEnabled === 'true' || envEnabled === 'false') {
      config.sources[xmlKey].enabled = envEnabled === 'true';
    }
  }
  return config;
}

function saveConfig(config) {
  fs.mkdirSync(path.dirname(RUNTIME_CONFIG_PATH), { recursive: true });
  fs.writeFileSync(RUNTIME_CONFIG_PATH, JSON.stringify(config, null, 2), 'utf8');
}

function runStep(label, cmd, args, env = {}, cwd = WORK_DIR) {
  console.log(`\n[Refresh] --> ${label}`);
  const res = spawnSync(cmd, args, {
    cwd,
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

function readSourceDSN() {
  const dsnFile = process.env.SOURCE_DATABASE_URL_FILE;
  return (dsnFile && fs.existsSync(dsnFile))
    ? fs.readFileSync(dsnFile, 'utf8').trim()
    : (process.env.SOURCE_DATABASE_URL || '').trim();
}

function syncXmlSourceConfig(xmlKey, cfg) {
  const dsn = readSourceDSN();
  if (!dsn) return;
  const mins = Math.max(15, Math.min(525600, Number(cfg.interval_minutes) || 720));
  const enabled = cfg.enabled ? 'true' : 'false';
  const sql = `
    UPDATE screening.source_connections
    SET enabled=${enabled}, poll_interval=make_interval(mins=>${mins})
    WHERE owner_scope='00000000-0000-0000-0000-000000000000' AND dataset_id='${xmlKey}' AND rights_state='approved';
  `;
  spawnSync('psql', [dsn, '-v', 'ON_ERROR_STOP=1', '-c', sql], { cwd: WORK_DIR, stdio: 'inherit' });
}

function triggerXmlSources(xmlKeys) {
  const dsn = readSourceDSN();
  if (!dsn || xmlKeys.length === 0) return;
  const inList = xmlKeys.map(k => `'${k}'`).join(',');
  const sql = `
    UPDATE screening.source_connections
    SET enabled=true, next_check_at=now(), failures=0, lease_token=NULL, lease_until=NULL, fence=fence+1
    WHERE owner_scope='00000000-0000-0000-0000-000000000000' AND dataset_id IN (${inList}) AND rights_state='approved';
  `;
  spawnSync('psql', [dsn, '-v', 'ON_ERROR_STOP=1', '-c', sql], { cwd: WORK_DIR, stdio: 'inherit' });
}

function readXmlSourceStatuses() {
  const dsn = readSourceDSN();
  if (!dsn) return {};
  const inList = XML_SOURCES.map(k => `'${k}'`).join(',');
  const sql = `SELECT json_agg(json_build_object('dataset_id',dataset_id,'enabled',enabled,'last_run_at',last_success_at,'next_run_at',next_check_at,'last_status',CASE WHEN last_error='' THEN 'ok' ELSE last_error END)) FROM screening.source_connections WHERE owner_scope='00000000-0000-0000-0000-000000000000' AND dataset_id IN (${inList});`;
  const res = spawnSync('psql', [dsn, '-t', '-A', '-c', sql], { cwd: WORK_DIR, encoding: 'utf8' });
  if (res.status !== 0 || !res.stdout) return {};
  try {
    const rows = JSON.parse(res.stdout.trim()) || [];
    const out = {};
    for (const r of rows) {
      out[r.dataset_id] = {
        last_run_at: r.last_run_at,
        next_run_at: r.next_run_at,
        last_status: r.last_status || 'pending',
      };
    }
    return out;
  } catch {
    return {};
  }
}

function ensureDatasetRegisteredInContainer(item, parserVersion) {
  const dsn = readSourceDSN();
  if (!dsn) return;

  const safeLabel = item.label.replace(/'/g, "''");
  const category = item.category || 'pep_evidence';
  const parser = (parserVersion || 'gh-pep-v1').replace(/'/g, "''");
  const sql = `
    INSERT INTO screening.source_datasets(owner_scope, id, label, category, enabled)
    VALUES ('00000000-0000-0000-0000-000000000000', '${item.dataset}', '${safeLabel}', '${category}', true)
    ON CONFLICT (owner_scope, id) DO UPDATE SET enabled=true, label=excluded.label;
    INSERT INTO screening.source_connections(owner_scope, dataset_id, connector, config, enabled, rights_state, rights_review_reference, max_stale)
    VALUES ('00000000-0000-0000-0000-000000000000', '${item.dataset}', '${item.dataset}', '{"parser":"${parser}"}', true, 'approved', 'Live PEP scraper intake', interval '30 days')
    ON CONFLICT (owner_scope, dataset_id) DO UPDATE SET enabled=true, rights_state='approved', connector=excluded.connector, config=excluded.config;
  `;
  const res = spawnSync('psql', [dsn, '-v', 'ON_ERROR_STOP=1', '-c', sql], {
    cwd: WORK_DIR,
    stdio: 'inherit',
  });
  if (res.status !== 0) {
    console.warn(`[Refresh] WARN: Could not pre-register ${item.dataset} via psql (status ${res.status}).`);
  }
}

function importDirectInContainer(strict) {
  const sourceImportBin = process.env.SOURCE_IMPORT_BIN || '/app/source-import';
  let imported = 0;
  for (const item of SNAPSHOT_MAPPINGS) {
    const src = path.join(WORK_DIR, item.file);
    if (!fs.existsSync(src)) continue;
    const parsed = JSON.parse(fs.readFileSync(src, 'utf8'));
    if (!Array.isArray(parsed.records) || parsed.records.length === 0) continue;

    ensureDatasetRegisteredInContainer(item, parsed.parser);

    const args = [
      '-file', src,
      '-dataset', item.dataset,
      '-snapshot-reference', `Scheduled live refresh ${new Date().toISOString()}`,
    ];
    const ok = runStep(`Import ${item.dataset} (${parsed.records.length} records)`, sourceImportBin, args);
    if (ok) imported++;
    else if (strict) throw new Error(`Failed to import ${item.dataset}`);
  }
  return imported;
}

function importViaCompose(strict) {
  const envFile = path.join(REPO_ROOT, '.local', 'provider-dev', 'compose.env');
  if (!fs.existsSync(envFile)) {
    throw new Error('Standalone provider is not initialized (.local/provider-dev/compose.env missing). Run: node docker/setup-provider.mjs');
  }
  const importDir = path.join(REPO_ROOT, '.local', 'provider-dev', 'imports');
  fs.mkdirSync(importDir, { recursive: true });
  const composeBase = [
    'compose', '--env-file', envFile, '-p', 'screening-provider-dev',
    '-f', path.join(REPO_ROOT, 'docker-compose.provider.yml'),
  ];

  let imported = 0;
  for (const item of SNAPSHOT_MAPPINGS) {
    const src = path.join(WORK_DIR, item.file);
    if (!fs.existsSync(src)) continue;
    const parsed = JSON.parse(fs.readFileSync(src, 'utf8'));
    if (!Array.isArray(parsed.records) || parsed.records.length === 0) continue;

    fs.copyFileSync(src, path.join(importDir, item.file));
    const args = [
      ...composeBase, 'run', '--rm', 'provider-import',
      '-file', `/input/${item.file}`, '-dataset', item.dataset,
      '-snapshot-reference', `Scheduled live refresh ${new Date().toISOString()}`,
    ];
    let ok = runStep(`Import ${item.dataset} (${parsed.records.length} records)`, 'docker', args, {
      PROVIDER_IMPORT_DIR: importDir,
    }, REPO_ROOT);
    if (!ok) {
      const regArgs = [
        ...composeBase, 'run', '--rm', 'provider-import',
        '-register', '-label', item.label, '-file', `/input/${item.file}`, '-dataset', item.dataset,
      ];
      ok = runStep(`Register & import ${item.dataset}`, 'docker', regArgs, {
        PROVIDER_IMPORT_DIR: importDir,
      }, REPO_ROOT);
    }
    if (ok) imported++;
    else if (strict) throw new Error(`Failed to import ${item.dataset}`);
  }
  return imported;
}

function readAdminToken() {
  const file = process.env.PEP_REFRESH_ADMIN_TOKEN_FILE;
  if (file && fs.existsSync(file)) return fs.readFileSync(file, 'utf8').trim();
  return (process.env.PEP_REFRESH_ADMIN_TOKEN || '').trim();
}

function authorizeRequest(req, expectedToken) {
  if (expectedToken.length < 32) return false;
  const header = req.headers.authorization || '';
  if (!header.startsWith('Bearer ')) return false;
  const provided = Buffer.from(header.slice(7));
  const expected = Buffer.from(expectedToken);
  if (provided.length !== expected.length) return false;
  return crypto.timingSafeEqual(provided, expected);
}

function startControlServer(opts, state, triggerFn) {
  const token = readAdminToken();
  const server = http.createServer((req, res) => {
    const url = new URL(req.url || '/', `http://${req.headers.host || 'localhost'}`);
    const send = (status, body) => {
      res.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'private, no-store' });
      res.end(JSON.stringify(body));
    };

    if (req.method === 'GET' && url.pathname === '/healthz') {
      return send(200, { status: 'alive', running: state.running });
    }
    if (!authorizeRequest(req, token)) {
      return send(token.length < 32 ? 503 : 401, {
        error: token.length < 32 ? 'refresh_control_unconfigured' : 'unauthorized',
      });
    }
    if (req.method === 'GET' && url.pathname === '/v1/refresh/status') {
      return send(200, {
        running: state.running,
        config: state.config,
        sources: { ...state.status, ...readXmlSourceStatuses() },
      });
    }
    if (req.method === 'PUT' && url.pathname === '/v1/refresh/config') {
      let raw = '';
      req.on('data', chunk => {
        raw += chunk;
        if (raw.length > 16384) req.destroy();
      });
      req.on('end', () => {
        try {
          const body = JSON.parse(raw || '{}');
          if (!body.sources || typeof body.sources !== 'object') {
            return send(400, { error: 'invalid_config_payload' });
          }
          for (const [k, patch] of Object.entries(body.sources)) {
            if (!state.config.sources[k] || typeof patch !== 'object') {
              return send(400, { error: `unknown_source:${k}` });
            }
            if (patch.enabled !== undefined) {
              if (typeof patch.enabled !== 'boolean') return send(400, { error: 'invalid_enabled' });
              state.config.sources[k].enabled = patch.enabled;
            }
            if (patch.interval_minutes !== undefined) {
              const mins = Number(patch.interval_minutes);
              if (!Number.isInteger(mins) || mins < 1 || mins > 43200) {
                return send(400, { error: 'invalid_interval_minutes' });
              }
              state.config.sources[k].interval_minutes = mins;
            }
          }
          saveConfig(state.config);
          for (const xmlKey of XML_SOURCES) {
            if (body.sources[xmlKey]) syncXmlSourceConfig(xmlKey, state.config.sources[xmlKey]);
          }
          return send(200, { config: state.config });
        } catch {
          return send(400, { error: 'invalid_json' });
        }
      });
      return;
    }
    if (req.method === 'POST' && url.pathname === '/v1/refresh/trigger') {
      let raw = '';
      req.on('data', chunk => {
        raw += chunk;
        if (raw.length > 4096) req.destroy();
      });
      req.on('end', () => {
        try {
          const body = raw ? JSON.parse(raw) : {};
          const allKeys = [...SOURCE_DEFS.map(s => s.key), ...XML_SOURCES];
          const keys = Array.isArray(body.sources) ? body.sources : allKeys;
          for (const k of keys) {
            if (!state.config.sources[k]) return send(400, { error: `unknown_source:${k}` });
          }
          const xmlKeys = keys.filter(k => XML_SOURCES.includes(k));
          const pepKeys = keys.filter(k => !XML_SOURCES.includes(k));
          if (xmlKeys.length > 0) triggerXmlSources(xmlKeys);
          if (pepKeys.length > 0) {
            if (state.running) return send(409, { error: 'refresh_already_running' });
            setImmediate(() => triggerFn(pepKeys));
          }
          return send(202, { accepted: true, sources: keys });
        } catch {
          return send(400, { error: 'invalid_json' });
        }
      });
      return;
    }
    return send(404, { error: 'not_found' });
  });

  server.listen(opts.controlPort, opts.controlHost, () => {
    console.log(`[Refresh] Control API listening on http://${opts.controlHost}:${opts.controlPort}`);
  });
}

async function runSources(opts, state, targetKeys) {
  if (state.running) return;
  state.running = true;
  try {
    fs.mkdirSync(WORK_DIR, { recursive: true });
    let needUnified = false;
    let needSubnational = false;

    for (const def of SOURCE_DEFS) {
      if (!targetKeys.includes(def.key)) continue;
      const ok = runStep(def.label, def.cmd(opts), def.args());
      const now = Date.now();
      const mins = state.config.sources[def.key].interval_minutes;
      state.status[def.key] = {
        last_run_at: new Date(now).toISOString(),
        next_run_at: new Date(now + mins * 60_000).toISOString(),
        last_status: ok ? 'ok' : 'failed',
      };
      if (def.builds.includes('unified')) needUnified = true;
      if (def.builds.includes('subnational')) needSubnational = true;
      if (!ok && opts.strict) throw new Error(`Strict mode aborted on: ${def.key}`);
    }

    if (needUnified) runStep('Build unified PEP snapshots', 'node', [path.join(PEP_DIR, 'build-unified-ghana-peps.mjs')]);
    if (needSubnational) runStep('Build sub-national MMDCE snapshot', 'node', [path.join(PEP_DIR, 'scrape-and-build-subnational-peps.mjs')]);

    if (!opts.skipImport) {
      const imported = opts.directImport ? importDirectInContainer(opts.strict) : importViaCompose(opts.strict);
      console.log(`\n[Refresh] Imported ${imported} live dataset(s) into standalone provider.`);
    }
  } finally {
    state.running = false;
  }
}

async function main() {
  const opts = parseArgs(process.argv.slice(2));
  const config = loadConfig(opts.intervalMinutes);
  const state = {
    running: false,
    config,
    status: Object.fromEntries(SOURCE_DEFS.map(d => [d.key, { last_run_at: null, next_run_at: new Date(0).toISOString(), last_status: 'pending' }])),
  };

  if (opts.once) {
    const enabledKeys = SOURCE_DEFS.filter(d => state.config.sources[d.key].enabled).map(d => d.key);
    await runSources(opts, state, enabledKeys);
    return;
  }

  if (opts.controlPort > 0) {
    startControlServer(opts, state, (keys) => runSources(opts, state, keys));
  }

  console.log('[Refresh] Scheduler started with per-source intervals:', JSON.stringify(state.config.sources));
  while (true) {
    if (!state.running) {
      const now = Date.now();
      const dueKeys = SOURCE_DEFS.filter(d => {
        const cfg = state.config.sources[d.key];
        const nextMs = Date.parse(state.status[d.key].next_run_at || 0);
        return cfg.enabled && now >= nextMs;
      }).map(d => d.key);
      if (dueKeys.length > 0) {
        try {
          await runSources(opts, state, dueKeys);
        } catch (err) {
          console.error(`[Refresh] Cycle error: ${err.message}`);
        }
      }
    }
    await new Promise(resolve => setTimeout(resolve, 60_000));
  }
}

main().catch(err => {
  console.error(`[Refresh] Fatal error: ${err.message}`);
  process.exit(1);
});
