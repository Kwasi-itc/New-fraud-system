export type DecisionEngineApiErrorEnvelope = {
  error?: {
    code?: string;
    message?: string;
  };
  details?: string;
};

export type JSONPrimitive = string | number | boolean | null;
export type JSONValue =
  | JSONPrimitive
  | JSONValue[]
  | { [key: string]: JSONValue };

export type Scenario = {
  id: string;
  tenant_id: string;
  name: string;
  description: string;
  trigger_object_type: string;
  live_iteration_id?: string | null;
  created_at: string;
  updated_at: string;
};

export type CreateScenarioRequest = {
  name: string;
  description?: string;
  trigger_object_type: string;
};

export type UpdateScenarioRequest = CreateScenarioRequest;

export type Iteration = {
  id: string;
  scenario_id: string;
  tenant_id: string;
  version: number;
  status: string;
  trigger_formula?: JSONValue;
  score_review_threshold?: number | null;
  score_block_and_review_threshold?: number | null;
  score_decline_threshold?: number | null;
  schedule?: string;
  created_at: string;
  committed_at?: string | null;
};

export type UpdateIterationRequest = {
  trigger_formula: JSONValue;
  score_review_threshold?: number | null;
  score_block_and_review_threshold?: number | null;
  score_decline_threshold?: number | null;
  schedule?: string;
};

export type Publication = {
  id: string;
  tenant_id: string;
  scenario_id: string;
  iteration_id: string;
  action: string;
  created_at: string;
};

export type PublicationActionRequest = {
  iteration_id: string;
  action: "publish" | "unpublish";
};

export type Rule = {
  id: string;
  iteration_id: string;
  tenant_id: string;
  display_order: number;
  name: string;
  description: string;
  formula: JSONValue;
  score_modifier: number;
  rule_group: string;
  snooze_group_id?: string | null;
  stable_rule_id: string;
  created_at: string;
  updated_at: string;
};

export type RuleGroupListResponse = {
  rule_groups: string[];
};

export type CreateRuleRequest = {
  display_order: number;
  name: string;
  description: string;
  formula: JSONValue;
  score_modifier: number;
  rule_group: string;
  snooze_group_id?: string | null;
  stable_rule_id: string;
};

export type UpdateRuleRequest = CreateRuleRequest;

export type RuleValidationResult = {
  rule_id: string;
  name: string;
  valid: boolean;
  errors: string[];
};

export type ValidationResult = {
  valid: boolean;
  model_revision: string;
  trigger_errors: string[];
  rule_results: RuleValidationResult[];
  errors: string[];
};

export type RuleFunctionArgument = {
  name: string;
  kind: string;
  required: boolean;
  description: string;
};

export type RuleFunction = {
  name: string;
  category: string;
  description: string;
  return_type: string;
  positional_arity?: number | null;
  supports_named_args: boolean;
  arguments: RuleFunctionArgument[];
  requires_model: boolean;
  requires_data_read: boolean;
  requires_platform: boolean;
  example: string;
};

export type ASTNodeDTO = {
  name?: string;
  constant?: JSONValue;
  children?: ASTNodeDTO[];
  named_children?: Record<string, ASTNodeDTO>;
};

export type EvaluateDecisionRequest = {
  object_id: string;
  object_type: string;
  fields: JSONValue;
};

export type Decision = {
  id: string;
  tenant_id: string;
  scenario_id: string;
  scenario_iteration_id: string;
  object_id: string;
  object_type: string;
  outcome: string;
  score: number;
  triggered: boolean;
  created_at: string;
};

export type DecisionDetail = Decision & {
  request_body?: JSONValue;
};

export type ListDecisionsRequest = {
  scenario_id?: string;
  object_type?: string;
  object_id?: string;
  limit?: number;
  offset?: number;
};

export type Pagination = {
  limit: number;
  offset: number;
  has_more: boolean;
  next_offset?: number;
};

export type RuleExecution = {
  id: string;
  decision_id: string;
  rule_id: string;
  rule_name: string;
  outcome: string;
  score_modifier: number;
  created_at: string;
};

export type DecisionEvaluationResult = {
  triggered: boolean;
  decision: Decision;
  rule_executions: RuleExecution[];
};

export type TestRunEvaluationResult = {
  live: {
    triggered: boolean;
    decision?: Decision;
    rule_executions?: RuleExecution[];
  };
  phantom: {
    triggered: boolean;
    decision?: Decision;
    rule_executions?: RuleExecution[];
  };
};

export type IngestionTriggerRequest = {
  object_type: string;
  object_id: string;
  fields: JSONValue;
};

export type MultiDecisionEvaluationResult = {
  results: DecisionEvaluationResult[];
};

export type TestRun = {
  id: string;
  tenant_id: string;
  scenario_id: string;
  live_iteration_id: string;
  phantom_iteration_id: string;
  status: string;
  created_at: string;
  expires_at: string;
  updated_at: string;
};

export type CreateTestRunRequest = {
  phantom_iteration_id: string;
  expires_at?: string;
};

export type TestRunDecisionSummary = {
  outcome: string;
  score: number;
  count: number;
};

export type TestRunRuleStat = {
  rule_id: string;
  rule_name: string;
  hit_count: number;
  no_hit_count: number;
  snoozed_count: number;
  total_count: number;
};

export type Workflow = {
  id: string;
  tenant_id: string;
  scenario_id: string;
  display_order: number;
  name: string;
  description: string;
  allowed_outcomes: string[];
  action_type: string;
  action_config: JSONValue;
  active: boolean;
  created_at: string;
  updated_at: string;
};

export type CreateWorkflowRequest = {
  name: string;
  description: string;
  allowed_outcomes: string[];
  action_type: string;
  action_config: JSONValue;
  active: boolean;
};

export type UpdateWorkflowRequest = CreateWorkflowRequest;

