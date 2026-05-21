# Implementation TODO

## Scaffold

- create Go module and service bootstrap
- add `cmd/server`
- add `cmd/worker`
- add config loader
- add auth middleware
- add health and readiness routes

## Upstream data model integration

- define `DataModelReader` port
- implement HTTP client for `data-model-service`
- define local assembled-model DTO mapping
- require top-level `revision_id` in published model contract
- add caching strategy for model reads

## Synchronous ingestion

- define ingest request and result models
- implement single-record ingest service
- implement batch ingest service
- implement structured validation error responses
- implement patch semantics
- persist `revision_id` used for validation
- enforce batch size limits

## Persistence

- define `TenantDataWriter` port
- implement PostgreSQL writer adapter
- write directly to tenant schemas managed by `data-model-service`
- decide chunking and upsert strategy
- document idempotency replay contract

## CSV flow

- define upload-log metadata schema
- implement CSV upload handler
- implement blob or file-storage adapter
- implement batch worker
- implement retry and terminal failure states

## Events

- define ingestion event schema
- implement event publisher port
- define durable outbox or equivalent handoff store
- add local no-op adapter
- connect successful ingests to monitoring, screening, and scoring handoff events

## Operations

- add request IDs
- add structured logs
- add metrics
- add tracing
- write OpenAPI spec
- add Dockerfile and docker-compose

## Testing

- unit tests for validation
- unit tests for patch behavior
- integration tests for single ingest
- integration tests for multi-ingest
- integration tests for CSV worker
- contract tests for `data-model-service` integration

## Completed In Current Slice

- Go module, bootstrap, migrate command, and service scaffold
- health and readiness routes
- service auth middleware
- `DataModelReader` HTTP client and contract mapping tests
- single-record ingest endpoints
- batch ingest endpoints
- payload validation against published object schemas
- patch semantics for partial updates
- PostgreSQL tenant data writer with direct tenant-schema upserts
- ingestion audit metadata persistence
- durable outbox-event metadata persistence
- idempotent response replay for repeated successful requests
- CSV upload endpoint, upload-log read APIs, and worker polling loop
- bounded CSV retry handling with terminal failure after max attempts
- OpenAPI for sync ingest and upload-log routes

## Remaining Priority

- integration tests against a real PostgreSQL tenant schema
- outbox delivery consumers for monitoring and scoring
- dead-letter or quarantine flow for permanently failed CSV jobs
