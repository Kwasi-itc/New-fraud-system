# Decision Engine Service

## Screening review routing

Scenario screening configuration create/update/read APIs now include nullable `review_inbox_id` (a nonzero UUID). Apply migration `000017_screening_review_inbox` before deploying these API/repository changes or the new case consumer. The field is separate from provider `config_json` and is not sent to the sanctions provider. Configuration update replaces the field; omission/null clears it. Existing configurations remain readable with a null route. UUID syntax is validated on save; case-manager validates tenant, destination existence and active state when first creating an investigation.

On a newly published decision-linked screening requiring review, case-manager reuses the decision's primary case. If none exists, it uses the originating configuration's review inbox to create a pending decision case and attach the decision and screening evidence atomically. A missing, archived or wrong-tenant destination leaves case delivery retryable; no inbox is inferred from monitoring or unrelated workflow actions. Configure routes before enabling new result producers. A configuration becoming inactive prevents future decision dispatch, but does not cancel already published results. No automatic historical backfill is performed.

Deployment order: pause/drain result producers, apply owner migration 000017 and required case migrations through 000012 (including primary-membership preflight), deploy the accepting case consumer and decision-engine API, configure routes, then deploy/resume screening producers and delivery workers. Preserve screening arrival outbox records: consumers verify source publication. Downgrade refuses to drop populated routing; a coordinated rollback must preserve/export and explicitly clear routes only after draining dependent work. Do not deploy an older consumer while new decision-arrival events are pending.

`DECISION_TEST_DATABASE_URL` enables the isolated configuration persistence/migration test. It creates/drops its own disposable database; it does not migrate the supplied application database. The investigator UI reuses case evidence views; a screening-configuration editor is not introduced in this slice.

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
- `OUTBOX_PUBLISHER_URL`: optional absolute HTTP(S) endpoint, configured identically
  on the decision API and worker. Unset/blank disables integration outbox event
  creation and its River queue consumer; decisions and rule executions still
  persist, and workflow actions, screening/scoring work and async decision
  callbacks remain enabled independently. A malformed nonblank URL fails startup.
  When enabled, event persistence and queue insertion remain in the decision's
  transaction; either failure rolls back the transaction. Runtime metrics expose
  `outbox_publication_enabled`. Disabling does not delete existing events/jobs:
  queued jobs remain available for later resumption. Already failed events whose
  River jobs completed under older versions are not automatically replayed.
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

## Initial investigator review

The case workspace now uses human-authenticated owner review commands. See [the investigator review contract](../investigator/README.md) for API semantics, migration order, shared Docker build context, trusted JWT configuration, and synchronization limitations.

## Durable execution projection

Screening status now reconciles through an atomic source queue and decision-owned SQL contract. See [deployment and recovery](../screening-service/EXECUTION_PROJECTION.md) for migrations, worker operation, callback compatibility and source-managed retry behavior.

Service `migrate down` preserves the shared River schema: it rolls back only
decision-engine metadata and logs that distinction. Stop/drain the owner's
consumers before metadata rollback; shared queue rollback requires a separately
reviewed whole-stack operation. No migration-down command is a queue repair.

## Portable decision persistence preparation

Decision/execution services now submit typed `ports.JobRequest` operations through
their mutation store. They no longer expose pgx transactions or import River;
PostgreSQL adapters own producer configuration, tenant/resource binding and the
actual transactional INSERT. API and worker wiring preserve current queues,
attempt limits and payloads. Missing required producers return errors rather than
successful no-ops; router construction propagates queue-client initialization errors.

Execution terminal state, required callback enqueue and enabled lifecycle outbox
writes share one short transaction. On failure, recovery may requeue only the same
still-running tenant/resource/attempt; newer attempts and committed terminal rows
are preserved. Evaluation and HTTP delivery stay outside these locks. This does
not establish exactly-once evaluation or full stale-worker effect fencing.
Optional outbox publication retains its separate configured enable/disable policy.

The [migration inventory](../../mysql_migration/implementation/modules/03_decision_source_inventory.md)
lists source groups, drift checks and remaining target contracts. Staged MySQL
adapters are described below; there is no runtime cutover. Existing River retry/redrive
deduplication needs separate lifecycle work.

The API, workers and migrator explicitly select the shared `public` River schema.
Startup checks pinned migration lineage, required objects and runtime access;
`/readyz` repeats these checks and returns 503 for a missing/incompatible queue
or missing pool. `/healthz` remains a liveness check. Readiness performs bounded,
read-only catalog queries, not migration/repair, queue scans or a complete drift
audit. Mutations still fail atomically if queue objects become invalid afterwards.

Run the disposable PostgreSQL baseline and boundary suite from the repository root:

```sh
docker compose -p decision-port-test -f backend/decision-engine-service/compose.test.yaml run --rm test
docker compose -p decision-port-test -f backend/decision-engine-service/compose.test.yaml down --volumes
```

The fixture has no application network, published database ports or persistent
database volume. Service integration tests require local `decision_test_*` admin
configuration, create a fresh generated database per test and drop only that DB.
The separate existing `DECISION_TEST_DATABASE_URL` store suite is not enabled by
this fixture; its specialized setup and full MySQL differential acceptance remain
separate work. Passing the preparatory suite is not migration release approval.

## Staged MySQL platform and scenario foundations

`internal/store/mysql` now provides tenant-/transaction-bound record-tag,
risk-snapshot and textual IP-flag repositories. `PlatformStore` requires a local
authorization callback and explicit row/text budgets, and exposes only implemented
operations. It is not a partially successful main decision transaction manager.
Current API/worker construction remains PostgreSQL/River.

