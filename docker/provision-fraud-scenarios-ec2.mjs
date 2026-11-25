#!/usr/bin/env node

import { readFile } from 'node:fs/promises';
import { pathToFileURL, fileURLToPath } from 'node:url';
import path from 'node:path';
import { exchangeClientProof } from '../backend/auth-service/scripts/client-proof.mjs';

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
const DEFAULT_MODEL_FILE = path.resolve(SCRIPT_DIR, '../fraud-data-model.json');
const DEFAULT_CLIENT_KEY_DIR = path.resolve(SCRIPT_DIR, '../.local/client-auth');
const REQUEST_TIMEOUT_MS = 20_000;
export const CLIENT_SCOPES = ['models:read', 'scenarios:read', 'scenarios:write'];

export const omittedRules = [
  { id: 'A5', reason: 'Needs device-sharing comparisons, but the supplied model has no device identifier and the rule is cross-record/pairwise.' },
  { id: 'D2', reason: 'Needs an ordered descending sequence; current aggregate pushdown returns scalar aggregates and has no ordering/window-function operation.' },
];

const fieldRef = (field) => ({
  function: 'field_ref',
  named_children: { field: { constant: field } },
});
const relatedField = (path, field) => ({
  function: 'related_field',
  named_children: { path: { constant: path }, field: { constant: field } },
});
const compare = (functionName, left, right) => ({
  function: functionName,
  children: [left, right],
});
const and = (...children) => ({ function: 'and', children });
const or = (...children) => ({ function: 'or', children });
const not = (child) => ({ function: 'not', children: [child] });
const eq = (field, value) => compare('eq', fieldRef(field), { constant: value });
const gt = (field, value) => compare('gt', fieldRef(field), { constant: value });
const gte = (left, right) => compare('gte', left, right);
const lt = (field, value) => compare('lt', fieldRef(field), { constant: value });
const multiply = (left, right) => ({ function: 'multiply', children: [left, right] });
const divide = (left, right) => ({ function: 'divide', children: [left, right] });

const aggregateFilter = (table, field, operator, value) => ({
  function: 'Filter',
  named_children: {
    tableName: { constant: table },
    fieldName: { constant: field },
    operator: { constant: operator },
    value,
  },
});
const aggregateWindowStart = (duration) => ({
  function: 'TimeAdd',
  named_children: {
    timestampField: fieldRef('initiated_at'),
    duration: { constant: duration },
    sign: { constant: '-' },
  },
});
const aggregate = (table, field, aggregator, filters) => ({
  function: 'Aggregator',
  named_children: {
    tableName: { constant: table },
    fieldName: { constant: field },
    aggregator: { constant: aggregator },
    filters: { function: 'List', children: filters },
  },
});
const transactionWindowFilters = (duration, additional = []) => [
  aggregateFilter('transactions', 'initiated_at', '>', aggregateWindowStart(duration)),
  aggregateFilter('transactions', 'initiated_at', '<=', fieldRef('initiated_at')),
  ...additional,
];

function makeRule(stableId, name, description, formula, scoreModifier) {
  return {
    stable_rule_id: stableId,
    name,
    description,
    formula,
    score_modifier: scoreModifier,
    rule_group: 'fraud-detection',
  };
}

function sortBy(items, key) {
  return items.sort((left, right) => String(key(left)).localeCompare(String(key(right))));
}

