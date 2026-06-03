# Screening Service Session Handoff

## Status

`screening-service` is now implemented as a standalone backend module with the main V1 screening surface in place.

The service is no longer planning-only. It includes:

- API server shell
- worker shell
- migrations
- postgres persistence
- provider integration layer
- ingestion, inbox, case, blob, and decision-engine integration ports
- tests across service, handler, and repository layers

Current verification state:

- `go test ./...` passes in `new/backend/screening-service`

## What Has Been Implemented

### Core screening flows

- screening intake
- freeform screening intake
- screening detail lookup
- list screenings by decision
- retry failed screening
- screening dispatch worker
- raw provider payload persistence
- normalized match persistence

### Match review flows

- review screening match
- add match comment
- enrich match from provider
- screening whitelist create, delete, and search

### Evidence flows

- file metadata persistence
- file listing and detail lookup
- blob upload session creation
- blob download URL retrieval
- case-side-effect publish on evidence upload

### Continuous screening flows

- continuous-screening config CRUD
- monitored-object CRUD
- monitored-object requeue
- ingestion-backed monitored-object worker

### Dataset update flows

- dataset catalog endpoint
- dataset freshness endpoint
- dataset update job CRUD
- dataset update worker lifecycle
- monitored-object re-screening on dataset update jobs
- dataset delta sync support

### Integration contracts

- inbox validation for continuous-screening configs
- case publish on screening review
- case publish on evidence upload
- decision-engine callback publish on screening status changes
- dedicated internal intake contract for decision-engine-triggered screening
- provider routing by provider key
- idempotent intake via `idempotency_key`

### Observability and hardening

- request ID middleware
- structured request logging
- worker phase logging
- in-process JSON metrics endpoint
- README operating notes
- service tests
- handler tests
- repository tests

## Main Files and Areas

### Runtime entrypoints

- [cmd/server/main.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/cmd/server/main.go)
- [cmd/worker/main.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/cmd/worker/main.go)
- [cmd/migrate/main.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/cmd/migrate/main.go)

### Core service layer

- [internal/service/screening_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/service/screening_service.go)
- [internal/service/dispatch_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/service/dispatch_service.go)
- [internal/service/continuous_worker.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/service/continuous_worker.go)
- [internal/service/dataset_update_worker.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/service/dataset_update_worker.go)

### HTTP layer

- [internal/httpapi/router.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/httpapi/router.go)
- [internal/httpapi/handlers/screening.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/httpapi/handlers/screening.go)
- [internal/httpapi/handlers/internal_decision.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/httpapi/handlers/internal_decision.go)
- [internal/httpapi/handlers/continuous.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/httpapi/handlers/continuous.go)
- [internal/httpapi/handlers/dataset_updates.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/httpapi/handlers/dataset_updates.go)
- [internal/httpapi/handlers/provider.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/httpapi/handlers/provider.go)

### Persistence and migrations

- [internal/store/postgres/screening_repository.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/store/postgres/screening_repository.go)
- [internal/migrations/metadata/000001_init.up.sql](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/migrations/metadata/000001_init.up.sql)
- [internal/migrations/metadata/000002_continuous_and_files.up.sql](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/migrations/metadata/000002_continuous_and_files.up.sql)
- [internal/migrations/metadata/000003_decision_engine_and_idempotency.up.sql](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/migrations/metadata/000003_decision_engine_and_idempotency.up.sql)

### Integration clients

- [internal/clients/provider/http_client.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/clients/provider/http_client.go)
- [internal/clients/ingestion/http_client.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/clients/ingestion/http_client.go)
- [internal/clients/inbox/http_client.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/clients/inbox/http_client.go)
- [internal/clients/case/http_client.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/clients/case/http_client.go)
- [internal/clients/blob/http_client.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/clients/blob/http_client.go)
- [internal/clients/decisionengine/http_client.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/internal/clients/decisionengine/http_client.go)

## Endpoint Surface Implemented

### Health and metrics

- `GET /healthz`
- `GET /readyz`
- `GET /metrics`

### Screening APIs

- `POST /v1/tenants/:tenantId/screenings`
- `POST /v1/tenants/:tenantId/screenings/freeform-search`
- `GET /v1/tenants/:tenantId/decisions/:decisionId/screenings`
- `GET /v1/tenants/:tenantId/screenings/:screeningId`
- `POST /v1/tenants/:tenantId/screenings/:screeningId/retry`
- `POST /v1/tenants/:tenantId/screening-matches/:matchId/review`
- `POST /v1/tenants/:tenantId/screening-matches/:matchId/comments`
- `POST /v1/tenants/:tenantId/screening-matches/:matchId/enrich`

### Files

