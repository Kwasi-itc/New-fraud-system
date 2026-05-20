# Ingestion Service Implementation Blueprint

## Goal

Build `new/backend/ingestion-service` as a standalone Go service with the same documentation-first and modular structure used for `data-model-service`.

## Delivery phases

## Phase 0: project scaffold

Create the baseline service structure:

- `cmd/server`
- `cmd/worker`
- `internal/app`
- `internal/domain/ingestion`
- `internal/httpapi/dto`
- `internal/httpapi/handlers`
- `internal/ports`
- `internal/service`
- `internal/store/postgres`
- `internal/docs`
- `docs/openapi.yaml`
- `Dockerfile`
- `docker-compose.yml`
- `go.mod`
- `Makefile`

Deliverables:

- service starts
- health and readiness endpoints
- auth middleware scaffold
- config loading
- request logging

## Phase 1: read model integration

Implement the upstream read contract to `data-model-service`.

Required capabilities:

- fetch tenant metadata
- fetch assembled data model
- read top-level `revision_id`
- map remote data model DTOs into local validation model
- support short-lived caching with explicit invalidation strategy

Deliverables:

- `DataModelReader` port
- HTTP client adapter
- mapping tests
- persisted or propagated model revision capture

## Phase 2: synchronous single-record ingestion

Implement:

- `POST /v1/tenants/:tenantId/ingest/:objectType`
- `PATCH /v1/tenants/:tenantId/ingest/:objectType`

Behavior:

- authorize request
- read tenant model
- pin `revision_id` for the execution
- validate object type
- parse payload
- apply patch or full-write rules
- persist directly into tenant schema
- return structured validation errors

Deliverables:

- domain validation model
- tenant data writer port
- single ingest handler and service
- integration tests

## Phase 3: synchronous multi-record ingestion

Implement:

- `POST /v1/tenants/:tenantId/ingest/:objectType/batch`
- `PATCH /v1/tenants/:tenantId/ingest/:objectType/batch`

Behavior:

- accept arrays
- pin `revision_id` for the batch execution
- aggregate validation failures by `object_id`
- reject duplicate batch `object_id`s
- enforce batch size limit
- write in transactional chunks where appropriate

Deliverables:

- batch validator
- grouped validation response DTO
- chunked write strategy

## Phase 4: upload logs and CSV intake

Implement:

- `POST /v1/tenants/:tenantId/ingest/:objectType/csv`
- `GET /v1/tenants/:tenantId/ingest/:objectType/upload-logs`
- `GET /v1/upload-logs/:uploadLogId`

Behavior:

- persist upload log row
- store or reference uploaded file
- enqueue worker job
- expose status transitions

Suggested upload-log states:

- `pending`
- `uploaded`
- `processing`
- `completed`
- `failed`
- `cancelled`

Deliverables:

- upload-log domain model
- metadata tables and migrations
- worker enqueue path

## Phase 5: worker-driven CSV execution

Implement `cmd/worker` and CSV processing flow.

Behavior:

- claim pending jobs
- stream CSV rows
- map rows to payloads
- validate and ingest in chunks
- persist per-job metrics and failures
- retry transient failures

Deliverables:

- worker runtime
- batch processing service
- retry strategy
- failure capture

## Phase 6: downstream event publication

Publish internal events after ingestion success.

Initial events:

- `record.ingested`
- `record.updated`
- `batch.ingestion.completed`
- `batch.ingestion.failed`
- `monitoring.requested`
- `screening.requested`
- `scoring.requested`

Deliverables:

- event publisher port
- outbox or equivalent durable handoff mechanism
- no-op adapter for local dev
- queue or bus adapter later

## Phase 7: operational hardening

Add:

- request IDs
- structured logs
- metrics
- tracing
- idempotency controls
- dead-letter handling for failed batch jobs
- admin replay or retry endpoint if needed

## Data model for service-owned metadata

Suggested metadata tables:

- `core_ingestion.upload_logs`
- `core_ingestion.ingestion_jobs`
- `core_ingestion.ingestion_job_failures`
- `core_ingestion.ingestion_audit`
- `core_ingestion.idempotency_keys`

## Interface contracts

### `DataModelReader`

Methods:

- `GetTenant(ctx, tenantID)`
- `GetAssembledDataModel(ctx, tenantID)`

Returned contract requirements:

- top-level `revision_id`
- assembled fields including enum values
- tenant active or provisioned state

### `TenantDataWriter`

Methods:

- `UpsertRecord(ctx, tenantID, objectType, record, opts)`
- `UpsertRecords(ctx, tenantID, objectType, records, opts)`

### `UploadLogRepository`

Methods:

- create log
- update status
- append metrics
- fetch one
- list by tenant and object type

### `BatchJobQueue`

Methods:

- enqueue CSV ingestion
- claim job
- reschedule retry

### `EventPublisher`

Methods:

- publish ingestion event

## Validation rules to implement

At minimum:

- object type exists
- `object_id` exists for full writes
- unknown fields rejected for strict routes
- required fields enforced for full writes
- patch semantics allow omission of non-provided fields
- field types match published schema
- enum or constrained values normalized or rejected
- ingestion audit captures the `revision_id` used

## Transaction strategy

Separate transaction domains clearly:

- metadata-store transaction for upload logs and job state
- tenant-data-store transaction for record persistence
- outbox transaction for downstream monitoring or scoring handoff

Do not assume one global transaction across:

- `data-model-service`
- service metadata store
- tenant data store

Instead:

- validate against a stable model snapshot
- record the `revision_id`
- write tenant data directly
- commit service metadata and durable handoff records
- deliver downstream monitoring or scoring work asynchronously

## Testing plan

### Unit tests

- payload validation
- patch behavior
- duplicate detection
- upload-log state transitions

### Integration tests

- ingest single record into local tenant DB
- ingest batch with mixed valid and invalid rows
- CSV worker happy path
- CSV worker retry path
- model mismatch path

### Contract tests

- `data-model-service` client mapping
- error translation from remote model contract
- `revision_id` propagation through ingestion audit and batch jobs

## Initial non-goals

- user-facing web UI
- full historical temporal versioning of records
- direct webhook-trigger ingestion
- fraud decision execution in the same service

## Explicit architectural stance

- preserve Marble-compatible outcomes first
- improve the implementation by replacing tight runtime coupling with durable event or outbox-driven handoff
- avoid adding a record-write API to `data-model-service`
