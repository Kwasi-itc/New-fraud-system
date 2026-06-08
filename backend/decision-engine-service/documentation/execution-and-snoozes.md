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

## How Rule Snoozing Works

Rule snoozing is not a global scenario switch. It is a narrow suppression record checked during decision evaluation.

The effective scope of a snooze is:

- `tenant_id`
- `scenario_id`
- `object_type`
- `object_id`
- `snooze_group_id`

That means a snooze only applies when all of the following are true:

- the decision is being evaluated for the same tenant
- the decision is being evaluated for the same scenario
- the request uses the same `object_type`
- the request uses the same `object_id`
- the matched rule has the same `snooze_group_id`
- the snooze has not expired yet

The intended authoring flow is:

- a rule is created or updated with a `snooze_group_id`
- a rule snooze record is created for an object and the same `snooze_group_id`
- later evaluations of that object under that scenario consult active snoozes before score is finalized

What happens during evaluation:

- the service loads active snoozes for the evaluated object
- it builds the set of active `snooze_group_id` values
- matched rules in those groups are marked as `snoozed`
- snoozed rules do not contribute their score modifier to the final decision score

This is temporary behavior. Once `expires_at` is in the past, the snooze stops applying automatically and the rule behaves normally again.

Validation notes:

- `object_type` is required
- `object_id` is required
- `snooze_group_id` is required
- `expires_at` must be after creation time

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

- lists active snoozes for the scenario and evaluated object filters

How it should be used:

- inspect whether object-level snoozes are suppressing expected rule hits
- confirm that a snooze is still active before investigating why a rule did not affect score

Important detail:

- the endpoint accepts `object_type` and `object_id` query parameters
- the active-list lookup is exact-match for those object values
- expired snoozes are not returned

### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/rule-snoozes`

What it does:

- creates a snooze record for a scenario/object combination

How it works:

- rules that reference the same snooze group can check this record during evaluation
- only evaluations for the same scenario and same object can use this snooze
- the snooze remains active until `expires_at`

How it should be used:

- temporarily suppress noisy or already-reviewed rule behavior on one object

## How To Test Rule Snoozing

Use the same object before and after the snooze so the difference is unambiguous.

Recommended flow:

1. Create or update a rule so it has a `snooze_group_id` and will definitely match a known payload.
2. Commit or publish the scenario iteration so the scenario has a live iteration.
3. Evaluate the scenario for a test object without any snooze.
4. Confirm the rule matches normally and contributes to score.
5. Create a rule snooze for the same `object_type`, `object_id`, and `snooze_group_id`.
6. Evaluate the same scenario again with the same object.
7. Confirm the previously matching rule is now recorded as `snoozed` and no longer contributes to score.
8. Call the list endpoint with `object_type` and `object_id` to confirm the snooze is active.
9. Let the snooze expire, then evaluate again and confirm the rule contributes to score once more.

Recommended checks:

- before snooze: the rule is a normal matched rule and affects score
- after snooze: the same rule execution is marked `snoozed`
- after snooze: the final score is lower if that rule was contributing points
- after expiry: the rule returns to normal behavior

Common testing mistakes:

- creating the snooze under the wrong scenario
- evaluating a different `object_id`
- using a rule that does not have the matching `snooze_group_id`
- setting `expires_at` in the past or too close to current time
- changing the payload object identity between the before and after evaluations

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
