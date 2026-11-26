#!/usr/bin/env node

import { randomBytes } from 'node:crypto';
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { pathToFileURL, fileURLToPath } from 'node:url';
import path from 'node:path';
import { buildScenarioDefinitions, portableModel } from './provision-fraud-scenarios-ec2.mjs';
import { exchangeClientProofDetails } from '../backend/auth-service/scripts/client-proof.mjs';

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const DEFAULT_MODEL_FILE = path.resolve(SCRIPT_DIR, '../fraud-data-model.json');
const DEFAULT_CLIENT_KEY_DIR = path.resolve(SCRIPT_DIR, '../.local/client-auth');
export const CLIENT_SCOPES = ['models:read', 'scenarios:read', 'ingest:write', 'decisions:evaluate'];
const REQUEST_TIMEOUT_MS = 30_000;
const ASYNC_POLL_TIMEOUT_MS = 120_000;
const ASYNC_POLL_INTERVAL_MS = 1_000;
const DEFAULT_TOTAL_TRANSACTIONS = 200_000;
const DEFAULT_DURATION_HOURS = 24;
const DEFAULT_BATCH_SIZE = 10;
const MAX_INGEST_BATCH_SIZE = 500;
const EVALUATION_CONCURRENCY = 5;

class TokenExchangeError extends Error {}
class HttpRequestError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}

function canonicalize(value) {
  if (Array.isArray(value)) return value.map(canonicalize);
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.keys(value).sort().map((key) => [key, canonicalize(value[key])]));
  }
  return value;
}

function canonicalJSON(value) {
  return JSON.stringify(canonicalize(value));
}

function parseOrigin(host) {
  let url;
  try {
    url = new URL(/^[a-z][a-z\d+.-]*:\/\//i.test(host) ? host : `http://${host}`);
  } catch {
    throw new Error('--host must be an origin such as 10.0.0.10:8088 or https://fraud.example:8088');
  }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password ||
      (url.pathname !== '/' && url.pathname !== '') || url.search || url.hash || !url.port) {
    throw new Error('--host must be an HTTP(S) origin with an explicit port and no path, query, or fragment');
  }
  return url.origin;
}

function parseArgs(args) {
  const parsed = {
    host: undefined,
    tenantId: undefined,
    modelFile: DEFAULT_MODEL_FILE,
    runId: `fs-${Date.now()}-${randomBytes(4).toString('hex')}`,
    lateCallbackSeconds: 300,
    structuringLimit: 1000,
    nearLimitPercent: 10,
    confirmWrite: false,
    dryRun: false,
    allowModelMismatch: false,
    deterministic: false,
    totalTransactions: DEFAULT_TOTAL_TRANSACTIONS,
    durationHours: DEFAULT_DURATION_HOURS,
    durationMinutes: undefined,
    batchSize: DEFAULT_BATCH_SIZE,
    clientId: undefined,
    keyDir: DEFAULT_CLIENT_KEY_DIR,
    tokenUrl: undefined,
    resume: false,
    checkpointFile: undefined,
  };

  const valuedOptions = new Map([
    ['--host', 'host'],
    ['--tenant-id', 'tenantId'],
    ['--model-file', 'modelFile'],
    ['--run-id', 'runId'],
    ['--late-callback-seconds', 'lateCallbackSeconds'],
    ['--structuring-limit', 'structuringLimit'],
    ['--structuring-band-percent', 'nearLimitPercent'],
    ['--total-transactions', 'totalTransactions'],
    ['--duration-hours', 'durationHours'],
    ['--duration-minutes', 'durationMinutes'],
    ['--batch-size', 'batchSize'],
    ['--client-id', 'clientId'],
    ['--key-dir', 'keyDir'],
    ['--token-url', 'tokenUrl'],
    ['--checkpoint-file', 'checkpointFile'],
  ]);

  for (let index = 0; index < args.length; index += 1) {
    const option = args[index];
    if (option === '--confirm-write') {
      parsed.confirmWrite = true;
      continue;
    }
    if (option === '--dry-run') {
      parsed.dryRun = true;
      continue;
    }
    if (option === '--allow-model-mismatch') {
      parsed.allowModelMismatch = true;
      continue;
    }
    if (option === '--deterministic') {
      parsed.deterministic = true;
      continue;
    }
    if (option === '--resume') {
      parsed.resume = true;
      continue;
    }
    if (option === '--help' || option === '-h') {
      parsed.help = true;
      continue;
    }
    const key = valuedOptions.get(option);
    if (!key) throw new Error(`Unknown option: ${option}`);
    const value = args[index + 1];
    if (!value || value.startsWith('--')) throw new Error(`Missing value for ${option}`);

    if (['lateCallbackSeconds', 'structuringLimit', 'nearLimitPercent', 'totalTransactions', 'durationHours', 'durationMinutes', 'batchSize'].includes(key)) {
      parsed[key] = Number(value);
    } else {
      parsed[key] = value;
    }
    index += 1;
  }

  if (parsed.help) return parsed;

  if (!Number.isInteger(parsed.lateCallbackSeconds) || parsed.lateCallbackSeconds <= 0) {
    throw new Error('--late-callback-seconds must be a positive integer');
  }
  if (!Number.isFinite(parsed.structuringLimit) || parsed.structuringLimit <= 0) {
    throw new Error('--structuring-limit must be a positive number');
  }
  if (!Number.isFinite(parsed.nearLimitPercent) || parsed.nearLimitPercent <= 0 || parsed.nearLimitPercent > 100) {
    throw new Error('--structuring-band-percent must be greater than 0 and no more than 100');
  }
  if (!Number.isInteger(parsed.totalTransactions) || parsed.totalTransactions <= 0) {
    throw new Error('--total-transactions must be a positive integer');
  }
  if (!Number.isInteger(parsed.batchSize) || parsed.batchSize < 1 || parsed.batchSize > MAX_INGEST_BATCH_SIZE) {
    throw new Error('--batch-size must be an integer between 1 and 500');
  }
  if (parsed.resume) {
    throw new Error('--resume is unavailable: decision requests are not idempotent and interrupted batches require manual reconciliation.');
  }
  if (!Number.isFinite(parsed.durationHours) || parsed.durationHours <= 0) {
    throw new Error('--duration-hours must be a positive number');
  }
  if (parsed.durationMinutes !== undefined && (!Number.isFinite(parsed.durationMinutes) || parsed.durationMinutes <= 0)) {
    throw new Error('--duration-minutes must be a positive number');
  }
  if (!/^[A-Za-z0-9][A-Za-z0-9_-]{0,40}$/.test(parsed.runId)) {
    throw new Error('--run-id must be 1-41 letters, numbers, underscores, or hyphens');
  }
  if (!parsed.dryRun || parsed.host) {
    if (!parsed.host) throw new Error('Required option missing: --host ip:port');
    parsed.origin = parseOrigin(parsed.host);
  }
  if (!parsed.dryRun || parsed.tenantId) {
    if (!parsed.tenantId) throw new Error('Required option missing: --tenant-id');
    if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(parsed.tenantId)) {
      throw new Error('--tenant-id must be a UUID');
    }
    parsed.tenantId = parsed.tenantId.toLowerCase();
  }
  if (!parsed.dryRun && !parsed.clientId) throw new Error('Required option missing: --client-id');
  if (parsed.clientId) {
    if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(parsed.clientId)) {
      throw new Error('--client-id must be a UUID');
    }
    parsed.clientId = parsed.clientId.toLowerCase();
  }
  if (!parsed.dryRun && !parsed.confirmWrite) {
    throw new Error('This test writes persistent fixture records and decisions. Review the warnings, then pass --confirm-write.');
  }

  parsed.durationMs = parsed.durationMinutes
    ? Math.round(parsed.durationMinutes * 60 * 1000)
    : Math.round(parsed.durationHours * 3600 * 1000);

  parsed.modelFile = path.resolve(parsed.modelFile);
  parsed.keyDir = path.resolve(parsed.keyDir);
  if (parsed.origin) parsed.tokenUrl ??= `${parsed.origin}/oauth/token`;
  parsed.checkpointFile = parsed.checkpointFile
    ? path.resolve(parsed.checkpointFile)
    : path.resolve(SCRIPT_DIR, `../.local/fraud-simulation-${parsed.runId}.json`);

  return parsed;
}

