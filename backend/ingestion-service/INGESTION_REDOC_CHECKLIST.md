# Ingestion Redoc Checklist

This checklist tracks the OpenAPI and Redoc work for `ingestion-service`.

## 1. Redoc and docs routes

- [x] Add `GET /redoc`
- [x] Keep `GET /docs` and `GET /openapi.yaml`
- [x] Update service docs to mention Redoc

## 2. Route descriptions

- [x] Add clearer descriptions for health and readiness routes
- [x] Add clearer descriptions for single-record ingest routes
- [x] Add clearer descriptions for batch ingest routes already present in OpenAPI
- [x] Add clearer descriptions for CSV upload and upload-log routes
- [x] Add clearer descriptions for aggregate query routes
- [x] Add route descriptions for the record read/search endpoints missing from OpenAPI
- [x] Add route descriptions for `PATCH /v1/tenants/{tenantId}/ingest/{objectType}/batch`

## 3. Field descriptions and examples

- [x] Clarify that `objectType` and `object_type` refer to the tenant data-model table name
- [x] Add field-level descriptions for ingest results, validation errors, upload logs, and aggregate query schemas
- [x] Add consistent examples where those fields appear
- [x] Add any missing request/response examples for newly documented record read/search routes

## 4. Query parameter coverage

- [x] Document existing query behavior already present in OpenAPI
- [x] Document `limit` for `GET /v1/tenants/{tenantId}/records/{objectType}`
- [x] Document `field`, `value`, and `limit` for `GET /v1/tenants/{tenantId}/records/{objectType}/search`

## 5. Public route coverage still missing from OpenAPI

- [x] `PATCH /v1/tenants/{tenantId}/ingest/{objectType}/batch`
- [x] `GET /v1/tenants/{tenantId}/records/{objectType}`
- [x] `GET /v1/tenants/{tenantId}/records/{objectType}/search`
- [x] `GET /v1/tenants/{tenantId}/records/{objectType}/{objectId}`

## 6. Verification

- [x] Compare `router.go` against `openapi.yaml` after the missing routes are added
- [x] Verify `/openapi.yaml`, `/docs`, and `/redoc` locally
