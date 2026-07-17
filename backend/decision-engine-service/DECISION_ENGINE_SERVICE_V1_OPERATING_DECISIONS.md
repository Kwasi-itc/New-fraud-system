# Decision Engine Service V1 Operating Decisions

This document records the current V1 operating decisions for the standalone `decision-engine-service`.

It is intentionally narrower than the broader design documents. The goal is to remove ambiguity around the choices the current implementation is already making.

## 1. Worker model

V1 now uses a mixed worker model:

- River for execution queues
  - scheduled executions
  - async decision executions
- the legacy worker runner for dispatch-style tasks
  - workflow dispatch
  - screening dispatch
  - scoring dispatch
  - outbox publishing

The legacy runner still supports two operating modes:

- `batch`
  - run one processing cycle and exit
  - suitable for cron, scheduled jobs, or ad hoc maintenance runs
- `poll`
  - run one processing cycle immediately, then continue polling on an interval
  - suitable for long-running service deployments

Current configuration knobs:

- `WORKER_MODE`
  - allowed values: `batch`, `poll`
  - default: `batch`
- `WORKER_TASKS`
  - comma-separated worker task selection
  - supported values: `scheduled`, `async`, `workflow_dispatch`, `screening_dispatch`, `scoring_dispatch`, `outbox`
  - default: all supported worker tasks enabled
- `WORKER_TASK_PRIORITIES`
  - comma-separated `task:priority` entries
  - lower numeric value means higher processing priority
  - tasks not overridden use built-in defaults
- `WORKER_POLL_INTERVAL`
  - default: `15s`
- `WORKER_BATCH_LIMIT`
  - default: `100`
- `SCHEDULED_EXECUTION_QUEUE_NAME`
  - default: `scheduled_executions`
- `SCHEDULED_EXECUTION_QUEUE_WORKERS`
  - default: `4`
- `SCHEDULED_EXECUTION_MAX_ATTEMPTS`
  - default: `3`
- `SCHEDULED_EXECUTION_RETRY_BACKOFF`
  - default: `30s`
- `ASYNC_EXECUTION_QUEUE_NAME`
  - default: `async_decision_executions`
- `ASYNC_EXECUTION_QUEUE_WORKERS`
  - default: `4`
- `ASYNC_EXECUTION_MAX_ATTEMPTS`
  - default: `3`
- `ASYNC_EXECUTION_RETRY_BACKOFF`
  - default: `30s`
- `LIVE_DECISION_CONCURRENCY_LIMIT`
  - default: `0`
  - `0` disables the live-path concurrency gate
  - values greater than `0` reject excess realtime decision requests with `429`
- `LIVE_ASYNC_FALLBACK_ENABLED`
  - default: `false`
  - when enabled, overloaded realtime decision requests are deferred into async execution instead of being rejected

### Current worker responsibilities

Each legacy worker cycle processes:

- pending workflow executions
- pending screening executions
- pending scoring requests
- pending outbox events

Execution queues are no longer claimed by the legacy poll runner.
They are processed by River workers using the stored execution ids.

### Current worker topology and ownership

The worker task groups and their owned responsibility are:

- `scheduled`
  - preserved as a logical task name for execution ownership, but runtime execution now happens through River
- `async`
  - preserved as a logical task name for execution ownership, but runtime execution now happens through River
- `workflow_dispatch`
  - dispatches persisted workflow execution intent
- `screening_dispatch`
  - dispatches persisted screening executions to providers
- `scoring_dispatch`
  - dispatches persisted scoring requests to providers
- `outbox`
  - publishes persisted outbox events

Deployments can still split the legacy dispatch tasks by `WORKER_TASKS`, but scheduled and async execution processing is now owned by River worker queues.

### Current worker runtime visibility

Each worker cycle now emits per-task runtime state in logs, including:

- run count
- failure count
- last duration
- last success time
- last failure time
- last error

### Current worker task priority model

Default legacy task priority order is:

1. `async`
2. `scheduled`
3. `workflow_dispatch`
4. `screening_dispatch`
5. `scoring_dispatch`
6. `outbox`

This order can be overridden with `WORKER_TASK_PRIORITIES`.

### Current execution retry behavior

