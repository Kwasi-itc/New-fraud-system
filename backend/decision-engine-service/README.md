# Decision Engine Service

## Execution-time object validation

Single-scenario, all-scenario, ingestion-triggered, live/phantom test-run, scheduled and async evaluations prepare each object before evaluating formulas or writing decision effects. Supplied fields are full objects: every active non-nullable business field is required, nullable omissions become NULL, and explicit NULL is accepted only for nullable fields. Unknown and archived fields are rejected. Empty or omitted fields retain stored-record lookup behavior; fetched records undergo the same validation, with invalid stored records classified as internal data-integrity failures.

The top-level object ID is authoritative. Preparation injects the model's record lookup field when omitted and rejects a conflicting supplied identity. Managed metadata is optional for evaluation. Present managed values follow their declared model type or the fixed physical-column contract (`id` is a string; `updated_at`, `valid_from`, `valid_until` are timestamps; `valid_until` may be NULL). Unrecognized managed names without a declared type are rejected.

Normalization accepts ingestion-compatible string forms for booleans, integers, floats, timestamps and IP addresses. Strings remain strings. Integers preserve signed 64-bit precision; unsafe already-rounded floating-point integers and non-finite floats are rejected. Timestamps normalize to UTC and IP addresses to canonical text. Enum membership uses the normalized value. HTTP payloads, persisted execution requests and record readers preserve JSON numeric text before normalization. Formula constants and arithmetic retain existing evaluator behavior; this change does not promise arbitrary-precision arithmetic.

One prepared object owns a new normalized map and one model snapshot. Scenario fan-out and live/phantom comparison reuse them. Direct PostgreSQL reads receive that snapshot and reject a different tenant; HTTP ingestion reads retain their independent service model resolution and cannot guarantee the same revision. The resolved revision is returned as `model_revision` and persisted with prepared decision request evidence. Model freshness follows the existing model-cache policy; execution-time resolution does not mean a fresh network request on every execution. The compiled validation cache is bounded to 128 tenant/object/revision entries with a 30-second TTL.

Malformed JSON returns 400. Supplied-object validation returns 422 with the existing structured `error`, plus `model_revision`, `validation_errors` and optional `truncated`. Issues are sorted by field/code, capped at 100 and never echo supplied values. Stored-record violations return 500 without exposing field details in the live response; invalid model type metadata returns 502. API client errors retain structured metadata and include field issues in the displayed message.

Async admission retains its existing transport contract. Validation occurs in the worker using the model resolved then. Invalid input terminates the execution without evaluation retries. Status, inline waits and callbacks expose `result_body.error` with category, revision, issues and truncation; `last_error` remains a safe summary. Terminal status, failure evidence, callback job and enabled lifecycle outbox writes commit together. Callback delivery retains its separate retry policy. If that transaction rolls back, a tenant-scoped recovery update makes the row claimable by the job retry and preserves any already-committed terminal result. Database unavailability during both persistence and recovery remains an operational recovery case. Scheduled failures stop retrying deterministic validation errors and include structured evidence in enabled lifecycle events; their status record retains its existing summary format.

No migration is required: failure evidence uses existing decision-owned JSONB storage in the shared database. Batch items and scenarios retain their existing partial-commit behavior after successful preparation.

### Verification and rollout

`go test ./...` and `go vet ./...` passed on 2026-10-06. Contract tests cover completeness, NULL, normalization, enums, identity, archives, integer fidelity, tenant/revision isolation, concurrent bounded caching, no decision effects on invalid preparation, and async transactional rollback. PostgreSQL integration tests compile but skip without `DECISION_ENGINE_TEST_DATABASE_URL`. The execution test helper now creates a disposable database using that explicit connection's CREATE DATABASE privilege and never falls back to `DATABASE_URL`. The added integration test uses real decision-engine migrations for rollback, tenant isolation and terminal-state guards. Race instrumentation was unavailable because cgo is disabled and no GCC compiler is installed. Frontend dependencies were unavailable, so client type/build checks remain pending.

Warm local validation benchmarks on Windows/amd64, Intel i7-1355U (three runs; float fields) measured:

| Fields | Time per object | Bytes per object | Allocations |
| --- | --- | --- | --- |
| 10 | 0.96–1.00 µs | 824 | 16 |
| 100 | 6.92–7.22 µs | 5,832 | 106 |
| 1,000 | 78.15–89.42 µs | 90,128 | 1,008 |

These measurements include normalization and construction of the new map, but exclude model copying/loading, record reads, cold compilation and scenario execution. Runtime metrics expose preparation latency/errors, model-resolution time and compiled-schema hits/builds/entries. End-to-end scale comparisons, database query plans, cold-cache/concurrent load measurements and an agreed overhead budget remain rollout gates. No production optimization claim is established by the local benchmark.

Before rollout, update producers and queued requests that send partial objects, or deliberately use stored-record lookup. API examples are illustrative and must contain every required field of the actual tenant model. Rebuild APIs and workers together, confirm data-model metadata availability, and run the disposable database and representative scale checks. The code changes do not deploy or restart running services.

Standalone Go service for the decision engine domain, extracted from the monolithic `api` service and designed to work alongside `data-model-service` and `ingestion-service`.

Current location in the workspace:

