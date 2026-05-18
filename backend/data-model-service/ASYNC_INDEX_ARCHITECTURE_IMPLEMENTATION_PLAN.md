# Async Index Architecture Implementation Plan

This document translates the proposed hybrid architecture in `documentation/architecture/data-model-service-architecture.md` into a concrete implementation plan for the standalone data model service.

## Goal

Keep structural schema mutations synchronous:

- tenant registration and provisioning
- table create and delete
- field create and delete
- required base indexes such as `object_id`
- unique field indexes tied directly to field lifecycle

Move optional or heavier index work to an asynchronous path:

- navigation indexes
- composite traversal indexes
- search-oriented indexes if introduced
- rebuild and repair index jobs

## Current State

The current service already supports:

- transactional metadata + tenant DDL writes
- synchronous table and field lifecycle
- synchronous unique index creation and deletion
- `core.index_jobs` metadata table

The current service does not yet support:

- an `IndexJob` domain model
- index job repository and service layer
- a worker process for `core.index_jobs`
- navigation option metadata CRUD
- navigation index orchestration
- index job API visibility
- non-empty navigation options in assembled read responses

## Design Boundary

This implementation should borrow Marble's async pattern, not Marble's whole platform model.

Preserve:

- explicit job persistence
- background execution
- retries and status transitions
- index introspection concepts where useful

Do not directly port:

- scenario-publication coupling
- scoring/rules coupling
- broad River queue infrastructure unless it is clearly justified
- Marble security and usecase graphs

## Recommended Delivery Strategy

Implement in two stages.

### Stage 1

Deliver the minimal async index architecture with:

- index job domain and repository
- worker process
- job claiming and execution
- API visibility for job status

This stage should not require navigation option CRUD yet.

### Stage 2

Add the higher-level product behavior:

- navigation option metadata
- navigation index job creation
- assembled read integration
- repair and retry workflows

## Workstreams

## 1. Domain Model

Add index-job concepts to `internal/domain/datamodel/model.go`.

Introduce:

- `IndexJob`
- `IndexJobType`
- `IndexJobStatus`

Recommended initial job types:

- `navigation`
- `search`
- `repair`

Recommended initial statuses:

- `pending`
- `running`
- `applied`
- `failed`
- `cancelled`

Recommended shape:

```go
type IndexJob struct {
    ID                  uuid.UUID
    TenantID            uuid.UUID
    TableID             *uuid.UUID
    TableName           string
    IndexType           string
    Columns             []string
    Status              string
    RequestedByOperation string
    ErrorMessage        *string
    AttemptCount        int
    RequestedAt         time.Time
    StartedAt           *time.Time
    CompletedAt         *time.Time
    ScheduledAt         *time.Time
    DedupeKey           string
}
```

Notes:

- `TableID` is preferable to `table_name` alone because metadata joins will be safer.
- `DedupeKey` should support idempotent enqueueing for repeated requests.
- `RequestedByOperation` helps connect jobs back to a schema or API action.

## 2. Metadata Schema

The current `core.index_jobs` table is too small for reliable async execution.

Update `internal/migrations/metadata` with a new additive migration to extend `core.index_jobs`.

Recommended new columns:

- `table_id UUID NULL REFERENCES core.model_tables(id) ON DELETE CASCADE`
- `requested_by_operation TEXT NOT NULL DEFAULT ''`
- `error_message TEXT NULL`
- `attempt_count INTEGER NOT NULL DEFAULT 0`
- `started_at TIMESTAMPTZ NULL`
- `scheduled_at TIMESTAMPTZ NULL`
- `dedupe_key TEXT NOT NULL DEFAULT ''`

Recommended indexes:

- index on `(status, scheduled_at, requested_at)`
- unique index on `dedupe_key` when non-empty
- index on `(tenant_id, requested_at DESC)`

Recommended migration approach:

- keep `000001_init` unchanged
- add a new migration such as `000003_index_jobs_hardening.up.sql`

## 3. Repository Interfaces

Extend `internal/ports/repositories_datamodel.go`.

Add:

```go
type IndexJobRepository interface {
    Create(ctx context.Context, job datamodel.IndexJob) error
    GetByID(ctx context.Context, id uuid.UUID) (datamodel.IndexJob, error)
    ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]datamodel.IndexJob, error)
    ClaimNext(ctx context.Context, now time.Time) (*datamodel.IndexJob, error)
    MarkRunning(ctx context.Context, id uuid.UUID, startedAt time.Time) error
    MarkApplied(ctx context.Context, id uuid.UUID, completedAt time.Time) error
    MarkFailed(ctx context.Context, id uuid.UUID, message string, completedAt time.Time) error
    IncrementAttempt(ctx context.Context, id uuid.UUID) error
    Retry(ctx context.Context, id uuid.UUID, scheduledAt time.Time) error
}
```

