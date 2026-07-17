# River Queue Migration Plan

## Goal

Replace the service-owned polling queue implementations in `new/backend` with River where it gives us a clear operational win:

- standard job lifecycle handling
- built-in retry behavior
- stronger worker management
- less duplicated queue code across services

This is not a blind one-for-one replacement. Some current flows are closer to a job queue, while others are really state machines with API-visible records. We should keep the caller-facing API contracts stable and change the execution engine underneath.

## Current state

The new backend does **not** use River today.

Current queue-like implementations:

- `decision-engine-service`
  - async decision executions
  - scheduled executions
  - worker poll loop in `cmd/worker/main.go`
- `data-model-service`
  - async index jobs
  - worker loop in `cmd/worker/main.go`
- `ingestion-service`
  - CSV upload processing via upload-log claiming
  - worker loop in `cmd/worker/main.go`
- `screening-service`
  - pending screenings
  - pending monitored-object work
  - dataset update jobs
  - worker loop in `cmd/worker/main.go`

Common current pattern:

- write work state into service tables
- poll with a worker loop
- claim rows directly with SQL
- update status fields manually
- implement retry and backoff in service code

## Recommended target

Use River as the **execution engine**, while keeping service-owned business tables as the **source of truth** for domain state.

That means:

- River job tables handle queueing, claiming, retries, scheduling, and worker concurrency
- service tables still hold business records such as:
  - async decision execution history
  - scheduled execution history
  - index jobs
  - upload logs
  - screenings and dataset jobs
- APIs continue to read and return those service-owned records

This is the important design choice. We should not move everything into raw River jobs and lose domain visibility.

## What should change

### Replace

- direct SQL claim methods like `ClaimQueued`, `ClaimDue`, `ClaimNextUploaded`, `ClaimNext`
- custom poll-loop logic whose main job is queue claiming
- hand-rolled retry scheduling based on `next_attempt_at`

### Keep

- service APIs
- domain tables that users and operators inspect
- idempotency keys where they are part of the product contract
- domain statuses that matter to clients

### Adapt

- workers become River workers
- enqueue actions write domain state and enqueue a River job in the same transaction where needed
- River job handlers update domain rows as work progresses

## Migration principles

1. Do not migrate all services at once.
2. Keep current HTTP contracts unchanged.
3. Keep domain records visible even if River is added.
4. Prefer one River-backed service at a time.
5. Cut over behind a feature flag where practical.

## Best rollout order

### Phase 1: `data-model-service`

Why first:

- smallest and cleanest async surface
- index jobs already look like classic background jobs
- low coordination cost compared with decision execution

Plan:

- introduce River client and worker bootstrap
- enqueue a River job when an index job is created or retried
- keep `index_jobs` table as the visible record
- make River worker load the index-job record by id and execute it
- stop using `ClaimNext` after cutover

Success criteria:

- create/retry/list/get index job APIs behave the same
- worker no longer needs SQL claiming logic
- retries come from River, not custom scheduling code

### Phase 2: `ingestion-service`

Why second:

- CSV processing is a clear async task
- upload logs are already the natural domain record

Plan:

- treat each uploaded CSV as a River job
- keep `upload_logs` as the user-visible processing record
- worker updates upload log state during processing
- remove `ClaimNextUploaded` polling path after cutover

Success criteria:

- CSV uploads still show the same upload-log lifecycle
- retry/failure behavior becomes River-driven

### Phase 3: `screening-service`

Why third:

- more moving parts than ingestion
- several worker flows exist, but they are still service-local

Plan:

- split work types into separate River jobs:
  - screening dispatch
  - monitored-object continuous checks
  - dataset update jobs
- keep screening and dataset tables as source-of-truth records
- migrate one work type at a time, not all three at once

Success criteria:

- retries and backlog handling move to River
- service remains easy to inspect via its own tables and endpoints

### Phase 4: `decision-engine-service`

Why last:

- highest coupling
- async decisions and scheduled executions are central to the platform
- current model also interacts with workflow, screening, scoring, and outbox dispatch

Plan:

- start with async decision executions only
- keep `async_decision_executions` as the domain record
- enqueue River jobs keyed by execution id
- worker loads execution record, runs evaluation, and updates the record
- only after that is stable, migrate scheduled executions

Success criteria:

- sync endpoints still work exactly the same
- async fallback from live evaluation still returns the same API shape
- execution status endpoints remain unchanged
- no regression in retry or overload handling

## Technical design

### Shared River foundation

Add a small shared pattern for the new backend:

- River client setup
- worker registration
- service auth and config wiring
- job naming conventions
- helper for enqueue-in-transaction patterns

This should be a thin shared package, not a new platform layer.

### Job payload design

Prefer small job payloads:

- pass ids, tenant ids, and lightweight routing fields
- avoid copying large request bodies into River unless necessary

Recommended examples:

- data model: `index_job_id`
- ingestion: `upload_log_id`
- screening: `screening_id`, `dataset_update_job_id`, `monitored_object_id`
- decision engine: `async_decision_execution_id`, `scheduled_execution_id`

### Idempotency

Where APIs already expose idempotency, keep it at the domain-record layer.

Recommended approach:

- first create or reuse the service-owned record
- enqueue River job against that record
- if duplicate enqueue happens, worker should safely no-op based on current record status

### Status model

Do not expose River-native state directly to API clients.

Clients should continue seeing service-owned statuses like:

- `queued`
- `processing` or `running`
- `completed`
- `failed`

River is an implementation detail; service records remain the contract.

## Main risks

### Risk 1: duplicate state machines

If we let both River and service tables act like user-facing state, the model gets messy fast.

Mitigation:

- River handles execution mechanics
- service tables remain the only public status model

### Risk 2: broken transactional guarantees

If we create a domain record but fail to enqueue, or enqueue without the domain record, we create drift.

Mitigation:

- use transactional enqueue patterns where supported
- otherwise write reconciliation checks for orphaned records

### Risk 3: oversized jobs

Pushing full payload blobs into River for every task will make operations harder.

Mitigation:

- keep River payloads small
- fetch the real work payload from domain tables when executing

### Risk 4: migrating scheduled execution semantics

Scheduled work already has timing, retries, and visibility. That is more sensitive than simple async dispatch.

Mitigation:

- migrate scheduled execution after plain async jobs
- preserve current API and status summaries during cutover

## Suggested delivery slices

### Slice A

- add River dependency and base wiring to one service
- register one worker
- enqueue one simple background job type

### Slice B

- cut over `data-model-service` index jobs fully

### Slice C

- cut over `ingestion-service` CSV uploads fully

### Slice D

- cut over one `screening-service` job type

### Slice E

- cut over `decision-engine-service` async decision executions

### Slice F

- cut over `decision-engine-service` scheduled executions

## Recommendation

Yes, we can replace the current queue alternatives in `new/backend` with River, but we should do it selectively and in phases.

The best first move is:

1. adopt River in `data-model-service`
2. prove the shared pattern
3. move `ingestion-service`
4. then tackle `screening-service`
5. leave `decision-engine-service` for last

That gives us a controlled migration instead of a risky platform-wide rewrite.
