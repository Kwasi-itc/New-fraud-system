# Test Runs

This document explains the test-run endpoints used for phantom evaluation and scenario testing.

## Endpoint Group

- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/test-runs`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/test-runs`
- `GET /v1/tenants/:tenantId/test-runs/:testRunId`
- `POST /v1/tenants/:tenantId/test-runs/:testRunId/evaluate`
- `POST /v1/tenants/:tenantId/test-runs/:testRunId/cancel`
- `GET /v1/tenants/:tenantId/test-runs/:testRunId/decision-data-by-score`
- `GET /v1/tenants/:tenantId/test-runs/:testRunId/data-by-rule-execution`

Primary files:

- [internal/httpapi/handlers/testruns.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/testruns.go)
- [internal/service/testrun_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/service/testrun_service.go)

## Parameters

- `tenantId`
  - tenant that owns the scenario and its test runs
- `scenarioId`
  - scenario whose test runs are being listed or created
- `testRunId`
  - one persisted test-run record

## Endpoint Meanings

- create/list routes operate under one scenario
- get/cancel/evaluate/stats routes operate on one saved test run

The evaluate request body carries the same kind of record payload used for live decision evaluation, but writes phantom results instead of live decisions.

## Endpoint Detail

### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/test-runs`

What it does:

- lists the historical and active test runs for one scenario

How it should be used:

- find existing test runs
- inspect which phantom iteration each run compared against
- check whether a run is still active, cancelled, or expired

### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/test-runs`

What it does:

- creates a new comparison session between the scenario's live iteration and a chosen phantom iteration

Request body fields:

- `phantom_iteration_id`
  - the draft or candidate iteration to compare against live behavior
- `expires_at`
  - when the test run should stop being considered active

How it should be used:

- before publishing a changed ruleset
- to compare decision outcomes between current live behavior and a proposed draft

### `GET /v1/tenants/:tenantId/test-runs/:testRunId`

What it does:

- returns one test-run record and its metadata

How it should be used:

- fetch details for a specific run selected from a list

### `POST /v1/tenants/:tenantId/test-runs/:testRunId/evaluate`

What it does:

- evaluates one record against both:
  - the live iteration
  - the phantom iteration

How it works:

- creates phantom decisions and phantom rule executions for the comparison side
- returns live and phantom results together so they can be compared directly

How it should be used:

- regression testing before publish
- rule tuning and threshold tuning

### `POST /v1/tenants/:tenantId/test-runs/:testRunId/cancel`

What it does:

- marks a test run as cancelled so it is no longer treated as active

How it should be used:

- when the comparison is no longer relevant
- when a phantom draft is obsolete

### `GET /v1/tenants/:tenantId/test-runs/:testRunId/decision-data-by-score`

What it does:

- returns summary counts grouped by outcome and score

How it should be used:

- compare how often scores shifted between live and phantom behavior
- spot threshold drift

### `GET /v1/tenants/:tenantId/test-runs/:testRunId/data-by-rule-execution`

What it does:

- returns per-rule hit/no-hit/snoozed statistics for the run

How it should be used:

- understand which rules are driving the differences
- identify silent or overfiring rules