- `new/backend/decision-engine-service`

This folder now contains an active standalone service implementation plus the original planning documents.

## Purpose

The service is intended to own the full decision-engine behavior:

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
- River-backed execution workers for scheduled and async execution queues
- legacy poll-loop support for workflow, screening, scoring, and outbox dispatch tasks
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
- execution queues now run through River while the remaining dispatch tasks still use the legacy poll runner
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
- `SCHEDULED_EXECUTION_QUEUE_NAME`
- `SCHEDULED_EXECUTION_QUEUE_WORKERS`
- `ASYNC_EXECUTION_QUEUE_NAME`
- `ASYNC_EXECUTION_QUEUE_WORKERS`
- `GEOIP_MMDB_PATH`
- `GEOIP_LOCALE` (default: `en`)

`SCREENING_SERVICE_URL` is the preferred screening dispatch target for the worker. `SCREENING_PROVIDER_URL` remains as a fallback compatibility variable.

## IP geolocation

When `GEOIP_MMDB_PATH` is set, the API and worker open the MMDB file as a
read-only memory map. The evaluator accesses it through the `GeoIPLookup` port;
it does not import the data into PostgreSQL or expose the reader directly to
rule code. Lookups are cached once per decision evaluation, including across
concurrently evaluated rules.

Fields typed `ip_address` in the tenant data model receive these derived rule
accessors in the editor:

- `(field).country`
- `(field).country_code`
- `(field).region`
- `(field).region_code`
- `(field).continent_code`
- `(field).location_found`

The corresponding AST functions are `IPCountry`, `IPCountryCode`, `IPRegion`,
`IPRegionCode`, `IPContinentCode`, and `IPGeoFound`. Invalid IP strings fail the
evaluation explicitly; valid addresses without an MMDB record return `null`
for text accessors and `false` for `IPGeoFound`.

DB-IP City Lite is distributed under CC BY 4.0 and requires attribution. See
[DB-IP's download and licensing page](https://db-ip.com/db/download/ip-to-city-lite).

### GeoIP diagnostics

Set `LOG_LEVEL=debug` (or `DECISION_ENGINE_LOG_LEVEL=debug` when using the root
Compose stack) to emit these structured events:

- `geoip_mmdb_lookup_completed`: normalized IP, match status, matched network,
  country, region, database type/build time, locale, and lookup duration
- `geoip_rule_value_resolved`: rule function and the exact scalar value returned
  to the evaluator, with tenant/object context
- `geoip_mmdb_lookup_failed`, `geoip_mmdb_decode_failed`, or
  `geoip_rule_lookup_failed`: explicit provider/evaluator failure details

For the local Compose stack:

```sh
docker compose logs -f decision-engine-service decision-engine-worker | rg 'geoip_'
```

These debug events include full IP addresses and derived location data. Keep
them disabled in production unless that logging is permitted by your privacy
and retention policy.

## Aggregate Pushdown

Aggregate-heavy Marble `Aggregator(...)` rules can now be pushed down to `ingestion-service` instead of pulling record sets back into the decision engine first.

Current behavior:

- the decision engine compiles supported aggregate AST into a logical aggregate query
- `ingestion-service` executes the aggregate close to the tenant data tables
- aggregate evaluation is remote-only; disabled, unsupported, or failed pushdown returns an explicit evaluation error
- strict mode remains available, but aggregate evaluation no longer uses a local fallback path

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

- `add_to_case`

- `add_to_case_if_possible`
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
    "inbox_id": "55555555-5555-4555-8555-555555555555",
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
    "url": "https://automation.example.com/workflow-actions",
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
    "url": "https://automation.example.com/workflow-actions",
    "event_name": "transaction.high_amount.review",
    "severity": "medium"
  },
  "active": true
}
```

Case actions require a UUID `inbox_id` and use the configured case endpoint. The versioned [case intake contract](../case-manager-service/INTAKE_CONTRACT.md) defines create/reuse, literal titles, authoritative decision metadata, authentication, and retry deduplication. External `add_tag` and `emit_event` actions require an explicit HTTP(S) `url`; internal service credentials are not forwarded to it.

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

- `add_to_case`

- `add_to_case_if_possible`
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
    "inbox_id": "55555555-5555-4555-8555-555555555555",
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
    "inbox_id": "55555555-5555-4555-8555-555555555555",
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

- `add_to_case`

- `add_to_case_if_possible`
- `add_tag`
- `emit_event`

Example `add_tag` action:

```json
{
  "action_type": "add_tag",
  "action_config": {
    "url": "https://automation.example.com/workflow-actions",
    "tag": "high_amount"
  }
}
```

Example `emit_event` action:

```json
{
  "action_type": "emit_event",
  "action_config": {
    "url": "https://automation.example.com/workflow-actions",
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

When the scheduled time arrives, a River worker runs the scenario and updates the execution record to `completed` or `failed`.

Use scheduled executions when the scenario should run later rather than immediately at API request time.

### Async decision executions

Endpoints:

- `GET /v1/tenants/{tenantId}/async-decision-executions`
- `POST /v1/tenants/{tenantId}/async-decision-executions`

Async decision executions are queued background evaluations. Instead of evaluating records inline, the API stores a queued execution record and a River worker processes it later.

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