export function buildScenarioDefinitions(
  lateCallbackSeconds = 300,
  { structuringLimit = 1000, nearLimitPercent = 10 } = {},
) {
  const creditLeg = eq('leg', 'credit');
  const debitLeg = eq('leg', 'debit');
  const nearLimitFloor = structuringLimit * (1 - nearLimitPercent / 100);
  const creditDestinationFilters = (duration) => transactionWindowFilters(duration, [
    aggregateFilter('transactions', 'leg', '=', { constant: 'credit' }),
    aggregateFilter('transactions', 'destination_ref', '=', fieldRef('destination_ref')),
  ]);
  const destinationAggregate = (field, aggregatorName, duration) => aggregate(
    'transactions',
    field,
    aggregatorName,
    creditDestinationFilters(duration),
  );
  const destinationVolume = (duration) => destinationAggregate('amount', 'SUM', duration);
  const historicalDestinationVolume = () => aggregate('transactions', 'amount', 'SUM', [
    aggregateFilter('transactions', 'initiated_at', '>', aggregateWindowStart('P30D')),
    aggregateFilter('transactions', 'initiated_at', '<=', aggregateWindowStart('24h')),
    aggregateFilter('transactions', 'leg', '=', { constant: 'credit' }),
    aggregateFilter('transactions', 'destination_ref', '=', fieldRef('destination_ref')),
  ]);
  const failedDebitFilters = (duration) => transactionWindowFilters(duration, [
    aggregateFilter('transactions', 'wallet_id', '=', fieldRef('wallet_id')),
    aggregateFilter('transactions', 'leg', '=', { constant: 'debit' }),
    aggregateFilter('transactions', 'state', '=', { constant: 'failed' }),
  ]);

  return [
    {
      name: 'Suspicious destination concentration',
      description: 'Destination rules use scalar signals and bounded aggregate pushdown queries.',
      trigger_object_type: 'transactions',
      thresholds: [50, 80, 95],
      trigger: creditLeg,
      rules: [
        makeRule('many-senders-one-destination', 'Many senders, one destination', 'At least three distinct users sent credit legs to this destination in the prior 24 hours.', gte(destinationAggregate('user_id', 'COUNT_DISTINCT', '24h'), { constant: 3 }), 50),
        makeRule('rapid-repeat-to-destination', 'Rapid repeat to destination', 'At least three credit legs were sent to this destination in the prior 60 minutes.', gte(destinationAggregate('amount', 'COUNT', '60m'), { constant: 3 }), 30),
        makeRule('destination-newly-seen', 'Destination newly seen', 'Destination first seen less than 24 hours ago.', lt('destination_first_seen_hours', 24), 20),
        makeRule(
          'destination-volume-spike-average',
          'Destination volume spike versus average',
          'Credit volume to this destination in the prior 24 hours is at least five times its average daily credit volume over the preceding 30 days, excluding the current 24-hour window.',
          and(
            compare('gt', historicalDestinationVolume(), { constant: 0 }),
            gte(
              destinationVolume('24h'),
              multiply({ constant: 5 }, divide(historicalDestinationVolume(), { constant: 30 })),
            ),
          ),
          35,
        ),
      ],
    },
    {
      name: 'Funding instrument risk',
      description: 'Wallet and linked-customer checks, including a shared-instrument aggregate.',
      trigger_object_type: 'wallets',
      thresholds: [40, 70, 90],
      trigger: { constant: true },
      rules: [
        makeRule(
          'third-party-wallet-no-kyc',
          'Third-party wallet without KYC',
          'Third-party wallet whose linked customer has no identity number.',
          and(eq('is_third_party', true), not(relatedField('wallet_to_customer', 'has_identity_number'))),
          40,
        ),
        makeRule('wallet-attached-near-signup', 'Wallet attached near signup', 'Wallet attached within one hour of signup.', lt('minutes_since_signup', 60), 30),
        makeRule('multiple-third-party-wallets', 'Multiple third-party wallets', 'At least two third-party wallets are recorded for the customer.', compare('gte', fieldRef('third_party_wallet_count'), { constant: 2 }), 50),
        makeRule('wallet-name-mismatch', 'Wallet name mismatch', 'Wallet holder name does not match the account name.', eq('name_match', 'none'), 35),
        makeRule('instrument-seen-on-another-account', 'Instrument seen on another account', 'The wallet-number hash appears on at least two wallet records.', gte(aggregate('wallets', 'wallet_number_hash', 'COUNT', [
          aggregateFilter('wallets', 'wallet_number_hash', '=', fieldRef('wallet_number_hash')),
        ]), { constant: 2 }), 45),
        makeRule('verified-with-unknown-name', 'Verified with unknown name match', 'Wallet is marked verified while its name match is unknown.', and(eq('is_verified', true), eq('name_match', 'unknown')), 15),
      ],
    },
    {
      name: 'Account lifecycle risk',
      description: 'Scalar and linked-customer lifecycle signals for debit initiation.',
      trigger_object_type: 'transactions',
      thresholds: [50, 80, 95],
      trigger: and(debitLeg, eq('state', 'initiated')),
      rules: [
        makeRule('day-zero-account', 'Day-zero account', 'Account is less than one hour old.', lt('account_age_hours', 1), 30),
        makeRule('new-account', 'New account', 'Account is less than 24 hours old.', lt('account_age_hours', 24), 20),
        makeRule('dormant-reactivation', 'Dormant reactivation', 'Linked customer has been inactive for more than 90 days.', compare('gt', relatedField('transaction_to_customer', 'days_since_last_activity'), { constant: 90 }), 30),
        makeRule('recent-credential-change', 'Recent credential change', 'Linked customer changed credentials within the last 24 hours.', compare('lt', relatedField('transaction_to_customer', 'auth_change_hours_ago'), { constant: 24 }), 40),
        makeRule('high-value-without-kyc', 'High value without KYC', 'Transaction amount exceeds 1000 and linked customer has no identity number.', and(gt('amount', 1000), not(relatedField('transaction_to_customer', 'has_identity_number'))), 25),
        makeRule('first-day-high-value', 'First-day high-value transaction', 'Customer transaction count is one and amount exceeds 1000.', and(eq('user_txn_count_24h', 1), gt('amount', 1000)), 20),
      ],
    },
    {
      name: 'Anomalous transaction behaviour',
      description: 'Supported per-record baseline and velocity rules at debit terminal states.',
      trigger_object_type: 'transactions',
      thresholds: [50, 75, 95],
      trigger: and(
        debitLeg,
        compare('in', fieldRef('state'), { constant: ['settled', 'failed', 'undetermined', 'blocked'] }),
      ),
      rules: [
        makeRule('repeated-failed-debits', 'Repeated failed debits', 'At least three failed debit attempts used this wallet in the prior 60 minutes.', gte(aggregate('transactions', 'user_id', 'COUNT', failedDebitFilters('60m')), { constant: 3 }), 40),
        makeRule('success-after-failed-debits', 'Success after failed debits', 'A settled debit follows at least three earlier failed debit attempts on the same wallet.', and(
          eq('state', 'settled'),
          gte(aggregate('transactions', 'user_id', 'COUNT', [
            aggregateFilter('transactions', 'wallet_id', '=', fieldRef('wallet_id')),
            aggregateFilter('transactions', 'leg', '=', { constant: 'debit' }),
            aggregateFilter('transactions', 'state', '=', { constant: 'failed' }),
            aggregateFilter('transactions', 'initiated_at', '<', fieldRef('initiated_at')),
          ]), { constant: 3 }),
        ), 45),
        makeRule('probe-then-escalate', 'Probe then escalate', 'A settled debit over 500 follows a settled debit under 10 by the same user within the prior hour.', and(
          eq('state', 'settled'),
          gt('amount', 500),
          gte(aggregate('transactions', 'user_id', 'COUNT', transactionWindowFilters('60m', [
            aggregateFilter('transactions', 'user_id', '=', fieldRef('user_id')),
            aggregateFilter('transactions', 'leg', '=', { constant: 'debit' }),
            aggregateFilter('transactions', 'state', '=', { constant: 'settled' }),
            aggregateFilter('transactions', 'amount', '<', { constant: 10 }),
            aggregateFilter('transactions', 'initiated_at', '<', fieldRef('initiated_at')),
          ])), { constant: 1 }),
        ), 40),
        makeRule('outside-user-baseline', 'Far outside user baseline', 'Amount is at least ten times the user median for the last 90 days.', compare('gte', fieldRef('amount'), { function: 'multiply', children: [fieldRef('user_median_amount_90d'), { constant: 10 }] }), 30),
        makeRule('burst-velocity', 'Burst velocity', 'At least ten user transactions are recorded in the last 24 hours.', compare('gte', fieldRef('user_txn_count_24h'), { constant: 10 }), 25),
        makeRule('structuring-near-limit', 'Structuring near the configured limit', `At least four debit transactions by this user in the prior 24 hours were between ${nearLimitFloor} and ${structuringLimit} (exclusive).`, gte(aggregate('transactions', 'user_id', 'COUNT', transactionWindowFilters('24h', [
          aggregateFilter('transactions', 'user_id', '=', fieldRef('user_id')),
          aggregateFilter('transactions', 'leg', '=', { constant: 'debit' }),
          aggregateFilter('transactions', 'amount', '>=', { constant: nearLimitFloor }),
          aggregateFilter('transactions', 'amount', '<', { constant: structuringLimit }),
        ])), { constant: 4 }), 35),
      ],
    },
    {
      name: 'Transaction resolution quality',
      description: 'Callback, resolution-state, and 24-hour repeat-resolution rules.',
      trigger_object_type: 'transactions',
      thresholds: [40, 70, 100],
      trigger: or(eq('state', 'callback_received'), eq('state', 'undetermined')),
      rules: [
        makeRule('late-callback', 'Late callback', `Callback arrived more than ${lateCallbackSeconds} seconds after initiation.`, compare('gt', fieldRef('seconds_to_callback'), { constant: lateCallbackSeconds }), 30),
        makeRule('very-late-callback', 'Very late callback', 'Callback arrived more than 900 seconds after initiation.', compare('gt', fieldRef('seconds_to_callback'), { constant: 900 }), 40),
        makeRule('late-credit-callback', 'Late credit callback', `Credit callback arrived more than ${lateCallbackSeconds} seconds after initiation.`, and(eq('leg', 'credit'), compare('gt', fieldRef('seconds_to_callback'), { constant: lateCallbackSeconds })), 20),
        makeRule('undetermined-resolution', 'Undetermined resolution', 'Transaction leg ended in an undetermined state.', eq('state', 'undetermined'), 35),
        makeRule('high-value-resolution', 'High-value resolution', 'Transaction amount exceeds 1000.', gt('amount', 1000), 10),
        makeRule('repeated-undetermined-resolution', 'Repeated undetermined resolution', 'At least two transaction legs for this user, including this one, are undetermined within the prior 24 hours.', gte(aggregate('transactions', 'user_id', 'COUNT', transactionWindowFilters('24h', [
          aggregateFilter('transactions', 'user_id', '=', fieldRef('user_id')),
          aggregateFilter('transactions', 'state', '=', { constant: 'undetermined' }),
        ])), { constant: 2 }), 30),
      ],
    },
  ];
}

