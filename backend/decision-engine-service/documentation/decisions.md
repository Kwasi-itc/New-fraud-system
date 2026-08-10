# Decisions

This document explains live evaluation, decision creation, decision reads, and ingestion-triggered evaluation.

## Endpoint Group

- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/evaluate`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/decisions`
- `GET /v1/tenants/:tenantId/decisions`
- `GET /v1/tenants/:tenantId/decisions/count`
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

### `GET /decisions/count`

Parameters:

- `tenantId`
  - returns only the count of tenant-level decisions matching the supplied filters

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
- includes object type, object id, execution mode, and record payload used to evaluate all live matching scenarios

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
- remains the primary record-fetch endpoint for UI tables
- supports `include_total_count=true` when a caller explicitly wants count metadata on the same response

How it should be used:

- tenant-wide review views
- admin dashboards and operational inspection
- primary rows query for async frontend loading

Response behavior notes:

- default behavior omits `pagination.total_count` and `pagination.total_pages`
- this keeps record fetches fast for search, filters, and deep paging
- use cursor pagination when possible for large result sets

### `GET /v1/tenants/:tenantId/decisions/count`

What it does:

- returns only `{ count: number }` for the same tenant-level filters supported by `GET /decisions`

How it should be used:

- run separately from the main decision rows request
- update result totals asynchronously after rows have rendered
- keep totals accurate for the active filter and search state without blocking the initial list paint

Response behavior notes:

- accepts the same filtering shape used by `GET /decisions`
- does not return decision rows or pagination metadata
- intended for secondary UI data such as `N results`

### `GET /v1/tenants/:tenantId/decisions/:decisionId`

What it does:

- loads one decision plus its rule executions
- returns structured rule-evaluation evidence on `rule_executions[].evaluation` for `hit` outcomes
- keeps list endpoints summary-only and does not return rule evidence there

How it should be used:

- drill into why a specific decision scored or resolved the way it did

Response behavior notes:

- `hit`
  - includes an `evaluation` snapshot that can be used to explain why the rule matched
- `no_hit`
  - returns the rule execution summary without an `evaluation` snapshot to limit payload size
- `snoozed`
  - returns the rule execution summary without an `evaluation` snapshot because the rule did not execute to completion for scoring
- `error`
  - the frontend handles this state defensively, but the current service path does not yet persist structured per-rule error evidence

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
- supports explicit request-level sync or async execution mode

Request body fields:

- `object_id`
- `object_type`
- `mode`
  - optional
  - `sync` attempts synchronous evaluation first and falls back to async if the live path is saturated
  - `async` defers immediately to async execution
  - default is `sync`
- `fields`
- `wait_timeout_ms`
  - optional async wait window in milliseconds used only when the request is deferred to async execution
- `callback_url`
  - optional HTTP callback URL used only when the request is deferred to async execution
- `source`
  - optional source marker from the upstream ingestion path

How it works:

- this is the split-service integration point from `ingestion-service`
- the decision engine treats the request as a post-ingest event and fans out across relevant scenarios
- when `mode=sync`, the handler attempts inline all-scenario evaluation and falls back to async execution if the shared live gate is full
- when `mode=async`, the handler enqueues async execution immediately
- `LIVE_DECISION_MODE=async_only` and forced async object-type policies still override the request and defer immediately

How it should be used:

- by `ingestion-service` after successful record writes
- by replay systems simulating ingestion events