function usage() {
  return `Usage:
  node docker/test-fraud-scenarios-ec2.mjs \\
    --host ip:port --tenant-id UUID --client-id UUID --confirm-write [options]

Core Options:
  --host ip:port                 Gateway origin (defaults to http:// when scheme is omitted)
  --tenant-id UUID               Existing tenant with the fraud model and published scenarios
  --total-transactions N         Total transactions to submit (default: 200000)
  --duration-hours N             Duration in hours to pace transactions (default: 24)
  --duration-minutes N           Override duration in minutes (useful for testing)
  --batch-size N                 Transaction batch size per HTTP request (default: 10)
  --model-file PATH              Portable model JSON (default: ../fraud-data-model.json)
  --allow-model-mismatch         Continue despite a differing tenant model (unsafe; see warning)
  --run-id ID                    Fixture prefix (generated when omitted)
  --confirm-write                Required; records and decisions persist in the tenant
  --dry-run                      Simulate traffic locally without making API requests
  --deterministic                Run legacy 1-per-rule deterministic fire/no-fire test instead of 24h simulation
  --checkpoint-file PATH         Custom path for run progress checkpoint (not resumable)
  --help                         Show this help

Auth Options:
  --client-id UUID               Existing registered auth client ID
  --key-dir DIRECTORY            Client private-key directory (default: .local/client-auth)
  --token-url URL                Public OAuth endpoint (default: <host>/oauth/token)

Requested scopes: ${CLIENT_SCOPES.join(' ')}. Tokens are kept in memory and refreshed before expiry.

Rule Configuration:
  --late-callback-seconds N      Match scenario provisioning (default: 300)
  --structuring-limit N          Match scenario provisioning (default: 1000)
  --structuring-band-percent N   Match scenario provisioning (default: 10)
`;
}

export class AuthTokenManager {
  constructor({ keyDir, clientId, tokenUrl, exchange = exchangeClientProofDetails, now = Date.now }) {
    this.keyDir = keyDir;
    this.clientId = clientId;
    this.tokenUrl = tokenUrl;
    this.exchange = exchange;
    this.now = now;
    this.accessToken = null;
    this.refreshAt = 0;
    this.refreshing = null;
  }

  async refreshToken() {
    if (this.refreshing) return this.refreshing;
    this.refreshing = Promise.resolve().then(async () => {
      const startedAt = this.now();
      try {
        const result = await this.exchange(this.keyDir, this.clientId, this.tokenUrl, CLIENT_SCOPES);
        const lifetime = result.expires_in * 1000;
        this.refreshAt = startedAt + lifetime - Math.min(120_000, lifetime / 10);
        if (this.now() >= startedAt + lifetime) throw new Error('Client token expired during exchange.');
        this.accessToken = result.access_token;
        return this.accessToken;
      } catch (error) {
        throw new TokenExchangeError(`Client token exchange failed: ${error.message}`, { cause: error });
      }
    });
    try {
      return await this.refreshing;
    } finally {
      this.refreshing = null;
    }
  }

  async getToken() {
    if (!this.accessToken || this.now() >= this.refreshAt) {
      await this.refreshToken();
    }
    return this.accessToken;
  }
}

// ---------------------------------------------------------------------------
// Diurnal Varying Frequency Pacer
// ---------------------------------------------------------------------------

export function calculateDiurnalMultiplier(elapsedMs, totalDurationMs) {
  // Simulates a 24-hour diurnal cycle with night lull, afternoon peak, and evening surge
  const dayMs = 24 * 3600 * 1000;
  const fraction = (elapsedMs % dayMs) / dayMs;
  // Diurnal curve: trough around 03:00 (f ~ 0.125), peak around 15:00 (f ~ 0.625)
  const multiplier = 1.0 + 0.65 * Math.sin(2 * Math.PI * (fraction - 0.25)) + 0.15 * Math.sin(4 * Math.PI * (fraction - 0.25));
  return Math.max(0.3, Math.min(2.0, multiplier));
}

export class TrafficPacer {
  constructor({ totalTransactions, durationMs, batchSize = 10 }) {
    this.totalTransactions = totalTransactions;
    this.durationMs = durationMs;
    this.batchSize = batchSize;
    this.totalBatches = Math.ceil(totalTransactions / batchSize);
    this.meanBatchRatePerSec = this.totalBatches / (durationMs / 1000);
    this.burstRemaining = 0;
  }

  nextDelayMs(elapsedMs) {
    if (this.burstRemaining > 0) {
      this.burstRemaining -= 1;
      return 25 + Math.floor(Math.random() * 35); // 25-60ms burst speed
    }

    // 1.5% chance to trigger a micro-burst
    if (Math.random() < 0.015) {
      this.burstRemaining = 4 + Math.floor(Math.random() * 8);
      return 30;
    }

    const multiplier = calculateDiurnalMultiplier(elapsedMs, this.durationMs);
    const currentRate = Math.max(0.1, this.meanBatchRatePerSec * multiplier);
    const meanIntervalMs = 1000 / currentRate;

    // Poisson inter-arrival time with exponential distribution
    const uniform = Math.max(0.0001, Math.min(0.9999, Math.random()));
    const sampledDelay = -meanIntervalMs * Math.log(uniform);

    // Clamp between 5ms and 15,000ms
    return Math.max(5, Math.min(15_000, Math.round(sampledDelay)));
  }
}