function canonicalize(value) {
  if (Array.isArray(value)) return value.map(canonicalize);
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.keys(value).sort().map((key) => [key, canonicalize(value[key])]),
    );
  }
  return value;
}

export function portableModel(model) {
  return {
    version: model.version,
    tables: sortBy((model.tables ?? []).map((table) => ({
      name: table.name,
      description: table.description ?? '',
      alias: table.alias ?? '',
      semantic_type: table.semantic_type ?? '',
      caption_field: table.caption_field ?? '',
      fields: sortBy((table.fields ?? []).map((field) => ({
        name: field.name,
        description: field.description ?? '',
        alias: field.alias ?? '',
        data_type: field.data_type,
        nullable: Boolean(field.nullable),
        is_enum: Boolean(field.is_enum),
        is_unique: Boolean(field.is_unique),
        enum_values: sortBy((field.enum_values ?? []).map((entry) => ({
          value: entry.value,
          label: entry.label,
          sort_order: entry.sort_order ?? 0,
        })), (entry) => `${entry.sort_order}:${entry.value}`),
      })), (field) => field.name),
      options: table.options ?? undefined,
      navigation_options: sortBy(
        (table.navigation_options ?? []).map((option) => ({
          source_field: option.source_field,
          target_table: option.target_table,
          filter_field: option.filter_field,
          ordering_field: option.ordering_field,
        })),
        (option) => `${option.target_table}:${option.source_field}:${option.filter_field}:${option.ordering_field}`,
      ),
    })), (table) => table.name),
    links: sortBy((model.links ?? []).map((link) => ({
      name: link.name,
      parent_table: link.parent_table,
      parent_field: link.parent_field,
      child_table: link.child_table,
      child_field: link.child_field,
    })), (link) => link.name),
    pivots: sortBy((model.pivots ?? []).map((pivot) => ({
      base_table: pivot.base_table,
      ...(pivot.field ? { field: pivot.field } : {}),
      ...(pivot.path_links?.length ? { path_links: pivot.path_links } : {}),
    })), (pivot) => `${pivot.base_table}:${pivot.field ?? ''}:${(pivot.path_links ?? []).join(',')}`),
  };
}