- `POST /v1/tenants/:tenantId/screenings/:screeningId/files`
- `POST /v1/tenants/:tenantId/screenings/:screeningId/file-uploads`
- `GET /v1/tenants/:tenantId/screenings/:screeningId/files`
- `GET /v1/tenants/:tenantId/screenings/:screeningId/files/:fileId`
- `GET /v1/tenants/:tenantId/screenings/:screeningId/files/:fileId/download`

### Whitelist

- `GET /v1/tenants/:tenantId/screening-whitelist`
- `POST /v1/tenants/:tenantId/screening-whitelist`
- `DELETE /v1/tenants/:tenantId/screening-whitelist`

### Continuous screening

- `GET /v1/tenants/:tenantId/continuous-screening-configs`
- `POST /v1/tenants/:tenantId/continuous-screening-configs`
- `GET /v1/tenants/:tenantId/continuous-screening-configs/:configId`
- `PUT /v1/tenants/:tenantId/continuous-screening-configs/:configId`
- `DELETE /v1/tenants/:tenantId/continuous-screening-configs/:configId`
- `GET /v1/tenants/:tenantId/continuous-screening-configs/:configId/monitored-objects`
- `POST /v1/tenants/:tenantId/continuous-screening-configs/:configId/monitored-objects`
- `GET /v1/tenants/:tenantId/monitored-objects/:monitoredObjectId`
- `DELETE /v1/tenants/:tenantId/monitored-objects/:monitoredObjectId`
- `POST /v1/tenants/:tenantId/monitored-objects/:monitoredObjectId/requeue`

### Provider and dataset jobs

- `GET /v1/screening-provider/catalog`
- `GET /v1/screening-provider/freshness`
- `GET /v1/tenants/:tenantId/dataset-update-jobs`
- `POST /v1/tenants/:tenantId/dataset-update-jobs`
- `GET /v1/tenants/:tenantId/dataset-update-jobs/:jobId`
- `POST /v1/tenants/:tenantId/dataset-update-jobs/:jobId/retry`

### Internal contract

- `POST /internal/v1/tenants/:tenantId/decision-screenings`

## Environment and Configuration Added

Relevant env vars:

- `DATABASE_URL`
- `SERVICE_AUTH_MODE`
- `SERVICE_AUTH_TOKEN`
- `SCREENING_PROVIDER_URL`
- `SCREENING_PROVIDER_URLS`
- `INGESTION_SERVICE_URL`
- `INBOX_SERVICE_URL`
- `CASE_SERVICE_URL`
- `BLOB_SERVICE_URL`
- `DECISION_ENGINE_URL`
- `HTTP_CLIENT_TIMEOUT`
- `WORKER_MODE`
- `WORKER_POLL_INTERVAL`
- `WORKER_BATCH_LIMIT`

Notes:

- `SCREENING_PROVIDER_URL` is the fallback default provider endpoint
- `SCREENING_PROVIDER_URLS` supports provider-key routing
- `DECISION_ENGINE_URL` is optional, but enables screening status callbacks

## What Is Left

The remaining work is mostly outside the core service implementation.

### Operational / integration follow-up

- run migration `000003_decision_engine_and_idempotency`
- set production env wiring for:
  - `SCREENING_PROVIDER_URLS`
  - `DECISION_ENGINE_URL`
  - other downstream service URLs

### Validation and adoption follow-up

- run end-to-end integration testing across:
  - decision engine -> screening service
  - screening service -> provider
  - screening service -> ingestion
  - screening service -> case
  - screening service -> blob
- validate provider-specific dataset delta semantics against the real provider implementation
- confirm auth mode and token wiring for internal service-to-service calls

### Optional future hardening

- deeper authorization model beyond token auth
- richer metrics export if Prometheus-style scraping is required
- stronger end-to-end tests or docker-compose integration tests
- production runbooks for retry, backlog recovery, and provider outage handling

## Recommended Next Starting Point

For the next session, start here:

1. apply the latest migration in the target environment
2. configure real service URLs and auth tokens
3. run an end-to-end screening execution through both services
4. run one provider dataset update job and confirm monitored-object re-screening
5. capture rollout evidence using `SCREENING_SERVICE_ROLLOUT_CHECKLIST.md`

## Reference Docs

- [README.md](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/README.md)
- [IMPLEMENTATION_TODO.md](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/IMPLEMENTATION_TODO.md)
- [SCREENING_SERVICE_IMPLEMENTATION_BLUEPRINT.md](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/SCREENING_SERVICE_IMPLEMENTATION_BLUEPRINT.md)
- [SCREENING_SERVICE_SOURCE_INVENTORY.md](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/SCREENING_SERVICE_SOURCE_INVENTORY.md)
- [SCREENING_SERVICE_ROLLOUT_CHECKLIST.md](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/SCREENING_SERVICE_ROLLOUT_CHECKLIST.md)
- [SCREENING_SERVICE_OPERATIONS_RUNBOOK.md](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/screening-service/SCREENING_SERVICE_OPERATIONS_RUNBOOK.md)