// ---------------------------------------------------------------------------
// Realistic Data Model & Scenario Generation (Organic Triggering)
// ---------------------------------------------------------------------------

function defaultValue(field, recordId, now) {
  const enumValue = field.enum_values?.[0]?.value;
  if (enumValue !== undefined) return enumValue;
  switch (field.data_type) {
    case 'string':
      return `test-${recordId}-${field.name}`;
    case 'bool':
      return false;
    case 'int':
    case 'integer':
      return 0;
    case 'float':
    case 'number':
    case 'decimal':
      return 0;
    case 'timestamp':
      return now.toISOString();
    case 'date':
      return now.toISOString().slice(0, 10);
    default:
      throw new Error(`No safe fixture value for required ${field.data_type} field "${field.name}".`);
  }
}

function completeRecord(tables, objectType, objectId, overrides = {}, now = new Date()) {
  const table = tables.get(objectType);
  if (!table) throw new Error(`Required model table "${objectType}" is missing.`);
  const record = { object_id: objectId };
  for (const field of table.fields) {
    if (!field.nullable && field.name !== 'object_id' && field.name !== 'updated_at') {
      record[field.name] = defaultValue(field, objectId, now);
    }
  }
  Object.assign(record, overrides, { object_id: objectId });
  return record;
}

export function createEntityPool(model, runId, { customerCount = 500, walletCount = 800, destinationCount = 100 } = {}) {
  const tables = new Map(model.tables.map((t) => [t.name, t]));
  const customers = [];
  const wallets = [];
  const destinations = [];
  const now = new Date();

  // Create Destinations: 5 hub destinations for concentration, several newly seen, others standard
  for (let i = 0; i < destinationCount; i += 1) {
    destinations.push({
      ref: `${runId}-dest-${i}`,
      isHub: i < 5,
      isNew: i >= 5 && i < 15,
      network: 'MTN',
    });
  }

  // Create Customers with realistic varying attributes
  for (let i = 0; i < customerCount; i += 1) {
    const userId = `${runId}-user-${i}`;
    const isDayZero = i < 10;
    const isNew = i >= 10 && i < 30;
    const isDormant = i >= 30 && i < 50;
    const isRecentCredChange = i >= 50 && i < 70;
    const hasNoIdentity = i >= 70 && i < 90;

    const accountAgeHours = isDayZero ? 0 : isNew ? 12 : 200 + (i * 10);
    const daysSinceLastActivity = isDormant ? 95 + (i % 20) : (i % 30);
    const authChangeHoursAgo = isRecentCredChange ? 2 : 500;

    const customer = completeRecord(tables, 'customers', userId, {
      has_identity_number: !hasNoIdentity,
      days_since_last_activity: daysSinceLastActivity,
      auth_change_hours_ago: authChangeHoursAgo,
    }, now);
    customer._accountAgeHours = accountAgeHours;
    customer._hasIdentity = !hasNoIdentity;
    customers.push(customer);
  }

  // Create Wallets: linked to customers
  for (let i = 0; i < walletCount; i += 1) {
    const customer = customers[i % customers.length];
    const walletId = `${runId}-wallet-${i}`;
    const isThirdParty = (i % 15 === 0);
    const isUnverified = (i % 25 === 0);
    const nameMismatch = (i % 35 === 0);
    const sharedHash = (i < 4) ? `shared-hash-${runId}` : `hash-${walletId}`;

    const wallet = completeRecord(tables, 'wallets', walletId, {
      user_id: customer.object_id,
      wallet_type: 'mobile',
      wallet_number_masked: `***${walletId.slice(-6)}`,
      wallet_number_hash: sharedHash,
      financial_entity_id: 'GH-MOMO',
      is_third_party: isThirdParty,
      is_verified: !isUnverified,
      name_match: nameMismatch ? 'none' : isUnverified ? 'unknown' : 'exact',
      attached_at: now.toISOString(),
      minutes_since_signup: (i % 40 === 0) ? 20 : 1200,
      third_party_wallet_count: isThirdParty ? 2 : 0,
    }, now);
    wallet._customer = customer;
    wallets.push(wallet);
  }

  return { customers, wallets, destinations, tables };
}

export class RealisticTransactionGenerator {
  constructor(entityPool, model, runId, options = {}) {
    this.pool = entityPool;
    this.model = model;
    this.tables = new Map(model.tables.map((t) => [t.name, t]));
    this.runId = runId;
    this.options = options;
    this.transactionCounter = 0;
    this.recentFailedWallets = new Set();
    this.probeUsers = new Map();
  }

  generateBatch(size = 10, currentTimestamp = new Date()) {
    const batch = [];
    for (let i = 0; i < size; i += 1) {
      batch.push(this.generateNext(currentTimestamp));
    }
    return batch;
  }

  generateNext(now = new Date()) {
    const txId = `${this.runId}-tx-${++this.transactionCounter}`;
    const wallet = this.pool.wallets[Math.floor(Math.random() * this.pool.wallets.length)];
    const customer = wallet._customer;

    // Organic scenario trigger probabilities
    const roll = Math.random();

    let leg = 'debit';
    let state = 'settled';
    let amount = Math.floor(10 + Math.random() * 250);
    let destination = this.pool.destinations[Math.floor(Math.random() * this.pool.destinations.length)];
    let destinationFirstSeenHours = destination.isNew ? 6 : 720;
    let secondsToCallback = 45;
    let userMedian90d = null;
    let userTxnCount24h = 1 + (this.transactionCounter % 4);

    // Scenario 1: Destination concentration (~5% of transactions)
    if (roll < 0.05) {
      leg = 'credit';
      destination = this.pool.destinations[Math.floor(Math.random() * 5)]; // Top hubs
      if (Math.random() < 0.25) {
        amount = 1200; // Destination volume spike
      }
    }
    // Scenario 3: Account lifecycle risk (~4% of transactions)
    else if (roll < 0.09) {
      leg = 'debit';
      state = 'initiated';
      if (customer._accountAgeHours < 24) {
        amount = customer._hasIdentity ? 150 : 1100; // Trigger high-value-without-kyc or day-zero
      }
    }
    // Scenario 4: Anomalous transaction behavior (~4% of transactions)
    else if (roll < 0.13) {
      leg = 'debit';
      const structuringFloor = this.options.structuringLimit * (1 - this.options.nearLimitPercent / 100);
      const subRoll = Math.random();
      if (subRoll < 0.35) {
        // Structuring near limit ($900-$999)
        amount = structuringFloor + Math.floor(Math.random() * (this.options.structuringLimit - structuringFloor));
        state = 'settled';
      } else if (subRoll < 0.65) {
        // Probe-then-escalate sequence
        if (this.probeUsers.has(customer.object_id)) {
          amount = 550;
          state = 'settled';
          this.probeUsers.delete(customer.object_id);
        } else {
          amount = 5;
          state = 'settled';
          this.probeUsers.set(customer.object_id, Date.now());
        }
      } else {
        // Velocity burst or failed debits
        userTxnCount24h = 12;
        state = 'settled';
      }
    }
    // Scenario 5: Resolution quality (~3% of transactions)
    else if (roll < 0.16) {
      state = Math.random() < 0.5 ? 'undetermined' : 'callback_received';
      secondsToCallback = this.options.lateCallbackSeconds + 50;
    }

    return completeRecord(this.tables, 'transactions', txId, {
      trans_id: txId,
      leg,
      state,
      operation: 'TRANSFER',
      channel: 'app',
      amount,
      user_median_amount_90d: userMedian90d,
      currency: 'GHS',
      country_id: 'GH',
      user_id: customer.object_id,
      wallet_id: wallet.object_id,
      source_msisdn: `+233${txId.slice(-9)}`,
      destination_ref: destination.ref,
      destination_network: destination.network,
      destination_first_seen_hours: destinationFirstSeenHours,
      funding_network: 'GH-MOMO',
      merchant_product_id: 'p2p-transfer',
      payment_ref: txId,
      is_recurring: false,
      expected_callback: state === 'callback_received',
      seconds_to_callback: secondsToCallback,
      account_age_hours: customer._accountAgeHours,
      user_txn_count_24h: userTxnCount24h,
      initiated_at: now.toISOString(),
    }, now);
  }
}

