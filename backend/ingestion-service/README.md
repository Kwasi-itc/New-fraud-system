# Ingestion Service

Standalone Go service for tenant-aware data ingestion, extracted from Marble's ingestion layer and aligned with the new `data-model-service`.

Current location in the workspace:

- `new/backend/ingestion-service`

This service is intended to own record intake, validation against the published data model, upsert behavior into tenant data stores, batch ingestion orchestration, and downstream event publication for monitoring workflows.

## Purpose

The service should manage:

- synchronous single-record ingestion
- synchronous multi-record ingestion
- partial upsert semantics
- CSV batch ingestion intake
- upload-log lifecycle
- tenant-aware payload validation using the published data model
- enum and constrained-value handling during ingestion
- idempotent upsert/write behavior into tenant data stores
- ingestion result and error reporting
- asynchronous batch job orchestration
- downstream event publication for decisioning, monitoring, and screening consumers

The service should not directly own:

- data-model metadata authoring and schema mutation
- fraud rule evaluation logic
- case management
- outbound customer webhook subscriptions and delivery

## Locked decisions

The current architecture decisions for this service are:

- `data-model-service` is the sole source of the published ingestion contract
- the published contract must be versioned with a top-level `revision_id`
- `ingestion-service` writes directly into tenant schemas managed by `data-model-service`
- monitoring and scoring should preserve Marble-compatible behavior
- monitoring and scoring handoff should be implemented through events or an outbox pattern rather than tight in-process coupling

## Why this is a separate service

Marble's current ingestion implementation is a platform boundary, not just a transaction-monitoring helper.

Today, the ingestion code:

- accepts API requests on dedicated ingestion endpoints
- validates payloads against the org data model
- writes records into the client database
- supports single, batch, and CSV workflows
- optionally triggers continuous-screening behavior
- feeds downstream fraud and scoring workflows

That makes ingestion a natural standalone service beside `data-model-service`, not a submodule of transaction monitoring.

## Marble behaviors this service should preserve

The current Marble backend exposes:

- private ingestion routes
  - `POST /ingestion/:object_type`
  - `PATCH /ingestion/:object_type`
  - `POST /ingestion/:object_type/multiple`
  - `PATCH /ingestion/:object_type/multiple`
  - `POST /ingestion/:object_type/batch`
- public ingestion routes
  - `POST /v1/ingest/:objectType`
  - `PATCH /v1/ingest/:objectType`
  - `POST /v1/ingest/:objectType/batch`
  - `PATCH /v1/ingest/:objectType/batch`

The current flow validates payloads against the data model, parses them into typed client objects, performs upsert-oriented ingestion, and optionally triggers monitoring and screening.

The current webhook subsystem is outbound delivery only. It is not the ingestion entrypoint and should remain separate.

## Proposed service boundary

- `data-model-service`
  - owns tenant registration, schema metadata, physical schema lifecycle, indexes, and published versioned model contracts
- `ingestion-service`
  - owns record intake, validation, direct tenant-schema persistence, batch processing, upload logs, and downstream ingestion events
- monitoring or decision services
  - consume ingested records or ingestion events and evaluate fraud logic

## Core capabilities for V1

- Gin HTTP API
- service-to-service auth
- read-only dependency on `data-model-service`
- version-pinned writes against published schema revisions
- PostgreSQL-backed metadata for batch jobs and upload logs
- tenant data writer abstraction
- synchronous ingestion endpoints
- batch ingestion endpoints
- CSV upload intake
- background worker for asynchronous CSV processing
- structured validation errors
- idempotent ingestion contract
- request IDs and structured logs
- embedded OpenAPI specification

## Project layout

```text
new/backend/ingestion-service/
  cmd/
    server/                 HTTP service entrypoint
    worker/                 batch ingestion worker
  docs/
    README.md               docs placeholder
  internal/
    README.md               package layout placeholder
  README.md
  SETUP_AND_RUN_GUIDE.md
  INGESTION_SERVICE_EXTRACTION_DESIGN.md
  INGESTION_SERVICE_IMPLEMENTATION_BLUEPRINT.md
  IMPLEMENTATION_TODO.md
  MF_HANDOFF.md
```

## High-level architecture

- `httpapi`
  - REST endpoints for ingest, batch ingest, and upload-log reads
- `service`
  - orchestration of ingest flows
- `ports`
  - interfaces to data-model reads, tenant data writes, job queue, and blob storage
- `store`
  - upload-log and batch-job persistence
- `worker`
  - CSV processing and retryable asynchronous ingestion execution

## Expected dependencies

The ingestion service should depend on:

- `data-model-service` for published versioned model contracts
- tenant data database or tenant data writer adapter for physical writes
- optional object storage for CSV artifacts
- optional queue/worker runtime for asynchronous jobs

The ingestion service should expose outputs to:

- fraud decisioning
- continuous screening
- analytics
- audit/event stream consumers

## API direction

Likely internal routes:

- `POST /v1/tenants/:tenantId/ingest/:objectType`
- `PATCH /v1/tenants/:tenantId/ingest/:objectType`
- `POST /v1/tenants/:tenantId/ingest/:objectType/batch`
- `PATCH /v1/tenants/:tenantId/ingest/:objectType/batch`
- `POST /v1/tenants/:tenantId/ingest/:objectType/csv`
- `GET /v1/tenants/:tenantId/ingest/:objectType/upload-logs`
- `GET /v1/upload-logs/:uploadLogId`

Expected upstream contract from `data-model-service`:

- `GET /v1/tenants/:tenantId/data-model`
- top-level `revision_id`
- assembled tables and fields
- enum values
- tenant provisioning or active status
- physical write-safe metadata needed by ingestion

## Key docs

- [INGESTION_SERVICE_EXTRACTION_DESIGN.md](./INGESTION_SERVICE_EXTRACTION_DESIGN.md)
- [INGESTION_SERVICE_IMPLEMENTATION_BLUEPRINT.md](./INGESTION_SERVICE_IMPLEMENTATION_BLUEPRINT.md)
- [IMPLEMENTATION_TODO.md](./IMPLEMENTATION_TODO.md)
- [SETUP_AND_RUN_GUIDE.md](./SETUP_AND_RUN_GUIDE.md)
