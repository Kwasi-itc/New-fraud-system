# Screening Redoc Checklist

This checklist tracks the OpenAPI and Redoc work for `screening-service`.

## 1. Redoc and docs routes

- [x] Add `GET /redoc`
- [x] Keep `GET /docs` and `GET /openapi.yaml`
- [x] Simplify the docs-page intro text

## 2. Route coverage

- [x] Add provider catalog and freshness routes
- [x] Add dataset-update job routes
- [x] Add screening create, freeform, get, list, and retry routes
- [x] Add screening match review, comment, and enrich routes
- [x] Add screening file and file-upload routes
- [x] Add whitelist search, create, and delete routes
- [x] Add continuous-screening config routes
- [x] Add monitored-object routes
- [x] Add internal decision-screening intake route

## 3. Descriptions and query parameters

- [x] Add plain-English tag descriptions
- [x] Add plain-English route descriptions for the main screening flows
- [x] Document whitelist query parameters
- [x] Document provider query parameters

## 4. Schema coverage

- [x] Add request schemas for screening, whitelist, file, continuous-screening, monitored-object, and dataset-update flows
- [x] Keep response schemas pragmatic where the service returns flexible domain JSON directly
- [x] Preserve the existing screening detail schema

## 5. Verification

- [x] Compare `router.go` against `openapi.yaml`
- [x] Verify `/openapi.yaml`, `/docs`, and `/redoc` locally