// ---------------------------------------------------------------------------
// HTTP Helpers & Service Ingestion
// ---------------------------------------------------------------------------

async function requestJson(origin, prefix, token, endpoint, { method = 'GET', body } = {}) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
  let response;
  try {
    response = await fetch(`${origin}${prefix}/v1${endpoint}`, {
      method,
      headers: {
        authorization: `Bearer ${token}`,
        accept: 'application/json',
        ...(body ? { 'content-type': 'application/json' } : {}),
      },
      ...(body ? { body: JSON.stringify(body) } : {}),
      signal: controller.signal,
      redirect: 'error',
    });
  } catch (error) {
    const reason = error.name === 'AbortError' ? 'request timed out' : error.message;
    throw new Error(`${method} ${prefix}/v1${endpoint} failed: ${reason}`);
  } finally {
    clearTimeout(timeout);
  }

  const text = await response.text();
  let result;
  if (text) {
    try {
      result = JSON.parse(text);
    } catch {
      throw new Error(`${method} ${prefix}/v1${endpoint} returned invalid JSON (HTTP ${response.status})`);
    }
  }

  if (!response.ok) {
    const details = result?.details ?? result?.error?.message ?? result?.error ?? response.statusText;
    const validationErrors = Array.isArray(result?.validation_errors) ? result.validation_errors : [];
    const examples = [...new Set(validationErrors.map(({ field, code }) => `${field}: ${code}`))].slice(0, 5);
    const validationSummary = validationErrors.length
      ? ` (${validationErrors.length} validation errors; ${examples.join(', ')})`
      : '';
    throw new HttpRequestError(`${method} ${prefix}/v1${endpoint} returned HTTP ${response.status}: ${details}${validationSummary}`, response.status);
  }
  return { status: response.status, body: result };
}

function assertEnvelope(value, key, label) {
  if (!value || typeof value[key] !== 'object' || value[key] === null) {
    throw new Error(`${label}: response did not contain "${key}"`);
  }
  return value[key];
}

async function loadModel(modelFile) {
  let envelope;
  try {
    envelope = JSON.parse(await readFile(modelFile, 'utf8'));
  } catch (error) {
    throw new Error(`Could not read model JSON at ${modelFile}: ${error.message}`);
  }
  if (!envelope?.data_model?.tables || !Array.isArray(envelope.data_model.tables)) {
    throw new Error('Model JSON must contain data_model.tables');
  }
  const model = portableModel(envelope.data_model);
  if (model.tables.length === 0) throw new Error('Model document has no tables');
  return model;
}

export async function checkExistingModel(options, tokenManager, model) {
  const response = await requestJson(
    options.origin,
    '/services/data-model',
    await tokenManager.getToken(),
    `/tenants/${encodeURIComponent(options.tenantId)}/data-model/export`,
  );
  const exported = portableModel(assertEnvelope(response.body, 'data_model', 'Model export'));
  if (canonicalJSON(exported) !== canonicalJSON(model)) {
    if (!options.allowModelMismatch) {
      throw new Error('Tenant data model differs from the local fraud-data-model.json; refusing to seed fixtures.');
    }
    console.warn('WARNING: Tenant data model differs from the local model; fixture writes may fail or have unintended meaning.');
    return false;
  }
  return true;
}

async function checkScenarios(options, tokenManager, definitions) {
  const tenantPath = `/tenants/${encodeURIComponent(options.tenantId)}`;
  const response = await requestJson(options.origin, '/services/decisions', await tokenManager.getToken(), `${tenantPath}/scenarios`);
  if (!Array.isArray(response.body?.scenarios)) throw new Error('Scenario list response is malformed.');
  const actualByName = new Map();
  for (const scenario of response.body.scenarios) {
    if (actualByName.has(scenario.name)) throw new Error(`Multiple scenarios are named "${scenario.name}".`);
    actualByName.set(scenario.name, scenario);
  }
  const live = new Map();
  for (const definition of definitions) {
    const scenario = actualByName.get(definition.name);
    if (!scenario?.id || !scenario.live_iteration_id || scenario.trigger_object_type !== definition.trigger_object_type) {
      throw new Error(`Scenario "${definition.name}" is missing, unpublished, or has a different trigger type.`);
    }
    const iterationResponse = await requestJson(
      options.origin,
      '/services/decisions',
      await tokenManager.getToken(),
      `${tenantPath}/scenarios/${encodeURIComponent(scenario.id)}/iterations/${encodeURIComponent(scenario.live_iteration_id)}`,
    );
    const iteration = assertEnvelope(iterationResponse.body, 'iteration', `Scenario "${definition.name}" live iteration`);
    if (iteration.status !== 'committed') throw new Error(`Scenario "${definition.name}" live iteration is not committed.`);

    const rulesResponse = await requestJson(
      options.origin,
      '/services/decisions',
      await tokenManager.getToken(),
      `${tenantPath}/scenarios/${encodeURIComponent(scenario.id)}/iterations/${encodeURIComponent(scenario.live_iteration_id)}/rules`,
    );
    if (!Array.isArray(rulesResponse.body?.rules)) throw new Error(`Scenario "${definition.name}" rule list is malformed.`);

    live.set(definition.name, { scenario, rules: rulesResponse.body.rules });
  }
  return live;
}

