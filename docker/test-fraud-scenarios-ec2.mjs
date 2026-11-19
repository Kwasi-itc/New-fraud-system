#!/usr/bin/env node

import { randomBytes } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { pathToFileURL, fileURLToPath } from 'node:url';
import path from 'node:path';
import { buildScenarioDefinitions, portableModel } from './provision-fraud-scenarios-ec2.mjs';

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const DEFAULT_MODEL_FILE = path.resolve(SCRIPT_DIR, '../fraud-data-model.json');
const REQUEST_TIMEOUT_MS = 30_000;
const ASYNC_POLL_TIMEOUT_MS = 120_000;
const ASYNC_POLL_INTERVAL_MS = 1_000;
const BATCH_SIZE = 50;

const compareByKey = (items, key) => items.sort((left, right) => String(key(left)).localeCompare(String(key(right))));

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
  };
  const valuedOptions = new Map([
    ['--host', 'host'],
    ['--tenant-id', 'tenantId'],
    ['--model-file', 'modelFile'],
    ['--run-id', 'runId'],
    ['--late-callback-seconds', 'lateCallbackSeconds'],
    ['--structuring-limit', 'structuringLimit'],
    ['--structuring-band-percent', 'nearLimitPercent'],
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
    if (option === '--help' || option === '-h') {
      parsed.help = true;
      continue;
    }
    const key = valuedOptions.get(option);
    if (!key) throw new Error(`Unknown option: ${option}`);
    const value = args[index + 1];
    if (!value || value.startsWith('--')) throw new Error(`Missing value for ${option}`);
    parsed[key] = ['lateCallbackSeconds', 'structuringLimit', 'nearLimitPercent'].includes(key) ? Number(value) : value;
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
  if (!parsed.dryRun && !parsed.confirmWrite) {
    throw new Error('This test writes persistent fixture records and decisions. Review the warnings, then pass --confirm-write.');
  }
  parsed.modelFile = path.resolve(parsed.modelFile);
  return parsed;
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

function usage() {
  return `Usage:
  SERVICE_AUTH_TOKEN=... node docker/test-fraud-scenarios-ec2.mjs \\
    --host ip:port --tenant-id UUID --confirm-write

Options:
  --host ip:port                 Gateway origin (defaults to http:// when scheme is omitted)
  --tenant-id UUID               Existing tenant with the fraud model and published scenarios
  --model-file PATH              Portable model JSON (default: ../fraud-data-model.json)
  --run-id ID                    Fixture prefix (generated when omitted)
  --late-callback-seconds N      Match scenario provisioning (default: 300)
  --structuring-limit N          Match scenario provisioning (default: 1000)
  --structuring-band-percent N   Match scenario provisioning (default: 10)
  --dry-run                      Generate and validate local cases; make no API requests
  --confirm-write                Required; records and decisions persist in the tenant
  --help                         Show this help

Auth:
  SERVICE_AUTH_TOKEN is used by both APIs. Override with INGESTION_SERVICE_TOKEN
  and/or DECISION_ENGINE_SERVICE_TOKEN if the services use distinct tokens.
`;
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

function tableMap(model) {
  return new Map(model.tables.map((table) => [table.name, table]));
}

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

export function buildTestCases(definitions, model, runId, { lateCallbackSeconds = 300, structuringLimit = 1000, nearLimitPercent = 10 } = {}) {
  const tables = tableMap(model);
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
      case 'destination-volume-spike-average': {
        c = tx({ leg: 'credit', amount: shouldHit ? 1000 : 1 });
        c.seedTransaction({ leg: 'credit', amount: 300 }, 2 * 24 * 60 * 60 * 1000);
        break;
      }
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
        c = tx({
          leg: 'debit', state: 'initiated', amount: shouldHit ? 1001 : 1001,
          user_txn_count_24h: shouldHit ? 1 : 2,
        });
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

function validateFixtureTypes(fixtures, model) {
  const tables = tableMap(model);
  for (const [objectType, records] of [
    ['customers', fixtures.customers],
    ['wallets', fixtures.wallets],
    ['transactions', fixtures.transactions],
  ]) {
    for (const record of records) {
      const table = tables.get(objectType);
      if (!record.object_id) throw new Error(`${objectType} fixture is missing object_id.`);
      for (const field of table.fields) {
        if (!field.nullable && record[field.name] == null) {
          throw new Error(`${objectType} fixture ${record.object_id} is missing required field "${field.name}".`);
        }
      }
    }
  }
}

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
    throw new Error(`${method} ${prefix}/v1${endpoint} returned HTTP ${response.status}: ${details}`);
  }
  return { status: response.status, body: result };
}

function assertEnvelope(value, key, label) {
  if (!value || typeof value[key] !== 'object' || value[key] === null) {
    throw new Error(`${label}: response did not contain "${key}"`);
  }
  return value[key];
}

async function checkExistingModel(options, token, model) {
  const response = await requestJson(
    options.origin,
    '/services/data-model',
    token,
    `/tenants/${encodeURIComponent(options.tenantId)}/data-model/export`,
  );
  const exported = portableModel(assertEnvelope(response.body, 'data_model', 'Model export'));
  if (canonicalJSON(exported) !== canonicalJSON(model)) {
    throw new Error('Tenant data model differs from the local fraud-data-model.json; refusing to seed fixtures.');
  }
}

async function checkScenarios(options, token, definitions) {
  const tenantPath = `/tenants/${encodeURIComponent(options.tenantId)}`;
  const response = await requestJson(options.origin, '/services/decisions', token, `${tenantPath}/scenarios`);
  if (!Array.isArray(response.body?.scenarios)) throw new Error('Scenario list response is malformed.');
  const actualByName = new Map();
  for (const scenario of response.body.scenarios) {
    if (actualByName.has(scenario.name)) throw new Error(`Multiple scenarios are named "${scenario.name}".`);
    actualByName.set(scenario.name, scenario);
  }
  const live = new Map();
  for (const definition of definitions) {
    const scenario = actualByName.get(definition.name);
    if (!scenario?.id || !scenario.live_iteration_id ||
        scenario.trigger_object_type !== definition.trigger_object_type) {
      throw new Error(`Scenario "${definition.name}" is missing, unpublished, or has a different trigger type.`);
    }
    const iterationResponse = await requestJson(
      options.origin,
      '/services/decisions',
      token,
      `${tenantPath}/scenarios/${encodeURIComponent(scenario.id)}/iterations/${encodeURIComponent(scenario.live_iteration_id)}`,
    );
    const iteration = assertEnvelope(iterationResponse.body, 'iteration', `Scenario "${definition.name}" live iteration`);
    if (iteration.status !== 'committed') throw new Error(`Scenario "${definition.name}" live iteration is not committed.`);
    const rulesResponse = await requestJson(
      options.origin,
      '/services/decisions',
      token,
      `${tenantPath}/scenarios/${encodeURIComponent(scenario.id)}/iterations/${encodeURIComponent(scenario.live_iteration_id)}/rules`,
    );
    if (!Array.isArray(rulesResponse.body?.rules)) throw new Error(`Scenario "${definition.name}" rule list is malformed.`);
    const actualByStableId = new Map();
    for (const rule of rulesResponse.body.rules) {
      if (actualByStableId.has(rule.stable_rule_id)) {
        throw new Error(`Scenario "${definition.name}" has duplicate stable rule ID "${rule.stable_rule_id}".`);
      }
      actualByStableId.set(rule.stable_rule_id, rule);
    }
    for (const expected of definition.rules) {
      const actual = actualByStableId.get(expected.stable_rule_id);
      if (!actual || canonicalJSON(actual.formula) !== canonicalJSON(expected.formula)) {
        throw new Error(`Scenario "${definition.name}" is missing or differs from rule "${expected.stable_rule_id}".`);
      }
    }
    live.set(definition.name, { scenario, rules: rulesResponse.body.rules });
  }
  return live;
}

async function ingestFixtures(options, token, objectType, records) {
  let count = 0;
  for (let offset = 0; offset < records.length; offset += BATCH_SIZE) {
    const chunk = records.slice(offset, offset + BATCH_SIZE);
    const response = await requestJson(
      options.origin,
      '/services/ingestion',
      token,
      `/tenants/${encodeURIComponent(options.tenantId)}/ingest/${encodeURIComponent(objectType)}/batch`,
      { method: 'POST', body: chunk },
    );
    if (response.status !== 200 || !Array.isArray(response.body?.results) || response.body.results.length !== chunk.length) {
      throw new Error(`Ingestion did not confirm all ${objectType} fixture records in batch ${Math.floor(offset / BATCH_SIZE) + 1}.`);
    }
    count += response.body.results.length;
    console.log(`Ingested ${count}/${records.length} ${objectType} fixtures.`);
  }
}

async function getEvaluation(options, token, scenario, testCase) {
  const tenantPath = `/tenants/${encodeURIComponent(options.tenantId)}`;
  const scenarioPath = `${tenantPath}/scenarios/${encodeURIComponent(scenario.id)}`;
  const response = await requestJson(options.origin, '/services/decisions', token, `${scenarioPath}/evaluate`, {
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
  const executionId = response.body.async_decision_execution.id;
  const deadline = Date.now() + ASYNC_POLL_TIMEOUT_MS;
  while (Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, ASYNC_POLL_INTERVAL_MS));
    const statusResponse = await requestJson(
      options.origin,
      '/services/decisions',
      token,
      `${tenantPath}/async-decision-executions/${encodeURIComponent(executionId)}`,
    );
    const execution = assertEnvelope(statusResponse.body, 'async_decision_execution', 'Async evaluation status');
    if (execution.status === 'failed') {
      throw new Error(`Async evaluation failed for ${testCase.rule.stable_rule_id}: ${execution.last_error || 'no error details'}`);
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
  const baselineRule = scenarioLive.rules.find(({ stable_rule_id }) => stable_rule_id === 'outside-user-baseline');
  if (baselineRule && testCase.rule.stable_rule_id !== 'outside-user-baseline') {
    const baselineExecution = executions.get(baselineRule.id);
    if (baselineExecution?.outcome !== 'skipped' || !baselineExecution.skip_reason) {
      throw new Error('Expected the null-dependent baseline rule to be skipped with a reason.');
    }
  }
  if (execution.outcome === 'snoozed') throw new Error(`Rule "${testCase.rule.stable_rule_id}" is snoozed; its behavior cannot be asserted.`);
  const actualHit = execution.outcome === 'hit';
  if (execution.outcome !== 'hit' && execution.outcome !== 'no_hit') {
    throw new Error(`Rule "${testCase.rule.stable_rule_id}" returned unexpected outcome "${execution.outcome}".`);
  }
  if (actualHit !== testCase.shouldHit) {
    throw new Error(`Expected ${testCase.shouldHit ? 'hit' : 'no_hit'}, received ${execution.outcome}.`);
  }
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
  const fixtures = buildTestCases(definitions, model, options.runId, options);
  validateFixtureTypes(fixtures, model);
  const ruleCount = definitions.reduce((total, scenario) => total + scenario.rules.length, 0);
  console.log(`Prepared ${ruleCount} rules x fire/no-fire cases (${fixtures.cases.length} evaluations).`);
  console.log(`Fixture volume: ${fixtures.customers.length} customers, ${fixtures.wallets.length} wallets, ${fixtures.transactions.length} transactions.`);
  if (options.dryRun) {
    console.log(`Dry run complete for run ID ${options.runId}; no API requests or changes were made.`);
    return;
  }

  const ingestionToken = process.env.INGESTION_SERVICE_TOKEN || process.env.SERVICE_AUTH_TOKEN;
  const decisionToken = process.env.DECISION_ENGINE_SERVICE_TOKEN || process.env.SERVICE_AUTH_TOKEN;
  const modelToken = process.env.DATA_MODEL_SERVICE_TOKEN || process.env.SERVICE_AUTH_TOKEN;
  if (!ingestionToken || !decisionToken || !modelToken) {
    throw new Error('Set SERVICE_AUTH_TOKEN or the applicable INGESTION_SERVICE_TOKEN, DECISION_ENGINE_SERVICE_TOKEN, and DATA_MODEL_SERVICE_TOKEN environment variables.');
  }

  console.log('WARNING: use a disposable test tenant. Ingested fixtures and evaluated decisions are not automatically deleted.');
  console.log(`Fixture object IDs will use run prefix "${options.runId}".`);
  await checkExistingModel(options, modelToken, model);
  const live = await checkScenarios(options, decisionToken, definitions);
  console.log('Verified exact model and all five published scenario versions before writing fixtures.');

  await ingestFixtures(options, ingestionToken, 'customers', fixtures.customers);
  await ingestFixtures(options, ingestionToken, 'wallets', fixtures.wallets);
  await ingestFixtures(options, ingestionToken, 'transactions', fixtures.transactions);

  const failures = [];
  for (const testCase of fixtures.cases) {
    const scenarioLive = live.get(testCase.scenario.name);
    try {
      const result = await getEvaluation(options, decisionToken, scenarioLive.scenario, testCase);
      verifyEvaluation(result, scenarioLive, testCase);
      console.log(`PASS ${testCase.rule.stable_rule_id}: ${testCase.shouldHit ? 'fires' : 'does not fire'}`);
    } catch (error) {
      failures.push({ rule: testCase.rule.stable_rule_id, shouldHit: testCase.shouldHit, error: error.message });
      console.error(`FAIL ${testCase.rule.stable_rule_id} (${testCase.shouldHit ? 'expected fire' : 'expected no-fire'}): ${error.message}`);
    }
  }

  if (failures.length) {
    console.error(`${failures.length}/${fixtures.cases.length} assertions failed. Run ID: ${options.runId}.`);
    process.exitCode = 1;
    return;
  }
  console.log(`PASS: all ${fixtures.cases.length} fire/no-fire evaluations across ${ruleCount} rules. Run ID: ${options.runId}.`);
}

export { parseArgs };

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  main().catch((error) => {
    console.error(`Fraud scenario test failed: ${error.message}`);
    process.exitCode = 1;
  });
}
