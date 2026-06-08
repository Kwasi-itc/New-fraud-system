# Decision Engine Service

Standalone Go service for Marble's decision engine domain, extracted from the monolithic `api` service and designed to work alongside `data-model-service` and `ingestion-service`.

Current location in the workspace:

- `new/backend/decision-engine-service`

This folder now contains an active standalone service implementation plus the original planning documents.

## Purpose

The service is intended to own the full Marble decision-engine behavior:

- scenario authoring
- scenario iteration authoring
- rule authoring
- rule snoozes
- screening config authoring
- scenario test runs
- phantom decisions for test runs
- AST validation
- publication and live-version management
- publication preparation checks
- runtime scenario evaluation
- decision creation and persistence
- rule execution persistence
- analytics field capture for decisions
- optional offloading and rehydration of large rule-evaluation payloads
- test-run summary generation
- scheduled execution orchestration
- async batch decision execution
- workflow triggering after decisions
- case-creation and case-attachment workflow actions
- decision-created and workflow-related webhook/event creation
- payload parsing and enrichment on decision-ingest evaluation paths
- screening integration through `screening-service`
- optional scoring integration

Notably separate from this scope unless explicitly pulled in later:

- Marble's broader `continuous_screening` subsystem and dataset-update workers

## Intended service boundary

The decision engine service should own:

- executable scenario definitions and versions
- runtime evaluation logic
- decisions and execution history
- scheduling and async execution state
- workflow definitions and dispatch triggers

The decision engine service should not own:

- tenant schema management
- physical table and field lifecycle
- raw ingestion writes
- being the source of truth for tenant data storage layout

## Expected dependencies

The service is expected to depend on:

- `data-model-service`
  - assembled tenant model contract
  - links, pivots, navigation options, field typing, tenant/model revision
- tenant data read access
  - direct PostgreSQL tenant-schema reads or an equivalent read abstraction
- `ingestion-service`
  - post-ingestion trigger integration, likely through events or explicit callbacks
- optional external services
  - `screening-service`
  - screening providers only when using fallback compatibility wiring
  - scoring services
  - webhook/event delivery infrastructure
  - case management / case review infrastructure if kept external

## Planned documents

- [DECISION_ENGINE_SERVICE_EXTRACTION_DESIGN.md](./DECISION_ENGINE_SERVICE_EXTRACTION_DESIGN.md)
- [DECISION_ENGINE_SERVICE_IMPLEMENTATION_BLUEPRINT.md](./DECISION_ENGINE_SERVICE_IMPLEMENTATION_BLUEPRINT.md)
- [DECISION_ENGINE_SERVICE_INTEGRATION_CONTRACTS.md](./DECISION_ENGINE_SERVICE_INTEGRATION_CONTRACTS.md)
- [DECISION_ENGINE_SERVICE_DOMAIN_BREAKDOWN.md](./DECISION_ENGINE_SERVICE_DOMAIN_BREAKDOWN.md)
- [DECISION_ENGINE_SERVICE_V1_OPERATING_DECISIONS.md](./DECISION_ENGINE_SERVICE_V1_OPERATING_DECISIONS.md)
- [IMPLEMENTATION_TODO.md](./IMPLEMENTATION_TODO.md)
- [MF_HANDOFF.md](./MF_HANDOFF.md)

## Current status

Implemented:

- Go service scaffold with `server`, `worker`, and `migrate` commands
- PostgreSQL-backed persistence and follow-up metadata migrations
- scenario authoring: list, create, get, update, copy, latest-rules
- iteration authoring: list, create draft, get, update, metadata, create-from-existing, commit
- publication lifecycle: publish, unpublish, preparation status, preparation start
- publication preparation readiness using `data-model-service`
- rule authoring APIs
- AST validation against `data-model-service`
- runtime evaluation, decision persistence, and rule execution persistence
- decision reads plus tenant-level create/list/create-all flows
- ingestion-triggered evaluation endpoint
- test runs, phantom decisions, phantom rule executions, cancel, and summaries/stats
- rule snoozes
- legacy flat workflow definitions and workflow execution persistence
- structured workflow rule/condition/action authoring and reorder
- structured workflow runtime matching and execution creation
- screening/scoring config authoring
- screening execution and scoring request persistence
- screening/scoring lifecycle inspection, status update, retry, and provider-result payload ingestion
- screening worker dispatch to `screening-service` intake
- screening status callback receiver at `POST /internal/screening-status-updates`
- outbox event persistence
- scheduled and async decision execution processing
- worker support for both one-shot batch runs and poll-loop execution
- maintained OpenAPI spec for the implemented route surface