async function ingestChunkWithRetry(options, tokenManager, objectType, records, maxRetries = 3) {
  for (let attempt = 1; attempt <= maxRetries; attempt += 1) {
    const token = await tokenManager.getToken();
    try {
      const response = await requestJson(
        options.origin,
        '/services/ingestion',
        token,
        `/tenants/${encodeURIComponent(options.tenantId)}/ingest/${encodeURIComponent(objectType)}/batch`,
        { method: 'POST', body: records },
      );
      if (response.status === 200 && Array.isArray(response.body?.results) && response.body.results.length === records.length) {
        return response.body.results.length;
      }
      throw new Error(`Ingestion did not confirm all records (HTTP ${response.status})`);
    } catch (error) {
      if (attempt === maxRetries || (error instanceof HttpRequestError && error.status >= 400 && error.status < 500 && error.status !== 408 && error.status !== 429)) throw error;
      const backoff = Math.min(5000, 500 * (2 ** attempt));
      await new Promise((r) => setTimeout(r, backoff));
    }
  }
}

export async function ingestBatchWithRetry(options, tokenManager, objectType, records) {
  if (records.length === 0) throw new Error(`Cannot ingest an empty ${objectType} batch.`);
  let ingested = 0;
  try {
    for (let offset = 0; offset < records.length; offset += MAX_INGEST_BATCH_SIZE) {
      ingested += await ingestChunkWithRetry(options, tokenManager, objectType, records.slice(offset, offset + MAX_INGEST_BATCH_SIZE));
    }
  } catch (error) {
    if (ingested > 0) console.error(`${ingested} ${objectType} records were already ingested before the failure.`);
    throw error;
  }
  return ingested;
}

export async function ingestSimulationEntities(options, tokenManager, pool) {
  const customers = pool.customers.map(({ _accountAgeHours, _hasIdentity, ...record }) => record);
  const wallets = pool.wallets.map(({ _customer, ...record }) => record);
  await ingestBatchWithRetry(options, tokenManager, 'customers', customers);
  await ingestBatchWithRetry(options, tokenManager, 'wallets', wallets);
}

// ---------------------------------------------------------------------------
// Legacy Deterministic Test Mode (Preserved for compatibility)
// ---------------------------------------------------------------------------

