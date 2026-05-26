# Health And Service Metadata

This document explains the health, readiness, docs, and service-metadata routes in the decision engine service.

## Endpoint Group

- `GET /healthz`
- `GET /readyz`
- `GET /v1/service-info`
- `GET /openapi.yaml`
- `GET /docs`

Primary files:

- [internal/httpapi/router.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/router.go)
- [internal/httpapi/handlers/health.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/health.go)
- [internal/httpapi/docs.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/docs.go)

## Route Meanings

### `GET /healthz`

Purpose:

- liveness probe
- answers whether the process is up

Parameters:

- none

How it works:

- the handler returns a simple alive response from the running process
- it does not try to prove upstream dependency readiness

How it should be used:

- Kubernetes or container liveness checks
- external uptime monitors
- a quick check that the process has not crashed

### `GET /readyz`

Purpose:

- readiness probe
- answers whether the service is ready to serve traffic
- checks database readiness when configured

Parameters:

- none

How it works:

- the handler verifies that the service is actually ready to serve requests
- when a database pool is configured, this is the route that reflects DB availability

How it should be used:

- readiness probes before routing traffic
- deployment checks during rollout
- diagnosing whether failures are process-level or dependency-level

### `GET /v1/service-info`

Purpose:

- returns lightweight service metadata
- exposes configured upstream URLs for `data-model-service` and `ingestion-service`

Parameters:

- none beyond normal service auth on `/v1`

How it works:

- the router returns static runtime metadata assembled from current config
- the response includes the service identity plus configured upstream base URLs

How it should be used:

- environment verification
- checking whether a deployment points at the correct `data-model-service` and `ingestion-service`
- quick smoke tests after config changes

### `GET /openapi.yaml`

Purpose:

- returns the embedded OpenAPI spec

Parameters:

- none

How it works:

- the embedded OpenAPI spec is served directly from the binary

How it should be used:

- client generation
- contract review
- source of truth for exact payload schemas

### `GET /docs`

Purpose:

- serves the embedded docs UI page for the OpenAPI spec

Parameters:

- none

How it works:

- serves the human-facing docs UI backed by the embedded OpenAPI spec

How it should be used:

- interactive endpoint exploration
- quick onboarding for engineers without opening the YAML directly