Still intentionally provisional or deferred:

- worker behavior still lives in the `cmd/worker` entrypoint rather than a dedicated internal worker package
- workflow side effects are still dispatch-shell behavior, not a settled case-management integration
- scoring still depends on external provider execution even though orchestration state is service-owned
- relation-heavy evaluator semantics still need tighter definition beyond the current baseline tests
- several planning documents still describe future-state architecture rather than the exact implemented state
- payload parsing/enrichment and evaluation offloading are still design-level scope, not implemented runtime features

## Current implementation shape

The current package layout differs slightly from the original planning docs:

- AST runtime and AST validation now live in `internal/runtime/ast_eval`
- application orchestration remains in `internal/service`
- worker behavior currently lives in the `cmd/worker` entrypoint rather than a dedicated `internal/worker` package
- screening, scoring, and platform helper integrations currently use service-owned repositories plus dispatch/provider shells
- workflow support now exists in two layers:
  - legacy flat workflow definitions for backward-compatible V1 behavior
  - structured workflow rules, conditions, and actions for closer monolith parity

## Environment

The service loads `.env` automatically if present. Use `.env.example` as the starting point.

Required variables:

- `DATABASE_URL`
- `DATA_MODEL_SERVICE_URL`
- `INGESTION_SERVICE_URL`

Common local defaults in `.env.example`:

- `DATA_MODEL_SERVICE_URL=http://localhost:8081`
- `INGESTION_SERVICE_URL=http://localhost:8080`
- `PORT=8082`

Relevant optional downstream variables:

- `SCREENING_SERVICE_URL`
- `SCREENING_PROVIDER_URL`
- `SCORING_PROVIDER_URL`
- `WORKFLOW_ACTION_URL`
- `OUTBOX_PUBLISHER_URL`
- `SERVICE_AUTH_MODE`
- `SERVICE_AUTH_TOKEN`
- `AGGREGATE_PUSHDOWN_MODE`
- `AGGREGATE_PUSHDOWN_AGGREGATES`

`SCREENING_SERVICE_URL` is the preferred screening dispatch target for the worker. `SCREENING_PROVIDER_URL` remains as a fallback compatibility variable.

## Aggregate Pushdown

Aggregate-heavy Marble `Aggregator(...)` rules can now be pushed down to `ingestion-service` instead of pulling record sets back into the decision engine first.

Current behavior:

- the decision engine compiles supported aggregate AST into a logical aggregate query
- `ingestion-service` executes the aggregate close to the tenant data tables
- unsupported shapes fall back to local in-memory aggregation when pushdown mode allows fallback
- strict mode fails fast instead of silently falling back

Current configuration:

- `AGGREGATE_PUSHDOWN_MODE`
  - `enabled`
  - `disabled`
  - `strict`
- `AGGREGATE_PUSHDOWN_AGGREGATES`
  - comma-separated allow-list for remote pushdown
  - default: `count`
  - example expanded rollout: `count,sum,avg,min,max`

Current V1 pushdown scope:

- supported aggregates:
  - `count`
  - `count_distinct`
  - `sum`
  - `avg`
  - `min`
  - `max`
- supported filter grouping:
  - `List` as AND
  - nested `and`
  - nested `or`
  - nested `not`
- supported runtime values:
  - payload-derived values
  - resolved time expressions

Current explicit V1 deferrals:

- `is_empty` pushdown is deferred
- fuzzy matching pushdown is deferred
- decision-history joins are deferred
- custom-list joins are deferred
- tag/risk helper joins are deferred
- broad relationship join pushdown is deferred
- arbitrary SQL-like expression pushdown is deferred

## Workflow Examples

The service supports two workflow models:

- legacy `workflows`
- structured `workflow-rules`

They are related, but they are not the same thing.

### Legacy workflows

The legacy workflow endpoint creates outcome-driven follow-up actions for a scenario:

- `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflows`

This workflow model triggers from final decision outcomes such as `approve`, `review`, `block_and_review`, or `decline`. It does not directly match rule formulas. If the workflow should depend on a specific rule hit or a payload expression, use the structured workflow-rules endpoints instead.

Current supported action types:

- `create_case`
- `add_tag`
- `emit_event`

Example `create_case` workflow:

```json
{
  "name": "High amount review case",
  "description": "Create a case when the scenario outcome is review",
  "allowed_outcomes": ["review"],
  "action_type": "create_case",
  "action_config": {
    "title": "High amount transaction",
    "reason": "Transaction amount exceeded threshold",
    "source": "decision-engine"
  },
  "active": true
}
```