export function validateDefinitionsAgainstModel(scenarios, model) {
  const tables = new Map(model.tables.map((table) => [
    table.name,
    new Set(table.fields.map((field) => field.name)),
  ]));
  const links = model.links;
  const inspect = (node, triggerTable, scenarioName) => {
    if (!node || typeof node !== 'object') return;
    if (node.function === 'field_ref') {
      const field = node.named_children?.field?.constant;
      if (typeof field !== 'string' || !tables.get(triggerTable)?.has(field)) {
        throw new Error(`Scenario "${scenarioName}" references unknown ${triggerTable} field "${field}".`);
      }
    } else if (node.function === 'related_field') {
      const linkPath = node.named_children?.path?.constant;
      const field = node.named_children?.field?.constant;
      if (typeof linkPath !== 'string' || typeof field !== 'string') {
        throw new Error(`Scenario "${scenarioName}" has an invalid related_field expression.`);
      }
      let relatedTable = triggerTable;
      for (const linkName of linkPath.split('.')) {
        const link = links.find((item) => item.name === linkName && item.child_table === relatedTable);
        if (!link) throw new Error(`Scenario "${scenarioName}" cannot resolve link "${linkName}" from "${relatedTable}".`);
        relatedTable = link.parent_table;
      }
      if (!tables.get(relatedTable)?.has(field)) {
        throw new Error(`Scenario "${scenarioName}" references unknown ${relatedTable} field "${field}".`);
      }
    }
    for (const child of node.children ?? []) inspect(child, triggerTable, scenarioName);
    for (const child of Object.values(node.named_children ?? {})) inspect(child, triggerTable, scenarioName);
  };
  for (const scenario of scenarios) {
    if (!tables.has(scenario.trigger_object_type)) {
      throw new Error(`Scenario "${scenario.name}" uses missing trigger table "${scenario.trigger_object_type}".`);
    }
    inspect(scenario.trigger, scenario.trigger_object_type, scenario.name);
    for (const rule of scenario.rules) inspect(rule.formula, scenario.trigger_object_type, scenario.name);
  }
}

export function parseArgs(args) {
  const parsed = {
    host: undefined,
    tenantId: undefined,
    clientId: undefined,
    keyDir: DEFAULT_CLIENT_KEY_DIR,
    tokenUrl: undefined,
    modelFile: DEFAULT_MODEL_FILE,
    lateCallbackSeconds: 300,
    structuringLimit: 1000,
    nearLimitPercent: 10,
    dryRun: false,
  };
  const valuedOptions = new Map([
    ['--host', 'host'],
    ['--tenant-id', 'tenantId'],
    ['--client-id', 'clientId'],
    ['--key-dir', 'keyDir'],
    ['--token-url', 'tokenUrl'],
    ['--model-file', 'modelFile'],
    ['--late-callback-seconds', 'lateCallbackSeconds'],
    ['--structuring-limit', 'structuringLimit'],
    ['--structuring-band-percent', 'nearLimitPercent'],
  ]);
  for (let index = 0; index < args.length; index += 1) {
    const option = args[index];
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
  if (parsed.dryRun) {
    parsed.modelFile = path.resolve(parsed.modelFile);
    if (parsed.host) parsed.origin = parseOrigin(parsed.host);
    if (parsed.tenantId) {
      if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(parsed.tenantId)) {
        throw new Error('--tenant-id must be a UUID');
      }
      parsed.tenantId = parsed.tenantId.toLowerCase();
    }
    return parsed;
  }
  if (!parsed.host) throw new Error('Required option missing: --host ip:port');
  if (!parsed.tenantId) throw new Error('Required option missing: --tenant-id');
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(parsed.tenantId)) {
    throw new Error('--tenant-id must be a UUID');
  }
  parsed.tenantId = parsed.tenantId.toLowerCase();
  parsed.origin = parseOrigin(parsed.host);
  if (!parsed.clientId) throw new Error('Required option missing: --client-id');
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(parsed.clientId) ||
      parsed.clientId === '00000000-0000-0000-0000-000000000000') {
    throw new Error('--client-id must be the registered API client UUID');
  }
  parsed.clientId = parsed.clientId.toLowerCase();
  parsed.keyDir = path.resolve(parsed.keyDir);
  parsed.tokenUrl ??= `${parsed.origin}/oauth/token`;
  parsed.modelFile = path.resolve(parsed.modelFile);
  return parsed;
}

