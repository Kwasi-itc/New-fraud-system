import http from 'k6/http';
import { check, fail, group, sleep } from 'k6';
import exec from 'k6/execution';
import { Counter, Rate, Trend } from 'k6/metrics';

const DATA_MODEL_URL = (__ENV.DATA_MODEL_URL || 'http://127.0.0.1:8080').replace(/\/$/, '');
const INGESTION_URL = (__ENV.INGESTION_URL || 'http://127.0.0.1:8081').replace(/\/$/, '');
const DECISION_ENGINE_URL = (__ENV.DECISION_ENGINE_URL || 'http://127.0.0.1:8082').replace(/\/$/, '');
const PROFILE = __ENV.STRESS_PROFILE || 'mixed';
const EXECUTOR_MODE = __ENV.STRESS_EXECUTOR || 'auto';
const RATE = Number(__ENV.STRESS_RATE || '20');
const VUS = Number(__ENV.STRESS_VUS || '20');
const DURATION = __ENV.STRESS_DURATION || '2m';
const PRE_ALLOCATED_VUS = Number(__ENV.STRESS_PRE_ALLOCATED_VUS || Math.max(VUS, RATE * 2));
const MAX_VUS = Number(__ENV.STRESS_MAX_VUS || Math.max(PRE_ALLOCATED_VUS, RATE * 4));
const HEAVY_SEED_RECORDS = Number(__ENV.HEAVY_SEED_RECORDS || '750');
const SCENARIO_COUNT = Number(__ENV.SCENARIO_COUNT || '2');
const RULES_PER_SCENARIO = Number(__ENV.RULES_PER_SCENARIO || '2');
const THINK_TIME_SECONDS = Number(__ENV.THINK_TIME_SECONDS || '0');

const failureRate = new Rate('stress_failures');
const setupFailures = new Counter('stress_setup_failures');
const ingestLatency = new Trend('stress_ingest_latency', true);
const decisionLatency = new Trend('stress_decision_latency', true);
const heavyDecisionLatency = new Trend('stress_heavy_decision_latency', true);
const callbackLatency = new Trend('stress_callback_latency', true);
const scenariosCreated = new Counter('stress_setup_scenarios_created');
const rulesCreated = new Counter('stress_setup_rules_created');
const setupIngestedRecords = new Counter('stress_setup_ingested_records');
const runtimeIngests = new Counter('stress_runtime_ingests');
const directEvaluations = new Counter('stress_direct_evaluations');
const heavyEvaluations = new Counter('stress_heavy_evaluations');
const callbackEvaluations = new Counter('stress_callback_evaluations');

const profileExecutions = {
  concurrent_single_ingest: 'concurrentSingleIngest',
  decision_evaluation: 'decisionEvaluation',
  heavy_rule_evaluation: 'heavyRuleEvaluation',
  ingestion_callback: 'ingestionCallback',
  mixed: 'mixedWorkload',
};

if (!profileExecutions[PROFILE]) {
  throw new Error(`Unknown STRESS_PROFILE ${PROFILE}. Use one of: ${Object.keys(profileExecutions).join(', ')}`);
}

const activeScenario = scenarioConfig(PROFILE);