export type WorkflowExecution = {
  id: string;
  tenant_id: string;
  workflow_id: string;
  decision_id: string;
  scenario_id: string;
  action_type: string;
  status: string;
  action_config: JSONValue;
  created_at: string;
};

export type WorkflowRule = {
  id: string;
  tenant_id: string;
  scenario_id: string;
  name: string;
  priority: number;
  fallthrough: boolean;
  created_at: string;
  updated_at: string;
};

export type CreateWorkflowRuleRequest = {
  name: string;
  fallthrough: boolean;
};

export type UpdateWorkflowRuleRequest = CreateWorkflowRuleRequest;

export type WorkflowCondition = {
  id: string;
  tenant_id: string;
  rule_id: string;
  function: string;
  params: JSONValue;
  created_at: string;
  updated_at: string;
};

export type CreateWorkflowConditionRequest = {
  function: string;
  params: JSONValue;
};

export type UpdateWorkflowConditionRequest = CreateWorkflowConditionRequest;

export type WorkflowAction = {
  id: string;
  tenant_id: string;
  rule_id: string;
  action_type: string;
  action_config: JSONValue;
  created_at: string;
  updated_at: string;
};

export type CreateWorkflowActionRequest = {
  action_type: string;
  action_config: JSONValue;
};

export type UpdateWorkflowActionRequest = CreateWorkflowActionRequest;

export type ScreeningConfig = {
  id: string;
  tenant_id: string;
  scenario_id: string;
  name: string;
  allowed_outcomes: string[];
  provider: string;
  config_json: JSONValue;
  active: boolean;
  created_at: string;
  updated_at: string;
};

export type CreateScreeningConfigRequest = {
  name: string;
  allowed_outcomes: string[];
  provider: string;
  config_json: JSONValue;
  active: boolean;
};

export type ScreeningExecution = {
  id: string;
  tenant_id: string;
  config_id: string;
  decision_id: string;
  scenario_id: string;
  status: string;
  request_json: JSONValue;
  response_json: JSONValue;
  provider_reference: string;
  last_error: string;
  created_at: string;
  updated_at: string;
  sent_at?: string | null;
  completed_at?: string | null;
  failed_at?: string | null;
};

export type UpdateScreeningExecutionStatusRequest = {
  status: "pending" | "queued" | "sent" | "completed" | "failed";
  provider_reference?: string;
  response_json?: JSONValue;
  last_error?: string;
};

export type ScoringConfig = {
  id: string;
  tenant_id: string;
  scenario_id: string;
  name: string;
  allowed_outcomes: string[];
  ruleset_ref: string;
  config_json: JSONValue;
  active: boolean;
  created_at: string;
  updated_at: string;
};

export type CreateScoringConfigRequest = {
  name: string;
  allowed_outcomes: string[];
  ruleset_ref: string;
  config_json: JSONValue;
  active: boolean;
};

export type ScoringRequest = {
  id: string;
  tenant_id: string;
  config_id: string;
  decision_id: string;
  scenario_id: string;
  status: string;
  request_json: JSONValue;
  response_json: JSONValue;
  provider_reference: string;
  last_error: string;
  created_at: string;
  updated_at: string;
  sent_at?: string | null;
  completed_at?: string | null;
  failed_at?: string | null;
};

export type UpdateScoringRequestStatusRequest = {
  status: "pending" | "queued" | "sent" | "completed" | "failed";
  provider_reference?: string;
  response_json?: JSONValue;
  last_error?: string;
};

export type RuleSnooze = {
  id: string;
  tenant_id: string;
  scenario_id: string;
  object_type: string;
  object_id: string;
  snooze_group_id: string;
  created_at: string;
  expires_at: string;
};

export type CreateRuleSnoozeRequest = {
  object_type: string;
  object_id: string;
  snooze_group_id: string;
  expires_at: string;
};

export type ScheduledExecution = {
  id: string;
  tenant_id: string;
  scenario_id: string;
  scenario_iteration_id: string;
  source: string;
  status: string;
  scheduled_for: string;
  request_body: JSONValue;
  created_at: string;
};

export type RecurringSchedule = {
  enabled: boolean;
  frequency: string;
  time_of_day: string;
  minute_of_hour: number;
  day_of_week: string;
  day_of_month: number;
  timezone: string;
  candidate_limit: number;
  next_run?: string | null;
  last_run?: string | null;
};

export type CreateScheduledExecutionRequest = {
  scheduled_for: string;
  items: EvaluateDecisionRequest[];
  candidate_limit: number;
};

export type AsyncDecisionExecution = {
  id: string;
  tenant_id: string;
  scenario_id: string;
  object_type: string;
  status: string;
  request_body: JSONValue;
  created_at: string;
};

export type CreateAsyncDecisionExecutionRequest = {
  scenario_id: string;
  object_type: string;
  items: JSONValue[];
};

export type CustomListEntry = {
  id: string;
  tenant_id: string;
  list_id?: string | null;
  list_name: string;
  value: string;
  created_at: string;
};

export type CustomList = {
  id: string;
  tenant_id: string;
  name: string;
  description: string;
  kind: string;
  created_at: string;
  updated_at: string;
};

export type CreateCustomListRequest = {
  name: string;
  description: string;
  kind: string;
};

export type UpdateCustomListRequest = CreateCustomListRequest;

export type CreateCustomListEntryRequest = {
  value: string;
};

export type UpdateCustomListEntryRequest = CreateCustomListEntryRequest;

export type RecordTag = {
  id: string;
  tenant_id: string;
  object_type: string;
  object_id: string;
  tag: string;
  created_at: string;
};

export type CreateRecordTagRequest = {
  object_type: string;
  object_id: string;
  tag: string;
};

export type RiskSnapshot = {
  id: string;
  tenant_id: string;
  object_type: string;
  object_id: string;
  risk_level: string;
  created_at: string;
};

