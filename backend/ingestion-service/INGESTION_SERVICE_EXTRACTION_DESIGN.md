# Ingestion Service Extraction Design

## Objective

Extract Marble's ingestion layer into a standalone backend that can be deployed beside `data-model-service` and consumed by the wider fraud platform.

The result should separate:

- schema ownership
- record ingestion
- fraud evaluation

## What Marble currently does

Marble's current ingestion behavior spans several concerns:

1. HTTP intake
2. payload validation against the current org data model
3. typed payload parsing
4. insert or update into tenant/client data storage
5. optional partial-upsert behavior
6. multi-record batch ingestion with validation aggregation
7. CSV upload intake and asynchronous processing
8. upload-log tracking
9. optional monitoring and screening hooks
10. downstream consumption by decisioning and scoring

That is already a standalone domain boundary in practice.

## Marble source behaviors to preserve

### 1. Synchronous ingestion

Marble supports:

- single object ingest
- multiple object ingest
- partial upsert using `PATCH`

Expected semantics:

- validate the object type against the tenant's data model
- reject schema mismatches clearly
- reject duplicate `object_id` values within the same batch
- return `201` when rows are ingested
- return `200` when the call is accepted but results in no new writes

### 2. Validation model

Marble validates incoming payloads against the active data model and returns structured validation errors keyed by object identity.

Expected semantics:

- object type must exist
- required fields must be present unless patch semantics permit omission
- unknown fields should be rejected for public API paths
- data types must match declared schema
- enum-like values may need catalog insertion or normalization before final write

### 3. Batch ingestion

Marble currently handles:

- JSON array batch ingestion
- CSV upload ingestion
- manual bucket-based ingestion through upload-log records

Expected semantics:

- batch size limits
- validation aggregation across records
- asynchronous worker path for larger CSV flows
- persisted upload log and status transitions

### 4. Optional monitoring hooks

Marble ingestion can attach monitoring intent:

- monitor object after ingestion
- optionally perform initial screening
- pass continuous-screening configuration IDs

This should remain an output contract, not become core write logic in V1.

Current decision:

- preserve Marble-compatible monitoring and scoring behavior in V1
- implement the handoff through explicit events, commands, or an outbox-driven worker model
- do not embed fraud decision execution inside the synchronous ingestion request path

### 5. Webhook boundary

Marble's `webhooks` code is outbound event delivery:

- webhook registration CRUD
- event queueing
- asynchronous dispatch
- retry and signature handling

It is not inbound ingestion.

Conclusion:

- ingestion should publish internal events
- a separate outbound delivery service may subscribe later if needed

## Proposed target responsibilities

The new ingestion service should own:

- ingestion API surface
- request authentication and tenant scoping
- fetching published data model definitions
- payload validation and coercion
- upsert planning
- persistence into tenant data storage
- upload-log lifecycle
- batch job orchestration
- ingestion events for downstream services

The service should not own:

- schema design mutations
- tenant provisioning
- data model authoring
- fraud rules/scenarios
- webhook delivery

## Integration with `data-model-service`

### Required inputs from data-model-service

The ingestion service should consume a stable read contract such as:

- tenant existence and active status
- assembled tenant data model
- top-level `revision_id`
- field definitions
- reserved physical fields
- uniqueness metadata
- enum metadata
- link or pivot metadata only if needed for advanced enrichment later

### Dependency direction

- `data-model-service` publishes schema contracts
- `ingestion-service` consumes those contracts
- `ingestion-service` must not mutate model metadata directly

### Contract versioning

The ingestion service should cache or pin the published model version used for a given ingestion execution so writes can be audited against a known schema revision.

The current decision is:

- `data-model-service` is the sole source of this contract
- `GET /v1/tenants/:tenantId/data-model` should expose a top-level `revision_id`
- every ingestion execution should record the `revision_id` it validated against

## Storage model

Split storage into two categories:

### 1. Service metadata store

Own tables for:

- upload logs
- batch jobs
- ingestion audit records
- failed-record details
- optional idempotency keys

### 2. Tenant data store

Write actual ingested records into tenant-specific physical tables or schemas.

This may still be PostgreSQL, but the write adapter should be abstracted so the ingestion service does not own schema mutation behavior.

Current decision:

- `ingestion-service` writes directly into the tenant schemas
- `data-model-service` does not expose a record-write API
- `data-model-service` remains the schema authority, not a database proxy

## Proposed internal modules

### `internal/domain/ingestion`

Owns:

- upload log state machine
- ingestion request/result types
- validation error model
- batch job model
- idempotency model

### `internal/service`

Owns use cases:

- ingest single record
- ingest multiple records
- ingest CSV upload
- list upload logs
- run upload job
- publish ingestion event

### `internal/ports`

Interfaces for:

- data model reader
- tenant data writer
- upload log repository
- batch job repository
- blob storage
- event publisher
- transaction manager

### `internal/httpapi`

Owns:

- REST handlers
- DTOs
- auth middleware
- error translation

### `internal/worker`

Owns:

- CSV ingestion execution
- retries
- dead-letter or failed terminal states

## API design guidance

### Internal service routes

Recommended internal routes:

- `POST /v1/tenants/:tenantId/ingest/:objectType`
- `PATCH /v1/tenants/:tenantId/ingest/:objectType`
- `POST /v1/tenants/:tenantId/ingest/:objectType/batch`
- `PATCH /v1/tenants/:tenantId/ingest/:objectType/batch`
- `POST /v1/tenants/:tenantId/ingest/:objectType/csv`
- `GET /v1/tenants/:tenantId/ingest/:objectType/upload-logs`
- `GET /v1/upload-logs/:uploadLogId`

### Response behavior

- `201 Created` for successful writes
- `200 OK` for no-op ingest
- `400 Bad Request` for schema and payload validation errors
- `404 Not Found` for unknown tenant or object type
- `409 Conflict` for hard idempotency or concurrency violations when applicable

## Event model

After successful ingestion, publish internal events such as:

- `record.ingested`
- `record.updated`
- `batch.ingestion.completed`
- `batch.ingestion.failed`
- `monitoring.requested`
- `screening.requested`
- `scoring.requested`

Consumers may include:

- transaction monitoring
- continuous screening
- scoring
- analytics

Compatibility requirement:

- event-driven internals must still preserve Marble-style outcomes for monitoring, optional initial screening, and scoring triggers

## V1 exclusions

Do not include in the first extraction:

- direct fraud rule execution inside the ingestion transaction
- outbound customer webhook delivery
- cross-service saga orchestration
- advanced enrichment pipelines
- manual bucket-side ingestion bypasses unless needed immediately

## Major design decisions

### Decision 1: standalone service

Chosen:

- standalone HTTP service

Reason:

- clearer ownership
- cleaner evolution path
- prevents transaction-monitoring from becoming a platform monolith

### Decision 2: data model is an upstream dependency

Chosen:

- ingestion reads published model contracts from `data-model-service`

Reason:

- preserves single ownership of schema
- avoids duplicate metadata logic

### Decision 3: screening and monitoring are outputs

Chosen:

- ingestion emits events or commands

Reason:

- keeps the write path isolated and testable
- allows multiple downstream consumers

### Decision 4: webhooks remain separate

Chosen:

- ingestion is API-first

Reason:

- matches Marble's current implementation
- avoids mixing inbound write APIs with outbound notification delivery