export function buildTestCases(definitions, model, runId, { lateCallbackSeconds = 300, structuringLimit = 1000, nearLimitPercent = 10 } = {}) {
  const tables = new Map(model.tables.map((t) => [t.name, t]));
  const customers = [];
  const wallets = [];
  const transactions = [];
  const cases = [];
  let nextRecord = 0;
  const makeID = (kind) => `${runId}-${kind}-${++nextRecord}`;
  const addCustomer = (userId, overrides = {}) => {
    customers.push(completeRecord(tables, 'customers', userId, overrides));
  };
  const addWallet = (userId, walletId, overrides = {}) => {
    wallets.push(completeRecord(tables, 'wallets', walletId, {
      user_id: userId,
      wallet_type: 'mobile',
      wallet_number_masked: `***${walletId.slice(-6)}`,
      wallet_number_hash: `hash-${walletId}`,
      financial_entity_id: 'TEST',
      is_third_party: false,
      is_verified: false,
      name_match: 'exact',
      attached_at: new Date().toISOString(),
      ...overrides,
      user_id: userId,
    }));
  };
  const makeTransaction = (testCase, overrides = {}, initiatedAt = testCase.now) => {
    const transactionId = makeID('txn');
    return completeRecord(tables, 'transactions', transactionId, {
      trans_id: transactionId,
      leg: 'debit',
      state: 'initiated',
      operation: 'TRANSFER',
      channel: 'app',
      amount: 1,
      user_median_amount_90d: null,
      currency: 'GHS',
      country_id: 'GH',
      user_id: testCase.userId,
      wallet_id: testCase.walletId,
      source_msisdn: `+233${transactionId.slice(-9)}`,
      destination_ref: testCase.destinationRef,
      destination_network: 'TEST',
      funding_network: 'TEST',
      merchant_product_id: 'test-fixture',
      payment_ref: transactionId,
      is_recurring: false,
      expected_callback: false,
      initiated_at: initiatedAt.toISOString(),
      ...overrides,
    });
  };
  const createCase = (scenario, rule, shouldHit, targetOverrides = {}, customerOverrides = {}, walletOverrides = {}) => {
    const prefix = `${runId}-${rule.stable_rule_id}-${shouldHit ? 'hit' : 'miss'}`;
    const userId = makeID('user');
    const walletId = makeID('wallet');
    const destinationRef = makeID('dest');
    const now = new Date();
    const testCase = { scenario, rule, shouldHit, prefix, userId, walletId, destinationRef, now };
    addCustomer(userId, customerOverrides);
    if (scenario.trigger_object_type === 'wallets') {
      const objectId = makeID('wallet-target');
      testCase.payload = completeRecord(tables, 'wallets', objectId, {
        user_id: userId,
        wallet_type: 'mobile',
        wallet_number_masked: `***${objectId.slice(-6)}`,
        wallet_number_hash: `hash-${objectId}`,
        financial_entity_id: 'TEST',
        is_third_party: false,
        is_verified: false,
        name_match: 'exact',
        attached_at: now.toISOString(),
        ...walletOverrides,
        ...targetOverrides,
        user_id: userId,
      }, now);
      testCase.walletId = objectId;
      testCase.walletHash = testCase.payload.wallet_number_hash;
      wallets.push(testCase.payload);
    } else {
      addWallet(userId, walletId);
      testCase.payload = makeTransaction(testCase, targetOverrides);
      transactions.push(testCase.payload);
    }
    testCase.seedTransaction = (overrides = {}, offsetMs = 60_000) => {
      const at = new Date(testCase.now.getTime() - offsetMs);
      const record = makeTransaction(testCase, overrides, at);
      transactions.push(record);
      return record;
    };
    testCase.seedWallet = (hash = testCase.walletHash) => {
      const seedUserId = makeID('user');
      const seedWalletId = makeID('wallet-seed');
      addCustomer(seedUserId);
      addWallet(seedUserId, seedWalletId, { wallet_number_hash: hash });
      return seedWalletId;
    };
    cases.push(testCase);
    return testCase;
  };

  const addRuleCase = (scenario, rule, shouldHit) => {
    const tx = (fields, customer = {}) => createCase(scenario, rule, shouldHit, fields, customer);
    const wallet = (fields, customer = {}) => createCase(scenario, rule, shouldHit, fields, customer);
    let c;
    switch (rule.stable_rule_id) {
      case 'many-senders-one-destination':
        c = tx({ leg: 'credit' });
        if (shouldHit) for (let index = 0; index < 2; index += 1) {
          c.seedTransaction({ leg: 'credit', user_id: `${c.prefix}-sender-${index}` }, 120_000 + index);
        }
        break;
      case 'rapid-repeat-to-destination':
        c = tx({ leg: 'credit' });
        if (shouldHit) for (let index = 0; index < 2; index += 1) c.seedTransaction({ leg: 'credit' }, 120_000 + index);
        break;
      case 'destination-newly-seen':
        c = tx({ leg: 'credit', destination_first_seen_hours: shouldHit ? 1 : 24 });
        break;
      case 'destination-volume-spike-average':
        c = tx({ leg: 'credit', amount: shouldHit ? 1000 : 1 });
        c.seedTransaction({ leg: 'credit', amount: 300 }, 2 * 24 * 60 * 60 * 1000);
        break;
      case 'third-party-wallet-no-kyc':
        c = wallet({ is_third_party: shouldHit }, { has_identity_number: !shouldHit });
        break;
      case 'wallet-attached-near-signup':
        c = wallet({ minutes_since_signup: shouldHit ? 30 : 120 });
        break;
      case 'multiple-third-party-wallets':
        c = wallet({ third_party_wallet_count: shouldHit ? 2 : 1 });
        break;
      case 'wallet-name-mismatch':
        c = wallet({ name_match: shouldHit ? 'none' : 'exact' });
        break;
      case 'instrument-seen-on-another-account':
        c = wallet({});
        if (shouldHit) c.seedWallet(c.walletHash);
        break;
      case 'verified-with-unknown-name':
        c = wallet({ is_verified: shouldHit, name_match: shouldHit ? 'unknown' : 'exact' });
        break;
      case 'day-zero-account':
        c = tx({ leg: 'debit', state: 'initiated', account_age_hours: shouldHit ? 0 : 2 });
        break;
      case 'new-account':
        c = tx({ leg: 'debit', state: 'initiated', account_age_hours: shouldHit ? 1 : 24 });
        break;
      case 'dormant-reactivation':
        c = tx({ leg: 'debit', state: 'initiated' }, { days_since_last_activity: shouldHit ? 120 : 30 });
        break;
      case 'recent-credential-change':
        c = tx({ leg: 'debit', state: 'initiated' }, { auth_change_hours_ago: shouldHit ? 1 : 48 });
        break;
      case 'high-value-without-kyc':
        c = tx({ leg: 'debit', state: 'initiated', amount: shouldHit ? 1001 : 1000 }, { has_identity_number: !shouldHit });
        break;
      case 'first-day-high-value':
        c = tx({ leg: 'debit', state: 'initiated', amount: shouldHit ? 1001 : 1001, user_txn_count_24h: shouldHit ? 1 : 2 });
        break;
      case 'repeated-failed-debits':
        c = tx({ leg: 'debit', state: shouldHit ? 'failed' : 'settled' });
        for (let index = 0; index < (shouldHit ? 2 : 1); index += 1) c.seedTransaction({ leg: 'debit', state: 'failed' }, 60_000 + index);
        break;
      case 'success-after-failed-debits':
        c = tx({ leg: 'debit', state: 'settled' });
        for (let index = 0; index < (shouldHit ? 3 : 2); index += 1) {
          c.seedTransaction({ leg: 'debit', state: 'failed' }, 2 * 24 * 60 * 60 * 1000 + index);
        }
        break;
      case 'probe-then-escalate':
        c = tx({ leg: 'debit', state: 'settled', amount: 501 });
        if (shouldHit) c.seedTransaction({ leg: 'debit', state: 'settled', amount: 5 }, 60_000);
        break;
      case 'outside-user-baseline':
        c = tx({ leg: 'debit', state: 'settled', amount: shouldHit ? 100 : 99, user_median_amount_90d: 10 });
        break;
      case 'burst-velocity':
        c = tx({ leg: 'debit', state: 'settled', user_txn_count_24h: shouldHit ? 10 : 9 });
        break;
      case 'structuring-near-limit': {
        const floor = structuringLimit * (1 - nearLimitPercent / 100);
        c = tx({ leg: 'debit', state: 'settled', amount: shouldHit ? floor : floor - 1 });
        if (shouldHit) for (let index = 0; index < 3; index += 1) {
          c.seedTransaction({ leg: 'debit', state: 'settled', amount: floor }, 60_000 + index);
        }
        break;
      }
      case 'late-callback':
        c = tx({ leg: 'credit', state: 'callback_received', seconds_to_callback: shouldHit ? lateCallbackSeconds + 1 : lateCallbackSeconds - 1 });
        break;
      case 'very-late-callback':
        c = tx({ leg: 'credit', state: 'callback_received', seconds_to_callback: shouldHit ? 901 : 899 });
        break;
      case 'late-credit-callback':
        c = tx({ leg: 'credit', state: 'callback_received', seconds_to_callback: shouldHit ? lateCallbackSeconds + 1 : lateCallbackSeconds - 1 });
        break;
      case 'undetermined-resolution':
        c = tx({ leg: 'debit', state: shouldHit ? 'undetermined' : 'callback_received' });
        break;
      case 'high-value-resolution':
        c = tx({ leg: 'debit', state: 'callback_received', amount: shouldHit ? 1001 : 1000 });
        break;
      case 'repeated-undetermined-resolution':
        c = tx({ leg: 'debit', state: 'undetermined' });
        if (shouldHit) c.seedTransaction({ leg: 'debit', state: 'undetermined' }, 60_000);
        break;
      default:
        throw new Error(`No fire/non-fire fixture is defined for rule "${rule.stable_rule_id}".`);
    }
  };

  for (const scenario of definitions) {
    for (const rule of scenario.rules) {
      addRuleCase(scenario, rule, true);
      addRuleCase(scenario, rule, false);
    }
  }
  return { cases, customers, wallets, transactions };
}

export async function getEvaluation(options, tokenManager, scenario, testCase) {
  const tenantPath = `/tenants/${encodeURIComponent(options.tenantId)}`;
  const scenarioPath = `${tenantPath}/scenarios/${encodeURIComponent(scenario.id)}`;
  const response = await requestJson(options.origin, '/services/decisions', await tokenManager.getToken(), `${scenarioPath}/evaluate`, {
    method: 'POST',
    body: {
      object_id: testCase.payload.object_id,
      object_type: testCase.scenario.trigger_object_type,
      fields: testCase.payload,
    },
  });
  if (response.status === 200) return assertEnvelope(response.body, 'result', 'Scenario evaluation');
  if (response.status !== 202 || !response.body?.async_decision_execution?.id) {
    throw new Error(`Scenario evaluation for ${testCase.rule.stable_rule_id} was deferred without an execution ID.`);
  }
  return pollAsyncEvaluation(options, tokenManager, response.body.async_decision_execution.id, testCase.rule.stable_rule_id);
}

