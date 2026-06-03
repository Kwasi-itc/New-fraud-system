# Screening Service Rollout Checklist

This checklist is for closing the remaining rollout work after the repo implementation was completed.

## 1. Migration confirmation

Confirm the screening database is on the latest schema:

- migration `000001_init`
- migration `000002_continuous_and_files`
- migration `000003_decision_engine_and_idempotency`

Evidence to capture:

- migration command used
- target database URL or environment name
- output showing all three migrations applied

## 2. Required environment wiring

### `screening-service`

- `DATABASE_URL`
- `SCREENING_PROVIDER_URL` or `SCREENING_PROVIDER_URLS`
- `INGESTION_SERVICE_URL`
- `INBOX_SERVICE_URL`
- `CASE_SERVICE_URL`
- `BLOB_SERVICE_URL`
- `DECISION_ENGINE_URL`
- `SERVICE_AUTH_MODE`
- `SERVICE_AUTH_TOKEN`

### `decision-engine-service`

- `DATABASE_URL`
- `DATA_MODEL_SERVICE_URL`
- `INGESTION_SERVICE_URL`
- `SCREENING_SERVICE_URL`
- `SERVICE_AUTH_MODE`
- `SERVICE_AUTH_TOKEN`

## 3. Service startup confirmation

Confirm all participating services are running and healthy:

- `decision-engine-service`
- `screening-service`
- provider endpoint selected by `SCREENING_PROVIDER_URLS`
- `ingestion-service`
- `case-service`
- `blob-service`
- `inbox-service`

Capture:

- `/healthz`
- `/readyz`
- effective env values or deployment manifests for the URLs above

## 4. End-to-end flow validation

Prove the live chain below with one real scenario and one real tenant object:

1. `decision-engine-service -> screening-service`
2. `screening-service -> provider`
3. `screening-service -> ingestion-service`
4. `screening-service -> case-service`
5. `screening-service -> blob-service`
6. `screening-service -> decision-engine-service` callback

Evidence to capture:

- decision-engine worker log showing dispatch to `/internal/v1/tenants/:tenantId/decision-screenings`
- screening-service intake log with request ID
- provider request/response correlation
- stored screening record and match rows
- callback log for `POST /internal/screening-status-updates`
- case-side effect evidence after review or file upload
- blob upload session creation and download URL retrieval

## 5. Provider dataset delta validation

Validate the real provider integration, not only the adapter shape:

- catalog endpoint returns expected datasets
- freshness endpoint returns expected timestamps or versions
- dataset update job can advance from `pending` to `completed`
- delta processing correctly re-screens monitored objects touched by the changed dataset
- retry behavior works for failed dataset update jobs

Evidence to capture:

- one completed dataset update job record
- one monitored-object re-screen triggered by that job
- provider payload or logs proving the expected dataset delta was consumed

## 6. Operational adoption checks

Before calling rollout complete, confirm:

- retry of failed screenings works
- retry of failed dataset update jobs works
- backlog can drain after the worker is restarted
- provider outage produces recoverable `failed` state with usable `last_error`
- token auth succeeds for both intake and callback directions when enabled

## 7. Closure criteria

Do not mark the extraction fully complete until there is live evidence for all of the following:

- screening traffic is entering `screening-service` instead of the old monolith/runtime path
- decision-engine status tracking is being updated via `/internal/screening-status-updates`
- provider-backed screenings complete through the new service boundary
- case and blob side effects execute from `screening-service`
- migration `000003_decision_engine_and_idempotency` is applied in the target environment

If any item above is missing, the repo implementation may still be complete, but rollout proof is not.
