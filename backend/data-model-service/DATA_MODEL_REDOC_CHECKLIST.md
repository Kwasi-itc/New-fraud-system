# Data Model Redoc Checklist

This checklist tracks the OpenAPI and Redoc work for `data-model-service`.

## 1. Redoc and docs routes

- [x] Add `GET /redoc`
- [x] Keep `GET /docs` and `GET /openapi.yaml`
- [x] Update service docs to mention Redoc

## 2. Route descriptions

- [x] Add clearer descriptions for tenant lifecycle endpoints
- [x] Add clearer descriptions for assembled model import/export endpoints
- [x] Add clearer descriptions for table, field, enum, link, pivot, table-option, and navigation-option endpoints
- [x] Add clearer descriptions for schema-history and index-job endpoints
- [x] Keep descriptions focused on caller-facing behavior rather than internal detail

## 3. Field descriptions and examples

- [x] Add field-level descriptions for key parameters and schemas
- [x] Clarify that assembled table names are the downstream object types
- [x] Add examples for key request and response payloads
- [x] Keep examples realistic and aligned with actual service behavior

## 4. Coverage and consistency

- [x] Check that live router routes are represented in OpenAPI
- [x] Fix stale prose route references in supporting docs
- [x] Review docs for obvious wording inconsistencies

## 5. Verification

- [x] Verify `/openapi.yaml`, `/docs`, and `/redoc` locally
