# Tenant Registration And Physical Schema Provisioning

This document explains the tenant endpoints in the data model service, what they do today, and what they are intended to represent in the system design.

## Purpose

The tenant layer does two distinct jobs:

1. register a tenant in metadata
2. provision that tenant's physical PostgreSQL schema

That separation is intentional. A tenant is first created as a metadata record, and only after provisioning does it become physically ready to hold tenant-specific data-model tables.

The tenant routes are defined in [internal/httpapi/router.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/httpapi/router.go):

- `POST /v1/tenants`
- `GET /v1/tenants`
- `GET /v1/tenants/:tenantId`
- `POST /v1/tenants/:tenantId/provision`

## What A Tenant Means

A tenant record contains:

- `id`
- `name`
- optional `external_key`
- `schema_name`
- `status`
- timestamps

The tenant model lives in [internal/domain/tenant/model.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/domain/tenant/model.go), and the API response shape lives in [internal/httpapi/dto/tenant.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/httpapi/dto/tenant.go).

Two important rules:

- new tenants start as `pending`
- the physical schema name is generated from the tenant id as `tenant_<uuid-without-dashes>`

This makes tenancy the top-level boundary for all later table, field, link, and pivot operations.

## POST /v1/tenants

Handler: [internal/httpapi/handlers/tenants.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/httpapi/handlers/tenants.go)  
Service: [internal/service/tenant_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/tenant_service.go)

### Purpose

Creates the tenant metadata record only.

### Example Request

```json
{
  "name": "Fraud Ops",
  "external_key": "fraud-ops"
}
```

### What It Does

- validates the input
- generates a tenant id
- computes the tenant schema name
- stores the tenant in metadata with status `pending`
- records a schema change log entry
- records a tenant schema migration history row

### What It Does Not Do

- it does not create the physical PostgreSQL schema yet

### Why That Separation Exists

- registration and provisioning are different lifecycle steps
- the tenant may need to exist in metadata before physical setup runs
- retries, operational approval, and audit become cleaner when physical provisioning is explicit

## GET /v1/tenants

### Purpose

Lists all registered tenants and their current state.

### Why It Exists

This is mainly an admin and visibility endpoint. It lets you see:

- which tenants exist
- whether they are still `pending` or already `active`
- what schema name each tenant owns

## GET /v1/tenants/:tenantId

### Purpose

Returns one tenant by id.

### Why It Exists

Use this when another service or admin workflow already has the tenant id and needs the tenant's current registration or provisioning state.

## POST /v1/tenants/:tenantId/provision

### Purpose

Creates the physical PostgreSQL schema for that tenant and marks the tenant as active.

This is the second half of tenant onboarding.

### What It Does

Inside the service, it:

- loads the tenant record
- calls the schema manager to provision the tenant schema
- records a schema change event for `provision_tenant_schema`
- records a tenant schema migration row
- updates tenant status from `pending` to `active`

### What It Means Operationally

After provisioning:

- the tenant now has an actual PostgreSQL schema
- the tenant is ready for data-model mutations such as table creation

That is why table creation belongs after provision, not before.

## What We Intend With This Design

The design intent is to make tenant ownership explicit and safe:

- every downstream table or field operation belongs to one tenant
- every tenant gets isolated physical schema space
- metadata and physical schema lifecycle are both auditable
- onboarding is operationally controllable instead of being hidden in first-use side effects

The tenant API is therefore the foundation of the whole service. Before the system can manage tables, fields, links, or pivots, it needs:

- a known tenant in metadata
- a provisioned schema for that tenant

## Practical Flow

1. `POST /v1/tenants`
2. receive a tenant with `status: pending`
3. `POST /v1/tenants/:tenantId/provision`
4. receive a tenant with `status: active`
5. start creating tables and fields for that tenant

## Related Files

- [internal/httpapi/router.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/httpapi/router.go)
- [internal/httpapi/handlers/tenants.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/httpapi/handlers/tenants.go)
- [internal/httpapi/dto/tenant.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/httpapi/dto/tenant.go)
- [internal/service/tenant_service.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/service/tenant_service.go)
- [internal/domain/tenant/model.go](/C:/Users/Kwasi%20Addo/Dev/Work/IT%20Consortium/Marble/marble/new/backend/data-model-service/internal/domain/tenant/model.go)