function parseOrigin(host) {
  const value = /^[a-z][a-z\d+.-]*:\/\//i.test(host) ? host : `http://${host}`;
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new Error('--host must be an origin such as 10.0.0.10:8080 or http://10.0.0.10:8080');
  }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password ||
      (url.pathname !== '/' && url.pathname !== '') || url.search || url.hash) {
    throw new Error('--host must be an HTTP(S) origin without credentials, path, query, or fragment');
  }
  if (!url.port) throw new Error('--host must include an explicit port');
  return url.origin;
}

function usage() {
  return `Usage:
  node docker/provision-fraud-scenarios-ec2.mjs --host ip:port --tenant-id UUID \\
    --client-id CLIENT_UUID [--key-dir DIRECTORY] [--token-url URL]

Options:
  --host ip:port                 Gateway origin (defaults to http:// when scheme is omitted)
  --tenant-id UUID               Required target tenant; this script never selects one
  --client-id UUID               Registered API client with the draft-authoring scopes
  --key-dir DIRECTORY            Client private-key directory (default: .local/client-auth)
  --token-url URL                 Public OAuth endpoint (default: <host>/oauth/token)
  --model-file PATH              Portable model JSON (default: ../fraud-data-model.json)
  --late-callback-seconds N      Late-callback threshold (default: 300)
  --structuring-limit N          D7 amount limit (default: 1000)
  --structuring-band-percent N   D7 percent below limit included in band (default: 10)
  --dry-run                      Validate local inputs and list planned rules only
  --help                         Show this help

Requested scopes: ${CLIENT_SCOPES.join(' ')}. The token is exchanged in memory and is never printed.
`;
}

function getEnvelope(response, key, label) {
  if (!response || !response[key] || typeof response[key] !== 'object') {
    throw new Error(`${label}: response did not contain "${key}"`);
  }
  return response[key];
}