export type CreateRiskSnapshotRequest = {
  object_type: string;
  object_id: string;
  risk_level: string;
};

export type IPFlag = {
  id: string;
  tenant_id: string;
  ip_address: string;
  flag: string;
  created_at: string;
};

export type CreateIPFlagRequest = {
  ip_address: string;
  flag: string;
};

export type OutboxEvent = {
  id: string;
  tenant_id: string;
  aggregate_type: string;
  aggregate_id: string;
  event_type: string;
  payload: JSONValue;
  status: string;
  created_at: string;
};

const decisionEngineServiceBaseUrl =
  process.env.NEXT_PUBLIC_DECISION_ENGINE_SERVICE_URL ?? "http://localhost:8082";
const decisionEngineServiceToken =
  process.env.NEXT_PUBLIC_DECISION_ENGINE_SERVICE_TOKEN;

async function decisionEngineFetch<T>(
  path: string,
  init?: RequestInit
): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("Accept", "application/json");

  if (init?.body && !(init.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  if (decisionEngineServiceToken) {
    headers.set("Authorization", `Bearer ${decisionEngineServiceToken}`);
  }

  const response = await fetch(`${decisionEngineServiceBaseUrl}${path}`, {
    ...init,
    headers,
  });

  if (!response.ok) {
    const errorBody = (await response.json().catch(() => null)) as
      | DecisionEngineApiErrorEnvelope
      | null;
    throw new Error(
      errorBody?.error?.message ??
        errorBody?.details ??
        `Request failed with status ${response.status}`
    );
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return (await response.json()) as T;
}

export const decisionEnginePaths = {
  ruleFunctions: () => "/v1/rule-functions",
  scenarios: (tenantId: string) => `/v1/tenants/${tenantId}/scenarios`,
  scenario: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}`,
  editorIdentifiers: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/editor-identifiers`,
  iterations: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/iterations`,
  iteration: (tenantId: string, scenarioId: string, iterationId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/iterations/${iterationId}`,
  draftIteration: (tenantId: string, scenarioId: string, iterationId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/iterations/${iterationId}/draft`,
  commitIteration: (tenantId: string, scenarioId: string, iterationId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/iterations/${iterationId}/commit`,
  deactivateIteration: (tenantId: string, scenarioId: string, iterationId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/iterations/${iterationId}/deactivate`,
  publications: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/publications`,
  rules: (tenantId: string, scenarioId: string, iterationId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/iterations/${iterationId}/rules`,
  ruleGroups: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/rule-groups`,
  rule: (
    tenantId: string,
    scenarioId: string,
    iterationId: string,
    ruleId: string
  ) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/iterations/${iterationId}/rules/${ruleId}`,
  validateIteration: (tenantId: string, scenarioId: string, iterationId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/iterations/${iterationId}/validate`,
  evaluateScenario: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/evaluate`,
  scenarioDecisions: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/decisions`,
  decisions: (tenantId: string) => `/v1/tenants/${tenantId}/decisions`,
  decision: (tenantId: string, decisionId: string) =>
    `/v1/tenants/${tenantId}/decisions/${decisionId}`,
  recordIngested: (tenantId: string) =>
    `/v1/tenants/${tenantId}/ingestion-events/record-ingested`,
  testRuns: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/test-runs`,
  testRun: (tenantId: string, testRunId: string) =>
    `/v1/tenants/${tenantId}/test-runs/${testRunId}`,
  evaluateTestRun: (tenantId: string, testRunId: string) =>
    `/v1/tenants/${tenantId}/test-runs/${testRunId}/evaluate`,
  cancelTestRun: (tenantId: string, testRunId: string) =>
    `/v1/tenants/${tenantId}/test-runs/${testRunId}/cancel`,
  testRunDecisionSummaries: (tenantId: string, testRunId: string) =>
    `/v1/tenants/${tenantId}/test-runs/${testRunId}/decision-data-by-score`,
  testRunRuleStats: (tenantId: string, testRunId: string) =>
    `/v1/tenants/${tenantId}/test-runs/${testRunId}/data-by-rule-execution`,
  workflows: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/workflows`,
  workflow: (tenantId: string, scenarioId: string, workflowId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/workflows/${workflowId}`,
  reorderWorkflows: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/workflows/reorder`,
  workflowExecutions: (tenantId: string, decisionId: string) =>
    `/v1/tenants/${tenantId}/decisions/${decisionId}/workflow-executions`,
  workflowRules: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/workflow-rules`,
  workflowRule: (tenantId: string, scenarioId: string, ruleId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/workflow-rules/${ruleId}`,
  reorderWorkflowRules: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/workflow-rules/reorder`,
  workflowConditions: (tenantId: string, scenarioId: string, ruleId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/workflow-rules/${ruleId}/conditions`,
  workflowCondition: (
    tenantId: string,
    scenarioId: string,
    ruleId: string,
    conditionId: string
  ) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/workflow-rules/${ruleId}/conditions/${conditionId}`,
  workflowActions: (tenantId: string, scenarioId: string, ruleId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/workflow-rules/${ruleId}/actions`,
  workflowAction: (
    tenantId: string,
    scenarioId: string,
    ruleId: string,
    actionId: string
  ) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/workflow-rules/${ruleId}/actions/${actionId}`,
  screeningConfigs: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/screening-configs`,
  screeningConfig: (tenantId: string, scenarioId: string, configId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/screening-configs/${configId}`,
  decisionScreeningExecutions: (tenantId: string, decisionId: string) =>
    `/v1/tenants/${tenantId}/decisions/${decisionId}/screening-executions`,
  screeningExecution: (tenantId: string, executionId: string) =>
    `/v1/tenants/${tenantId}/screening-executions/${executionId}`,
  screeningExecutionStatus: (tenantId: string, executionId: string) =>
    `/v1/tenants/${tenantId}/screening-executions/${executionId}/status`,
  retryScreeningExecution: (tenantId: string, executionId: string) =>
    `/v1/tenants/${tenantId}/screening-executions/${executionId}/retry`,
  scoringConfigs: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/scoring-configs`,
  scoringConfig: (tenantId: string, scenarioId: string, configId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/scoring-configs/${configId}`,
  decisionScoringRequests: (tenantId: string, decisionId: string) =>
    `/v1/tenants/${tenantId}/decisions/${decisionId}/scoring-requests`,
  scoringRequest: (tenantId: string, requestId: string) =>
    `/v1/tenants/${tenantId}/scoring-requests/${requestId}`,
  scoringRequestStatus: (tenantId: string, requestId: string) =>
    `/v1/tenants/${tenantId}/scoring-requests/${requestId}/status`,
  retryScoringRequest: (tenantId: string, requestId: string) =>
    `/v1/tenants/${tenantId}/scoring-requests/${requestId}/retry`,
  ruleSnoozes: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/rule-snoozes`,
  ruleSnooze: (tenantId: string, scenarioId: string, snoozeId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/rule-snoozes/${snoozeId}`,
  scheduledExecutions: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/scheduled-executions`,
  scheduledExecution: (tenantId: string, scenarioId: string, executionId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/scheduled-executions/${executionId}`,
  recurringSchedule: (tenantId: string, scenarioId: string) =>
    `/v1/tenants/${tenantId}/scenarios/${scenarioId}/recurring-schedule`,
  asyncDecisionExecutions: (tenantId: string) =>
    `/v1/tenants/${tenantId}/async-decision-executions`,
  customLists: (tenantId: string) =>
    `/v1/tenants/${tenantId}/platform/custom-lists`,
  customList: (tenantId: string, listId: string) =>
    `/v1/tenants/${tenantId}/platform/custom-lists/${listId}`,
  customListEntriesByList: (tenantId: string, listId: string) =>
    `/v1/tenants/${tenantId}/platform/custom-lists/${listId}/entries`,
  customListEntriesImport: (tenantId: string, listId: string) =>
    `/v1/tenants/${tenantId}/platform/custom-lists/${listId}/entries/import`,
  customListEntry: (tenantId: string, listId: string, entryId: string) =>
    `/v1/tenants/${tenantId}/platform/custom-lists/${listId}/entries/${entryId}`,
  customListEntries: (tenantId: string) =>
    `/v1/tenants/${tenantId}/platform/custom-list-entries`,
  recordTags: (tenantId: string) => `/v1/tenants/${tenantId}/platform/record-tags`,
  riskSnapshots: (tenantId: string) =>
    `/v1/tenants/${tenantId}/platform/risk-snapshots`,
  ipFlags: (tenantId: string) => `/v1/tenants/${tenantId}/platform/ip-flags`,
  outboxEvents: (tenantId: string) => `/v1/tenants/${tenantId}/outbox-events`,
} as const;

export const decisionEngineApi = {
  listRuleFunctions: async () =>
    decisionEngineFetch<{ rule_functions: RuleFunction[] }>(
      decisionEnginePaths.ruleFunctions()
    ),
  listScenarios: async (tenantId: string) =>
    decisionEngineFetch<{ scenarios: Scenario[] }>(
      decisionEnginePaths.scenarios(tenantId)
    ),
  createScenario: async (tenantId: string, payload: CreateScenarioRequest) =>
    decisionEngineFetch<{ scenario: Scenario }>(
      decisionEnginePaths.scenarios(tenantId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  getScenario: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<{ scenario: Scenario }>(
      decisionEnginePaths.scenario(tenantId, scenarioId)
    ),
  updateScenario: async (
    tenantId: string,
    scenarioId: string,
    payload: UpdateScenarioRequest
  ) =>
    decisionEngineFetch<{ scenario: Scenario }>(
      decisionEnginePaths.scenario(tenantId, scenarioId),
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    ),
  deleteScenario: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<void>(decisionEnginePaths.scenario(tenantId, scenarioId), {
      method: "DELETE",
    }),
  listEditorIdentifiers: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<{
      payload_accessors: ASTNodeDTO[];
      database_accessors: ASTNodeDTO[];
    }>(decisionEnginePaths.editorIdentifiers(tenantId, scenarioId)),
  listIterations: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<{ iterations: Iteration[] }>(
      decisionEnginePaths.iterations(tenantId, scenarioId)
    ),
  createIteration: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<{ iteration: Iteration }>(
      decisionEnginePaths.iterations(tenantId, scenarioId),
      { method: "POST" }
    ),
  createDraftIteration: async (
    tenantId: string,
    scenarioId: string,
    iterationId: string
  ) =>
    decisionEngineFetch<{ iteration: Iteration }>(
      decisionEnginePaths.draftIteration(tenantId, scenarioId, iterationId),
      { method: "POST" }
    ),
  getIteration: async (tenantId: string, scenarioId: string, iterationId: string) =>
    decisionEngineFetch<{ iteration: Iteration }>(
      decisionEnginePaths.iteration(tenantId, scenarioId, iterationId)
    ),
  updateIteration: async (
    tenantId: string,
    scenarioId: string,
    iterationId: string,
    payload: UpdateIterationRequest
  ) =>
    decisionEngineFetch<{ iteration: Iteration }>(
      decisionEnginePaths.iteration(tenantId, scenarioId, iterationId),
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    ),
  commitIteration: async (tenantId: string, scenarioId: string, iterationId: string) =>
    decisionEngineFetch<{ iteration: Iteration }>(
      decisionEnginePaths.commitIteration(tenantId, scenarioId, iterationId),
      { method: "POST" }
    ),
  deactivateIteration: async (tenantId: string, scenarioId: string, iterationId: string) =>
    decisionEngineFetch<{ publications: Publication[] }>(
      decisionEnginePaths.deactivateIteration(tenantId, scenarioId, iterationId),
      { method: "POST" }
    ),
  listPublications: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<{ publications: Publication[] }>(
      decisionEnginePaths.publications(tenantId, scenarioId)
    ),
  publishIteration: async (
    tenantId: string,
    scenarioId: string,
    payload: PublicationActionRequest
  ) =>
    decisionEngineFetch<{ publications: Publication[] }>(
      decisionEnginePaths.publications(tenantId, scenarioId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  listRules: async (tenantId: string, scenarioId: string, iterationId: string) =>
    decisionEngineFetch<{ rules: Rule[] }>(
      decisionEnginePaths.rules(tenantId, scenarioId, iterationId)
    ),
  listRuleGroups: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<RuleGroupListResponse>(
      decisionEnginePaths.ruleGroups(tenantId, scenarioId)
    ),
  createRule: async (
    tenantId: string,
    scenarioId: string,
    iterationId: string,
    payload: CreateRuleRequest
  ) =>
    decisionEngineFetch<{ rule: Rule }>(
      decisionEnginePaths.rules(tenantId, scenarioId, iterationId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  updateRule: async (
    tenantId: string,
    scenarioId: string,
    iterationId: string,
    ruleId: string,
    payload: UpdateRuleRequest
  ) =>
    decisionEngineFetch<{ rule: Rule }>(
      decisionEnginePaths.rule(tenantId, scenarioId, iterationId, ruleId),
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    ),
  deleteRule: async (
    tenantId: string,
    scenarioId: string,
    iterationId: string,
    ruleId: string
  ) =>
    decisionEngineFetch<void>(
      decisionEnginePaths.rule(tenantId, scenarioId, iterationId, ruleId),
      { method: "DELETE" }
    ),
  validateIteration: async (tenantId: string, scenarioId: string, iterationId: string) =>
    decisionEngineFetch<{ validation: ValidationResult }>(
      decisionEnginePaths.validateIteration(tenantId, scenarioId, iterationId),
      { method: "POST" }
    ),
  evaluateScenario: async (
    tenantId: string,
    scenarioId: string,
    payload: EvaluateDecisionRequest
  ) =>
    decisionEngineFetch<{ result: DecisionEvaluationResult }>(
      decisionEnginePaths.evaluateScenario(tenantId, scenarioId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  listScenarioDecisions: async (
    tenantId: string,
    scenarioId: string,
    pagination?: Pick<ListDecisionsRequest, "limit" | "offset">
  ) => {
    const params = new URLSearchParams();
    if (pagination?.limit != null) {
      params.set("limit", String(pagination.limit));
    }
    if (pagination?.offset != null) {
      params.set("offset", String(pagination.offset));
    }
    const query = params.toString();
    return decisionEngineFetch<{ decisions: Decision[]; pagination: Pagination }>(
      `${decisionEnginePaths.scenarioDecisions(tenantId, scenarioId)}${query ? `?${query}` : ""}`
    );
  },
  listDecisions: async (tenantId: string, filters?: ListDecisionsRequest) => {
    const params = new URLSearchParams();

    if (filters?.scenario_id) {
      params.set("scenario_id", filters.scenario_id);
    }
    if (filters?.object_type) {
      params.set("object_type", filters.object_type);
    }
    if (filters?.object_id) {
      params.set("object_id", filters.object_id);
    }
    if (filters?.limit != null) {
      params.set("limit", String(filters.limit));
    }
    if (filters?.offset != null) {
      params.set("offset", String(filters.offset));
    }

    const query = params.toString();
    return decisionEngineFetch<{ decisions: Decision[]; pagination: Pagination }>(
      `${decisionEnginePaths.decisions(tenantId)}${query ? `?${query}` : ""}`
    );
  },
  getDecision: async (tenantId: string, decisionId: string) =>
    decisionEngineFetch<{ decision: DecisionDetail; rule_executions: RuleExecution[] }>(
      decisionEnginePaths.decision(tenantId, decisionId)
    ),
  evaluateIngestionEvent: async (tenantId: string, payload: IngestionTriggerRequest) =>
    decisionEngineFetch<{ result: MultiDecisionEvaluationResult }>(
      decisionEnginePaths.recordIngested(tenantId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  listTestRuns: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<{ test_runs: TestRun[] }>(
      decisionEnginePaths.testRuns(tenantId, scenarioId)
    ),
  createTestRun: async (
    tenantId: string,
    scenarioId: string,
    payload: CreateTestRunRequest
  ) =>
    decisionEngineFetch<{ test_run: TestRun }>(
      decisionEnginePaths.testRuns(tenantId, scenarioId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  getTestRun: async (tenantId: string, testRunId: string) =>
    decisionEngineFetch<{ test_run: TestRun }>(decisionEnginePaths.testRun(tenantId, testRunId)),
  evaluateTestRun: async (
    tenantId: string,
    testRunId: string,
    payload: EvaluateDecisionRequest
  ) =>
    decisionEngineFetch<{ result: TestRunEvaluationResult }>(
      decisionEnginePaths.evaluateTestRun(tenantId, testRunId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  cancelTestRun: async (tenantId: string, testRunId: string) =>
    decisionEngineFetch<{ test_run: TestRun }>(decisionEnginePaths.cancelTestRun(tenantId, testRunId), {
      method: "POST",
    }),
  listTestRunDecisionSummaries: async (tenantId: string, testRunId: string) =>
    decisionEngineFetch<{ decisions: TestRunDecisionSummary[] }>(
      decisionEnginePaths.testRunDecisionSummaries(tenantId, testRunId)
    ),
  listTestRunRuleStats: async (tenantId: string, testRunId: string) =>
    decisionEngineFetch<{ rules: TestRunRuleStat[] }>(
      decisionEnginePaths.testRunRuleStats(tenantId, testRunId)
    ),
  listWorkflows: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<{ workflows: Workflow[] }>(
      decisionEnginePaths.workflows(tenantId, scenarioId)
    ),
  createWorkflow: async (
    tenantId: string,
    scenarioId: string,
    payload: CreateWorkflowRequest
  ) =>
    decisionEngineFetch<{ workflow: Workflow }>(
      decisionEnginePaths.workflows(tenantId, scenarioId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  getWorkflow: async (tenantId: string, scenarioId: string, workflowId: string) =>
    decisionEngineFetch<{ workflow: Workflow }>(
      decisionEnginePaths.workflow(tenantId, scenarioId, workflowId)
    ),
  updateWorkflow: async (
    tenantId: string,
    scenarioId: string,
    workflowId: string,
    payload: UpdateWorkflowRequest
  ) =>
    decisionEngineFetch<{ workflow: Workflow }>(
      decisionEnginePaths.workflow(tenantId, scenarioId, workflowId),
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    ),
  deleteWorkflow: async (tenantId: string, scenarioId: string, workflowId: string) =>
    decisionEngineFetch<void>(
      decisionEnginePaths.workflow(tenantId, scenarioId, workflowId),
      { method: "DELETE" }
    ),
  reorderWorkflows: async (
    tenantId: string,
    scenarioId: string,
    ordered_ids: string[]
  ) =>
    decisionEngineFetch<{ workflows: Workflow[] }>(
      decisionEnginePaths.reorderWorkflows(tenantId, scenarioId),
      {
        method: "POST",
        body: JSON.stringify({ ordered_ids }),
      }
    ),
  listWorkflowExecutions: async (tenantId: string, decisionId: string) =>
    decisionEngineFetch<{ workflow_executions: WorkflowExecution[] }>(
      decisionEnginePaths.workflowExecutions(tenantId, decisionId)
    ),
  listWorkflowRules: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<{ workflow_rules: WorkflowRule[] }>(
      decisionEnginePaths.workflowRules(tenantId, scenarioId)
    ),
  createWorkflowRule: async (
    tenantId: string,
    scenarioId: string,
    payload: CreateWorkflowRuleRequest
  ) =>
    decisionEngineFetch<{ workflow_rule: WorkflowRule }>(
      decisionEnginePaths.workflowRules(tenantId, scenarioId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  getWorkflowRule: async (tenantId: string, scenarioId: string, ruleId: string) =>
    decisionEngineFetch<{ workflow_rule: WorkflowRule }>(
      decisionEnginePaths.workflowRule(tenantId, scenarioId, ruleId)
    ),
  updateWorkflowRule: async (
    tenantId: string,
    scenarioId: string,
    ruleId: string,
    payload: UpdateWorkflowRuleRequest
  ) =>
    decisionEngineFetch<{ workflow_rule: WorkflowRule }>(
      decisionEnginePaths.workflowRule(tenantId, scenarioId, ruleId),
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    ),
  deleteWorkflowRule: async (tenantId: string, scenarioId: string, ruleId: string) =>
    decisionEngineFetch<void>(
      decisionEnginePaths.workflowRule(tenantId, scenarioId, ruleId),
      { method: "DELETE" }
    ),
  reorderWorkflowRules: async (
    tenantId: string,
    scenarioId: string,
    ordered_ids: string[]
  ) =>
    decisionEngineFetch<{ workflow_rules: WorkflowRule[] }>(
      decisionEnginePaths.reorderWorkflowRules(tenantId, scenarioId),
      {
        method: "POST",
        body: JSON.stringify({ ordered_ids }),
      }
    ),
  listWorkflowConditions: async (tenantId: string, scenarioId: string, ruleId: string) =>
    decisionEngineFetch<{ conditions: WorkflowCondition[] }>(
      decisionEnginePaths.workflowConditions(tenantId, scenarioId, ruleId)
    ),
  createWorkflowCondition: async (
    tenantId: string,
    scenarioId: string,
    ruleId: string,
    payload: CreateWorkflowConditionRequest
  ) =>
    decisionEngineFetch<{ workflow_condition: WorkflowCondition }>(
      decisionEnginePaths.workflowConditions(tenantId, scenarioId, ruleId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  updateWorkflowCondition: async (
    tenantId: string,
    scenarioId: string,
    ruleId: string,
    conditionId: string,
    payload: UpdateWorkflowConditionRequest
  ) =>
    decisionEngineFetch<{ workflow_condition: WorkflowCondition }>(
      decisionEnginePaths.workflowCondition(
        tenantId,
        scenarioId,
        ruleId,
        conditionId
      ),
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    ),
  deleteWorkflowCondition: async (
    tenantId: string,
    scenarioId: string,
    ruleId: string,
    conditionId: string
  ) =>
    decisionEngineFetch<void>(
      decisionEnginePaths.workflowCondition(
        tenantId,
        scenarioId,
        ruleId,
        conditionId
      ),
      { method: "DELETE" }
    ),
  listWorkflowActions: async (tenantId: string, scenarioId: string, ruleId: string) =>
    decisionEngineFetch<{ actions: WorkflowAction[] }>(
      decisionEnginePaths.workflowActions(tenantId, scenarioId, ruleId)
    ),
  createWorkflowAction: async (
    tenantId: string,
    scenarioId: string,
    ruleId: string,
    payload: CreateWorkflowActionRequest
  ) =>
    decisionEngineFetch<{ workflow_action: WorkflowAction }>(
      decisionEnginePaths.workflowActions(tenantId, scenarioId, ruleId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  updateWorkflowAction: async (
    tenantId: string,
    scenarioId: string,
    ruleId: string,
    actionId: string,
    payload: UpdateWorkflowActionRequest
  ) =>
    decisionEngineFetch<{ workflow_action: WorkflowAction }>(
      decisionEnginePaths.workflowAction(tenantId, scenarioId, ruleId, actionId),
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    ),
  deleteWorkflowAction: async (
    tenantId: string,
    scenarioId: string,
    ruleId: string,
    actionId: string
  ) =>
    decisionEngineFetch<void>(
      decisionEnginePaths.workflowAction(tenantId, scenarioId, ruleId, actionId),
      { method: "DELETE" }
    ),
  listScreeningConfigs: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<{ screening_configs: ScreeningConfig[] }>(
      decisionEnginePaths.screeningConfigs(tenantId, scenarioId)
    ),
  createScreeningConfig: async (
    tenantId: string,
    scenarioId: string,
    payload: CreateScreeningConfigRequest
  ) =>
    decisionEngineFetch<{ screening_config: ScreeningConfig }>(
      decisionEnginePaths.screeningConfigs(tenantId, scenarioId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  getScreeningConfig: async (tenantId: string, scenarioId: string, configId: string) =>
    decisionEngineFetch<{ screening_config: ScreeningConfig }>(
      decisionEnginePaths.screeningConfig(tenantId, scenarioId, configId)
    ),
  updateScreeningConfig: async (
    tenantId: string,
    scenarioId: string,
    configId: string,
    payload: CreateScreeningConfigRequest
  ) =>
    decisionEngineFetch<{ screening_config: ScreeningConfig }>(
      decisionEnginePaths.screeningConfig(tenantId, scenarioId, configId),
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    ),
  deleteScreeningConfig: async (tenantId: string, scenarioId: string, configId: string) =>
    decisionEngineFetch<void>(
      decisionEnginePaths.screeningConfig(tenantId, scenarioId, configId),
      { method: "DELETE" }
    ),
  listScreeningExecutions: async (tenantId: string, decisionId: string) =>
    decisionEngineFetch<{ screening_executions: ScreeningExecution[] }>(
      decisionEnginePaths.decisionScreeningExecutions(tenantId, decisionId)
    ),
  getScreeningExecution: async (tenantId: string, executionId: string) =>
    decisionEngineFetch<{ screening_execution: ScreeningExecution }>(
      decisionEnginePaths.screeningExecution(tenantId, executionId)
    ),
  updateScreeningExecutionStatus: async (
    tenantId: string,
    executionId: string,
    payload: UpdateScreeningExecutionStatusRequest
  ) =>
    decisionEngineFetch<{ screening_execution: ScreeningExecution }>(
      decisionEnginePaths.screeningExecutionStatus(tenantId, executionId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  retryScreeningExecution: async (tenantId: string, executionId: string) =>
    decisionEngineFetch<{ screening_execution: ScreeningExecution }>(
      decisionEnginePaths.retryScreeningExecution(tenantId, executionId),
      { method: "POST" }
    ),
  listScoringConfigs: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<{ scoring_configs: ScoringConfig[] }>(
      decisionEnginePaths.scoringConfigs(tenantId, scenarioId)
    ),
  createScoringConfig: async (
    tenantId: string,
    scenarioId: string,
    payload: CreateScoringConfigRequest
  ) =>
    decisionEngineFetch<{ scoring_config: ScoringConfig }>(
      decisionEnginePaths.scoringConfigs(tenantId, scenarioId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  getScoringConfig: async (tenantId: string, scenarioId: string, configId: string) =>
    decisionEngineFetch<{ scoring_config: ScoringConfig }>(
      decisionEnginePaths.scoringConfig(tenantId, scenarioId, configId)
    ),
  updateScoringConfig: async (
    tenantId: string,
    scenarioId: string,
    configId: string,
    payload: CreateScoringConfigRequest
  ) =>
    decisionEngineFetch<{ scoring_config: ScoringConfig }>(
      decisionEnginePaths.scoringConfig(tenantId, scenarioId, configId),
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    ),
  deleteScoringConfig: async (tenantId: string, scenarioId: string, configId: string) =>
    decisionEngineFetch<void>(
      decisionEnginePaths.scoringConfig(tenantId, scenarioId, configId),
      { method: "DELETE" }
    ),
  listScoringRequests: async (tenantId: string, decisionId: string) =>
    decisionEngineFetch<{ scoring_requests: ScoringRequest[] }>(
      decisionEnginePaths.decisionScoringRequests(tenantId, decisionId)
    ),
  getScoringRequest: async (tenantId: string, requestId: string) =>
    decisionEngineFetch<{ scoring_request: ScoringRequest }>(
      decisionEnginePaths.scoringRequest(tenantId, requestId)
    ),
  updateScoringRequestStatus: async (
    tenantId: string,
    requestId: string,
    payload: UpdateScoringRequestStatusRequest
  ) =>
    decisionEngineFetch<{ scoring_request: ScoringRequest }>(
      decisionEnginePaths.scoringRequestStatus(tenantId, requestId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  retryScoringRequest: async (tenantId: string, requestId: string) =>
    decisionEngineFetch<{ scoring_request: ScoringRequest }>(
      decisionEnginePaths.retryScoringRequest(tenantId, requestId),
      { method: "POST" }
    ),
  listRuleSnoozes: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<{ rule_snoozes: RuleSnooze[] }>(
      decisionEnginePaths.ruleSnoozes(tenantId, scenarioId)
    ),
  createRuleSnooze: async (
    tenantId: string,
    scenarioId: string,
    payload: CreateRuleSnoozeRequest
  ) =>
    decisionEngineFetch<{ rule_snooze: RuleSnooze }>(
      decisionEnginePaths.ruleSnoozes(tenantId, scenarioId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  deleteRuleSnooze: async (tenantId: string, scenarioId: string, snoozeId: string) =>
    decisionEngineFetch<void>(
      decisionEnginePaths.ruleSnooze(tenantId, scenarioId, snoozeId),
      { method: "DELETE" }
    ),
  listScheduledExecutions: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<{ scheduled_executions: ScheduledExecution[] }>(
      decisionEnginePaths.scheduledExecutions(tenantId, scenarioId)
    ),
  getScheduledExecution: async (tenantId: string, scenarioId: string, executionId: string) =>
    decisionEngineFetch<{ scheduled_execution: ScheduledExecution }>(
      decisionEnginePaths.scheduledExecution(tenantId, scenarioId, executionId)
    ),
  getRecurringSchedule: async (tenantId: string, scenarioId: string) =>
    decisionEngineFetch<{ recurring_schedule: RecurringSchedule }>(
      decisionEnginePaths.recurringSchedule(tenantId, scenarioId)
    ),
  updateRecurringSchedule: async (
    tenantId: string,
    scenarioId: string,
    payload: RecurringSchedule
  ) =>
    decisionEngineFetch<{ recurring_schedule: RecurringSchedule }>(
      decisionEnginePaths.recurringSchedule(tenantId, scenarioId),
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    ),
  createScheduledExecution: async (
    tenantId: string,
    scenarioId: string,
    payload: CreateScheduledExecutionRequest
  ) =>
    decisionEngineFetch<{ scheduled_execution: ScheduledExecution }>(
      decisionEnginePaths.scheduledExecutions(tenantId, scenarioId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  listAsyncDecisionExecutions: async (tenantId: string) =>
    decisionEngineFetch<{ async_decision_executions: AsyncDecisionExecution[] }>(
      decisionEnginePaths.asyncDecisionExecutions(tenantId)
    ),
  createAsyncDecisionExecution: async (
    tenantId: string,
    payload: CreateAsyncDecisionExecutionRequest
  ) =>
    decisionEngineFetch<{ async_decision_execution: AsyncDecisionExecution }>(
      decisionEnginePaths.asyncDecisionExecutions(tenantId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  listCustomLists: async (tenantId: string) =>
    decisionEngineFetch<{ custom_lists: CustomList[] }>(
      decisionEnginePaths.customLists(tenantId)
    ),
  getCustomList: async (tenantId: string, listId: string) =>
    decisionEngineFetch<{ custom_list: CustomList }>(
      decisionEnginePaths.customList(tenantId, listId)
    ),
  createCustomList: async (
    tenantId: string,
    payload: CreateCustomListRequest
  ) =>
    decisionEngineFetch<{ custom_list: CustomList }>(
      decisionEnginePaths.customLists(tenantId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  updateCustomList: async (
    tenantId: string,
    listId: string,
    payload: UpdateCustomListRequest
  ) =>
    decisionEngineFetch<{ custom_list: CustomList }>(
      decisionEnginePaths.customList(tenantId, listId),
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    ),
  deleteCustomList: async (tenantId: string, listId: string) =>
    decisionEngineFetch<void>(decisionEnginePaths.customList(tenantId, listId), {
      method: "DELETE",
    }),
  listCustomListEntries: async (tenantId: string, listName?: string) =>
    decisionEngineFetch<{ custom_list_entries: CustomListEntry[] }>(
      `${decisionEnginePaths.customListEntries(tenantId)}${
        listName ? `?list_name=${encodeURIComponent(listName)}` : ""
      }`
    ),
  listCustomListEntriesByList: async (tenantId: string, listId: string) =>
    decisionEngineFetch<{ custom_list_entries: CustomListEntry[] }>(
      decisionEnginePaths.customListEntriesByList(tenantId, listId)
    ),
  createCustomListEntry: async (
    tenantId: string,
    listId: string,
    payload: CreateCustomListEntryRequest
  ) =>
    decisionEngineFetch<{ custom_list_entry: CustomListEntry }>(
      decisionEnginePaths.customListEntriesByList(tenantId, listId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  updateCustomListEntry: async (
    tenantId: string,
    listId: string,
    entryId: string,
    payload: UpdateCustomListEntryRequest
  ) =>
    decisionEngineFetch<{ custom_list_entry: CustomListEntry }>(
      decisionEnginePaths.customListEntry(tenantId, listId, entryId),
      {
        method: "PUT",
        body: JSON.stringify(payload),
      }
    ),
  importCustomListEntries: async (tenantId: string, listId: string, file: File) => {
    const formData = new FormData();
    formData.append("file", file);

    return decisionEngineFetch<{ imported_count: number }>(
      decisionEnginePaths.customListEntriesImport(tenantId, listId),
      {
        method: "POST",
        body: formData,
      }
    );
  },
  deleteCustomListEntry: async (tenantId: string, listId: string, entryId: string) =>
    decisionEngineFetch<void>(
      decisionEnginePaths.customListEntry(tenantId, listId, entryId),
      {
        method: "DELETE",
      }
    ),
  listRecordTags: async (tenantId: string) =>
    decisionEngineFetch<{ record_tags: RecordTag[] }>(
      decisionEnginePaths.recordTags(tenantId)
    ),
  createRecordTag: async (tenantId: string, payload: CreateRecordTagRequest) =>
    decisionEngineFetch<{ record_tag: RecordTag }>(
      decisionEnginePaths.recordTags(tenantId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  createRiskSnapshot: async (
    tenantId: string,
    payload: CreateRiskSnapshotRequest
  ) =>
    decisionEngineFetch<{ risk_snapshot: RiskSnapshot }>(
      decisionEnginePaths.riskSnapshots(tenantId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  listIpFlags: async (tenantId: string) =>
    decisionEngineFetch<{ ip_flags: IPFlag[] }>(
      decisionEnginePaths.ipFlags(tenantId)
    ),
  createIpFlag: async (tenantId: string, payload: CreateIPFlagRequest) =>
    decisionEngineFetch<{ ip_flag: IPFlag }>(
      decisionEnginePaths.ipFlags(tenantId),
      {
        method: "POST",
        body: JSON.stringify(payload),
      }
    ),
  listOutboxEvents: async (tenantId: string) =>
    decisionEngineFetch<{ outbox_events: OutboxEvent[] }>(
      decisionEnginePaths.outboxEvents(tenantId)
    ),
};
