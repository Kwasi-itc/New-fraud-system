# Data Model Service River Migration Checklist

## Objective

Move `data-model-service` index-job execution from the old polling worker model to River while keeping `index_jobs` as the caller-visible job record.

## Current status

- River is the active execution path for index jobs
- `index_jobs` remains the source of truth
- create and retry flows enqueue River work transactionally
- the worker runs targeted execution by `index_job_id`
- the old polling claim path is no longer the active model

## Checklist

### 1. Dependencies and bootstrap

- [x] Add River dependencies to `go.mod`
- [x] Add River pgx v5 driver wiring
- [x] Bootstrap River in `cmd/worker/main.go`
- [x] Keep worker startup in the existing worker entrypoint

### 2. Job contract

- [x] Define a minimal River payload with:
  - `index_job_id`
- [x] Keep `index_jobs` as the source of truth
- [x] Load the full job record at execution time

### 3. Worker implementation

- [x] Add a River worker that executes one index job by id
- [x] Preserve existing managed-index creation logic
- [x] Preserve no-op behavior when the managed index already exists
- [x] Preserve schema-change audit behavior

### 4. Retry and failure behavior

- [x] Keep `index_jobs` status fields aligned with execution state
- [x] Preserve terminal failure visibility in `index_jobs`
- [x] Preserve retry behavior through the existing API and domain record
- [x] Remove reliance on polling `scheduled_at` semantics for active execution

### 5. Enqueue path

- [x] Enqueue River work when an index job is created
- [x] Enqueue River work when an index job is retried
- [x] Make enqueue transactional when a DB transaction is available
- [x] Keep a no-op enqueuer path for tests and nil-db construction

### 6. Repository and service cleanup

- [x] Stop using `ClaimNext` from the active execution path
- [x] Remove dead polling-specific repository methods
- [x] Remove the old polling runner from active queue ownership

### 7. Config

- [x] Add River queue config:
  - `INDEX_JOB_QUEUE_NAME`
  - `INDEX_JOB_QUEUE_WORKERS`
- [x] Keep domain retry config where it still drives visible behavior
- [x] Remove hardcoded queue settings from the service bootstrap

### 8. Testing

- [x] Add or update unit coverage for the River-backed worker flow
- [x] Keep package-level tests passing
- [x] Run DB-backed integration coverage for the River-backed path

### 9. Cutover

- [x] Remove the polling runner after the River path is stable
- [x] Keep `index_jobs` as the only client-facing status source

## Verification

- [x] `go test -p 1 -run Integration ./internal/service ./internal/store/postgres ./internal/httpapi`
- [x] `go test ./internal/app ./internal/service ./internal/worker ./internal/httpapi ./internal/reconcile ./internal/store/postgres ./cmd/...`

## Exit criteria

- creating an index job enqueues River-backed work
- retrying an index job re-enqueues River-backed work
- `index_jobs` remains the only client-facing status source
- schema-change audit behavior still works
- package and DB-backed integration checks pass
