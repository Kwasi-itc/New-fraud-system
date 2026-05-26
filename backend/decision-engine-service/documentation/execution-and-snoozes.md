# Execution And Snoozes

This document explains rule snoozes, scheduled executions, and async decision execution endpoints.

## Endpoint Group

- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/rule-snoozes`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/rule-snoozes`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions`
- `GET /v1/tenants/:tenantId/async-decision-executions`
- `POST /v1/tenants/:tenantId/async-decision-executions`

Primary files:

- [internal/httpapi/handlers/snoozes.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/snoozes.go)
- [internal/httpapi/handlers/executions.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/executions.go)

## Parameters

- `tenantId`
  - tenant boundary for all execution and snooze records
- `scenarioId`
  - scenario that owns the rule snooze or scheduled execution

## Request Meanings

### Rule snooze creation

The request body identifies:

- `object_type`
  - the business object category being snoozed
- `object_id`
  - the exact object instance being snoozed
- `snooze_group_id`
  - the snooze bucket/key that matching rules check against
- `expires_at`
  - when the snooze stops applying

### Scheduled execution creation

The request body identifies:

- when the scenario should run
- what object payload/request body should be evaluated at that time

### Async decision execution creation

The request body identifies:

- `scenario_id`
  - scenario to use for the async run
- `object_type`
  - type of records being evaluated
- `items`
  - array of decision-evaluation payloads to process asynchronously

## Endpoint Detail

### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/rule-snoozes`

What it does:

- lists active snoozes relevant to the scenario

How it should be used:

- inspect whether object-level snoozes are suppressing expected rule hits

### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/rule-snoozes`

What it does:

- creates a snooze record for a scenario/object combination

How it works:

- rules that reference the same snooze group can check this record during evaluation

How it should be used:

- temporarily suppress noisy or already-reviewed rule behavior on one object

### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions`

What it does:

- lists scheduled runs configured for the scenario

### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/scheduled-executions`

What it does:

- creates a scheduled decision execution record

How it should be used:

- run a scenario at a future time against a prepared set of items

### `GET /v1/tenants/:tenantId/async-decision-executions`

What it does:

- lists tenant-level async decision execution jobs

### `POST /v1/tenants/:tenantId/async-decision-executions`

What it does:

- creates an async decision execution job

How it should be used:

- bulk evaluation workloads
- replay/backfill scenarios where synchronous evaluation would be too large