export const options = {
  scenarios: {
    [PROFILE]: activeScenario,
  },
  thresholds: thresholdsForProfile(PROFILE),
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

function scenarioConfig(profile) {
  const execName = profileExecutions[profile];
  const mode = resolvedExecutorMode(profile);
  if (mode === 'constant-vus') {
    return {
      executor: 'constant-vus',
      vus: VUS,
      duration: DURATION,
      exec: execName,
    };
  }
  return {
    executor: 'constant-arrival-rate',
    rate: RATE,
    timeUnit: '1s',
    duration: DURATION,
    preAllocatedVUs: PRE_ALLOCATED_VUS,
    maxVUs: MAX_VUS,
    exec: execName,
  };
}

function resolvedExecutorMode(profile) {
  if (EXECUTOR_MODE === 'constant-vus' || EXECUTOR_MODE === 'arrival-rate') {
    return EXECUTOR_MODE === 'arrival-rate' ? 'constant-arrival-rate' : EXECUTOR_MODE;
  }
  if (profile === 'concurrent_single_ingest' || profile === 'mixed') {
    return 'constant-vus';
  }
  return 'constant-arrival-rate';
}

function thresholdsForProfile(profile) {
  const thresholds = {
    checks: ['rate>=0.99'],
    stress_failures: ['rate<0.01'],
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<2000', 'p(99)<5000'],
  };
  if (profile === 'concurrent_single_ingest' || profile === 'mixed') {
    thresholds.stress_ingest_latency = ['p(95)<1500'];
  }
  if (profile === 'decision_evaluation' || profile === 'mixed') {
    thresholds.stress_decision_latency = ['p(95)<2500'];
  }
  if (profile === 'heavy_rule_evaluation' || profile === 'mixed') {
    thresholds.stress_heavy_decision_latency = ['p(95)<3000'];
  }
  if (profile === 'ingestion_callback' || profile === 'mixed') {
    thresholds.stress_callback_latency = ['p(95)<3000'];
  }
  return thresholds;
}

export function setup() {
  waitForReady(DATA_MODEL_URL, 'data-model');
  waitForReady(INGESTION_URL, 'ingestion');
  waitForReady(DECISION_ENGINE_URL, 'decision-engine');

  const model = createTenantModel();
  const heavyOwner = uniqueName('heavy_owner');
  const scenarioCount = Math.max(1, SCENARIO_COUNT);
  const rulesPerScenario = Math.max(1, RULES_PER_SCENARIO);
  const needsHeavy = PROFILE === 'heavy_rule_evaluation' || PROFILE === 'mixed';

  if (needsHeavy) {
    seedHeavyRecords(model, heavyOwner, HEAVY_SEED_RECORDS);
  }

  const simpleScenarios = [];
  const heavyScenarios = [];
  for (let index = 0; index < scenarioCount; index += 1) {
    const simpleScenario = createScenarioBundle(model, `simple_decision_${index}`, simpleRules(rulesPerScenario));
    addDecisionAmplifiers(model.tenant_id, simpleScenario.scenario.id);
    simpleScenarios.push(simpleScenario);

    if (needsHeavy) {
      const heavyScenario = createScenarioBundle(
        model,
        `heavy_related_records_${index}`,
        heavyRules(model.transactions.name, heavyOwner, rulesPerScenario),
      );
      addDecisionAmplifiers(model.tenant_id, heavyScenario.scenario.id);
      heavyScenarios.push(heavyScenario);
    }
  }

  return {
    model,
    simpleScenario: simpleScenarios[0],
    heavyScenario: heavyScenarios[0] || null,
    simpleScenarios,
    heavyScenarios,
    heavyOwner,
    profile: PROFILE,
    counts: {
      requested_scenario_count: scenarioCount,
      rules_per_scenario: rulesPerScenario,
      simple_scenarios_created: simpleScenarios.length,
      heavy_scenarios_created: heavyScenarios.length,
      scenarios_created: simpleScenarios.length + heavyScenarios.length,
      rules_created: (simpleScenarios.length + heavyScenarios.length) * rulesPerScenario,
      setup_ingested_records: needsHeavy ? HEAVY_SEED_RECORDS : 0,
      workflows_created: simpleScenarios.length + heavyScenarios.length,
      screening_configs_created: simpleScenarios.length + heavyScenarios.length,
      scoring_configs_created: simpleScenarios.length + heavyScenarios.length,
    },
  };
}

export function concurrentSingleIngest(data) {
  group('concurrent single ingest', () => {
    const res = timedPost(
      `${INGESTION_URL}/v1/tenants/${data.model.tenant_id}/ingest/${data.model.transactions.name}`,
      transactionPayload(uniqueName('single'), 1200),
      { endpoint: 'single_ingest' },
      ingestLatency,
    );
    assertResponse(res, 200, 'single ingest accepted');
    runtimeIngests.add(1);
  });
  maybeSleep();
}

export function decisionEvaluation(data) {
  group('decision evaluation', () => {
    const record = transactionPayload(uniqueName('decision'), 1800);
    const bundle = pickBundle(data.simpleScenarios);
    evaluateScenario(data.model.tenant_id, bundle.scenario.id, data.model.transactions.name, record, decisionLatency);
    directEvaluations.add(1);
  });
  maybeSleep();
}

export function heavyRuleEvaluation(data) {
  group('heavy rule evaluation', () => {
    if (!data.heavyScenarios || data.heavyScenarios.length === 0) {
      fail('heavy scenario was not prepared');
    }
    const record = transactionPayload(uniqueName('heavy_eval'), 25, data.heavyOwner);
    const bundle = pickBundle(data.heavyScenarios);
    evaluateScenario(data.model.tenant_id, bundle.scenario.id, data.model.transactions.name, record, heavyDecisionLatency);
    heavyEvaluations.add(1);
  });
  maybeSleep();
}

export function ingestionCallback(data) {
  group('ingestion callback evaluation', () => {
    const record = transactionPayload(uniqueName('callback'), 1800);
    const res = timedPost(
      `${DECISION_ENGINE_URL}/v1/tenants/${data.model.tenant_id}/ingestion-events/record-ingested`,
      {
        object_id: record.object_id,
        object_type: data.model.transactions.name,
        fields: record,
        source: 'k6-stress',
      },
      { endpoint: 'ingestion_callback' },
      callbackLatency,
    );
    assertResponse(res, 200, 'ingestion callback evaluated');
    callbackEvaluations.add(1);
  });
  maybeSleep();
}

export function mixedWorkload(data) {
  const lane = exec.scenario.iterationInTest % 4;
  if (lane === 0) {
    concurrentSingleIngest(data);
  } else if (lane === 1) {
    decisionEvaluation(data);
  } else if (lane === 2 && data.heavyScenarios && data.heavyScenarios.length > 0) {
    heavyRuleEvaluation(data);
  } else {
    ingestionCallback(data);
  }
}

function waitForReady(baseUrl, name) {
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    const res = http.get(`${baseUrl}/readyz`, requestParams({ endpoint: `${name}_readyz` }));
    if (res.status === 200) {
      return;
    }
    sleep(0.5);
  }
  setupFailures.add(1);
  fail(`${name} was not ready at ${baseUrl}`);
}