Example `add_tag` workflow:

```json
{
  "name": "Tag high amount decisions",
  "description": "Add a tag when the scenario outcome is review",
  "allowed_outcomes": ["review"],
  "action_type": "add_tag",
  "action_config": {
    "tag": "high_amount"
  },
  "active": true
}
```

Example `emit_event` workflow:

```json
{
  "name": "Emit high amount event",
  "description": "Emit an event for reviewed high amount transactions",
  "allowed_outcomes": ["review"],
  "action_type": "emit_event",
  "action_config": {
    "event_name": "transaction.high_amount.review",
    "severity": "medium"
  },
  "active": true
}
```

`action_config` is currently service-owned JSON and remains downstream-integration specific. The service persists and dispatches this payload, but the final consumer contract for case-management style actions is still provisional.

### Structured workflow-rules

Structured workflow-rules are the more detailed model:

- `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules`
- `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules/{ruleId}/conditions`
- `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/workflow-rules/{ruleId}/actions`

This model lets you define:

- a workflow rule
- one or more conditions
- one or more actions

Current condition functions are:

- `always`
- `never`
- `outcome_in`
- `rule_hit`
- `payload_evaluates`

Current action types are:

- `create_case`
- `add_tag`
- `emit_event`

If a scenario has structured workflow-rules, the runtime uses them in preference to legacy workflows for that scenario.

### How workflows relate to decisions

The decision engine flow is:

1. evaluate the scenario trigger formula
2. evaluate the scenario rules
3. sum matching rule score modifiers
4. derive the final decision outcome from the scenario thresholds
5. create workflow executions for that decision

Legacy workflows trigger from the final outcome only.

Structured workflow-rules can trigger from:

- the final outcome
- a specific rule hit
- a payload expression

### High-amount example

Suppose a scenario contains a rule:

- formula: `amount > 20000`
- score modifier: `20`

If the scenario thresholds map score `20` to outcome `review`, then:

- a legacy workflow with `allowed_outcomes: ["review"]` will run
- a structured workflow-rule can be made more specific and require either `review`, the exact rule hit, or both

#### Legacy workflow version

This is enough when the only requirement is "if the final outcome is review, do something":

```json
{
  "name": "High amount review case",
  "description": "Create a case when the scenario outcome is review",
  "allowed_outcomes": ["review"],
  "action_type": "create_case",
  "action_config": {
    "title": "High amount transaction",
    "reason": "Transaction amount exceeded threshold",
    "source": "decision-engine"
  },
  "active": true
}
```

#### Structured workflow-rule version

This is the better fit when the workflow should be tied to the high-amount rule itself.

Step 1. Create the workflow-rule shell:

```json
{
  "name": "High amount escalation",
  "fallthrough": false
}
```

What this means:

- `name` is the human-readable label for the structured automation rule
- `fallthrough` controls whether the engine should continue checking lower-priority structured workflow-rules after this one matches

`fallthrough: false` means:

- create the actions for this matched workflow-rule
- stop evaluating the remaining structured workflow-rules for the scenario

`fallthrough: true` means:

- create the actions for this matched workflow-rule
- continue evaluating the remaining structured workflow-rules for the scenario

Use `fallthrough: false` when the first matching workflow-rule should win. Use `fallthrough: true` when multiple workflow-rules should be allowed to stack.

Step 2. Add an `outcome_in` condition if the action should only happen for review outcomes:

```json
{
  "function": "outcome_in",
  "params": ["review"]
}
```

What this means:

- the structured workflow-rule should only match if the final decision outcome is `review`

This is different from checking a payload field directly. At this stage the decision engine has already evaluated the scenario rules, calculated the score, and derived the final outcome. The condition is checking that final outcome.

You can also allow multiple outcomes:

```json
{
  "function": "outcome_in",
  "params": ["review", "block_and_review"]
}
```

That means the workflow-rule can match either of those outcomes.

Step 3. Add a `rule_hit` condition tied to the amount rule id:

```json
{
  "function": "rule_hit",
  "params": {
    "rule_ids": ["high_amount_rule_id"]
  }
}
```

What this means:

- the structured workflow-rule should only match if the specific decision-engine rule identified by `high_amount_rule_id` hit during scenario evaluation

This is the main difference between structured workflow-rules and legacy workflows. Legacy workflows can only react to the final outcome. Structured workflow-rules can react to the specific reason that outcome happened.

This is useful when multiple different scenario rules could all lead to the same final outcome. For example:

- `amount > 20000` could lead to `review`
- a velocity rule could also lead to `review`

