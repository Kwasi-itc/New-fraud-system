# Decision Engine Service Integration Contracts

## Overview

This document describes the current integration surface used by the implemented standalone `decision-engine-service`, plus the main contract decisions that are still open.

The current runtime depends directly on:

- `data-model-service` for the tenant model
- `ingestion-service` for tenant record reads
- internal persistence for decision history, workflows, outbox, screening, scoring, and helper data

## Current contract with `data-model-service`

The implemented code uses a single tenant-model read through `ports.DataModelReader`:

- `GetTenantModel(ctx, tenantID) (TenantModel, error)`

The current HTTP client calls:

- `GET /v1/tenants/{tenantId}/data-model`

### Required response shape

The current decision engine expects:

- `data_model.revision_id`
- `data_model.ingestion_contract.record_lookup_field`
- `data_model.tables`
- per table:
  - `name`
  - `fields`
  - `links_to_single`
- per field:
  - `name`
  - `data_type`
- per single-link:
  - `name`
  - `parent_table_name`
  - `parent_field_name`
  - `child_table_name`
  - `child_field_name`

### What the current runtime uses this for

- iteration and rule validation
- field existence checks
- field type checks
- trigger-object compatibility checks
- `related_field` path traversal validation
- `related_count` and `related_field` object-type and field validation

### What is not currently required

The implemented service does not currently require the broader future-state contract once discussed in planning docs, such as:

- pivots
- navigation options beyond `links_to_single`
- tenant writability flags
- feature-access metadata

Those may still be needed later, but they are not part of the current runtime dependency.

## Current contract with tenant data reads

The implemented runtime hides tenant-record access behind `ports.TenantDataReader`:

- `GetRecord(ctx, tenantID, objectType, objectID)`
- `ListRecords(ctx, tenantID, objectType, limit)`
- `QueryRecords(ctx, tenantID, objectType, fieldName, value, limit)`

The current HTTP-backed implementation uses `ingestion-service` as the source for those reads.

## Current contract with `ingestion-service`

The current HTTP client calls:

- `GET /v1/tenants/{tenantId}/records/{objectType}/{objectId}`
- `GET /v1/tenants/{tenantId}/records/{objectType}?limit={n}`
- `GET /v1/tenants/{tenantId}/records/{objectType}/search?field={fieldName}&value={value}&limit={n}`

### Required response shape

For one-record reads:

- `record.object_id`
- `record.object_type`
- `record.fields`

For list/search reads:

- `records[]`
- per record:
  - `object_id`
  - `object_type`
  - `fields`

### What the current runtime uses these for

- evaluating record payloads against published scenarios
- `related_count`
- `related_field`
- relation traversal through `links_to_single`
- batch and scheduled execution request processing

### Current limits of this contract

The standalone decision engine does not yet use a richer query abstraction for:

- aggregates
- server-side joins
- historical alert lookups from ingestion-side data
- paginated scans beyond a simple `limit`

Current helper functions therefore operate on a deliberately narrow read contract.

## Current ingestion-to-decision trigger contract

The current trigger path is an explicit callback into the decision engine:

- `POST /v1/tenants/{tenantId}/ingestion-events/record-ingested`

Current request shape:

- `object_id`
- `object_type`
- `fields`
- optional `source`

The current behavior is:

- evaluate all live scenarios for the tenant
- return a multi-scenario evaluation result synchronously

### Important current behavior note

Despite the earlier design preference for an event-driven flow, the implemented baseline is currently an HTTP callback contract, not an event bus contract.

That means the remaining architectural decision is not “what do we imagine,” but rather:

- keep the HTTP callback as the V1 production trigger
- move to an event-driven trigger later
- or support both with one canonical envelope

## Current internal contracts already owned by this service

These capabilities are no longer only external planning topics. They already exist inside the service boundary:

- decision persistence and decision-history reads
- rule execution persistence
- workflow definition persistence
- workflow execution persistence
- rule snooze persistence
- scheduled execution persistence
- async decision execution persistence
- outbox event persistence
- screening config and execution persistence
- scoring config and request persistence
- helper repositories for:
  - custom list entries
  - record tags
  - risk snapshots
  - IP flags

This means the “integration contract” work left is mostly about external boundaries, not these already-owned persistence concerns.

## Current workflow-side contract reality

The implemented service can create workflow execution records and advance dispatch status, but the actual downstream side effect contract is still provisional.

What exists now:

- workflow definitions
- workflow execution records
- dispatch processing shell

What is still open:

- whether workflow actions call a case-management service directly
- whether workflow actions publish commands/events for another service to execute
- the exact payload for create-case or attach-decision style actions
- retry, idempotency, and failure semantics for workflow side effects

## Current screening and scoring contract reality

The standalone service already owns:

- screening config authoring
- screening execution records
- scoring config authoring
- scoring request records
- dispatch/status progression shells

What is still open:

- exact scoring provider contracts
- workflow and scoring downstream response handling lifecycle
- retry behavior and terminal-failure behavior for non-screening side effects
- whether scoring stays in this service boundary for V1 and beyond

## Current OpenAPI and HTTP contract status

The service now has a maintained OpenAPI spec in:

- `internal/httpapi/openapi.yaml`

That spec mirrors the implemented request and response envelopes for:

- authoring endpoints
- validation
- decision evaluation
- ingestion-triggered evaluation
- workflows
- screening and scoring
- snoozes
- scheduled and async execution APIs
- platform helper APIs
- outbox inspection

## Still-open contract decisions

The highest-value remaining contract decisions are:

- whether the ingestion-trigger remains synchronous HTTP or moves to events
- whether `TenantDataReader` continues to read through `ingestion-service` or moves to direct tenant-schema reads
- whether workflow side effects execute in-process or through a downstream case-management contract
- whether helper-data repositories remain local service-owned V1 storage or become external dependencies
- whether screening and scoring stay as local orchestration plus provider contracts, or move behind other service boundaries

## Guiding rule

The standalone decision engine should continue depending on explicit interfaces and documented HTTP contracts, not monolith-era shared database assumptions.