async function pollAsyncEvaluation(options, tokenManager, executionId, label) {
  const tenantPath = `/tenants/${encodeURIComponent(options.tenantId)}`;
  const deadline = Date.now() + ASYNC_POLL_TIMEOUT_MS;
  while (Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, ASYNC_POLL_INTERVAL_MS));
    const statusResponse = await requestJson(
      options.origin,
      '/services/decisions',
      await tokenManager.getToken(),
      `${tenantPath}/client-async-decision-executions/${encodeURIComponent(executionId)}`,
    );
    const execution = assertEnvelope(statusResponse.body, 'async_decision_execution', 'Async evaluation status');
    if (execution.status === 'failed') {
      throw new Error(`Async evaluation failed for ${label} (execution ${executionId}).`);
    }
    if (execution.status === 'completed') {
      const result = typeof execution.result_body === 'string'
        ? JSON.parse(execution.result_body)
        : execution.result_body;
      if (!result || typeof result !== 'object') throw new Error(`Async evaluation ${executionId} has no result body.`);
      return result;
    }
  }
  throw new Error(`Async evaluation ${executionId} did not finish within ${ASYNC_POLL_TIMEOUT_MS / 1000} seconds.`);
}

export async function evaluateTransaction(options, tokenManager, transaction) {
  const tenantPath = `/tenants/${encodeURIComponent(options.tenantId)}`;
  const response = await requestJson(options.origin, '/services/decisions', await tokenManager.getToken(), `${tenantPath}/decisions/all`, {
    method: 'POST',
    body: { object_id: transaction.object_id, object_type: 'transactions', fields: transaction },
  });
  let result;
  if (response.status === 200) {
    result = assertEnvelope(response.body, 'result', 'Transaction evaluation');
  } else if (response.status === 202 && response.body?.async_decision_execution?.id) {
    result = await pollAsyncEvaluation(options, tokenManager, response.body.async_decision_execution.id, transaction.object_id);
  } else {
    throw new Error(`Transaction evaluation for ${transaction.object_id} returned an unexpected response.`);
  }
  if (result.object_id !== transaction.object_id || !Array.isArray(result.results)) {
    throw new Error(`Transaction evaluation for ${transaction.object_id} returned a malformed result.`);
  }
  return result;
}

export async function evaluateTransactionBatch(options, tokenManager, transactions) {
  if (transactions.length === 0) throw new Error('Cannot evaluate an empty transaction batch.');
  let next = 0;
  const failures = [];
  let completed = 0;
  let triggered = 0;
  const workers = Array.from({ length: Math.min(EVALUATION_CONCURRENCY, transactions.length) }, async () => {
    while (next < transactions.length) {
      const transaction = transactions[next++];
      try {
        const result = await evaluateTransaction(options, tokenManager, transaction);
        completed++;
        triggered += result.results.filter(item => item.triggered).length;
      } catch (error) {
        failures.push({ objectId: transaction.object_id, message: error.message });
      }
    }
  });
  await Promise.all(workers);
  if (failures.length) {
    throw new Error(`${completed}/${transactions.length} transaction evaluations completed; ${failures.length} failed or have an unknown outcome: ${failures.map(({ objectId, message }) => `${objectId}: ${message}`).join('; ')}. Do not retry without reconciliation.`);
  }
  return { completed, triggered };
}

function verifyEvaluation(result, scenarioLive, testCase) {
  if (!result.triggered) throw new Error('Scenario trigger did not match the test payload.');
  if (!Array.isArray(result.rule_executions)) throw new Error('Evaluation response did not include rule executions.');
  const executions = new Map(result.rule_executions.map((execution) => [
    execution.rule_id ?? execution.RuleID,
    { ...execution, outcome: execution.outcome ?? execution.Outcome },
  ]));
  const rule = scenarioLive.rules.find(({ stable_rule_id }) => stable_rule_id === testCase.rule.stable_rule_id);
  const execution = rule && executions.get(rule.id);
  if (!execution) throw new Error(`No execution result returned for rule "${testCase.rule.stable_rule_id}".`);
  const actualHit = execution.outcome === 'hit';
  if (execution.outcome !== 'hit' && execution.outcome !== 'no_hit') {
    throw new Error(`Rule "${testCase.rule.stable_rule_id}" returned unexpected outcome "${execution.outcome}".`);
  }
  if (actualHit !== testCase.shouldHit) {
    throw new Error(`Expected ${testCase.shouldHit ? 'hit' : 'no_hit'}, received ${execution.outcome}.`);
  }
}

// ---------------------------------------------------------------------------
// Main 24-Hour Simulation & Runner
// ---------------------------------------------------------------------------

async function runDeterministic(options, model, definitions) {
  const fixtures = buildTestCases(definitions, model, options.runId, options);
  console.log(`Prepared ${fixtures.cases.length} deterministic fire/no-fire cases across provisioned rules.`);
  if (options.dryRun) {
    console.log(`Dry run complete for run ID ${options.runId}; no API requests made.`);
    return;
  }

  const tokenManager = new AuthTokenManager(options);
  await checkExistingModel(options, tokenManager, model);
  const live = await checkScenarios(options, tokenManager, definitions);

  console.log('Seeding customers, wallets, and deterministic transaction fixtures...');
  await ingestBatchWithRetry(options, tokenManager, 'customers', fixtures.customers);
  await ingestBatchWithRetry(options, tokenManager, 'wallets', fixtures.wallets);
  await ingestBatchWithRetry(options, tokenManager, 'transactions', fixtures.transactions);

  const failures = [];
  for (const testCase of fixtures.cases) {
    const scenarioLive = live.get(testCase.scenario.name);
    try {
      const result = await getEvaluation(options, tokenManager, scenarioLive.scenario, testCase);
      verifyEvaluation(result, scenarioLive, testCase);
      console.log(`PASS ${testCase.rule.stable_rule_id}: ${testCase.shouldHit ? 'fires' : 'does not fire'}`);
    } catch (error) {
      if (error instanceof TokenExchangeError) throw error;
      failures.push({ rule: testCase.rule.stable_rule_id, shouldHit: testCase.shouldHit, error: error.message });
      console.error(`FAIL ${testCase.rule.stable_rule_id}: ${error.message}`);
    }
  }

  if (failures.length) {
    console.error(`${failures.length}/${fixtures.cases.length} assertions failed.`);
    process.exitCode = 1;
    return;
  }
  console.log(`PASS: All ${fixtures.cases.length} deterministic evaluations passed.`);
}

