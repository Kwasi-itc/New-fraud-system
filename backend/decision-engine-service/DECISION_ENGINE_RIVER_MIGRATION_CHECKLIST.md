# Decision Engine River Migration Checklist

## Objective

Move `decision-engine-service` execution queueing from the custom polling worker to River while keeping the existing execution tables as the caller-visible source of truth.

## Scope

This migration covers:

- async decision execution enqueue
- scheduled execution enqueue
- execution retry handling
- recurring schedule materialization enqueue
- worker bootstrap for execution queues
- workflow dispatch enqueue
- screening dispatch enqueue
- scoring dispatch enqueue
- outbox publish enqueue

This migration does **not** yet change:

- public HTTP endpoints
- `core.async_decision_executions` as the visible async execution record
- `core.scheduled_executions` as the visible scheduled execution record

## Current status

- River job contracts and workers have been added for:
  - async decision executions
  - scheduled executions
  - workflow dispatch
  - screening dispatch
  - scoring dispatch
  - outbox publish
- execution creation now enqueues River work
- execution retry now re-enqueues River work
- recurring schedule materialization now enqueues River work
- decision creation now enqueues River work for workflow, screening, scoring, and outbox follow-up records
- screening and scoring retry flows now re-enqueue River work
- execution lifecycle outbox events now enqueue River work
- `cmd/worker` now starts River workers for execution and dispatch queues
- the legacy poll runner no longer owns active queue execution

## Checklist

### 1. Dependencies and bootstrap

- [x] Add River dependencies to `go.mod`
- [x] Add River pgx v5 driver wiring
- [x] Add River bootstrap to `cmd/worker`
- [x] Start River workers for execution and dispatch queues

### 2. Job contracts

- [x] Define a minimal River payload for async execution:
  - `execution_id`
- [x] Define a minimal River payload for scheduled execution:
  - `execution_id`
- [x] Define River payloads for workflow, screening, scoring, and outbox jobs:
  - `tenant_id`
  - record id
- [x] Keep execution tables as the source of truth
- [x] Load the full execution record at run time

### 3. Worker implementation

- [x] Add a River worker for async decision executions
- [x] Add a River worker for scheduled executions
- [x] Add River workers for workflow dispatch, screening dispatch, scoring dispatch, and outbox publish
- [x] Add service entrypoints to run one execution by id
- [x] Add service entrypoints to run one workflow execution, screening execution, scoring request, and outbox event by id
- [x] Move active execution state transitions to targeted `StartAttempt(...)` repository methods

### 4. Retry and failure behavior

- [x] Re-enqueue retryable async execution failures through River
- [x] Re-enqueue retryable scheduled execution failures through River
- [x] Re-enqueue screening retries through River
- [x] Re-enqueue scoring retries through River
- [x] Keep terminal `failed` visibility in execution tables
- [x] Keep `completed` visibility in execution tables
- [x] Keep lifecycle outbox events for queued, completed, failed, and retry-scheduled transitions

### 5. Enqueue path

- [x] Enqueue River work when an async decision execution is created
- [x] Enqueue River work when a scheduled execution is created
- [x] Enqueue River work when a scheduled recurring run is materialized
- [x] Re-enqueue River work on manual retry
- [x] Enqueue River work when workflow executions are created
- [x] Enqueue River work when screening executions are created
- [x] Enqueue River work when scoring requests are created
- [x] Enqueue River work when outbox events are created
- [x] Make enqueue transactional when a DB transaction is available
- [x] Keep a safe no-op enqueuer path for nil-db and direct-test construction

### 6. Repository and service cleanup

- [x] Add `StartAttempt(...)` for async decision executions
- [x] Add `StartAttempt(...)` for scheduled executions
- [x] Remove execution queues from the active poll-loop worker path
- [x] Remove now-obsolete `ClaimQueued(...)` and `ClaimDue(...)` methods after the codebase no longer references them anywhere
- [x] Remove workflow, screening, scoring, and outbox queues from the active poll-loop worker path

### 7. Config

- [x] Add River queue config:
  - `ASYNC_EXECUTION_QUEUE_NAME`
  - `ASYNC_EXECUTION_QUEUE_WORKERS`
  - `SCHEDULED_EXECUTION_QUEUE_NAME`
  - `SCHEDULED_EXECUTION_QUEUE_WORKERS`
  - `WORKFLOW_DISPATCH_QUEUE_NAME`
  - `WORKFLOW_DISPATCH_QUEUE_WORKERS`
  - `SCREENING_DISPATCH_QUEUE_NAME`
  - `SCREENING_DISPATCH_QUEUE_WORKERS`
  - `SCORING_DISPATCH_QUEUE_NAME`
  - `SCORING_DISPATCH_QUEUE_WORKERS`
  - `OUTBOX_QUEUE_NAME`
  - `OUTBOX_QUEUE_WORKERS`
- [x] Keep domain retry config:
  - `ASYNC_EXECUTION_MAX_ATTEMPTS`
  - `ASYNC_EXECUTION_RETRY_BACKOFF`
  - `SCHEDULED_EXECUTION_MAX_ATTEMPTS`
  - `SCHEDULED_EXECUTION_RETRY_BACKOFF`
- [x] Remove execution-specific reliance on `WORKER_POLL_INTERVAL`

### 8. Testing

- [x] `go test ./internal/... ./cmd/...` passes with a repo-local `GOCACHE`
- [x] Update service/test doubles to support the River-backed execution path
- [ ] Run DB-backed integration checks for real River-backed execution processing against local Postgres

### 9. Docs cleanup

- [x] Update README to describe River-backed execution queues
- [x] Update operating notes to describe the mixed River plus legacy worker model
- [x] Update execution docs so scheduled and async execution flows describe River workers

### 10. Remaining worker migration

- [x] Move `workflow_dispatch` to River
- [x] Move `screening_dispatch` to River
- [x] Move `scoring_dispatch` to River
- [x] Move `outbox` to River
- [x] Remove the legacy poll runner completely from active queue ownership

## Exit criteria

- async decision execution creation enqueues River-backed work
- scheduled execution creation enqueues River-backed work
- recurring schedule materialization enqueues River-backed work
- retryable execution failures re-enqueue through River
- workflow, screening, scoring, and outbox follow-up work enqueue through River
- `async_decision_executions` remains the async status source
- `scheduled_executions` remains the scheduled-run status source
- package-level tests pass