The [full mapping review](../../mysql_migration/implementation/modules/03_decision_source_inventory.md#full-owner-mapping-and-review-checkpoint)
covers all fresh decision-owned schema objects. The user-approved semantic
choices and remaining privilege/queue dependencies are recorded there.
The machine artifact is proposed design, not a deployment migration or evidence
that existing data/grants were inspected.

The dedicated platform fixture is:

```sh
docker compose -p decision-platform-mysql-test -f backend/decision-engine-service/compose.mysql.test.yaml run --rm test
docker compose -p decision-platform-mysql-test -f backend/decision-engine-service/compose.mysql.test.yaml down --volumes
```

It uses generated disposable reference/target databases and no application
network or published ports. MySQL 9.7.1 cached-image exploratory tests are not
acceptance on the committed 8.4.8 target. No runtime cutover or full decision
MySQL persistence/queue implementation is supplied by this slice.

Batch D01–D05 adds decision-owned exact keys and scenario/iteration/rule/publication
definitions, an optional live-iteration relation and `IterationCreator.CreateDraft`.
`metadata.ScenarioFoundationStatements` appends to the three existing platform
steps; `ScenarioFoundationPlan` requires independently supplied exact definitions.
It does not learn/adopt an existing application schema, install at startup or
provision accounts. The locked local definer and `ScenarioDefinerGrants` are
explicit privileged deployment inputs; runtime has no dictionary access or
standalone resolver EXECUTE. Production account verification, frozen 8.4.8
definitions and the complete owner installer remain pending.

Name triggers overwrite supplied key IDs from the actual tenant/name bytes.
Dictionary hashes are non-unique accelerators; complete bytes establish equality.
Buckets/keys are retained after scenario deletion; GC is not implemented. Caller
authorization still comes from trusted local identity, not a tenant parameter.
Live links use tenant/scenario/iteration foreign keys; deleting the referenced
iteration removes only the link, leaving the scenario and its tenant intact.
Owner guards reject disabled FK/unique checks and ownership changes. Creation
receipts/publication events reject direct UPDATE/DELETE; parent cascades retain
the source deletion policy. See [MySQL FK actions](https://dev.mysql.com/doc/refman/8.4/en/create-table-foreign-keys.html)
and [trigger security](https://dev.mysql.com/doc/refman/8.4/en/create-trigger.html).

Draft creation uses a dedicated counter per tenant/scenario. Lock order is local
authorization, counter, creation receipt, then iteration insertion/counter update
and receipt insertion. It does not lock a tenant or scenario parent FOR UPDATE.
Only the staged creator allocates versions; arbitrary direct iteration INSERTs
are not a supported allocation path. Whole-mutation failures roll back all three
writes. A stable operation UUID and stable input IDs are mandatory; changed input
for that operation conflicts. The hash covers the typed JSON-encoded input
(whitespace compacted; it is not a semantic JSON-equivalence algorithm).
This is a storage component: the authoring service must validate the AST against
the current model before invoking it. It is not a complete scenario-authoring API.

Receipts store the original encoded response bytes, not the current mutable
iteration. They survive iteration deletion, preserving replay of that original
creation; deleting the whole scenario removes its counter and scoped receipts.
An uncertain commit must be reconciled with the same operation/input and fresh
authorization. There is no automatic retry or claim of exactly-once external
effects. Formula JSON is read back on the insertion transaction before encoding
the response. Inputs are explicitly bounded to 1 MiB encoded and receipts to
2 MiB; overflow errors are not truncated successful values. UTC microsecond time
is generated during processing without request-boundary timestamp checks.

Subsequent local work adds staged scenario/rule CRUD, commit/clone/publication
atomicity, decision/evidence and test-run/phantom storage, bounded history
search/paging/snoozes, configuration arrays and structured workflow reads. These
stores deliberately expose only implemented slices; none is a partially
successful live `TransactionManager`. Required queue/effect composition remains
unfinished. The [task checklist](../../mysql_migration/implementation/modules/03_decision_engine_service.md#granular-execution-plan-five-tasks-per-batch)
distinguishes completed storage work from full-owner acceptance.

The approved auth, case-inbox/tag and screening-source consumer contracts fail closed
when a provider is absent. Their fixture providers are tests, not implementations
of AU-01, CM-01 or SR-01. MySQL activation stays blocked pending those owners.

`BrowserWorkflowStore` implements snapshot list, optimistic-version save and
audited soft delete, but is not wired into the router. It uses the portable
v1/v2 browser decoder, retains existing condition/action IDs and rejects arbitrary
structured configurations rather than overwriting them. All legacy/structured
workflow configuration writers take a dedicated per-scenario authoring guard;
they do not take an exclusive lock on the scenario used by decision FKs. The
additive browser lineage has 109 steps, preserving the 106-step effects prefix.
Workflow mutations require that guard installation; there is no old-schema
fallback. Staged `AuthoringStore.CopyScenario` and
`ConfigurationStore.CopyWorkflowRules` now copy bounded repeatable-read source
snapshots into one atomic destination mutation. Fresh identities and ordered
children retain relationships; source history/decisions/jobs are not copied.
Scenario-copy scope matches the source operation: iterations, rules and active
structured workflows, not legacy workflow definitions or screening/scoring
configurations. Source snooze-group references are retained, not cloned/reset.
Live selection requires a new guarded publication and event. Explicit local
dependency guards must validate current case/tag/model permissions; workflow-only
copy also fences concurrent destination changes. There is no blind retry after
uncertain commit and no successful replay inferred from an existing destination.
`BrowserWorkflowStore.Copy` now composes bounded destination validation and
batched copy audit inserts with graph creation on that SAME transaction. It
rejects unsupported browser graphs, checks the actual destination model through
a mandatory local guard, appends priorities under the destination authoring guard
and records the trusted actor/source scenario with the new rule identities.
Audit or preparation failure rolls back every copied rule and child. Pinned
MySQL 8.4.8 correctness tests cover denied/inactive/missing providers, tenant
isolation, audit failure, bounds and concurrent copies. Real provider integration
and full cross-engine browser acceptance remain open.

The auth-owned `decision.workflow.read` and `decision.workflow.author` actions
must retain the source's platform-administrator AND case-access requirement;
ordinary scenario authoring permission must not imply browser workflow access.
The established verifier supplies the actor/session/membership binding, and the
store checks that the browser actor matches it. Identity authorization precedes
case authorization, the authoring guard and workflow row locks. An unlocked
old-rule preview is only authorization input; save/delete reload and fence its
version under the guard before mutation. Owner providers must use
short local SQL and the caller transaction, not network calls under locks.

Case-owned `case_manager_authorize_decision_tags_v1` accepts at most 25 unique,
nonzero tag UUIDs as a sorted JSON array (including `[]` for no tags), checking
tenant, permission and active status when enabling. The read-only
`case_manager_visible_decision_inboxes_v1` accepts up to 500 unique inbox UUIDs
and returns a JSON array of the visible subset. Inactive but authorized inboxes
remain visible for editing history. NULL, malformed, duplicate, oversized or
unrequested returned IDs fail closed. Listing uses one bounded visibility query,
not a query per rule. These are tested consumer contracts only; no case-owner
provider, grants or activation is supplied here.

The additionally approved `case_manager_authorize_decision_inboxes_v1` accepts
the trusted actor, tenant, session, membership version, a sorted JSON array of
at most 500 unique nonzero inbox UUIDs, `require_active` and `write`. The case
owner must authorize EVERY requested inbox, recheck current identity/permissions
and lock in deterministic order for writes, on the caller's transaction. It must
not commit, issue DDL, write unrelated owner data or perform network IO. A false
result denies the operation; a missing/NULL result is an unavailable contract,
not successful empty authorization. Browser copying uses one batch rather than
one inbox call per rule; tag checks use sorted batches of at most 25 unique IDs.
Save checks old/new inbox authority before workflow locks and requires active
new routing when enabled, while allowing an authorized inactive historical
inbox. This is a tested consumer contract, NOT a case-provider implementation.

`DecisionUnitOfWorkStore` combines queued effects, explicitly authorized custom
lists and optionally requested fenced execution operations on one MySQL
transaction. Stores must share the exact pool/schema/tenant/row/byte scope.
The portable `DecisionTransactionManager` is consumed by the actual
`DecisionService`; it does not pretend to implement legacy unfenced execution
mutations. Explicit source enqueue calls revalidate the already-atomic initial
delivery, not create duplicates. Missing execution capabilities or ignored
required-job failures poison the entire transaction. Scheduled/async prepared
output completion fences owner scope and the lease before AND after all writes;
completion, callbacks and required jobs roll back together. Evaluation/network
work must happen before these short persistence callbacks. Request preparation
and pinned scheduled evaluation are now staged below. Runtime worker/identity
composition remains unfinished; the old commit-per-item loop must not be reused.

Target-only `internal/mysqlapi` adapters expose generation, cancellation and
replay fields without changing PostgreSQL DTOs/routes. Reads and generation-
conditional cancellation use the authorized execution store, including caller
tenant/scenario checks before UUID-only mutations. Cancellation JSON requires an
explicit signed `expected_generation` (zero is valid); missing/NULL/duplicate/
unknown/oversized/trailing input is rejected. Creation-response conversion does
not claim inline evaluation. Actual SQL adapter tests cover tenant/scenario
checks, stale generations, cancellation replay, callback-only cancellation,
revocation and required-callback insertion rollback. The checkpoint below adds
admission, waiting and expected-generation redrive adapters. Public HTTP route
integration and real owner-provider/runtime activation remain pending.

### Staged execution request preparation and pinned evaluation

`internal/mysqlexecution.NewPreparer` requires the execution and registry readers
to share the exact pool, logical database, tenant and projection budgets, plus
the real evaluator's required read dependencies. `PrepareRequest` authorizes
the request, resolves a fresh model, verifies the published trigger/aggregate
registry and validates the full bounded batch. Payload and model snapshots are
detached, IDs stay byte-exact (including trailing spaces), and repeated object
IDs remain independent items rather than being silently deduplicated. Invalid
items never produce a partial successful batch. No-scenario requests prepare
only payload/model evidence; all-live scenario selection remains execution-time.
This operation is NOT durable admission or a new HTTP route. Later creation must
recheck current identity/live/model bindings before committing an owner and jobs.

`PrepareScheduled` reads the real running owner at its expected tenant/attempt/
generation and loads that owner's committed iteration/rules, bypassing live
scenario/iteration/rule caches. The shared real evaluator creates opaque local
output plans, with independent per-item AST/aggregate caches and no persistence
transaction. Model/provider/record IO occurs before the eventual short write
transaction. Explicit items and bounded candidate selection are supported; an
empty candidate set is valid, but missing/invalid input is not a successful
no-match. Count, normalized-byte, model and AST budgets fail explicitly.

Pinning freezes iteration formulas/rules/thresholds, not the data model or every
mutable scenario parent field. Parent trigger object type, scenario-level
routing and historical reads retain the existing current-state contract; they
must validate against the attempt's current published model. Each data read
rechecks its registry gate. This is not a global historical-data snapshot.

`PreparedScheduled.PersistAll` writes on the supplied decision mutation contract
and returns provisional results; it does not commit or acknowledge a job. The
staged worker now verifies its identity against the actual lease and calls
`DecisionUnitOfWorkStore.CompleteScheduled` once for the whole batch. Plans are
single-use: rollback/uncertain commit requires reconciliation/repreparation,
not automatic replay. Source PostgreSQL live evaluation still uses its existing
path and source-compatible identity normalization. No workers, routes, owner
providers or database selector are activated by this staged implementation.

### Staged worker, admission and reader composition (2026-10-30)

`NewTargetPreparer` now connects the shared evaluator to actual same-scope
`mysql.EvaluationReaders`: configuration/workflow reads, snoozes, platform risk/
tags/IP flags and custom lists. These expose narrow read capabilities, not
nil-embedded writable repositories. Every SQL read retains its owner authority,
projection limits and tenant predicates. The supplied model provider must still
implement the real data-model owner's published-version contract. Missing
providers are not replaced with successful empty results.

`ScheduledWorker` and `AsyncWorker` compose actual scoped claim/start stores,
the shared `DeliveryPoller`/`LeaseSupervisor`, preparation and fenced unit of
work. Constructors reject different pools, tenants, schemas, queue policies or
budgets. Polling/work/heartbeat durations and concurrency are explicit. Claim
commits before execution; evaluation/provider IO does not retain owner write
locks. Completion binds the exact prepared identity to the actual lease and
persists the WHOLE batch, evidence, effects/jobs, result, enabled lifecycle event
and required callback together. A callback/job failure rolls back every output.

Scheduled attempts retain the owner's stored iteration. Async attempts select
current live iterations once per attempt, using bounded batched metadata/rule
reads and one registry gate per used table, not per scenario/rule. All item
outputs persist atomically. The source async result format is intentionally
preserved: it contains the FINAL item's explicit/multi-scenario result, not a
new array containing every item's result. Zero live scenarios remains valid,
but payload/model validation is never skipped. Total fan-out/output bytes are
bounded independently of input-item count.

Validation failures and bounded business retries record redacted owner errors
and atomic lifecycle events. Confirmed output rollback retries transport without
consuming a new domain attempt; plans are never reused. Cancelled contexts,
stale leases and uncertain transactions never authorize an unfenced failure or
replay. Ambiguous completion reads the exact permanent receipt and owner outcome;
only confirmed completed state counts as success. Unconfirmed outcomes remain
observable for rescue/reconciliation. A heartbeat error racing a committed result
is conservatively observable under the shared supervisor contract.

`Admission.CreateScheduled/CreateAsync` performs bounded detached input/model/
AST preparation first, then reauthorizes and rechecks the selected live link and
registered published versions inside owner/job/event creation. Concurrent key
replays create no duplicate work/events; an existing authorized original request
can be replayed after model/publication changes. Durable bodies preserve omitted
field maps as stored-record lookups rather than serializing managed fields as
incoming payloads. `AdmissionOutcomeUnknown` retains the server-generated
execution ID even without a supplied idempotency key. A future HTTP adapter must
preserve this reconciliation ID rather than invite a new submission.

`mysqlapi.ExecutionSubmission` adapts real creation and bounded committed-state
waiting. Waiting takes independently authorized short reads and holds no SQL
transaction while sleeping. Failed/cancelled/expired waits are not completed-
inline success. A wait error AFTER committed admission retains the execution ID
in `ExecutionWaitError`; it does not mean admission rolled back. Separate redrive
adapters require explicit expected generation and redrive authority. Callback
redrive preserves the terminal evaluation/result.

Pinned MySQL 8.4.8 and PostgreSQL 16 component/regression tests cover these paths,
including concurrency, revocation, stale publication/model, required-job/event
rollback, exact IDs and shutdown joining. This is NOT public HTTP or deployment
acceptance. Runtime route/auth wiring, real foreign owner providers, callback/
workflow/screening/scoring/outbox business dispatch, calendar loop composition,
restricted grants/readiness and frozen release rehearsal remain open. No source
runtime selector, API route, worker command, live database or migration lineage
is activated/changed by these components. Performance/load/query-plan testing
remains deferred at the user's request.

The staged tenant reader consumes the data-model owner's physical registry and
table-scoped published version (not the global revision). It pins the shared
registry barrier through each data query, validates registered field identities
and types, and never guesses a physical name or switches readers on failure.
The approved indexed object lookup extension uses a registered key column and
read-only owner resolver, without exposing the private exact-key dictionary.
Current PostgreSQL HTTP model responses do not supply the required table version;
the target model provider/complete publication preparation still need composition.

Exact object IDs remain binary exact, including trailing spaces. History search
uses the approved `utf8mb4_0900_as_ci` contract, not deployment-specific PostgreSQL
locale emulation; see [MySQL comparison collations](https://dev.mysql.com/doc/refman/8.4/en/case-sensitivity.html).
General aggregate comparisons retain SQL NULL semantics (`neq` excludes NULL);
explicit null predicates remain available. A wallet ownership rule still needs
valid incoming hash/user input enforced by its model/preparation or explicit
rule guards. A missing stored owner does not establish another account.

The approved optional `text_equality_indexes` owner payload contains field/index
UUIDs, `sha256_binary_v1`, and explicit generated-column/index names. When present,
string equality (including each `IN` element) narrows candidates with the
registered nonunique hash and retains a complete binary value comparison.
`neq`, range and prefix predicates do not gain an indexed guarantee from this
strategy. Tests use 5,000 LONGTEXT wallets with a hot hash and a synthetic forced
collision; they are query-plan evidence, not production EPS evidence.
The data-model typed planner, payload builder and verified publication
mapping persistence/retention passed scoped MySQL 8.4.8 SQL race tests.
Target `RegistryAggregatePublicationGuard` uses the same AST compiler
and SQL capability compiler as evaluation. It validates static identifiers,
cross-table filter bindings, literals and dynamic expression types, pins used
owner registry versions in deterministic order and composes a mandatory remaining
full preparation guard. Typed witnesses validate compilation only, never replace
actual runtime input checking or execute queries against tenant records. It does
not call the model HTTP provider under locks or certify unrelated AST/index
preparation. Real full owner/provider composition remains pending.
Missing optional mappings retain the existing unindexed
comparison path; this must not be labelled performance-ready by an activation
guard. No runtime selector or automatic owner index creation was added.

`EffectStore` stages decision/evidence, workflow/screening/scoring execution,
outbox and record-tag/risk/IP writes on one authorized owner transaction. Parent
iteration bindings are derived from authoritative decisions; foreign-key checks
bind effects to the same tenant/scenario/config. Batch writes/readback use at
most 100 identities per statement and fail rather than return partial results.
As with the other owner stores, callers must propagate every repository error
and roll back the whole mutation. Results are provisional until commit.
`WithCustomLists` adds an explicit custom-list capability to that exact effect/job
transaction, not a second pool/transaction. Separate `decision.list.read` and
`decision.list.mutate` owner-auth actions run before owner locks. Read-only access
cannot mutate lists even inside a writable decision transaction; a generic
persistence grant is insufficient. Missing capability/provider fails explicitly.
The real auth provider must support these actions before activation. Full main
TransactionManager composition remains separate from this staged integration.
Global worker listings deliberately require an explicit system-scope adapter;
the tenant store does not disguise a tenant subset as a global queue.

Screening reconciliation consumes the approved read-only owner publication,
locks source before projection and advances only a newer version. Once attached,
legacy dispatcher writes are rejected. Only decision-owned projection rows are
updated. The real screening/case/auth providers and complete durable lifecycle
composition remain activation prerequisites. This storage slice must not be used as
an independently activated dispatcher or reported as completed D21–D25 work.

The approved `internal/mysqljobs` target v1 identity contract covers all seven
decision job kinds using `(kind, tenant, resource, domain_attempt,
redrive_generation)` as the exact reservation tuple. Transport retries keep that
tuple; a legitimate new domain retry or authorized redrive does not collide with
retained completion of an earlier one. Callback counters are separate from
evaluation counters. Payload encoding and queue overrides are not logical
identity. The strict codec rejects ambiguous/unversioned input but does not
authorize the caller or prove owner state/lease validity. Durable reservation,
receipt and worker composition for all seven kinds remain unfinished; workers
remain inactive.

`NewQueuedEffectStore` is the explicitly job-producing alternative to the
persistence-only `NewEffectStore`. Its additive 115-step lineage implements
workflow, screening, scoring and outbox first-attempt deliveries. All four queue
policies are mandatory and copied at construction. Bounded 100-row receipt/job
batches commit with their authorized owner effects; required enqueue failure
also poisons the transaction if the callback ignores the repository error.
Same-tenant FKs bind every receipt to one real owner row. The full exact
attempt/generation tuple is unique; receipts outlive transport rows, and neither
transport retries nor deletion of a transport row may allocate that tuple again.
`EffectRetryStore` now stages separately authorized, expected-identity domain
retry and redrive for these four dispatch owners. Each operation locks one
resource, its latest permanent receipt and its terminal transport before
resetting the eligible owner and inserting the required successor atomically.
Retry increments the domain attempt within an explicit configured budget;
redrive increments its generation without resetting the domain attempt or
replenishing that budget. Both counters are monotone, so the existing exact
identity index supports a reverse `LIMIT 1` current-attempt fence. A scoped
5,000-receipt MySQL 8.4.8 plan used that index and read one row; this is not an
application-throughput claim. Active transports must finish/rescue first.
Failed dispatches are retryable; an uncertain provider `sent` state requires
explicit redrive, never automatic retry. Successful work and source-managed
screening projections cannot be reset. Source-compatible provider resets clear
the prior response/reference/error/timestamps, retaining immutable request and
owner relationships. Real provider idempotency/reconciliation and trusted
retry/redrive identities remain activation prerequisites. Permanent receipts
are not cleaned; owner deletion remains restricted while receipts exist.

`EffectDeliveryStore` adds tenant/kind-scoped claim, heartbeat and transactional
finish for those four owners, with a separate mandatory worker authorizer.
Claims use `SKIP LOCKED`; lost commit acknowledgement exposes no lease. The
shared River error-count retry policy is reused without conflating transport
and domain attempts. Finish locks resource -> receipt -> delivery, validates
the complete lease, runs only a short local-SQL outcome callback and rechecks
expiry before committing. A stale/replayed acknowledgement cannot commit local
effects. External calls belong outside that transaction and still require
receiver idempotency; this is not an exactly-once network-delivery guarantee.
The current-attempt receipt is also fenced before four-effect finish/rescue;
superseded workers cannot write newer owner outcomes. The real worker identity
provider, external business handlers and full runtime/startup/grant verification
remain open. Recovery, cancellation and retention have staged implementations
and scoped tests, not automatically activated workers.
That four-owner transport does not accept scheduled/async/callback jobs. No
API/worker runtime selector was changed.

The additive `EffectCancellationStatements` lineage appends five steps to the
unchanged 140-step execution plan. It records permanent, exact-attempt dispatch
cancellations and prevents deletion of effect receipts. `EffectRetryStore.Cancel`
requires distinct authority and atomically records a redacted reason and cancels
an available/running transport. It preserves business results and never claims
to undo an external send. Same-identity/same-reason replay returns the original
record, including after transport retention; changed reasons and stale identities
fail. Public adapters must expose the separate dispatch cancellation state, not
rewrite provider status. Only explicitly authorized redrive can resend cancelled
pending work. Screening-owned projections remain an owner-contract dependency.

`NewOwnedEffectDeliveryMaintenance` supplies a real local dispatch-exhaustion
mutation for the shared maintenance scheduler. It fails pending decision-owned
dispatches atomically with transport discard, preserving evidence; sent/completed
work and screening-owned projections fail closed for reconciliation. No goroutine,
external handler or application worker is enabled by constructing this adapter.

`ExecutionStore` separately stages scheduled evaluations, asynchronous evaluations
and callbacks in the additive 140-step lineage, preserving all 115 earlier steps.
Its required action-specific authority distinguishes read, request, worker run,
redrive, schedule administration and schedule tick. It does not implement legacy
mutation methods that cannot express an expected attempt, generation or lease.
All three copied queue policies and a separate positive callback limit are
mandatory. Each new request commits with its required exact job receipt/delivery;
ignored repository mutation errors poison this owner transaction too.

Nonempty request keys are byte-exact, tenant-scoped and first-request-wins, even
if a later body differs. Empty keys disable deduplication. Recurring evaluations
also deduplicate the same tenant/scenario/scheduled instant. Conflicting explicit
ID, key and occurrence bindings fail instead of choosing an arbitrary match.
The existing private collision-safe key resolver enforces uniqueness; a nonunique
SHA index plus full binary residual makes key replay a bounded lookup. Async
scenario ownership is an optional same-tenant link: deleting a scenario removes
only the link, not the execution or its tenant identity. Scheduled executions
pin their same-tenant scenario/iteration with restrictive foreign keys.

Worker mutations require the complete current SQL delivery lease, domain attempt
and redrive generation. Lock order is execution -> receipt -> delivery. Final
expiry checks roll back outcome changes and required successor/callback jobs
together. Resuming the same running domain attempt does not consume a new domain
retry. Evaluation, callback and transport attempts are independent. Authorized
redrive uses an expected generation; async redrive also cancels old callback
identities and invalidates their tokens. An already sent external HTTP request
cannot be undone: send outside all owner transactions, with receiver idempotency
based on the complete delivery identity. No exactly-once external-send claim.

The interval schedule definitions require an explicit anchor, interval and shared-worker
`catch_up`, `coalesce` or `skip_late` policy. Configuration is immutable: replace
by deactivating the old definition and inserting a new one atomically. At most
one definition per tenant/scenario is active. A tick locks only that schedule,
checks its published iteration, reads database time and materializes at most 100
occurrences with bounded batched reads/inserts. Executions, required jobs,
occurrence receipts, skipped counts and next watermark commit together. Missed
work is never silently discarded by a default policy. The due cursor reads only
an indexed, authorized tenant page, not complete execution histories.

The approved corrected calendar v1 contract is implemented separately in
`internal/recurrence`; calendars are NEVER converted to elapsed intervals:

- Explicit hourly minute, or daily/weekly/monthly local `HH:MM`, named IANA zone,
  frequency and DST policies are required. Weekly means the local weekday.
- Monthly day 29–31 clamps independently within each month, then returns to the
  requested day in later months. It does not drift from February or skip March.
- A nonexistent local time is skipped and counted. An ambiguous fall-back time
  runs once, at the earlier instant. Invalid configuration is an error, not a
  successful skip. These are intentional source-edge corrections, not a claim
  to reproduce PostgreSQL scheduler bugs.
- A calendar definition requires an explicit local anchor rather than `StartAt`
  or `Interval`. Its immutable versioned spec, original anchor, next logical
  local slot and indexed UTC `next_at` are retained in the SAME schedule owner.
  Creation counts initial gap slots; a tick counts any subsequently crossed
  gaps, even when the next real occurrence is in the future.
- Catch-up emits bounded real occurrences; coalescing emits the latest due one;
  skip-late emits that one only within the explicit elapsed lateness allowance.
  Skipped totals include missed nominal slots and gaps, never a second fold.
  Skip-late windows must be shorter than the minimum nominal period (1h, 24h,
  7d or 28d respectively); this is not a guarantee of fixed elapsed spacing.
- Coalescing uses civil arithmetic and a bounded timezone-offset search, not a
  backlog/history scan. Each gap/due search is capped at 128 slots; unsupported
  offsets/transitions and DATETIME-range exhaustion fail the transaction rather
  than silently skipping work. Clock and timezone resolution stay outside SQL.
- Execution/job/occurrence writes and both cursors commit atomically. A cursor
  that no longer resolves to its stored UTC instant fails closed. All replicas
  must pin the same timezone database release (including OS-vs-Go tzdata source)
  before activation; changing timezone data needs an owner migration/review.

This refines the new, unreleased execution draft and appends two cleanup-index
ALTERs (140 steps total), without modifying any earlier 115 effect-job steps.
It is not an upgrade for a previously frozen 138-step release. ALTER references
require the exact preceding table snapshot, not an absent-object precondition.
Calendar SQL tests and their exact execution status are in the
module checklist. The source/public-DTO adapter, real scheduling loop and worker
composition remain inactive; defining this contract does not enable a scheduler.

`ExecutionDeliveryStore` stages tenant/kind-scoped claim and heartbeat for these
three queues using the same fixed-catalog lease machinery as the four effect
queues. Its typed wrapper does not expose generic effect completion. Owner
`RetryDelivery` records transport failures without consuming another evaluation
attempt, reuses the shared error-count retry policy and atomically records owner
failure plus a required failure callback on exhaustion. A callback-only
authorized redrive allocates a new generation without rerunning or clearing a
terminal evaluation. Delivery receipts remain retained, including on exhaustion.

This is staged owner code, not an active evaluator or a production worker.
Source calendar adapter/runtime integration, domain handlers, real
auth/provider composition, restricted deployment
grants and startup/readiness integration remain open. Receipt deletion is not
enabled and owner deletion is intentionally restricted. Test execution evidence
and pinned-version acceptance status are recorded in the module checklist; the
presence of this code does not establish full seven-kind or runtime readiness.

Staged expired-delivery recovery now has explicit `Expired`/`Rescue` paths for
both fixed owner families. Discovery projects only identities, in tenant/kind/
queue-scoped keyset pages of at most 100. Candidates are not work leases. Rescue
requires separate worker rescue authority; execution recovery additionally
requires owner rescue authority and exactly matching pool/database/tenant/queue.
It locks resource → receipt → delivery and verifies the observed token, transport
attempt and expiry, both under lock and at the final write. It never temporarily
extends an expired lease to pass the normal worker API.

Each recovered expiry records one transport error, retains its exact receipt
and reuses the shared error-history retry policy, without consuming a domain
attempt. Exhaustion commits the real owner failure and required async callback
with transport discard. Four-effect exhaustion requires an explicit trusted
local owner handler, not an implicit success/no-op; real handler composition is
still activation work. Local outcome failures roll everything back. An external
send already performed still needs receiver idempotency; rescue cannot undo it.
Native guard/policy tests do not prove SQL locking or query cost. Disposable
seven-kind, stale-token, concurrent-rescue, rollback and 5,000-row query-plan
test code is written but Docker execution is deferred by user request.

The approved execution cancellation contract adds a target-only `cancelled`
status and separate owner/callback cancellation timestamps. It does NOT alter
the PostgreSQL status enum or activate legacy handlers. Dedicated cancellation
authority and an expected generation are required. Pending/running cancellation
requires and invalidates the exact active delivery; a missing delivery fails
the transaction. It preserves the generation, so repeating the same cancellation
does not allocate duplicate jobs. Owner status and revoked tokens fence stale
workers. Cancelling a scheduled occurrence does not disable its definition.

Async cancellation commits a required cancellation callback when configured;
enqueue failure also rolls back owner cancellation and transport cancellation.
Cancelled is terminal for callback dispatch but not eligible for evaluation
redrive. Callback-only cancellation leaves evaluation status/result/timestamps
untouched. A separately authorized callback redrive creates a new generation
and clears only callback cancellation state. Already-sent callbacks cannot be
unsent; an in-flight external send can still occur despite local cancellation.
Four-effect domain cancellation and real business handlers remain owner/provider
composition work, not supplied by this execution-only state contract.

`CleanupTerminalPage` is a bounded, tenant/kind/queue-authorized maintenance
primitive, not an activated sweeper. It uses shared retention: completed and
cancelled transports expire after 24 hours, discarded after seven days. Owners
and permanent identity receipts NEVER expire through this operation. Discovery
uses an appended cleanup index and at most 100 IDs; batch receipt locks precede
batch delivery locks, avoiding a reversed finish/cancel/rescue lock order.
The cursor advances across locked/skipped identities, so zero deletions does
not mean the stream is exhausted. Paced cycles must wrap after a nil cursor.
Full current lineage/index attestation and narrow SELECT/DELETE transport grants
are runtime prerequisites; no DELETE grant on receipts/owners is required.

### Target stage 1–2 checkpoint (2026-10-30)

These are opt-in components, not a backend switch or permission to start MySQL
services. PostgreSQL/River command selection remains unchanged.

- `internal/mysqlapi.ExecutionHTTP` supplies creation, single-record status,
  wait-on-creation, expected-generation cancellation and separately authorized
  retry/redrive routes. A mandatory request-scoped factory must derive trusted
  identity from authenticated context. Strict bounded JSON rejects unknown and
  duplicate fields; responses redact internal failures and retain the execution
  ID when admission committed or its outcome needs reconciliation. Execution
  collection/filter and recurring-schedule administration routes are not
  registered by this narrow component; legacy routes must not be mixed in as
  a silent fallback when selecting the target backend.
- `internal/mysqlexecution.Scheduler` processes a bounded keyset page and bounded
  occurrence batch per tick, paced between pages. Calendar wall-clock planning,
  database-time eligibility, owner locks, receipts and watermark/job atomicity
  remain in the real SQL owner. Ambiguous commits stop the loop, not an automatic
  replay. Concurrent deactivation/publication mismatch is an observable skip.
- `mysqlexecution.Publication` performs full committed-rule/model/type validation
  before write locks, then reauthorizes and rechecks the parent and sorted owner
  registry gates on commit. Trigger, aggregate and intermediate related-field
  tables are included. No model-provider/network call runs under owner locks.
- `mysqlexecution.DispatchWorker` binds callback/workflow/screening/scoring/outbox
  work to actual scoped owners and the shared supervisor/poller. It requires
  `ports.ReceiptDispatcher`; legacy send-only HTTP clients do not satisfy the
  contract and are not silently wrapped. Send intent commits before IO. A
  resumed intent reconciles first; only definitive absence permits another
  idempotent send of the same key/body. Unknown/timeout outcomes remain unknown.
  Definitive acceptance/rejection, the owner outcome and queue acknowledgement
  commit together. Callback rejection reserves a bounded domain retry atomically.
  Cancellation cannot undo an external action already accepted.
- The optional **149-step** `metadata.DispatchStatements/DispatchPlan` appends
  four steps to the unchanged 145-step cancellation lineage. The decision-owned
  intent table has tenant-consistent receipt FKs, immutable identities/body/hash,
  permanent terminal receipts and no cleanup grant. Bodies are stored as exact
  bounded JSON wire bytes in a binary column: MySQL JSON-number reformatting must
  not invalidate the SHA-256 receipt binding. This intentionally increases
  retained storage; no intent-body archival/expiry policy is activated here.
- `mysql.RuntimeReadiness` is a cold deployment prerequisite, not a hot health
  probe or a cached readiness certificate. It requires reviewed exact migration
  definitions, all real owner-health contracts, matching runtime/inspection
  database+writer identity, actual pool maxima and the explicit shared connection
  inventory. The inspection pool/role is separate so API roles need no installer
  privileges. The inventory cannot discover pools/replicas omitted by operators.

Pinned disposable MySQL 8.4.8/PostgreSQL 16 correctness fixtures cover SQL
effect/browser comparisons, callback crash/rescue, cancellation/finish races,
rollback, missing providers, revoked/path tenant authority and restricted worker
grants. Foreign-owner test routines are fixtures, not provider implementations.
Full public backend selection/routes, production identity/model/case/screening/
dispatch provider conformance, frozen release/grants rehearsal and activation
remain open. Performance/load/query-plan acceptance is deferred; no production
or EC2 database was changed.

Cancellation, retention, locked-page/rollback and seven-kind SQL tests are
written but await the Docker window. Native compilation/vet/guard evidence is
separate in the checklist and does not establish MySQL locking/DDL correctness.
The lock-read syntax follows the [MySQL 8.4 locking-read contract](https://dev.mysql.com/doc/refman/8.4/en/innodb-locking-reads.html);
target-version SQL execution and representative maintenance plans remain open.

### Inactive public API and application composition (2026-10-30)

This checkpoint supersedes earlier notes that collection and recurring-admin
adapters were missing. It does **not** activate MySQL or supply missing owners.

`internal/mysqlapi` now provides scheduled/async collection and status-summary
routes alongside the existing create/detail/wait/cancel/redrive routes. Collection
queries accept `status`, `min_attempt_count`, `limit` and `cursor`. The default
limit is 100 (clamped to the configured row budget), with a maximum of 500.
Unknown/duplicate/malformed query parameters are rejected. Cursors bind the
tenant, scenario, filters and immutable descending time/UUID ordering. They are
not authorization or a cross-request snapshot: permission is rechecked on every
page, and mutable status filters may change between pages. Collection rows omit
request/result bodies, callback URLs and error text; individual detail reads
retain those separate bounded projections. Status summaries include cancelled
owners. These queries use the existing history ordering; filtered-query cost
and representative plans remain performance-acceptance work, not a measured
optimization claim.

Recurring administration uses
`/v1/tenants/:tenantId/scenarios/:scenarioId/recurring-schedule`:

- `GET` returns the active definition, or `recurring_schedule: null`.
- `GET /:scheduleId` reads a retained definition, including disabled ones.
- `PUT` creates/replaces a calendar definition. Supply a new `schedule_id`, the
  observed `expected_schedule_id` (zero UUID for initial absence), an explicit
  `expected_watermark_version`, `calendar`, `start_local`, `missed_policy`,
  `lateness_ms` and a positive bounded `candidate_limit`. Retry limits are server
  policy, not caller-controlled. Calendar v1 retains the approved month-end
  clamp, DST-gap skip and first-overlap policies.
- `DELETE` requires the exact active schedule ID and observed watermark version.

Replacement validates the published model/full formulas before the write
transaction, then rechecks current authority, live binding and registry gates.
Old deactivation and new creation are atomic. Lock order agrees with occurrence
materialization: schedule, live link, then registry. A concurrent tick can make
the supplied watermark stale and yield a conflict. Disabling a definition stops
future materialization; it does not cancel executions already queued. A lost
commit acknowledgement must be reconciled through the original schedule ID,
not blindly repeated with a new identity. Definitions/history are not deleted.

`internal/mysqlruntime` builds the actual same-pool/database/tenant read stores,
queued effects, execution unit of work, evaluator, publication, admission,
execution HTTP endpoints and optional trusted browser consumer. It requires
explicit policies, real request authentication, separate user/service authority
providers, a fresh read-only `TenantModelReader`, all five receipt dispatchers
and exact pool-bound readiness. The legacy model HTTP client does not invent
MySQL publication tokens. Missing browser session authority returns an
unavailable capability rather than silently granting access.

`Runtime.Start` checks the cold 149-step lineage and owner readiness before
returning a handler. It builds seven delivery loops, seven bounded maintenance
loops and one calendar scheduler per explicitly configured tenant, within a
total slot budget. HTTP tenant scope is restricted to that deployment inventory;
no queue payload grants service authority. `RunWorkers` is explicit and single
use: failure cancels and joins siblings and withdraws readiness. `Shutdown`
withdraws admission first and waits within the caller's deadline. Pools and the
HTTP listener remain caller-owned; `HTTPServer` configures bounded socket-level
header/body/write/idle deadlines without listening. Hot readiness checks real
owner availability and required queue columns/access, not the full installer
DDL/trigger shape on every HTTP request.

Receipt-aware maintenance refuses terminal exhaustion when a send intent exists
without committed owner/receipt acknowledgement. A lost external response is
not proof of rejection; it requires outcome reconciliation. Retriable transport
work still resumes with the original key/body. Permanent identity receipts and
send intents remain retained; this pass adds no archival policy.

`DATABASE_BACKEND` defaults to `postgres`. Selecting `mysql` fails explicitly
before PostgreSQL startup; the deployed commands do not enable the target
composition yet. Real auth/session/revocation, service identities, complete
published-model delivery, case permissions, screening source publication and
receipt-provider conformance remain dependencies. The user deferred additional
auth action adapters to the auth integration pass. Existing send-only HTTP
clients are not treated as receipt-aware providers. No cross-owner DML, EC2,
Aurora, application-database changes or performance acceptance is included.

### Decision-owned release verification and rehearsal (2026-10-30)

`internal/mysqlrelease` and the separate `/app/mysql-release` executable prepare
the decision release boundary without activating a mixed-backend stack. The
legacy `/app/migrate` still handles PostgreSQL; do not point it at MySQL.
`DATABASE_BACKEND=mysql` still fails closed in API/worker startup. This checkpoint
does not implement any foreign-owner provider or the deferred auth adapters.

The release artifact contains format `1`, the exact shared database and locked
localhost definer, MySQL `8.4.8`, a full Git revision, approved source-tree SHA-256,
immutable image digest and all 149 migration steps with frozen canonical
before/after definitions. Its encoding is compact `encoding/json` output plus
one LF. Verification rejects duplicate/unknown fields, oversized/noncanonical
JSON, altered SQL, incomplete prefixes, broken definition chains and identity
mismatches. SQL must equal the compiled decision source, not arbitrary artifact
SQL. A SHA-256 binds bytes; it does not authenticate the publisher or approve a
release. The approved checksum/source/image values must come from the reviewed
release channel, independently of the file being checked.

Production definitions must be captured/reviewed on an independent, disposable
reference server with the same exact schema/definer/version, then frozen and
promoted with the image. There is deliberately no command that learns existing
deployment-target definitions and calls them approved. No production artifact
has been frozen/promoted in this pass. Rehearsal definitions are private fixture
observations, NOT production release artifacts. A clean reviewed source checkout
and an external comparison of the actual deployed image digest/OCI labels are
still required; passing a claimed manifest identity is not image attestation.

The decision Dockerfile now includes its local `../mysqlstore` dependency before
module download. Build inputs `SOURCE_REVISION` and `SOURCE_TREE_SHA256` become
OCI labels and are embedded into the release verifier. An unstamped or
mismatched verifier can perform offline artifact checks, but cannot verify a
deployment database. Image digest comparison remains an operator/pipeline gate.
Example build for a later approved release, from the repository root:

```sh
docker build -f backend/decision-engine-service/Dockerfile \
  --build-arg SOURCE_REVISION="$APPROVED_REVISION" \
  --build-arg SOURCE_TREE_SHA256="$APPROVED_SOURCE_SHA256" \
  -t "$REVIEWED_CANDIDATE_TAG" backend
```

Do not use the current dirty working tree as an approved release checkout.
The complete production image build/promotion is not established by the local
Go compilation or component SQL tests.

The verifier accepts these common arguments:

```text
--artifact /release/decision.json --sha256 <approved-artifact-sha256>
--database <shared-database> --definer <locked-decision-definer>
--mysql-version 8.4.8 --revision <approved-40-hex-revision>
--source-sha256 <approved-source-digest> --image sha256:<approved-image-digest>
```

- `--mode artifact` verifies offline; no database is accessed.
- `--mode grants --role api|worker --runtime-user <dedicated-account>
  --runtime-host <literal-host>` prints decision-owned DML grants, without
  applying them, creating accounts, granting foreign access or altering roles.
- `--mode verify --address <writer-host:port> --server-uuid <verified-writer-uuid>
  --user <separate-inspector> --password-env DECISION_INSTALLER_PASSWORD
  --ca <trusted-ca.pem>` verifies TLS, exact version/database/writer, writer
  status, locked definer, complete checksummed lineage and full object shapes.
  The target check is repeated on the actual pinned verification connection.
  This operation does not create a ledger or repair missing/dirty objects.

There is **no production `apply`, `force`, `down` or `activate` operation**.
The shared runner supports pinned DDL locking/recovery, but has no caller target
guard immediately before writes on that same session. A pooled diagnostic
preflight alone is not sufficient: reconnect/failover could change the target
between checking and applying. A shared-installer owner must provide/rehearse
that guard before production writes are exposed. This pass does not modify that
shared component or substitute a copied migration runner.

Owner grants enumerate every decision table and fail if a new table lacks an
explicit classification. There are no schema wildcards, DDL, grant options,
runtime dictionary access or resolver execution. API cannot delete delivery
copies or insert send intents; workers can clean delivery copies but cannot
delete decisions, identity receipts or send intents. Required `FOR UPDATE`
receipt fences use `UPDATE(id)` on immutable receipt keys; API intent fences use
`UPDATE(receipt_id)`. Owner guards still reject changes. These narrow privileges
follow [MySQL locking-read requirements](https://dev.mysql.com/doc/refman/8.4/en/innodb-locking-reads.html).
Table grants are not tenant authorization; trusted caller-transaction checks
remain mandatory. Existing accounts/inherited roles need independent grant
review; printing grants does not revoke any pre-existing privilege.

Cold inspection is a separate deployment policy gate. Full routine-definition
visibility requires a definer or broader inspection privilege; ordinary
object-level `EXECUTE` does not expose the routine body. Do not grant global
`SELECT`, unlocked definer access or installer credentials to runtime accounts
to make readiness pass. The [SHOW CREATE PROCEDURE contract](https://dev.mysql.com/doc/refman/8.4/en/show-create-procedure.html)
and [routine privilege documentation](https://dev.mysql.com/doc/refman/8.4/en/stored-routines-privileges.html)
explain this constraint. A production inspection-account policy remains open;
this pass does not claim a fully least-privileged deployed inspector.

Disposable rehearsal covers fresh install, unchanged 140→145→149 upgrade,
dirty-before-DDL and dirty-after-DDL recovery, repeat apply, missing schema,
wrong writer, unlocked definer, tampered checksums and drift refusal. Separate
foreign-owner table and migration sentinels must remain unchanged. Real decision
service evaluation is exercised under the explicit API grant plan, including
threshold outcomes/evidence, tenant denial and required-job failure rollback.
Worker grants are exercised through real claim/intent/receipt/ack SQL plus
forbidden dictionary/foreign-owner/DDL/history-mutation negatives. No external
provider is contacted, and this is not complete provider or production-grant
acceptance.

Controlled activation remains a coordinated deployment gate:

1. Obtain the approved target/window, clean source and immutable image/artifact;
   review all API/worker/inspection/installer and foreign contract grants.
2. Require real auth/model/case/screening and receipt-aware providers, the shared
   pre-write target guard and an approved complete connection/worker budget.
3. Rehearse backup/restore and source-writer fencing, including all outstanding
   executions, cancellations and unknown external outcomes. Do not duplicate
   deliveries or silently omit unresolved intents.
4. Install/verify through the approved shared installer before accepting traffic.
   Start with mutation/delivery admission disabled; verify actual images, target,
   readiness, authorization negatives, bounded evaluation and outcome/queue
   reconciliation before enabling the single authoritative writer set.
5. Stop admission and claimers on mismatch or owner/readiness loss. Before target
   writes, a fenced return to source can be considered. After target writes or
   external effects, do not flip URLs: reconcile accepted operations and receipts
   through the approved recovery plan or roll forward.

No EC2, Aurora, application database, live container, configured backend or
runtime activation has been changed by this work. Performance/load acceptance
remains explicitly deferred. Full deployment and controlled activation cannot
be reported complete from these decision-only fixtures.