If you only want the workflow action when the high-amount rule was the cause, use `rule_hit`.

You can also list multiple rule ids:

```json
{
  "function": "rule_hit",
  "params": {
    "rule_ids": ["high_amount_rule_id", "velocity_rule_id"]
  }
}
```

That means the condition passes if any listed rule hit.

Step 4. Add the action:

```json
{
  "action_type": "create_case",
  "action_config": {
    "title": "High amount transaction",
    "reason": "Amount rule hit and outcome is review",
    "source": "decision-engine"
  }
}
```

What this means:

- if all conditions on the structured workflow-rule match, create one workflow execution with action type `create_case`
- `action_config` is the payload persisted with that workflow execution and sent to the downstream dispatcher

The currently supported action types are:

- `create_case`
- `add_tag`
- `emit_event`

Example `add_tag` action:

```json
{
  "action_type": "add_tag",
  "action_config": {
    "tag": "high_amount"
  }
}
```

Example `emit_event` action:

```json
{
  "action_type": "emit_event",
  "action_config": {
    "event_name": "transaction.high_amount.review",
    "severity": "medium"
  }
}
```

### How structured conditions combine

Conditions on a structured workflow-rule combine with AND semantics.

With the example above, the structured workflow-rule only creates the case when both conditions match:

- the decision outcome is `review`
- the specific high-amount rule hit

If either condition fails, the action is not created.

### End-to-end runtime explanation

Take this evaluated payload:

```json
{
  "object_id": "txn_10001",
  "object_type": "transactions",
  "fields": {
    "object_id": "txn_10001",
    "productid": "prod_001",
    "updated_at": "2026-06-02T10:30:00Z",
    "amount": 25000,
    "merchantid": "m_12345"
  }
}
```

If the scenario contains the rule `amount > 20000` with score modifier `20`, the decision flow is:

1. the scenario evaluates the payload
2. the high-amount rule hits
3. the score increases by `20`
4. the scenario thresholds produce final outcome `review`
5. the structured workflow-rule is evaluated
6. `outcome_in(["review"])` passes
7. `rule_hit(["high_amount_rule_id"])` passes
8. the `create_case` action is turned into a workflow execution

If the final outcome is `review` but the high-amount rule did not hit, the workflow-rule does not match. If the high-amount rule hits but the final outcome is not `review`, the workflow-rule also does not match.

### Other condition possibilities

Current structured workflow-rule conditions are:

- `always`
- `never`
- `outcome_in`
- `rule_hit`
- `payload_evaluates`

`always`

```json
{
  "function": "always",
  "params": null
}
```

This always passes. Use it when you want every decision reaching that workflow-rule to create actions.

`never`

```json
{
  "function": "never",
  "params": null
}
```

This always fails. It is mostly useful for testing or temporarily disabling a workflow-rule path without deleting it.

`payload_evaluates`

```json
{
  "function": "payload_evaluates",
  "params": {
    "expression": {
      "name": "gt",
      "children": [
        {
          "name": "Payload",
          "children": [
            {
              "constant": "amount"
            }
          ]
        },
        {
          "constant": 20000
        }
      ]
    }
  }
}
```

This re-evaluates a boolean AST expression against the payload at the workflow-rule stage. In the example above it checks whether `amount > 20000`.

Use `rule_hit` when the automation must follow the exact scenario rule that matched. Use `payload_evaluates` when the automation should depend on a direct payload condition, even if that condition is not represented by a single scenario rule.

### Plain-English meaning of the example

The example structured workflow-rule says:

- create a workflow-rule called `High amount escalation`
- stop checking further structured workflow-rules after it matches
- only match if the final decision outcome is `review`
- only match if the specific high-amount scenario rule hit
- if both are true, create a case workflow action

In plain English:

`If this transaction was reviewed because the high-amount rule fired, create a case.`

### When to use which

Use legacy `workflows` when:

- outcome-based automation is enough
- one action per outcome is sufficient
- you want the simplest authoring flow

Use structured `workflow-rules` when:

- the action should depend on a specific rule hit
- the action should depend on a payload expression
- you need multiple conditions
- you need multiple actions from the same matched workflow rule

## Execution And Snoozing

The Swagger `Execution` section is about running decisioning work now, later, or in the background. It is separate from `workflow-executions`, which are the follow-up actions created after a decision already exists.

### Rule snoozes

Endpoints:

- `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/rule-snoozes`
- `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/rule-snoozes`

Rule snoozing is a temporary suppression mechanism for scenario rules. It is used when a rule would normally hit for a specific object, but you want to mute that rule for that object until an expiry time.

