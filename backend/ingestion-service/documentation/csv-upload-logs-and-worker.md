# CSV Upload Logs And Worker Flow

This document explains the CSV ingestion endpoints and the upload-log lifecycle in the standalone ingestion service.

## Purpose

This endpoint group is where the service accepts CSV uploads, persists upload-log state, and processes those uploads asynchronously through the worker flow.

These routes are defined in [internal/httpapi/router.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/httpapi/router.go), handled in [internal/httpapi/handlers/upload_log.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/httpapi/handlers/upload_log.go), and orchestrated by [internal/service/upload_log_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/service/upload_log_service.go).

## Endpoint Group

Current CSV and upload-log routes:

- `POST /v1/tenants/:tenantId/ingest/:objectType/csv`
- `GET /v1/tenants/:tenantId/ingest/:objectType/upload-logs`
- `GET /v1/upload-logs/:uploadLogId`

## Shared Route Parameters

- `tenantId`
  - the tenant that owns the target object type and upload logs
  - must be a valid UUID
- `objectType`
  - the logical object/table name the CSV rows should be ingested into
  - must exist in the tenant's published model contract
- `uploadLogId`
  - the id of one persisted CSV upload-log record

## Design Intent

The CSV path exists so callers can submit larger ingestion workloads without forcing them through one huge synchronous JSON request.

The service owns:

- accepting the uploaded file
- storing upload metadata and payload
- exposing upload-log status
- processing uploaded logs through the worker
- retrying transient failures up to a configured cap

The service does not currently expose a separate queue product or dead-letter subsystem. Upload logs themselves are the V1 operational record.

## Create CSV Upload

Endpoint:

- `POST /v1/tenants/:tenantId/ingest/:objectType/csv`

### Parameters

- path `tenantId`
  - target tenant for CSV processing
- path `objectType`
  - target object type for the uploaded rows
- form field `file`
  - required uploaded CSV file
- query `mode`
  - optional ingest mode override
  - `patch` means partial-update semantics
  - omitted means full-write create semantics

### Request Shape

This endpoint expects multipart form upload with:

- form field `file`

Optional query parameter:

- `mode=patch`

If `mode` is omitted, the upload is processed as full-write create semantics.

### Immediate Behavior

On acceptance, the service:

- reads the uploaded file payload
- creates an upload log
- stores the payload in PostgreSQL
- marks the new log as `uploaded`

Response code:

- `202 Accepted`

### Response Shape

Example:

```json
{
  "upload_log": {
    "id": "c5c4b87c-2a08-4d9a-8f0e-f5a5b0d4fb44",
    "tenant_id": "11111111-1111-1111-1111-111111111111",
    "object_type": "transactions",
    "mode": "create",
    "filename": "transactions.csv",
    "content_type": "text/csv",
    "status": "uploaded",
    "total_rows": 0,
    "successful_rows": 0,
    "failed_rows": 0,
    "attempt_count": 0
  }
}
```

## Upload-Log Reads

### List Upload Logs

Endpoint:

- `GET /v1/tenants/:tenantId/ingest/:objectType/upload-logs`

### Parameters

- path `tenantId`
  - tenant whose upload logs should be listed
- path `objectType`
  - object type whose upload logs should be listed

This returns the upload logs for one tenant/object-type boundary.

Use this for:

- operational visibility
- UI status polling
- checking whether an upload completed or failed

### Get One Upload Log

Endpoint:

- `GET /v1/upload-logs/:uploadLogId`

### Parameters

- path `uploadLogId`
  - id of the upload-log record to fetch

This returns one upload log by id.

## Upload-Log Lifecycle

Primary domain statuses are defined in [internal/domain/ingestion/validation.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/domain/ingestion/validation.go):

- `pending`
- `uploaded`
- `processing`
- `completed`
- `failed`

Practical V1 flow today:

- new CSV uploads are created as `uploaded`
- the worker claims one uploaded log and transitions it to `processing`
- if parsing and batch ingestion succeed, it becomes `completed`
- if a validation failure occurs, it becomes terminal `failed`
- if a retryable processing failure occurs, it returns to `uploaded` until max attempts are exhausted
- after max attempts, it becomes terminal `failed`

The response surface also exposes:

- `total_rows`
- `successful_rows`
- `failed_rows`
- `attempt_count`
- `error_message`

## Worker Behavior

Primary implementation: [internal/service/upload_log_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/service/upload_log_service.go)

Current worker flow:

- claim next uploaded log
- parse CSV header row into field names
- convert rows into the same record shape used by JSON batch ingest
- run `BatchIngest`
- update counters and final status

Current retry behavior:

- malformed CSV or transient service failures are retryable
- validation failures are terminal immediately
- retries are capped by `maxAttempts` in the upload-log service

This is intentionally simple V1 worker behavior rather than a separate queue subsystem.

## CSV Contract Assumption

Current CSV parsing assumes:

- first row contains headers
- headers map directly to field names in the published object schema
- blank CSV cells are treated as `null`
- non-blank values are passed as strings into the same normalization path used by JSON ingestion

That means field coercion still happens in the normal validation layer after CSV parsing.

## Overall Intention

This endpoint group is meant to make CSV ingestion operationally visible and retryable without adding a second ingestion model.

The key idea is:

- JSON ingest and CSV ingest should converge on the same validation and write semantics
- upload logs are the durable operational record for async CSV workflows

## Related Files

- [internal/httpapi/router.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/httpapi/router.go)
- [internal/httpapi/handlers/upload_log.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/httpapi/handlers/upload_log.go)
- [internal/service/upload_log_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/service/upload_log_service.go)
- [internal/httpapi/dto/upload_log.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/httpapi/dto/upload_log.go)