Expose this repository through:

- `internal/ports/transactions.go`
- `internal/store/postgres/transaction_manager.go`

## 4. PostgreSQL Index Job Repository

Add `internal/store/postgres/index_job_repository.go`.

Responsibilities:

- persist jobs
- claim pending jobs safely
- update state transitions
- list tenant job history

Claiming strategy:

- use `FOR UPDATE SKIP LOCKED`
- select the oldest eligible `pending` job
- respect `scheduled_at`

This repository should be written to support multiple worker instances safely.

## 5. Schema Manager Extensions

The current schema manager only supports unique indexes.

Extend `internal/ports/schema_manager.go` and `internal/tenantdb/postgres/indexing.go`.

Add methods such as:

- `CreateNavigationIndex`
- `DropNavigationIndex`
- `CreateSearchIndex`
- `CreateIndexJobTarget`

Recommended direction:

- introduce a generic `CreateManagedIndex` method driven by `IndexJob`
- keep `CreateUniqueIndex` and `DropUniqueIndex` as explicit synchronous helpers

Example direction:

```go
CreateManagedIndex(ctx context.Context, tenant tenant.Tenant, table datamodel.Table, job datamodel.IndexJob) error
```

Important constraints:

- index names must be deterministic
- DDL must be idempotent where possible
- identifier sanitization rules must be reused

## 6. Index Introspection

Some worker operations will need to know whether an index already exists or is invalid.

Use Marble's `api/repositories/pg_indexes/pg_indexes.go` as reference only.

Create a lightweight equivalent in the new service, likely under:

- `internal/tenantdb/postgres/index_introspection.go`

Responsibilities:

- inspect existing tenant-schema indexes
- classify managed index state
- support repair and retry logic later

Keep this narrower than Marble's generic index parsing unless broader coverage is needed.

## 7. Service Layer

Add an `IndexJobService` under `internal/service/index_job_service.go`.

Responsibilities:

- validate async-index requests
- enqueue jobs transactionally
- list job status
- retry failed jobs
- optionally dedupe equivalent pending work

This service should be the only place that decides:

- which index work stays synchronous
- which index work becomes asynchronous

Recommended rule set:

- `object_id` unique index remains synchronous
- field uniqueness remains synchronous
- navigation and optional secondary indexes become asynchronous

## 8. Worker Process

Add a new executable:

- `cmd/worker/main.go`

Recommended first implementation:

- polling worker, not River

Why:

- the service currently has no job runtime
- a poller is enough for `core.index_jobs`
- it keeps the standalone service operationally simpler than Marble

Worker loop responsibilities:

1. claim next pending job
2. load tenant and table context
3. mark job running
4. execute index DDL through schema manager
5. mark job applied or failed
6. sleep briefly when no work is available

Recommended support package:

- `internal/worker/index_job_runner.go`

## 9. App Wiring And Configuration

Add worker-specific config to `internal/app/config.go`.

Potential settings:

- `INDEX_WORKER_ENABLED`
- `INDEX_WORKER_POLL_INTERVAL`
- `INDEX_WORKER_BATCH_SIZE`
- `INDEX_WORKER_MAX_ATTEMPTS`

You may keep the HTTP server and worker as separate commands for clarity:

- `cmd/server`
- `cmd/worker`

Do not combine them into one process initially.

## 10. HTTP API Additions

Add index-job visibility endpoints to `internal/httpapi/router.go`.

Recommended initial routes:

- `GET /v1/tenants/:tenantId/index-jobs`
- `GET /v1/index-jobs/:jobId`
- `POST /v1/index-jobs/:jobId/retry`

Add handlers:

- `internal/httpapi/handlers/index_jobs.go`

Add DTOs:

- `internal/httpapi/dto/index_job.go`

These routes provide observability even before navigation option CRUD is added.

## 11. Navigation Option Product Layer

The architecture and OpenAPI mention navigation options, but current assembled reads return empty arrays.

There is a product decision to make first:

- computed navigation options
- stored navigation options

Recommended approach:

- store navigation options explicitly in metadata

Why:

- async indexes need an explicit source of truth
- clients can manage intent directly
- status tracking becomes clearer

This likely requires:

- a new metadata table such as `core.navigation_options`
- a domain model such as `NavigationOptionDefinition`
- repository, service, DTO, and handler support

