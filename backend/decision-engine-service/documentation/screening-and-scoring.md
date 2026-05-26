# Screening And Scoring

This document explains screening config CRUD, scoring config CRUD, execution/request inspection, and provider-result lifecycle updates.

## Endpoint Group

- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId`
- `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId`
- `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId`
- `GET /v1/tenants/:tenantId/decisions/:decisionId/screening-executions`
- `GET /v1/tenants/:tenantId/screening-executions/:executionId`
- `POST /v1/tenants/:tenantId/screening-executions/:executionId/status`
- `POST /v1/tenants/:tenantId/screening-executions/:executionId/retry`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs`
- `POST /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs`
- `GET /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId`
- `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId`
- `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId`
- `GET /v1/tenants/:tenantId/decisions/:decisionId/scoring-requests`
- `GET /v1/tenants/:tenantId/scoring-requests/:requestId`
- `POST /v1/tenants/:tenantId/scoring-requests/:requestId/status`
- `POST /v1/tenants/:tenantId/scoring-requests/:requestId/retry`

Primary files:

- [internal/httpapi/handlers/screening.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/screening.go)
- [internal/httpapi/handlers/scoring.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/scoring.go)

## Shared Parameters

- `tenantId`
  - tenant boundary for configs and provider execution state
- `scenarioId`
  - scenario that owns the config
- `configId`
  - screening or scoring config identifier
- `decisionId`
  - decision whose screening/scoring side effects are being inspected
- `executionId`
  - screening execution identifier
- `requestId`
  - scoring request identifier

## Request Meanings

### Config create/update

Screening config request fields:

- `name`
  - display/authoring name
- `allowed_outcomes`
  - decision outcomes that should trigger screening
- `provider`
  - upstream screening provider name or identifier
- `config_json`
  - provider-specific configuration payload
- `active`
  - whether the config participates in live execution

Scoring config request fields:

- `name`
- `allowed_outcomes`
- `ruleset_ref`
  - scoring ruleset or provider-side scoring reference
- `config_json`
- `active`

### Status update endpoints

Screening/scoring status update request fields:

- `status`
  - lifecycle state such as `pending`, `sent`, `completed`, or `failed`
- `provider_reference`
  - upstream provider job/request identifier
- `response_json`
  - provider result payload persisted by the service
- `last_error`
  - last provider-side or orchestration error text

### Retry endpoints

Retry means:

- clear the current provider-result failure state
- move the execution/request back to pending so the dispatch path can send it again

## Endpoint Detail

### Screening config endpoints

#### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs`

What it does:

- lists screening configurations attached to a scenario

#### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs`

What it does:

- creates a screening config for the scenario

How it should be used:

- declare when a decision should result in a screening request

#### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId`

What it does:

- loads one screening config

#### `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId`

What it does:

- updates one screening config

#### `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/screening-configs/:configId`

What it does:

- deletes one screening config

### Screening execution endpoints

#### `GET /v1/tenants/:tenantId/decisions/:decisionId/screening-executions`

What it does:

- lists screening executions created as side effects of one decision

#### `GET /v1/tenants/:tenantId/screening-executions/:executionId`

What it does:

- loads one screening execution record including provider-state fields

#### `POST /v1/tenants/:tenantId/screening-executions/:executionId/status`

What it does:

- updates screening execution lifecycle and provider result data

How it should be used:

- provider callback handlers
- internal operational correction tools

#### `POST /v1/tenants/:tenantId/screening-executions/:executionId/retry`

What it does:

- resets and requeues a failed or previously sent screening execution

### Scoring config endpoints

#### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs`

What it does:

- lists scoring configs attached to a scenario

#### `POST /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs`

What it does:

- creates one scoring config

How it should be used:

- declare when a decision should generate a scoring request

#### `GET /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId`

What it does:

- loads one scoring config

#### `PUT /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId`

What it does:

- updates one scoring config

#### `DELETE /v1/tenants/:tenantId/scenarios/:scenarioId/scoring-configs/:configId`

What it does:

- deletes one scoring config

### Scoring request endpoints

#### `GET /v1/tenants/:tenantId/decisions/:decisionId/scoring-requests`

What it does:

- lists scoring requests created for one decision

#### `GET /v1/tenants/:tenantId/scoring-requests/:requestId`

What it does:

- loads one scoring request including provider-state fields

#### `POST /v1/tenants/:tenantId/scoring-requests/:requestId/status`

What it does:

- updates scoring request lifecycle and provider result data

#### `POST /v1/tenants/:tenantId/scoring-requests/:requestId/retry`

What it does:

- resets and requeues a failed or previously sent scoring request