function createTenantModel() {
  const tenant = postJson(DATA_MODEL_URL, '/v1/tenants', {
    name: uniqueName('stress_tenant'),
    external_key: uniqueName('stress_ext'),
  }, 201, 'tenant').tenant;
  postJson(DATA_MODEL_URL, `/v1/tenants/${tenant.id}/provision`, {}, 200, 'tenant provision');

  const transactions = postJson(DATA_MODEL_URL, `/v1/tenants/${tenant.id}/tables`, {
    name: uniqueName('transactions'),
    description: 'Stress transaction table',
    alias: 'Transactions',
    semantic_type: 'entity',
  }, 201, 'transactions table').table;
  const accounts = postJson(DATA_MODEL_URL, `/v1/tenants/${tenant.id}/tables`, {
    name: uniqueName('accounts'),
    description: 'Stress account table',
    alias: 'Accounts',
    semantic_type: 'entity',
  }, 201, 'accounts table').table;

  const fields = {};
  [
    { name: 'amount', data_type: 'int', nullable: false },
    { name: 'status', data_type: 'string', nullable: false, is_enum: true, enum_values: [{ value: 'pending', label: 'Pending', sort_order: 10 }] },
    { name: 'account_id', data_type: 'string', nullable: true },
    { name: 'ip', data_type: 'ip_address', nullable: true },
    { name: 'merchant', data_type: 'string', nullable: false },
    { name: 'email', data_type: 'string', nullable: false },
    { name: 'country', data_type: 'string', nullable: false },
    { name: 'owner_id', data_type: 'string', nullable: true },
    { name: 'event_time', data_type: 'timestamp', nullable: true },
    { name: 'note', data_type: 'string', nullable: true },
  ].forEach((field) => {
    const created = postJson(DATA_MODEL_URL, `/v1/tables/${transactions.id}/fields`, field, 201, `transaction field ${field.name}`).field;
    fields[created.name] = created;
  });

  const accountKey = postJson(DATA_MODEL_URL, `/v1/tables/${accounts.id}/fields`, {
    name: 'account_key',
    data_type: 'string',
    nullable: false,
    is_unique: true,
  }, 201, 'account key').field;
  postJson(DATA_MODEL_URL, `/v1/tables/${accounts.id}/fields`, {
    name: 'account_status',
    data_type: 'string',
    nullable: false,
  }, 201, 'account status');
  postJson(DATA_MODEL_URL, `/v1/tables/${accounts.id}/fields`, {
    name: 'owner_id',
    data_type: 'string',
    nullable: true,
  }, 201, 'account owner');
  postJson(DATA_MODEL_URL, `/v1/tenants/${tenant.id}/links`, {
    name: 'account',
    parent_table_id: accounts.id,
    parent_field_id: accountKey.id,
    child_table_id: transactions.id,
    child_field_id: fields.account_id.id,
  }, 201, 'account link');

  return {
    tenant,
    tenant_id: tenant.id,
    transactions,
    accounts,
  };
}

