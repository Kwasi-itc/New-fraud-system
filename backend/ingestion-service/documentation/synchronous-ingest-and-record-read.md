# Synchronous Ingest And Record Read

This document explains the synchronous JSON ingest and record-read endpoints in the standalone ingestion service.

## Purpose

This endpoint group is where the service accepts tenant-scoped JSON record writes and exposes lightweight record reads against tenant data.

These routes are defined in [internal/httpapi/router.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/httpapi/router.go) and primarily handled in [internal/httpapi/handlers/ingest.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/httpapi/handlers/ingest.go).

The orchestration behind them lives in [internal/service/ingest_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/service/ingest_service.go).

## Endpoint Group

Current synchronous ingest and read routes:

- `POST /v1/tenants/:tenantId/ingest/:objectType`
- `PATCH /v1/tenants/:tenantId/ingest/:objectType`
- `POST /v1/tenants/:tenantId/ingest/:objectType/batch`
- `PATCH /v1/tenants/:tenantId/ingest/:objectType/batch`
- `GET /v1/tenants/:tenantId/records/:objectType`
- `GET /v1/tenants/:tenantId/records/:objectType/search`
- `GET /v1/tenants/:tenantId/records/:objectType/:objectId`

## Shared Route Parameters

These routes reuse the same core parameters:

- `tenantId`
  - the tenant whose published data model and tenant-schema data are being used
  - must be a valid UUID
- `objectType`
  - the logical object/table name being ingested or queried
  - examples: `transactions`, `customers`, `accounts`
  - this must exist in the published model contract for the tenant
- `objectId`
  - the record identifier inside that object type
  - in practice this maps to the platform lookup field, currently `object_id`

## Design Intent

The intent of this service boundary is:

- `data-model-service` owns the published ingestion contract
- `ingestion-service` validates payloads against that contract
- `ingestion-service` writes directly into tenant data tables
- downstream systems consume the resulting records or outbox events

The ingestion service is therefore not a schema-authoring service. It is a contract-enforced write and read boundary over tenant data.

## Shared Validation Behavior

Primary domain logic: [internal/domain/ingestion/validation.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/domain/ingestion/validation.go)

Every synchronous ingest route depends on the published model contract returned by `data-model-service`.

Current validation rules include:

- object type must exist and not be archived
- `object_id` is required
- managed system fields other than `object_id` are rejected
- unknown or archived fields are rejected
- full writes enforce required non-nullable fields
- patch writes allow partial field submission
- enum fields must use managed enum values
- supported field coercions include:
  - `bool`
  - `int`
  - `float`
  - `string`
  - `timestamp`
  - `ip_address`

Validation failures return `422 Unprocessable Entity` with `validation_errors`.

## Single-Record Ingest

Endpoints:

- `POST /v1/tenants/:tenantId/ingest/:objectType`
- `PATCH /v1/tenants/:tenantId/ingest/:objectType`

### What These Do

These endpoints ingest one JSON object at a time for one tenant and one object type.

- `POST` means full-write semantics
- `PATCH` means partial-update semantics

### Parameters

- path `tenantId`
  - target tenant for validation and write
- path `objectType`
  - target object type to ingest into
- header `Idempotency-Key`
  - optional request deduplication key
  - if reused with the same payload, the service replays the successful prior response
  - if reused with a different payload, the service rejects the request

### Request Shape

The request body must be a JSON object.

Example:

```json
{
  "object_id": "txn_1001",
  "status": "pending",
  "amount": 120.45,
  "customer_id": "cust_42"
}
```

### Response Shape

Successful writes return:

- `object_id`
- `action`
- `revision_id`
- `replayed`

Example:

```json
{
  "result": {
    "object_id": "txn_1001",
    "action": "upserted",
    "revision_id": "rev_20260526_01",
    "replayed": false
  }
}
```

### Idempotency

If the caller sends `Idempotency-Key`, the service stores and replays successful responses for identical repeated requests.

Current behavior:

- same key plus same payload replays the prior response
- same key plus different payload returns conflict

Conflict response code:

- `409 Conflict`

## Batch Ingest

Endpoints:

- `POST /v1/tenants/:tenantId/ingest/:objectType/batch`
- `PATCH /v1/tenants/:tenantId/ingest/:objectType/batch`

### What These Do

These endpoints ingest an array of records in one request.

The batch path uses the same validation and upsert rules as single-record ingest, but returns one result per row.

### Parameters

- path `tenantId`
  - target tenant for validation and write
- path `objectType`
  - target object type to ingest into
- header `Idempotency-Key`
  - optional deduplication key for the whole batch request

### Request Shape

The request body must be a JSON array of objects.

Example:

```json
[
  {
    "object_id": "txn_1001",
    "status": "pending",
    "amount": 120.45
  },
  {
    "object_id": "txn_1002",
    "status": "approved",
    "amount": 87.20
  }
]
```

### Response Shape

Example:

```json
{
  "results": [
    {
      "object_id": "txn_1001",
      "action": "upserted",
      "revision_id": "rev_20260526_01",
      "replayed": false
    },
    {
      "object_id": "txn_1002",
      "action": "upserted",
      "revision_id": "rev_20260526_01",
      "replayed": false
    }
  ]
}
```

If any rows fail validation, the batch returns `422` with row-level validation errors instead of a partial success envelope.

## Record Reads

Primary read handlers also live in [internal/httpapi/handlers/ingest.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/httpapi/handlers/ingest.go).

### List Records

Endpoint:

- `GET /v1/tenants/:tenantId/records/:objectType`

Supported query parameters:

- `limit`

Default limit:

- `100`

### Parameters

- path `tenantId`
  - tenant whose stored records should be listed
- path `objectType`
  - object type whose records should be listed
- query `limit`
  - optional max number of records to return
  - defaults to `100`

Use this for a lightweight tenant/object-type inventory read.

### Query Records

Endpoint:

- `GET /v1/tenants/:tenantId/records/:objectType/search`

Supported query parameters:

- `field`
- `value`
- `limit`

### Parameters

- path `tenantId`
  - tenant whose stored records should be queried
- path `objectType`
  - object type to search within
- query `field`
  - required field name to filter on
  - must be a field that exists on the object type
- query `value`
  - value to match against the selected field
- query `limit`
  - optional max number of records to return
  - defaults to `100`

This is the simplest filtered read surface in the service today. It is intended for direct field-equals lookups, not for general-purpose query authoring.

### Get One Record

Endpoint:

- `GET /v1/tenants/:tenantId/records/:objectType/:objectId`

### Parameters

- path `tenantId`
  - tenant whose record should be loaded
- path `objectType`
  - object type that owns the record
- path `objectId`
  - lookup value of the target record

This returns one stored record by object id within one tenant/object-type boundary.

## Overall Intention

This endpoint group is meant to make `ingestion-service` the synchronous write and lookup boundary for tenant records.

That means:

- model validation comes from `data-model-service`
- write semantics live here
- tenant-table persistence lives here
- synchronous consumers do not need to know tenant schema details directly

## Related Files

- [internal/httpapi/router.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/httpapi/router.go)
- [internal/httpapi/handlers/ingest.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/httpapi/handlers/ingest.go)
- [internal/service/ingest_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/service/ingest_service.go)
- [internal/domain/ingestion/validation.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/ingestion-service/internal/domain/ingestion/validation.go)
