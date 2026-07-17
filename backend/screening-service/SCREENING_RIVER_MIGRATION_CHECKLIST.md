# Screening Service River Migration Checklist

## Objective

Move `screening-service` worker queues from the polling loop to River while keeping service tables as the caller-visible source of truth.

## Planned rollout

1. `screening_dispatch`
2. `dataset_update_jobs`
3. `continuous_screening` monitored-object work

## Current status

- `screening_dispatch` is now River-backed
- `dataset_update_jobs` is now River-backed
- `continuous_screening` monitored-object work is now River-backed
- the active queue paths are no longer owned by the polling loop

## Checklist

### 1. Screening dispatch

- [x] Add River dependencies and bootstrap
- [x] Define a River payload for one screening dispatch job
- [x] Enqueue River work when a screening is created
- [x] Enqueue River work when a failed screening is retried
- [x] Add a targeted `RunScreening(...)` service entrypoint
- [x] Remove screening dispatch from the active polling loop

### 2. Dataset update jobs

- [x] Define a River payload for one dataset update job
- [x] Enqueue River work when a dataset update job is created
- [x] Enqueue River work when a failed dataset update job is retried
- [x] Add a targeted `RunJob(...)` entrypoint
- [x] Remove dataset update processing from the active polling loop

### 3. Continuous screening

- [x] Define a River payload for one monitored object
- [x] Enqueue River work when a monitored object is created
- [x] Enqueue River work when a monitored object is requeued
- [x] Keep the current monitored-object status model while River owns retries
- [x] Add a targeted `RunMonitoredObject(...)` entrypoint
- [x] Remove monitored-object processing from the active polling loop

### 4. Verification

- [x] Package-level `go test ./internal/... ./cmd/...` passes
- [ ] DB-backed worker verification passes
- [ ] Worker docs reflect the River-backed model
