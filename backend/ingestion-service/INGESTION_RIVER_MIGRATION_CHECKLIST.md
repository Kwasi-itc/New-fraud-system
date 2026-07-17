# Ingestion Service River Migration Checklist

## Objective

Move `ingestion-service` CSV upload execution from the current polling worker model to River while keeping `upload_logs` as the caller-visible operational record.

## Scope

This migration covers:

- CSV upload enqueue
- CSV upload execution
- retry handling
- worker bootstrap

This migration does **not** change:

- public HTTP endpoints
- `core_ingestion.upload_logs` as the visible job record
- synchronous single-record and batch ingest flows

## Current status

- `upload_logs` still acts as the durable record for async CSV work
- River job contract and worker have been added
- CSV upload creation now enqueues River work
- retry re-enqueues River work instead of relying on a polling loop
- `cmd/worker` now starts a River worker
- package-level tests are passing

## Checklist

### 1. Dependencies and bootstrap

- [x] Add River dependencies to `go.mod`
- [x] Add River pgx v5 driver wiring
- [x] Add a River job contract for upload-log execution
- [x] Replace the polling worker loop in `cmd/worker`

### 2. Job contract

- [x] Define a minimal River payload with:
  - `upload_log_id`
- [x] Keep `upload_logs` as the source of truth
- [x] Load the full upload log record at execution time

### 3. Worker implementation

- [x] Add a River worker that executes one upload log by id
- [x] Keep CSV decode, batch ingest, and status transitions in the existing service flow
- [x] Keep terminal `failed` state in `upload_logs`
- [x] Keep `completed` state and row counters in `upload_logs`

### 4. Retry and failure behavior

- [x] Re-enqueue retryable failures through River
- [x] Keep attempt counting in `upload_logs`
- [x] Keep terminal failure visibility in `upload_logs`
- [x] Stop relying on `ClaimNextUploaded` polling semantics

### 5. Enqueue path

- [x] Enqueue River work when a CSV upload log is created
- [x] Make enqueue transactional when running with a DB-backed transaction manager
- [x] Keep a no-op enqueuer path for tests and nil-db router bootstrap

### 6. Repository and service cleanup

- [x] Replace `ClaimNextUploaded` with targeted execution by upload log id
- [x] Add `StartAttempt(...)` repository method for River execution
- [x] Add `RunLog(...)` service entrypoint for one upload log id
- [x] Remove the active polling-only worker flow from `cmd/worker`

### 7. Config

- [x] Add River queue config:
  - `UPLOAD_LOG_QUEUE_NAME`
  - `UPLOAD_LOG_QUEUE_WORKERS`
- [x] Keep `WORKER_MAX_ATTEMPTS`
- [ ] Remove now-obsolete `WORKER_POLL_INTERVAL` config and references where still only used by old docs

### 8. Testing

- [x] Package-level `go test ./internal/... ./cmd/...` passes
- [x] Update integration tests to execute one upload log by id instead of polling
- [x] Run DB-backed ingestion integration tests against the shared local Postgres on `5434`

### 9. Docs cleanup

- [ ] Update README to stop describing the polling worker as the active model
- [ ] Update setup/run guide to describe the River worker
- [ ] Update CSV worker docs to reflect River-backed execution

## Exit criteria

- CSV upload creation enqueues River-backed work
- River worker processes one upload log by id
- retryable failures re-enqueue through River
- `upload_logs` remains the only client-facing async status source
- package-level tests pass
- DB-backed integration tests pass
