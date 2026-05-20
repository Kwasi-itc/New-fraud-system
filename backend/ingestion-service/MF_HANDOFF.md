# MF Handoff

This file is a compact handoff for the planned standalone ingestion service in `new/backend/ingestion-service`.

## What this project is

This is the planned standalone ingestion subsystem for the new fraud platform.

It is intended to sit beside:

- `new/backend/data-model-service`

and to own:

- record ingestion APIs
- validation against published data models
- upsert behavior into tenant data stores
- batch and CSV ingestion orchestration
- ingestion event publication to downstream services

## Why it exists

Reviewing Marble's current backend shows that ingestion is already a bounded platform concern:

- dedicated ingestion endpoints
- dedicated ingestion use case
- batch and CSV ingestion flow
- upload logs
- worker-based CSV execution
- optional screening hooks

It should therefore become its own service, not live under transaction monitoring.

## Current status

Implemented in this directory so far:

- service folder scaffold
- architecture and extraction design docs
- implementation blueprint
- implementation TODO list
- handoff and setup placeholders

Not implemented yet:

- Go module
- server
- worker
- persistence
- API handlers
- `data-model-service` client

## Locked architecture decisions

- `data-model-service` is the sole source of the published ingestion contract
- the published ingestion contract must expose a top-level `revision_id`
- `ingestion-service` writes directly into tenant schemas
- `data-model-service` should not expose a business-record write API
- monitoring and scoring should preserve Marble-compatible behavior
- monitoring and scoring handoff should be implemented through events or a durable outbox pattern

## Recommended next steps

1. create the Go module and HTTP bootstrap
2. define the `DataModelReader` and `TenantDataWriter` ports
3. implement synchronous single-record ingestion
4. implement batch ingestion
5. add upload-log metadata and CSV worker
6. add downstream event publication

## Key design decisions already made

- ingestion is a standalone service
- `data-model-service` is the upstream schema authority
- transaction monitoring is a downstream consumer, not the owner of ingestion
- webhooks remain an outbound delivery concern and are not the ingestion boundary

## Important docs

- [README.md](./README.md)
- [INGESTION_SERVICE_EXTRACTION_DESIGN.md](./INGESTION_SERVICE_EXTRACTION_DESIGN.md)
- [INGESTION_SERVICE_IMPLEMENTATION_BLUEPRINT.md](./INGESTION_SERVICE_IMPLEMENTATION_BLUEPRINT.md)
- [IMPLEMENTATION_TODO.md](./IMPLEMENTATION_TODO.md)