export async function runContinuousSimulation(options, model, definitions) {
  console.log(`================================================================`);
  console.log(`24-Hour Fraud Scenarios Simulation (${options.totalTransactions} transactions)`);
  console.log(`Duration: ${(options.durationMs / 3600000).toFixed(2)} hours | Run ID: ${options.runId}`);
  console.log(`================================================================`);

  const pool = createEntityPool(model, options.runId);
  const generator = new RealisticTransactionGenerator(pool, model, options.runId, options);
  const pacer = new TrafficPacer({
    totalTransactions: options.totalTransactions,
    durationMs: options.durationMs,
    batchSize: options.batchSize,
  });

  if (options.dryRun) {
    console.log(`[Dry Run] Generating sample of 100 transactions to demonstrate varying frequency and organic triggers...`);
    const sample = generator.generateBatch(100);
    const triggers = sample.filter((tx) => tx.amount >= 900 || tx.state !== 'settled' || tx.leg === 'credit');
    console.log(`[Dry Run] Generated 100 sample transactions (${triggers.length} exhibited organic fraud signals).`);
    console.log(`[Dry Run] Diurnal multiplier at 0h: ${calculateDiurnalMultiplier(0, options.durationMs).toFixed(2)}x`);
    console.log(`[Dry Run] Diurnal multiplier at 6h: ${calculateDiurnalMultiplier(6 * 3600000, options.durationMs).toFixed(2)}x`);
    console.log(`[Dry Run] Diurnal multiplier at 12h: ${calculateDiurnalMultiplier(12 * 3600000, options.durationMs).toFixed(2)}x`);
    console.log(`[Dry Run] Dry run complete. No network requests were made.`);
    return;
  }

  const tokenManager = new AuthTokenManager(options);
  const modelMatches = await checkExistingModel(options, tokenManager, model);
  const liveScenarios = await checkScenarios(options, tokenManager, definitions);
  console.log(`${modelMatches ? 'Verified data model and' : 'Verified'} ${liveScenarios.size} live scenarios.`);

  // Ingest baseline entity cohort (customers and wallets)
  console.log(`Ingesting baseline entity cohort (${pool.customers.length} customers, ${pool.wallets.length} wallets)...`);
  await ingestSimulationEntities(options, tokenManager, pool);
  console.log('Baseline entities ingested successfully.');

  let completedTransactions = 0;
  let ingestedTransactions = 0;
  let triggeredScenarios = 0;
  let incompleteBatch = null;
  const startTime = Date.now();

  const saveCheckpoint = async () => {
    await mkdir(path.dirname(options.checkpointFile), { recursive: true });
    await writeFile(options.checkpointFile, JSON.stringify({
      runId: options.runId,
      ingestedTransactions,
      evaluatedTransactions: completedTransactions,
      triggeredScenarios,
      incompleteBatch,
      totalTransactions: options.totalTransactions,
      elapsedMs: Date.now() - startTime,
      lastUpdatedAt: new Date().toISOString(),
    }, null, 2));
  };

  let running = true;
  let interrupted = false;
  const onSignal = () => {
    console.log('\n[Shutdown] Stopping after the current batch finishes...');
    running = false;
    interrupted = true;
  };
  process.on('SIGINT', onSignal);
  process.on('SIGTERM', onSignal);

  let lastStatusLog = Date.now();
  console.log(`[Simulation] Starting continuous dispatch loop. Target: ${options.totalTransactions} transactions.`);

  let runError;
  try {
    while (running && completedTransactions < options.totalTransactions) {
      const elapsed = Date.now() - startTime;
      if (elapsed >= options.durationMs) {
        console.log(`[Simulation] Reached target duration of ${(options.durationMs / 3600000).toFixed(2)} hours.`);
        break;
      }

      const currentBatchSize = Math.min(options.batchSize, options.totalTransactions - completedTransactions);
      const batch = generator.generateBatch(currentBatchSize, new Date());
      incompleteBatch = { objectIds: batch.map(transaction => transaction.object_id), stage: 'ingestion' };
      await ingestBatchWithRetry(options, tokenManager, 'transactions', batch);
      ingestedTransactions += batch.length;
      incompleteBatch.stage = 'evaluation';
      const evaluated = await evaluateTransactionBatch(options, tokenManager, batch);
      completedTransactions += evaluated.completed;
      triggeredScenarios += evaluated.triggered;
      incompleteBatch = null;

      if (completedTransactions === batch.length) {
        console.log(`[Simulation] First batch: ${batch.length} transactions ingested and evaluated.`);
      }

      if (Date.now() - lastStatusLog >= 10 * 60 * 1000) {
        lastStatusLog = Date.now();
        const progress = ((completedTransactions / options.totalTransactions) * 100).toFixed(2);
        const elapsedHours = (elapsed / 3600000).toFixed(2);
        const remainingHours = Math.max(0, (options.durationMs - elapsed) / 3600000).toFixed(2);
        const currentRate = (completedTransactions / (elapsed / 1000)).toFixed(2);
        const diurnalM = calculateDiurnalMultiplier(elapsed, options.durationMs).toFixed(2);

        console.log(`----------------------------------------------------------------`);
        console.log(`[Status] ${completedTransactions}/${options.totalTransactions} evaluated (${progress}%) | Rate: ${currentRate} tx/s (diurnal factor ${diurnalM}x)`);
        console.log(`[Time] Elapsed: ${elapsedHours}h | Remaining: ${remainingHours}h | Triggered scenarios: ${triggeredScenarios}`);
        console.log(`----------------------------------------------------------------`);
        await saveCheckpoint();
      }

      if (running && completedTransactions < options.totalTransactions) {
        const delayMs = pacer.nextDelayMs(elapsed);
        await new Promise((r) => setTimeout(r, delayMs));
      }
    }
  } catch (error) {
    runError = error;
  } finally {
    process.off('SIGINT', onSignal);
    process.off('SIGTERM', onSignal);
    try {
      await saveCheckpoint();
    } catch (error) {
      if (runError) throw new Error(`${runError.message}; checkpoint save failed: ${error.message}`, { cause: runError });
      throw new Error(`Checkpoint save failed: ${error.message}`, { cause: error });
    }
  }

  if (runError) throw runError;
  if (interrupted) process.exitCode = 130;
  else if (completedTransactions < options.totalTransactions) process.exitCode = 1;
  const totalElapsedSec = (Date.now() - startTime) / 1000;
  console.log(`================================================================`);
  const outcome = interrupted ? 'stopped' : completedTransactions < options.totalTransactions ? 'ended before reaching target' : 'complete';
  console.log(`Simulation ${outcome}: evaluated ${completedTransactions} transactions in ${(totalElapsedSec / 3600).toFixed(2)} hours.`);
  console.log(`Triggered scenario evaluations: ${triggeredScenarios}`);
  console.log(`Checkpoint saved at: ${options.checkpointFile}`);
  console.log(`================================================================`);
}

async function main() {
  let options;
  try {
    options = parseArgs(process.argv.slice(2));
  } catch (error) {
    console.error(`Error: ${error.message}\n\n${usage()}`);
    process.exitCode = 2;
    return;
  }
  if (options.help) {
    console.log(usage());
    return;
  }

  const model = await loadModel(options.modelFile);
  const definitions = buildScenarioDefinitions(options.lateCallbackSeconds, options);

  if (options.deterministic) {
    await runDeterministic(options, model, definitions);
  } else {
    await runContinuousSimulation(options, model, definitions);
  }
}

export { parseArgs };

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  main().catch((error) => {
    console.error(`Fraud scenario simulation failed: ${error.message}`);
    process.exitCode = 1;
  });
}