- scheduled executions and async decision executions increment `attempt_count` when a River worker starts an attempt
- failed executions are automatically retried with exponential backoff based on the configured base delay
- once `max_attempts` is reached, execution status becomes `failed` and `failed_at` is recorded
- retryable executions are re-enqueued with a future run time based on `next_attempt_at`

### Current execution deduplication behavior

- scheduled and async decision execution creation accepts an optional `idempotency_key`
- when the same tenant submits the same `idempotency_key` again, the service returns the existing execution row instead of creating a duplicate
- recurring scheduled executions continue to preserve uniqueness by schedule time and source

### Current execution lifecycle events

Deferred execution state changes now emit outbox events:

- `scheduled_execution.queued`
- `scheduled_execution.retry_scheduled`
- `scheduled_execution.completed`
- `scheduled_execution.failed`
- `async_decision_execution.queued`
- `async_decision_execution.retry_scheduled`
- `async_decision_execution.completed`
- `async_decision_execution.failed`

## 1.1 Realtime overload protection

The synchronous decision endpoints can now apply an explicit live concurrency cap.

Current behavior:

- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/evaluate`
- `POST /v1/tenants/:tenantId/decisions`
- `POST /v1/tenants/:tenantId/decisions/all`
- `POST /v1/tenants/:tenantId/ingestion-events/record-ingested`

All share the same in-process non-blocking gate when `LIVE_DECISION_CONCURRENCY_LIMIT` is configured.

If the limit is reached:

- when `LIVE_ASYNC_FALLBACK_ENABLED=false`, the request is rejected immediately with `429`
- when `LIVE_ASYNC_FALLBACK_ENABLED=true`, the request is accepted as an async decision execution and returns `202`

### Current sync vs deferred workload split

- synchronous by default:
  - direct single-scenario evaluation
  - direct all-live-scenarios evaluation
  - ingestion-triggered all-live-scenarios evaluation
- deferred by explicit API:
  - async decision executions
  - scheduled executions
- deferred under overload when async fallback is enabled:
  - single-scenario decision creation and evaluation
  - all-live-scenarios decision creation
  - ingestion-triggered fan-out evaluation

### Current synchronous side effects review

The live decision path still does these synchronously:

- scenario lookup and live-iteration lookup
- rule evaluation
- decision persistence
- rule execution persistence
- creation of workflow execution records
- creation of screening execution records
- creation of scoring request records
- creation of deferred execution lifecycle outbox events

The live decision path does not synchronously do these side effects:

- workflow external dispatch
- screening provider dispatch
- scoring provider dispatch
- outbox publication delivery

### Caching and queueing stance

- in-memory caching is used for hot evaluation metadata and repeated reads
- async execution and scheduled work use River-backed jobs
- dispatch uses the legacy worker/job path
- Redis is not the default execution queue for decision processing
- Redis should only be introduced where a measured hot-path cache benefit is clear

### Cache invalidation rules

- scenario and iteration metadata caches are short-lived and TTL-based
- publishing or changing live scenario state relies on the short TTL window rather than explicit distributed cache busting
- tenant model caching is also TTL-based and assumes model changes must tolerate that short propagation window
- a stronger distributed invalidation mechanism is deferred until a measured consistency need justifies the extra complexity

### Webhook and screening-enrichment scope

- decision-engine-service does not currently own generic webhook dispatch or delivery as a first-class runtime path
- workflow side effects are dispatched through the workflow-action integration path
- explicit webhook delivery infrastructure remains outside the current service boundary
- rich screening enrichment is not a current live-path runtime feature; provider-side orchestration remains the owning boundary

### Payload offloading stance

- V1 does not enable payload offloading for execution, rule-evaluation, screening, or scoring payloads by default
- offloading should only be introduced after measured row-size, storage-growth, or query-cost pressure
- the current design keeps this as a deliberate deferred capability, not an accidental omission

### Benchmark rollout process

Before enabling a new optimization class broadly:

1. run focused package tests for the touched path
2. run the existing stress-test or benchmark harness for that path in isolation
3. compare baseline latency and throughput against the pre-change numbers
4. only keep the optimization enabled by default if it improves the measured workload without causing correctness regressions

### V1 decision

V1 keeps both worker modes instead of forcing one deployment shape. This preserves compatibility with both:

- a long-running standalone service deployment
- a scheduled-runner or cron-style deployment

## 2. Workflow and case side effects

The decision engine does not own case management in V1.

Instead, it owns:

- workflow definitions
- workflow execution records
- dispatch state transitions

The actual side effect should be executed through an external workflow-action or case-management endpoint.

### Current dispatch contract

The workflow dispatcher currently sends:

- `workflow_execution_id`
- `tenant_id`
- `workflow_id`
- `workflow_rule_id`
- `workflow_action_id`
- `decision_id`
- `scenario_id`
- `action_type`
- `action_config`
- `created_at`

### V1 decision

Workflow side effects remain cross-service in V1.

That means:

- this service persists intent and dispatch state
- a downstream workflow-action or case-management service performs the actual case mutation
- retries and dispatch failures remain visible on workflow execution records

This keeps case-management ownership out of the decision engine boundary while preserving Marble-inspired workflow intent.

## 3. Helper data ownership

The current standalone service already owns helper persistence for:

- custom list entries
- record tags
- risk snapshots
- IP flags

### V1 decision

These helper datasets are treated as service-owned V1 dependencies, not temporary scaffolding.

That means:

- evaluator helper functions continue reading them locally
- their APIs remain in the decision engine service for V1
- they should be treated as part of the supported standalone decisioning boundary unless a later extraction is deliberate and planned

## 4. Screening and scoring ownership

The current service already owns:

- screening config authoring
- screening execution records
- scoring config authoring
- scoring request records
- dispatch and status progression
- inspection of one screening execution or scoring request
- explicit status updates for one screening execution or scoring request
- retry of one screening execution or scoring request by resetting it to `pending`

It does not own provider execution itself. Provider interaction is still adapter-based.

### V1 decision

Screening and scoring remain hybrid concerns in V1:

- orchestration state is owned by `decision-engine-service`
- provider execution remains external

This means the service is the source of truth for decision-time orchestration state, while provider contracts remain replaceable integrations.

### Current limitation

The service still does not ingest rich provider result payloads in V1.

That means:

- providers can drive lifecycle state back into the service
- providers cannot yet submit a first-class persisted result document through the current API

## 5. Rule-engine semantic baseline

The standalone rule engine currently treats the following as the baseline semantics to preserve:

- `past_decision_count`
  - counts internal decision-history records for the current object
  - optional `outcome` narrows the count
- `related_count`
  - reads records from `TenantDataReader`
  - requires `object_type` and `field`
  - without `equals`, counts non-`nil` values for the given field
  - with `equals`, counts records whose field value equals the evaluated right-hand value
- `related_field`
  - follows `links_to_single`
  - returns `nil` if the lookup value is missing or no related record is found
  - errors when the link path or target field is invalid against the tenant model

### V1 decision

These semantics are the current contract of the standalone runtime unless an explicit compatibility change is made.

Future Marble-alignment work should be treated as a conscious contract change, not as an implicit refactor.

## 6. Aggregate pushdown rollout and scope

The standalone service now supports aggregate pushdown for Marble `Aggregator(...)` functions through `ingestion-service`.

### V1 decision

Aggregate pushdown is staged rather than forced as an unconditional replacement for local aggregation.

Current control knobs:

- `AGGREGATE_PUSHDOWN_MODE`
  - `enabled`
  - `disabled`
  - `strict`
- `AGGREGATE_PUSHDOWN_AGGREGATES`
  - comma-separated remote aggregate allow-list
  - current default: `count`

This means:

- V1 can keep `count` as the first remote aggregate in production
- wider remote aggregate rollout can be enabled explicitly without changing rule authoring
- unsupported or disabled aggregate shapes still have a compatibility path when mode is not `strict`

### Current supported remote scope

- aggregates:
  - `count`
  - `count_distinct`
  - `sum`
  - `avg`
  - `min`
  - `max`
- filter grouping:
  - `List` as AND
  - nested `and`
  - nested `or`
  - nested `not`
- filter operators:
  - `eq`
  - `neq`
  - `gt`
  - `gte`
  - `lt`
  - `lte`
  - `in`
- runtime value resolution before dispatch:
  - payload-derived values
  - resolved time expressions

### Explicit V1 deferrals

The following are explicitly deferred from remote pushdown in V1:

- `is_empty`
- fuzzy matching
- decision-history joins
- custom-list joins
- tag and risk helper joins
- broad relationship join traversal
- arbitrary SQL-like expression pushdown

These are deferred by contract, not by accident. A later change to support them should be treated as an explicit capability expansion.