function assertEqual(actual, expected, message) {
  if (JSON.stringify(canonicalize(actual)) !== JSON.stringify(canonicalize(expected))) {
    throw new Error(message);
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
  return result;
}

async function loadAndValidateModel(modelFile) {
  let envelope;
  try {
    envelope = JSON.parse(await readFile(modelFile, 'utf8'));
  } catch (error) {
    throw new Error(`Could not read model JSON at ${modelFile}: ${error.message}`);
  }
  if (!envelope?.data_model || !Array.isArray(envelope.data_model.tables) ||
      !Array.isArray(envelope.data_model.links) || !Array.isArray(envelope.data_model.pivots)) {
    throw new Error('Model JSON must contain data_model.tables, data_model.links, and data_model.pivots');
  }
  const model = portableModel(envelope.data_model);
  if (!model.version || model.tables.length === 0) throw new Error('Model document is empty or has no version');
  return model;
}

export async function verifyExistingModel(origin, tenantId, token, model) {
  const path = `/tenants/${encodeURIComponent(tenantId)}/data-model/export`;
  const exportModel = async () => portableModel(getEnvelope(
    await requestJson(origin, '/services/data-model', token, path),
    'data_model',
    'Model export',
  ));
  const current = await exportModel();
  if (current.tables.length === 0 && current.links.length === 0 && current.pivots.length === 0) {
    throw new Error('Target tenant has no data model. Import the supplied model through a human-authorized setup path before running client draft provisioning.');
  }
  assertEqual(current, model, 'Existing tenant model does not exactly match the supplied model; refusing to modify or recreate it.');
  console.log(`Existing data model verified (${model.tables.length} tables); no import attempted.`);
}

function rulePayload(rule, displayOrder) {
  return {
    display_order: displayOrder,
    ...rule,
  };
}

function assertRuleMatches(actual, expected) {
  return actual.stable_rule_id === expected.stable_rule_id &&
    actual.name === expected.name &&
    actual.score_modifier === expected.score_modifier &&
    JSON.stringify(canonicalize(actual.formula)) === JSON.stringify(canonicalize(expected.formula));
}

export function findMissingRules(existingRules, desiredRules) {
  const byStableId = new Map();
  for (const rule of existingRules) {
    if (!rule.stable_rule_id) continue;
    if (byStableId.has(rule.stable_rule_id)) {
      throw new Error(`Existing iteration has duplicate stable_rule_id "${rule.stable_rule_id}".`);
    }
    byStableId.set(rule.stable_rule_id, rule);
  }
  const missing = [];
  for (const expected of desiredRules) {
    const existing = byStableId.get(expected.stable_rule_id);
    if (!existing) {
      missing.push(expected);
    } else if (!assertRuleMatches(existing, expected)) {
      throw new Error(`Existing rule "${expected.stable_rule_id}" conflicts with the requested definition; refusing to overwrite it.`);
    }
  }
  return missing;
}

function ruleIdentity(rule) {
  return rule.stable_rule_id || `${rule.name}:${rule.display_order}`;
}

function ruleSignature(rule) {
  return canonicalize({
    identity: ruleIdentity(rule),
    display_order: rule.display_order,
    name: rule.name,
    description: rule.description,
    formula: rule.formula,
    score_modifier: rule.score_modifier,
    rule_group: rule.rule_group,
    snooze_group_id: rule.snooze_group_id ?? null,
  });
}

function iterationSettings(iteration) {
  return canonicalize({
    trigger_formula: iteration.trigger_formula,
    score_review_threshold: iteration.score_review_threshold,
    score_block_and_review_threshold: iteration.score_block_and_review_threshold,
    score_decline_threshold: iteration.score_decline_threshold,
    schedule: iteration.schedule ?? '',
  });
}

function assertRuleSetExtendsBase(baseRules, draftRules, desiredRules, scenarioName) {
  const draftByIdentity = new Map(draftRules.map((rule) => [ruleIdentity(rule), rule]));
  for (const baseRule of baseRules) {
    const draftRule = draftByIdentity.get(ruleIdentity(baseRule));
    if (!draftRule || JSON.stringify(ruleSignature(draftRule)) !== JSON.stringify(ruleSignature(baseRule))) {
      throw new Error(`Scenario "${scenarioName}" has an existing draft that changes or removes a base rule; refusing to publish it.`);
    }
  }
  const baseIdentities = new Set(baseRules.map(ruleIdentity));
  const desired = new Map(desiredRules.map((rule) => [rule.stable_rule_id, rule]));
  for (const draftRule of draftRules) {
    if (baseIdentities.has(ruleIdentity(draftRule))) continue;
    const intended = desired.get(draftRule.stable_rule_id);
    if (!intended || !assertRuleMatches(draftRule, intended)) {
      throw new Error(`Scenario "${scenarioName}" has an unrelated rule in its draft; inspect it before rerunning.`);
    }
  }
}

function assertInitialDraftIsSafe(draftRules, desiredRules, scenarioName) {
  const desired = new Map(desiredRules.map((rule) => [rule.stable_rule_id, rule]));
  for (const rule of draftRules) {
    const expected = desired.get(rule.stable_rule_id);
    if (!expected || !assertRuleMatches(rule, expected)) {
      throw new Error(`Scenario "${scenarioName}" has an unrelated rule in its uncommitted draft; inspect it before rerunning.`);
    }
  }
}

async function getRules(origin, token, scenarioPath, iterationId) {
  const response = await requestJson(origin, '/services/decisions', token, `${scenarioPath}/iterations/${encodeURIComponent(iterationId)}/rules`);
  if (!Array.isArray(response?.rules)) throw new Error(`Iteration ${iterationId} returned a malformed rule list.`);
  return response.rules;
}

async function getIteration(origin, token, scenarioPath, iterationId) {
  return getEnvelope(
    await requestJson(origin, '/services/decisions', token, `${scenarioPath}/iterations/${encodeURIComponent(iterationId)}`),
    'iteration',
    `Iteration ${iterationId} readback`,
  );
}

async function configureIteration(origin, token, scenarioPath, iteration, definition) {
  const iterationPath = `${scenarioPath}/iterations/${encodeURIComponent(iteration.id)}`;
  const update = await requestJson(origin, '/services/decisions', token, iterationPath, {
    method: 'PUT',
    body: {
      trigger_formula: definition.trigger,
      score_review_threshold: definition.thresholds[0],
      score_block_and_review_threshold: definition.thresholds[1],
      score_decline_threshold: definition.thresholds[2],
      schedule: iteration.schedule ?? '',
    },
  });
  const updated = getEnvelope(update, 'iteration', 'Update iteration');
  if (JSON.stringify(canonicalize(updated.trigger_formula)) !== JSON.stringify(canonicalize(definition.trigger)) ||
      updated.score_review_threshold !== definition.thresholds[0] ||
      updated.score_block_and_review_threshold !== definition.thresholds[1] ||
      updated.score_decline_threshold !== definition.thresholds[2]) {
    throw new Error(`Scenario "${definition.name}" draft settings did not persist.`);
  }
}

async function validateDraftIteration(origin, token, scenarioPath, iteration, definition, rules) {
  const scenarioName = definition.name;
  if (iteration.status !== 'draft') {
    throw new Error(`Scenario "${scenarioName}" iteration must remain a draft; current status is "${iteration.status}".`);
  }
  const missing = findMissingRules(rules, definition.rules);
  if (missing.length) {
    throw new Error(`Scenario "${scenarioName}" iteration is missing rules: ${missing.map(({ stable_rule_id }) => stable_rule_id).join(', ')}`);
  }

  const iterationPath = `${scenarioPath}/iterations/${encodeURIComponent(iteration.id)}`;
  const validationResponse = await requestJson(origin, '/services/decisions', token, `${iterationPath}/validate`, {
    method: 'POST',
    body: {},
  });
  const validation = getEnvelope(validationResponse, 'validation', 'Iteration validation');
  if (!validation.valid || validation.rule_results?.length !== rules.length ||
      validation.rule_results.some((rule) => !rule.valid)) {
    throw new Error(`Scenario "${scenarioName}" validation failed: ${JSON.stringify(validation.errors ?? validation.trigger_errors ?? validation.rule_results)}`);
  }
  console.log(`Validated ${rules.length} rules for ${scenarioName}.`);
  const validatedIteration = await getIteration(origin, token, scenarioPath, iteration.id);
  if (validatedIteration.status !== 'draft') {
    throw new Error(`Scenario "${scenarioName}" changed out of draft status during validation.`);
  }
  console.log(`Validated draft remains unpublished: ${scenarioName} (iteration ${iteration.version}).`);
}

async function createOrUpdateScenarioDraft(origin, tenantId, token, definition, existingScenario) {
  const tenantPath = `/tenants/${encodeURIComponent(tenantId)}`;
  let scenario = existingScenario;
  if (!scenario) {
    const response = await requestJson(origin, '/services/decisions', token, `${tenantPath}/scenarios`, {
      method: 'POST',
      body: {
        name: definition.name,
        description: definition.description,
        trigger_object_type: definition.trigger_object_type,
      },
    });
    scenario = getEnvelope(response, 'scenario', `Create scenario "${definition.name}"`);
    console.log(`Created scenario: ${definition.name}`);
  }
  if (!scenario.id || scenario.name !== definition.name ||
      scenario.trigger_object_type !== definition.trigger_object_type) {
    throw new Error(`Existing scenario "${definition.name}" has a conflicting trigger object type or invalid identity.`);
  }

  const scenarioPath = `${tenantPath}/scenarios/${encodeURIComponent(scenario.id)}`;
  const expectedScenarioId = scenario.id;
  scenario = getEnvelope(await requestJson(origin, '/services/decisions', token, scenarioPath), 'scenario', 'Scenario readback');
  if (scenario.id !== expectedScenarioId || scenario.name !== definition.name ||
      scenario.trigger_object_type !== definition.trigger_object_type) {
    throw new Error(`Scenario "${definition.name}" failed readback verification.`);
  }

  const iterationResponse = await requestJson(origin, '/services/decisions', token, `${scenarioPath}/iterations`);
  if (!Array.isArray(iterationResponse?.iterations)) throw new Error(`Scenario "${definition.name}" returned a malformed iteration list.`);
  const iterations = iterationResponse.iterations;
  let liveIteration = null;
  if (scenario.live_iteration_id) {
    liveIteration = iterations.find(({ id }) => id === scenario.live_iteration_id) ??
      await getIteration(origin, token, scenarioPath, scenario.live_iteration_id);
    if (liveIteration.status !== 'committed') {
      throw new Error(`Scenario "${definition.name}" points to a non-committed live iteration.`);
    }
    if (JSON.stringify(canonicalize(liveIteration.trigger_formula)) !== JSON.stringify(canonicalize(definition.trigger))) {
      throw new Error(`Scenario "${definition.name}" live trigger differs from this script; refusing to change it.`);
    }
    const liveRules = await getRules(origin, token, scenarioPath, liveIteration.id);
    if (findMissingRules(liveRules, definition.rules).length === 0) {
      console.log(`Already complete; leaving live scenario unchanged: ${definition.name}`);
      return;
    }
  }

  const committed = iterations
    .filter(({ status }) => status === 'committed')
    .sort((left, right) => right.version - left.version);
  const baseIteration = liveIteration ?? committed[0] ?? null;
  const baseRules = baseIteration ? await getRules(origin, token, scenarioPath, baseIteration.id) : [];
  const drafts = iterations.filter(({ status }) => status === 'draft');
  if (drafts.length > 1) {
    throw new Error(`Scenario "${definition.name}" has multiple drafts; inspect them before provisioning.`);
  }
  if (baseIteration && !liveIteration &&
      JSON.stringify(canonicalize(baseIteration.trigger_formula)) !== JSON.stringify(canonicalize(definition.trigger))) {
    throw new Error(`Scenario "${definition.name}" committed trigger differs from this script; refusing to change it.`);
  }
  if (!liveIteration && baseIteration && drafts.length === 0 &&
      findMissingRules(baseRules, definition.rules).length === 0) {
    console.log(`Already complete in a committed, non-live iteration; leaving it unpublished: ${definition.name}`);
    return;
  }

  let draft;
  let draftRules;
  if (drafts.length === 1) {
    draft = drafts[0];
    draftRules = await getRules(origin, token, scenarioPath, draft.id);
    if (baseIteration) {
      if (JSON.stringify(iterationSettings(draft)) !== JSON.stringify(iterationSettings(baseIteration))) {
        throw new Error(`Scenario "${definition.name}" draft settings diverge from its base version; refusing to overwrite it.`);
      }
      assertRuleSetExtendsBase(baseRules, draftRules, definition.rules, definition.name);
    } else {
      assertInitialDraftIsSafe(draftRules, definition.rules, definition.name);
      await configureIteration(origin, token, scenarioPath, draft, definition);
      draft = await getIteration(origin, token, scenarioPath, draft.id);
    }
    console.log(`Resuming existing draft version ${draft.version}: ${definition.name}`);
  } else if (baseIteration) {
    const response = await requestJson(origin, '/services/decisions', token, `${scenarioPath}/iterations/${encodeURIComponent(baseIteration.id)}/draft`, {
      method: 'POST',
      body: {},
    });
    draft = getEnvelope(response, 'iteration', 'Create draft from existing iteration');
    if (!draft.id || draft.status !== 'draft' ||
        JSON.stringify(iterationSettings(draft)) !== JSON.stringify(iterationSettings(baseIteration))) {
      throw new Error(`Scenario "${definition.name}" draft does not match its source iteration.`);
    }
    draftRules = await getRules(origin, token, scenarioPath, draft.id);
    if (draftRules.length !== baseRules.length) {
      throw new Error(`Scenario "${definition.name}" draft did not clone every source rule.`);
    }
    assertRuleSetExtendsBase(baseRules, draftRules, definition.rules, definition.name);
    console.log(`Created draft version ${draft.version} from existing rules: ${definition.name}`);
  } else {
    const response = await requestJson(origin, '/services/decisions', token, `${scenarioPath}/iterations`, {
      method: 'POST',
      body: {},
    });
    draft = getEnvelope(response, 'iteration', 'Create initial iteration');
    if (!draft.id || draft.status !== 'draft') throw new Error(`Scenario "${definition.name}" did not create a draft iteration.`);
    await configureIteration(origin, token, scenarioPath, draft, definition);
    draft = await getIteration(origin, token, scenarioPath, draft.id);
    draftRules = await getRules(origin, token, scenarioPath, draft.id);
  }

  draftRules ??= await getRules(origin, token, scenarioPath, draft.id);
  const missingRules = findMissingRules(draftRules, definition.rules);
  for (const expected of missingRules) {
    const displayOrder = draftRules.reduce((highest, rule) => Math.max(highest, rule.display_order ?? 0), 0) + 1;
    const rulePath = `${scenarioPath}/iterations/${encodeURIComponent(draft.id)}/rules`;
    const response = await requestJson(origin, '/services/decisions', token, rulePath, {
      method: 'POST',
      body: rulePayload(expected, displayOrder),
    });
    const created = getEnvelope(response, 'rule', `Create rule "${expected.name}"`);
    if (!assertRuleMatches(created, expected)) {
      throw new Error(`Rule "${expected.name}" create response differs from the requested rule.`);
    }
    draftRules = await getRules(origin, token, scenarioPath, draft.id);
    if (findMissingRules(draftRules, [expected]).length) {
      throw new Error(`Rule "${expected.name}" failed readback verification.`);
    }
    console.log(`  Added missing rule: ${expected.name}`);
  }

  draftRules = await getRules(origin, token, scenarioPath, draft.id);
  await validateDraftIteration(origin, token, scenarioPath, draft, definition, draftRules);
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
  const model = await loadAndValidateModel(options.modelFile);
  const scenarios = buildScenarioDefinitions(options.lateCallbackSeconds, options);
  validateDefinitionsAgainstModel(scenarios, model);
  console.log(`Model: ${model.tables.length} tables; planned scenarios: ${scenarios.length}.`);
  console.log('All supported scalar and Aggregator-pushdown rules will be provisioned.');
  if (omittedRules.length) console.log('Rules still omitted:');
  for (const rule of omittedRules) {
    console.log(`  ${rule.id}: ${rule.reason}`);
  }

  if (options.dryRun) {
    console.log('Dry run complete; no network requests or changes were made.');
    return;
  }
  const token = await exchangeClientProof(options.keyDir, options.clientId, options.tokenUrl, CLIENT_SCOPES);

  const tenantPath = `/tenants/${encodeURIComponent(options.tenantId)}`;
  const tenantResponse = await requestJson(options.origin, '/services/data-model', token, tenantPath);
  const tenant = getEnvelope(tenantResponse, 'tenant', 'Tenant lookup');
  if (tenant.id !== options.tenantId || tenant.status !== 'active') {
    throw new Error(`Target tenant must exist and be active; received id=${tenant.id}, status=${tenant.status}.`);
  }
  const existing = await requestJson(options.origin, '/services/decisions', token, `${tenantPath}/scenarios`);
  if (!Array.isArray(existing?.scenarios)) throw new Error('Decision Engine scenario list response is malformed.');
  const byName = new Map();
  for (const scenario of existing.scenarios) {
    if (byName.has(scenario.name)) {
      throw new Error(`Multiple existing scenarios are named "${scenario.name}"; resolve the ambiguity before provisioning.`);
    }
    byName.set(scenario.name, scenario);
  }
  console.log(`Verified active tenant ${options.tenantId}; existing scenarios will be reconciled by name and stable rule ID.`);

  await verifyExistingModel(options.origin, options.tenantId, token, model);
  for (const definition of scenarios) {
    await createOrUpdateScenarioDraft(
      options.origin,
      options.tenantId,
      token,
      definition,
      byName.get(definition.name),
    );
  }
  console.log('Provisioning complete: existing model and live scenarios unchanged; supported rules reconciled in validated, unpublished drafts.');
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  main().catch((error) => {
    console.error(`Provisioning failed: ${error.message}`);
    process.exitCode = 1;
  });
}
