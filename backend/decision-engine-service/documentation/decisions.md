# Decisions

This document explains live evaluation, decision creation, decision reads, and ingestion-triggered evaluation.

## Endpoint Group

- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/evaluate`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/decisions`
- `GET /v1/tenants/:tenantId/decisions`
- `GET /v1/tenants/:tenantId/decisions/:decisionId`
- `POST /v1/tenants/:tenantId/decisions`
- `POST /v1/tenants/:tenantId/decisions/all`
- `POST /v1/tenants/:tenantId/ingestion-events/record-ingested`

Primary files:

- [internal/httpapi/handlers/decisions.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/decisions.go)
- [internal/service/decision_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/service/decision_service.go)

## Shared Parameters

- `tenantId`
  - tenant boundary for evaluation and decision history
- `scenarioId`
  - scenario being evaluated or used to filter decision history
- `decisionId`
  - one persisted decision record

## Endpoint Meanings

### `POST /scenarios/:scenarioId/evaluate`

Parameters:

- `tenantId`
  - tenant whose model and data are used
- `scenarioId`
  - exact scenario to evaluate

Request body meaning:

- carries the target record payload
- includes the object type and object id context needed for evaluation

### `GET /scenarios/:scenarioId/decisions`

Parameters:

- `tenantId`
  - tenant boundary
- `scenarioId`
  - returns decisions created for that scenario

### `GET /decisions`

Parameters:

- `tenantId`
  - returns tenant-level decision history across scenarios

### `GET /decisions/:decisionId`

Parameters:

- `tenantId`
  - tenant that owns the decision
- `decisionId`
  - decision to load, including rule executions

### `POST /decisions`

Parameters:

- `tenantId`
  - tenant where the decision should be created

Request body meaning:

- creates one decision explicitly from a provided evaluation request

### `POST /decisions/all`

Parameters:

- `tenantId`
  - tenant where the multi-scenario decision creation should happen

Request body meaning:

- evaluates all matching/live scenarios for the provided object payload

### `POST /ingestion-events/record-ingested`

Parameters:

- `tenantId`
  - tenant whose live scenarios should react to the ingested record

Request body meaning:

- represents an ingestion callback event
- includes object type, object id, and record payload used to evaluate all live matching scenarios

## Endpoint Detail

### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/evaluate`

What it does:

- evaluates one scenario against one provided object payload

Request body fields:

- `object_id`
  - record identifier being evaluated
- `object_type`
  - object type of the record
- `fields`
  - actual record payload consumed by the trigger and rules

How it works:

- loads the scenario and its live iteration
- evaluates trigger formula first
- if triggered, evaluates ordered rules
- computes outcome and score
- persists the decision and rule executions
- creates workflow/screening/scoring side-effect records where configured

How it should be used:

- direct synchronous evaluation from internal tools
- replay or manual investigation flows

### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/decisions`

What it does:

- returns decision history for one scenario

How it should be used:

- scenario-specific audit trails
- debugging how one scenario has behaved over time

### `GET /v1/tenants/:tenantId/decisions`

What it does:

- returns decision history across the tenant

How it should be used:

- tenant-wide review views
- admin dashboards and operational inspection

### `GET /v1/tenants/:tenantId/decisions/:decisionId`

What it does:

- loads one decision plus its rule executions

How it should be used:

- drill into why a specific decision scored or resolved the way it did

### `POST /v1/tenants/:tenantId/decisions`

What it does:

- creates one decision explicitly from a request payload

Request body fields:

- `scenario_id`
  - scenario to evaluate
- `object_id`
  - record identifier
- `object_type`
  - record type
- `fields`
  - record payload

How it should be used:

- when the caller already knows the exact scenario to evaluate but wants a tenant-level create endpoint

### `POST /v1/tenants/:tenantId/decisions/all`

What it does:

- evaluates all applicable/live scenarios for one payload

How it should be used:

- broad replay or backfill flows
- “run all relevant scenarios for this record” operational actions

### `POST /v1/tenants/:tenantId/ingestion-events/record-ingested`

What it does:

- handles an ingestion callback and evaluates all live scenarios whose trigger object type matches

Request body fields:

- `object_id`
- `object_type`
- `fields`
- `source`
  - optional source marker from the upstream ingestion path

How it works:

- this is the split-service integration point from `ingestion-service`
- the decision engine treats the request as a post-ingest event and fans out across relevant scenarios

How it should be used:

- by `ingestion-service` after successful record writes
- by replay systems simulating ingestion events