function createScenarioBundle(model, name, rules) {
  const scenario = postJson(DECISION_ENGINE_URL, `/v1/tenants/${model.tenant_id}/scenarios`, {
    name: uniqueName(name),
    trigger_object_type: model.transactions.name,
  }, 201, `${name} scenario`).scenario;
  scenariosCreated.add(1);
  const iteration = postJson(DECISION_ENGINE_URL, `/v1/tenants/${model.tenant_id}/scenarios/${scenario.id}/iterations`, {}, 201, `${name} iteration`).iteration;
  putJson(DECISION_ENGINE_URL, `/v1/tenants/${model.tenant_id}/scenarios/${scenario.id}/iterations/${iteration.id}`, {
    trigger_formula: trueNode(),
    score_review_threshold: 10,
    score_block_and_review_threshold: 30,
    score_decline_threshold: 60,
    schedule: '',
  }, 200, `${name} iteration update`);

  rules.forEach((rule, index) => {
    postJson(DECISION_ENGINE_URL, `/v1/tenants/${model.tenant_id}/scenarios/${scenario.id}/iterations/${iteration.id}/rules`, {
      display_order: index + 1,
      name: rule.name,
      description: rule.name,
      formula: rule.formula,
      score_modifier: rule.score_modifier,
      rule_group: 'stress',
      stable_rule_id: uniqueName(rule.name),
    }, 201, `${name} rule ${rule.name}`);
    rulesCreated.add(1);
  });

  validateIteration(model.tenant_id, scenario.id, iteration.id, name);
  postJson(DECISION_ENGINE_URL, `/v1/tenants/${model.tenant_id}/scenarios/${scenario.id}/iterations/${iteration.id}/commit`, {}, 200, `${name} commit`);
  postJson(DECISION_ENGINE_URL, `/v1/tenants/${model.tenant_id}/scenarios/${scenario.id}/publications`, {
    action: 'publish',
    iteration_id: iteration.id,
  }, 200, `${name} publish`);

  return { scenario, iteration };
}

function simpleRules(count) {
  const rules = [];
  for (let index = 0; index < count; index += 1) {
    const limit = 1000 + (index * 25);
    rules.push({
      name: `amount_over_${limit}`,
      formula: amountGtNode(limit),
      score_modifier: 5,
    });
  }
  return rules;
}

function heavyRules(objectType, ownerId, count) {
  const rules = [];
  for (let index = 0; index < count; index += 1) {
    if (index % 2 === 0) {
      rules.push({
        name: `same_owner_velocity_gt_${500 + index}`,
        formula: relatedCountGtNode(objectType, 'owner_id', ownerId, 500 + index),
        score_modifier: 5,
      });
    } else {
      rules.push({
        name: `amount_control_gt_${1000000 + index}`,
        formula: amountGtNode(1000000 + index),
        score_modifier: 1,
      });
    }
  }
  return rules;
}