Recommended routes:

- `POST /v1/tables/:tableId/navigation-options`
- `GET /v1/tables/:tableId/navigation-options`
- `DELETE /v1/navigation-options/:navigationOptionId`

On create:

- write navigation option metadata
- enqueue corresponding index job

## 12. Assembled Read Integration

Update `internal/store/postgres/assembled_data_model_repository.go`.

Current behavior:

- `NavigationOptions` is always empty

Target behavior:

- include stored navigation options
- optionally include index job status or readiness indicators later

This is the point where the architecture becomes visible to downstream consumers.

## 13. Schema Change And Audit Integration

Async index work should still be auditable.

Decide how to record it:

- create a schema-change log entry when the job is enqueued
- create another when the job is applied or fails

Recommended:

- enqueue event: `request_index_job`
- completion event: `apply_index_job`
- failure event: `fail_index_job`

This keeps operational history consistent with existing mutation logging.

## 14. Retry, Failure, And Repair Policy

Define clear worker behavior for failures.

Recommended initial policy:

- worker increments `attempt_count`
- worker records `error_message`
- worker marks job `failed`
- retry is manual through API in the first version

Later improvements:

- backoff scheduling
- automatic retries up to a max attempt count
- reconcile-triggered repair jobs

Manual retry is sufficient for the first cut.

## 15. Testing Plan

### Domain Tests

Add tests for:

- index job type and status validation
- dedupe key generation if implemented in domain logic

### Repository Tests

Add PostgreSQL-backed tests for:

- create job
- list jobs by tenant
- claim next job
- mark running/applied/failed
- retry transition

### Service Tests

Add tests for:

- synchronous vs async routing decisions
- enqueue dedupe rules
- retry validation

### Worker Tests

Add tests for:

- successful index execution
- failure marking
- idempotent no-op when target index already exists

### Integration Tests

Add end-to-end tests for:

- create navigation option -> job created
- worker processes job -> index exists
- assembled read returns navigation option

## 16. Suggested Sequence

Implement in this order.

### Phase 1: Core async job infrastructure

1. add `IndexJob` domain model
2. add migration to harden `core.index_jobs`
3. add repository interface and PostgreSQL repository
4. wire repository into transaction manager
5. add `IndexJobService`
6. add `GET /index-jobs` and retry API

### Phase 2: Worker execution

7. extend schema manager for managed secondary indexes
8. add worker runner package
9. add `cmd/worker`
10. add worker integration tests

### Phase 3: Navigation option product flow

11. add navigation option metadata table and domain model
12. add navigation option CRUD API
13. enqueue index jobs from navigation option creation
14. update assembled data model reads

### Phase 4: Repair and hardening

15. add index introspection
16. add repair and reconcile-triggered job creation
17. add richer failure and retry behavior

## 17. File-Level Change Map

Expected new or updated files include:

- `internal/domain/datamodel/model.go`
- `internal/migrations/metadata/00000x_*.sql`
- `internal/ports/repositories_datamodel.go`
- `internal/ports/transactions.go`
- `internal/ports/schema_manager.go`
- `internal/store/postgres/index_job_repository.go`
- `internal/store/postgres/transaction_manager.go`
- `internal/tenantdb/postgres/indexing.go`
- `internal/tenantdb/postgres/index_introspection.go`
- `internal/service/index_job_service.go`
- `internal/httpapi/router.go`
- `internal/httpapi/dto/index_job.go`
- `internal/httpapi/handlers/index_jobs.go`
- `internal/worker/index_job_runner.go`
- `internal/app/config.go`
- `cmd/worker/main.go`

Likely future files for navigation options:

- `internal/domain/datamodel/navigation_option.go`
- `internal/store/postgres/navigation_option_repository.go`
- `internal/service/navigation_option_service.go`
- `internal/httpapi/dto/navigation_option.go`
- `internal/httpapi/handlers/navigation_options.go`

## 18. Decisions To Lock Before Coding

These should be confirmed before implementation begins.

1. Worker runtime:
   - polling worker now
   - River only if requirements expand

2. Navigation option representation:
   - stored metadata, not purely computed

3. Synchronous vs async split:
   - keep unique indexes synchronous
   - move optional secondary indexes async

4. Retry policy:
   - manual retry first
   - automatic retry later if needed

## 19. Recommendation

The best first implementation is:

- polling-based `core.index_jobs` worker
- explicit index job repository and API
- stored navigation option metadata
- assembled read integration after the async infrastructure is stable

That gives the service the architecture described in the documentation without prematurely rebuilding Marble's broader worker platform.
