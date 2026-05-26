# Platform Helpers

This document explains the helper-data endpoints used by evaluator functions and platform lookups.

## Endpoint Group

- `GET /v1/tenants/:tenantId/platform/custom-list-entries`
- `POST /v1/tenants/:tenantId/platform/custom-list-entries`
- `GET /v1/tenants/:tenantId/platform/record-tags`
- `POST /v1/tenants/:tenantId/platform/record-tags`
- `POST /v1/tenants/:tenantId/platform/risk-snapshots`
- `GET /v1/tenants/:tenantId/platform/ip-flags`
- `POST /v1/tenants/:tenantId/platform/ip-flags`

Primary files:

- [internal/httpapi/handlers/platform.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/httpapi/handlers/platform.go)
- [internal/service/platform_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/decision-engine-service/internal/service/platform_service.go)

## Parameters

- `tenantId`
  - tenant that owns the helper-data records

Common query parameter meanings:

- custom-list list routes filter by `list_name`
- record-tag list routes filter by `object_type` and `object_id`
- ip-flag list routes filter by `ip_address`

## Request Meanings

- custom list entry requests identify a `list_name` and `value`
- record tag requests identify an `object_type`, `object_id`, and `tag`
- risk snapshot requests identify an object plus a `risk_level`
- IP flag requests identify an `ip_address` and `flag`

## Endpoint Detail

### `GET /v1/tenants/:tenantId/platform/custom-list-entries`

What it does:

- lists entries from a named custom list

How it should be used:

- inspect allowlists, blocklists, or other evaluator-consumed lists

### `POST /v1/tenants/:tenantId/platform/custom-list-entries`

What it does:

- creates one custom-list value

### `GET /v1/tenants/:tenantId/platform/record-tags`

What it does:

- lists tags applied to an object

### `POST /v1/tenants/:tenantId/platform/record-tags`

What it does:

- creates one object tag

How it should be used:

- support evaluator functions or workflow logic that depends on tag state

### `POST /v1/tenants/:tenantId/platform/risk-snapshots`

What it does:

- creates or persists a risk snapshot for one object

How it should be used:

- persist summary risk state that evaluator functions may later read

### `GET /v1/tenants/:tenantId/platform/ip-flags`

What it does:

- lists IP flags for a given address

### `POST /v1/tenants/:tenantId/platform/ip-flags`

What it does:

- creates one IP flag record

How it should be used:

- support IP-based evaluator checks and manual intelligence capture