function pickBundle(bundles) {
  const index = exec.scenario.iterationInTest % bundles.length;
  return bundles[index];
}

function validateIteration(tenantId, scenarioId, iterationId, label) {
  const body = postJson(
    DECISION_ENGINE_URL,
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/iterations/${iterationId}/validate`,
    {},
    200,
    `${label} validation`,
  );
  if (!body.validation || body.validation.valid !== true) {
    setupFailures.add(1);
    fail(`${label} validation was not valid: ${JSON.stringify(body)}`);
  }
}

function addDecisionAmplifiers(tenantId, scenarioId) {
  postJson(DECISION_ENGINE_URL, `/v1/tenants/${tenantId}/scenarios/${scenarioId}/workflows`, {
    name: uniqueName('stress_workflow'),
    description: 'Stress workflow dispatch artifact',
    allowed_outcomes: ['review', 'block_and_review', 'decline'],
    action_type: 'emit_event',
    action_config: { topic: 'stress.decision' },
    active: true,
  }, 201, 'workflow');
  postJson(DECISION_ENGINE_URL, `/v1/tenants/${tenantId}/scenarios/${scenarioId}/screening-configs`, {
    name: uniqueName('stress_screening'),
    allowed_outcomes: ['review', 'block_and_review', 'decline'],
    provider: 'stress-provider',
    config_json: { mode: 'stress' },
    active: true,
  }, 201, 'screening config');
  postJson(DECISION_ENGINE_URL, `/v1/tenants/${tenantId}/scenarios/${scenarioId}/scoring-configs`, {
    name: uniqueName('stress_scoring'),
    allowed_outcomes: ['review', 'block_and_review', 'decline'],
    ruleset_ref: 'stress-ruleset',
    config_json: { mode: 'stress' },
    active: true,
  }, 201, 'scoring config');
}

function seedHeavyRecords(model, ownerId, count) {
  const batchSize = 500;
  for (let start = 0; start < count; start += batchSize) {
    const rows = [];
    const end = Math.min(start + batchSize, count);
    for (let index = start; index < end; index += 1) {
      rows.push(transactionPayload(`${ownerId}_seed_${index}`, 25, ownerId));
    }
    postJson(INGESTION_URL, `/v1/tenants/${model.tenant_id}/ingest/${model.transactions.name}/batch`, rows, 200, `seed heavy records ${start}`);
    setupIngestedRecords.add(rows.length);
  }
}

function evaluateScenario(tenantId, scenarioId, objectType, record, trend) {
  const res = timedPost(
    `${DECISION_ENGINE_URL}/v1/tenants/${tenantId}/scenarios/${scenarioId}/evaluate`,
    {
      object_id: record.object_id,
      object_type: objectType,
      fields: record,
    },
    { endpoint: 'scenario_evaluate' },
    trend,
  );
  assertResponse(res, 200, 'scenario evaluated');
}

function timedPost(url, body, tags, trend) {
  const res = http.post(url, JSON.stringify(body), requestParams(tags));
  trend.add(res.timings.duration, tags);
  return res;
}

function postJson(baseUrl, path, body, expectedStatus, label) {
  const res = http.post(`${baseUrl}${path}`, JSON.stringify(body), requestParams({ endpoint: label }));
  return expectJson(res, expectedStatus, label);
}

function putJson(baseUrl, path, body, expectedStatus, label) {
  const res = http.put(`${baseUrl}${path}`, JSON.stringify(body), requestParams({ endpoint: label }));
  return expectJson(res, expectedStatus, label);
}

function expectJson(res, expectedStatus, label) {
  if (res.status !== expectedStatus) {
    setupFailures.add(1);
    fail(`${label} returned ${res.status}, expected ${expectedStatus}: ${res.body}`);
  }
  if (!res.body) {
    return {};
  }
  return parseJson(res, label);
}

function assertResponse(res, expectedStatus, label) {
  const ok = check(res, {
    [label]: (response) => response.status === expectedStatus,
  });
  failureRate.add(!ok);
  if (!ok) {
    console.error(`${label} returned ${res.status}: ${res.body}`);
  }
}

function parseJson(res, label) {
  try {
    return res.json();
  } catch (error) {
    setupFailures.add(1);
    fail(`${label} did not return JSON: ${res.body}`);
    return {};
  }
}

function requestParams(tags = {}) {
  const headers = {
    'Content-Type': 'application/json',
  };
  if (__ENV.SERVICE_AUTH_TOKEN) {
    headers.Authorization = `Bearer ${__ENV.SERVICE_AUTH_TOKEN}`;
  }
  return {
    headers,
    timeout: '30s',
    tags,
  };
}

function transactionPayload(objectId, amount = 1250, ownerId = null) {
  const suffix = objectId.replace(/[^A-Za-z0-9_]/g, '_');
  return {
    object_id: objectId,
    amount,
    status: 'pending',
    account_id: `acct_${suffix}`,
    ip: '1.2.3.4',
    merchant: 'ITC Market',
    email: `risk_${suffix}@example.com`,
    country: 'gh',
    owner_id: ownerId || `owner_${suffix}`,
    event_time: new Date().toISOString(),
    note: null,
  };
}

function trueNode() {
  return { function: 'eq', children: [{ constant: 1 }, { constant: 1 }] };
}

function amountGtNode(limit) {
  return {
    function: 'gt',
    children: [
      { function: 'field_ref', named_children: { field: { constant: 'amount' } } },
      { constant: limit },
    ],
  };
}

function relatedCountGtNode(objectType, matchField, ownerId, limit) {
  return {
    function: 'gt',
    children: [
      {
        function: 'list_count',
        children: [
          {
            function: 'related_records',
            named_children: {
              object_type: { constant: objectType },
              match_field: { constant: matchField },
              equals: { constant: ownerId },
              timestamp_field: { constant: 'event_time' },
              within_hours: { constant: 1 },
            },
          },
        ],
      },
      { constant: limit },
    ],
  };
}

function uniqueName(prefix) {
  const random = Math.floor(Math.random() * 1000000000);
  return `${prefix}_${Date.now()}_${random}`;
}

function maybeSleep() {
  if (THINK_TIME_SECONDS > 0) {
    sleep(THINK_TIME_SECONDS);
  }
}

export function handleSummary(data) {
  const focused = {
    summary_version: 2,
    run: {
      profile: PROFILE,
      executor: activeScenario.executor,
      executor_mode: EXECUTOR_MODE,
      configured_vus: VUS,
      pre_allocated_vus: PRE_ALLOCATED_VUS,
      max_vus: MAX_VUS,
      rate: RATE,
      duration: DURATION,
      heavy_seed_records: HEAVY_SEED_RECORDS,
      scenario_count: SCENARIO_COUNT,
      rules_per_scenario: RULES_PER_SCENARIO,
    },
    setup: data.setup_data ? {
      tenant_id: data.setup_data.model.tenant_id,
      object_type: data.setup_data.model.transactions.name,
      simple_scenario_id: data.setup_data.simpleScenario.scenario.id,
      heavy_scenario_id: data.setup_data.heavyScenario ? data.setup_data.heavyScenario.scenario.id : null,
      simple_scenario_ids: data.setup_data.simpleScenarios.map((bundle) => bundle.scenario.id),
      heavy_scenario_ids: data.setup_data.heavyScenarios.map((bundle) => bundle.scenario.id),
      heavy_owner: data.setup_data.heavyOwner,
      counts: data.setup_data.counts,
    } : null,
    workload_counts: {
      runtime_ingests: metricCount(data, 'stress_runtime_ingests'),
      direct_evaluations: metricCount(data, 'stress_direct_evaluations'),
      heavy_evaluations: metricCount(data, 'stress_heavy_evaluations'),
      callback_evaluations: metricCount(data, 'stress_callback_evaluations'),
      total_iterations: metricCount(data, 'iterations'),
      total_http_requests: metricCount(data, 'http_reqs'),
    },
    success: {
      checks_passed_rate: metricRate(data, 'checks'),
      http_failed_rate: metricRate(data, 'http_req_failed'),
      stress_failed_rate: metricRate(data, 'stress_failures'),
    },
    latency_ms: {
      ingest: trendStats(data, 'stress_ingest_latency'),
      direct_decision: trendStats(data, 'stress_decision_latency'),
      heavy_decision: trendStats(data, 'stress_heavy_decision_latency'),
      ingestion_callback: trendStats(data, 'stress_callback_latency'),
      overall_http: trendStats(data, 'http_req_duration'),
    },
  };

  return {
    'stress-tests/performance-summary.json': JSON.stringify(focused, null, 2),
    stdout: focusedTextSummary(focused),
  };
}

function trendStats(data, name) {
  const values = metricValues(data, name);
  if (!values) {
    return null;
  }
  return {
    avg: values.avg,
    med: values.med,
    p90: values['p(90)'],
    p95: values['p(95)'],
    p99: values['p(99)'],
    max: values.max,
  };
}

function metricCount(data, name) {
  const values = metricValues(data, name);
  if (!values) {
    return 0;
  }
  return values.count || 0;
}

function metricValue(data, name) {
  const values = metricValues(data, name);
  if (!values) {
    return null;
  }
  return values.value;
}

function metricRate(data, name) {
  const values = metricValues(data, name);
  if (!values) {
    return null;
  }
  if (values.rate !== undefined) {
    return values.rate;
  }
  if (values.value !== undefined) {
    return values.value;
  }
  if (values.passes !== undefined && values.fails !== undefined) {
    const total = values.passes + values.fails;
    return total === 0 ? null : values.passes / total;
  }
  return null;
}

function metricValues(data, name) {
  const metric = data.metrics[name];
  if (!metric) {
    return null;
  }
  return metric.values || metric;
}

function focusedTextSummary(summary) {
  return [
    '',
    'Stress Test Performance Summary',
    `  profile: ${summary.run.profile}`,
    `  executor: ${summary.run.executor}`,
    `  configured VUs: ${summary.run.configured_vus}`,
    `  pre-allocated VUs: ${summary.run.pre_allocated_vus}`,
    `  max VUs: ${summary.run.max_vus}`,
    `  target rate: ${summary.run.rate} iterations/s`,
    `  scenario count: ${summary.run.scenario_count}`,
    `  rules per scenario: ${summary.run.rules_per_scenario}`,
    `  setup scenarios created: ${summary.setup ? summary.setup.counts.scenarios_created : 'n/a'}`,
    `  setup rules created: ${summary.setup ? summary.setup.counts.rules_created : 'n/a'}`,
    `  iterations: ${summary.workload_counts.total_iterations}`,
    `  http requests: ${summary.workload_counts.total_http_requests}`,
    `  runtime ingests: ${summary.workload_counts.runtime_ingests}`,
    `  direct evaluations: ${summary.workload_counts.direct_evaluations}`,
    `  heavy evaluations: ${summary.workload_counts.heavy_evaluations}`,
    `  callback evaluations: ${summary.workload_counts.callback_evaluations}`,
    `  checks passed rate: ${formatNumber(summary.success.checks_passed_rate)}`,
    `  http failed rate: ${formatNumber(summary.success.http_failed_rate)}`,
    '',
    'Latency p95 / p99 / max, ms',
    `  ingest: ${formatTrend(summary.latency_ms.ingest)}`,
    `  direct decision: ${formatTrend(summary.latency_ms.direct_decision)}`,
    `  heavy decision: ${formatTrend(summary.latency_ms.heavy_decision)}`,
    `  ingestion callback: ${formatTrend(summary.latency_ms.ingestion_callback)}`,
    `  overall HTTP: ${formatTrend(summary.latency_ms.overall_http)}`,
    '',
    'Focused JSON written to stress-tests/performance-summary.json',
    '',
  ].join('\n');
}

function formatTrend(stats) {
  if (!stats) {
    return 'no samples';
  }
  return `${formatNumber(stats.p95)} / ${formatNumber(stats.p99)} / ${formatNumber(stats.max)}`;
}

function formatNumber(value) {
  if (value === null || value === undefined) {
    return 'n/a';
  }
  return Number(value).toFixed(2);
}