The scope is exact, not broad. A snooze only applies when the evaluation uses the same `tenantId`, `scenarioId`, `object_type`, `object_id`, and a rule with the same `snooze_group_id`.

Practical example:

- a high-amount rule normally hits for transaction `txn_10001`
- you have already reviewed that case and want to suppress that rule for that transaction until tomorrow
- you create a rule snooze for the relevant `snooze_group_id`, `object_type`, and `object_id`

During decision evaluation, active snoozes are checked before rule scoring is finalized. A snoozed rule is recorded as snoozed instead of behaving like a normal hit.

In practical terms:

- the rule still appears in execution results
- its outcome becomes `snoozed`
- it no longer contributes score while the snooze is active
- the behavior ends automatically after `expires_at`

Testing pattern:

- evaluate once without a snooze and note the score
- create a rule snooze for the same object and rule group
- evaluate the same object again
- confirm the rule is now `snoozed` and the score impact is removed

### Scheduled executions

Endpoints:

- `GET /v1/tenants/{tenantId}/scenarios/{scenarioId}/scheduled-executions`
- `POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/scheduled-executions`

Scheduled executions are for future runs of a scenario. You create a scheduled execution record with a `scheduled_for` time and either:

- a fixed list of evaluation items
- or no explicit items, in which case the worker can load candidate records from the trigger object type

When the worker sees that the scheduled time has arrived, it runs the scenario and marks the execution as completed or failed.

Use scheduled executions when the scenario should run later rather than immediately at API request time.

### Async decision executions

Endpoints:

- `GET /v1/tenants/{tenantId}/async-decision-executions`
- `POST /v1/tenants/{tenantId}/async-decision-executions`

Async decision executions are queued background evaluations. Instead of evaluating records inline, the API stores a queued execution record and the worker processes it later.

Use async decision executions when:

- you want to evaluate many items in the background
- you do not want the client request to wait for all evaluations to finish
- you want worker-driven processing rather than synchronous API evaluation

If a `scenario_id` is supplied, the async execution runs that scenario. If it is omitted, the service can evaluate all live scenarios for each item.

### Workflow executions versus execution endpoints

These are different concepts:

- `scheduled-executions` and `async-decision-executions` are about running the decision engine
- `workflow-executions` are about actions created after the decision engine has already run

Related follow-up inspection endpoints include:

- `GET /v1/tenants/{tenantId}/decisions/{decisionId}/workflow-executions`
- `GET /v1/tenants/{tenantId}/decisions/{decisionId}/screening-executions`
- `GET /v1/tenants/{tenantId}/decisions/{decisionId}/scoring-requests`

Plain-English summary:

- use `rule-snoozes` to temporarily mute rules for specific objects
- use `scheduled-executions` to run a scenario later
- use `async-decision-executions` to run decisioning in the background
- use `workflow-executions` to inspect post-decision actions
- use screening and scoring execution endpoints to inspect downstream enrichment work

## Screening Service Contract

Screening dispatch now targets:

- `POST /internal/v1/tenants/:tenantId/decision-screenings` on `screening-service`

Screening status callbacks are now received at:

- `POST /internal/screening-status-updates`

The current decision-engine screening config JSON is expected to provide enough information to build the downstream intake request. The worker supports these practical fields today:

- `queries`
  - literal array of screening queries
- `entity_type`
  - optional query type forwarded with each query
- `query_fields.name`
  - source field name from the evaluated object used to build the screening query
- `query.name`
  - literal name or source field name fallback
- `provider_config`
  - forwarded to `screening-service`
- `limit_override`
  - forwarded to `screening-service`
- `unique_counterparty_identifier`
  - forwarded as-is
- `counterparty_id_field`
  - source field name used to derive `unique_counterparty_identifier`

## Provider status callback shape

Screening executions and scoring requests can now be updated with provider-result metadata, not just a status string.

Example screening execution update:

```json
{
  "status": "completed",
  "provider_reference": "screening-job-42",
  "response_json": {
    "matches": [
      {
        "dataset": "pep",
        "score": 0.98
      }
    ],
    "provider_status": "cleared"
  },
  "last_error": ""
}
```

Example scoring request update:

```json
{
  "status": "failed",
  "provider_reference": "score-run-17",
  "response_json": {
    "provider_status": "error",
    "reason_code": "upstream_timeout"
  },
  "last_error": "provider timeout after 30s"
}
```

Persisted screening/scoring execution records now include:

- `request_json`
- `response_json`
- `provider_reference`
- `last_error`
- `created_at`
- `updated_at`
- `sent_at`
- `completed_at`
- `failed_at`
