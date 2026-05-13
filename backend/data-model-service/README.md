# Data Model Service

Standalone Go service for tenant-aware data model management, extracted from Marble and rebuilt as an independent module.

Current location in the workspace:

- `new/backend/data-model-service`

This service is intended to be used by another fraud platform as a dedicated schema-management module. It owns metadata, per-tenant physical PostgreSQL schemas, schema mutation workflows, and supporting operational tooling.

## Purpose

The service manages:

- tenant registration and provisioning
- logical data-model metadata
- physical per-tenant PostgreSQL tables and columns
- links and pivots used for higher-level graph navigation
- table display/options metadata
- schema change audit logging
- schema drift reconciliation

The service does not yet manage:

- workflow/scenario/rule dependency registration across external systems
- background index job execution
- rich tenant-scoped authorization
- schema migration history orchestration beyond metadata tables

## Core capabilities

- Gin HTTP API
- PostgreSQL metadata store
- per-tenant PostgreSQL schema provisioning
- table CRUD
- field CRUD
- link CRUD
- pivot CRUD
- table options read and upsert
- assembled data model read endpoint
- schema change log endpoint
- tenant schema migration history endpoint
- transactional metadata + tenant DDL mutations
- bearer-token service auth
- embedded OpenAPI specification
- Swagger-style docs page
- request IDs and structured request logging
- schema reconciliation CLI
- first PostgreSQL-backed integration test path

## Local tooling

Optional local tooling:

- `mise` for loading `.env` automatically into local commands

This service now supports both styles:

- plain `go run` from the service root because the app loads `.env` locally
- `mise exec -- ...` if you want the same workflow style used by `api/`

## Project layout

```text
new/backend/data-model-service/
  cmd/
    migrate/                 metadata migration runner
    reconcile/              schema drift report CLI
    server/                 HTTP service entrypoint
  internal/
    app/                    config and app bootstrap
    domain/
      datamodel/            core domain types and validation
      tenant/               tenant domain types
    httpapi/
      dto/                  API request/response shapes
      handlers/             Gin handlers
    migrations/
      metadata/             SQL migrations for metadata schema
    ports/                  interfaces for repositories, schema manager, transactions
    reconcile/              drift detection logic
    service/                application services / use cases
    store/postgres/         PostgreSQL repositories and transaction manager
    tenantdb/postgres/      per-tenant schema DDL manager
  DATA_MODEL_SERVICE_EXTRACTION_DESIGN.md
  DATA_MODEL_SERVICE_IMPLEMENTATION_BLUEPRINT.md
  IMPLEMENTATION_TODO.md
  docker-compose.yml
  Dockerfile
  Makefile
```

## Architecture

The service is split into a few layers:

- `domain`
  - core entities such as tenants, tables, fields, links, pivots, and table options
  - validation and naming rules
- `service`
  - orchestration of business operations
  - dry-run delete conflict checks
  - schema change audit creation
  - transactional mutation handling
- `store/postgres`
  - metadata persistence
  - transaction manager
- `tenantdb/postgres`
  - physical tenant schema DDL
  - table creation, column creation, and unique index management
- `httpapi`
  - transport layer
  - auth middleware
  - health and operational routes
- `reconcile`
  - compares metadata expectations to physical tenant schemas

## Metadata model

The metadata database currently contains these primary tables:

- `core.tenants`
- `core.model_tables`
- `core.model_fields`
- `core.model_links`
- `core.model_pivots`
- `core.table_options`
- `core.schema_change_log`
- `core.tenant_schema_migrations`
- `core.index_jobs`

## Tenant physical schema model

Each tenant gets its own PostgreSQL schema, for example:

- `tenant_<uuid-without-dashes>`

Each managed tenant table currently includes implicit physical columns:

- `id`
- `object_id`
- `updated_at`
- `valid_from`
- `valid_until`

Reserved field names are blocked at the metadata layer so logical metadata cannot drift from this physical layout.

## API summary

Public operational routes:

- `GET /healthz`
- `GET /readyz`
- `GET /openapi.yaml`
- `GET /docs`

Authenticated `/v1` routes:

- `POST /v1/tenants`
- `GET /v1/tenants`
- `GET /v1/tenants/:tenantId`
- `POST /v1/tenants/:tenantId/provision`
- `GET /v1/tenants/:tenantId/data-model`
- `POST /v1/tenants/:tenantId/tables`
- `PATCH /v1/tables/:tableId`
- `DELETE /v1/tables/:tableId?dry_run=true`
- `POST /v1/tables/:tableId/fields`
- `PATCH /v1/fields/:fieldId`
- `DELETE /v1/fields/:fieldId?dry_run=true`
- `POST /v1/tenants/:tenantId/links`
- `DELETE /v1/links/:linkId?dry_run=true`
- `GET /v1/tenants/:tenantId/pivots`
- `POST /v1/tenants/:tenantId/pivots`
- `DELETE /v1/pivots/:pivotId?dry_run=true`
- `GET /v1/tables/:tableId/options`
- `PUT /v1/tables/:tableId/options`
- `GET /v1/tenants/:tenantId/schema-change-log`
- `GET /v1/tenants/:tenantId/schema-migrations`
- `GET /v1/admin/reconcile`

## Auth

Supported service auth modes:

- `disabled`
- `token`

When `SERVICE_AUTH_MODE=token`, all `/v1` routes require:

```http
Authorization: Bearer <SERVICE_AUTH_TOKEN>
```

Health routes remain unauthenticated.

## OpenAPI and Swagger

The service now exposes:

- raw OpenAPI spec: `GET /openapi.yaml`
- browser docs page: `GET /docs`

The docs page loads a Swagger UI frontend and points it at the embedded OpenAPI document.

## Transactions and mutation behavior

Write operations are designed so metadata writes and tenant physical DDL execute within one PostgreSQL transaction boundary where possible.

This is important for operations like:

- create table
- create field
- update field uniqueness
- delete field
- delete table

If physical DDL fails during a transactional mutation, metadata changes should roll back with it.

## Delete behavior

Current V1 delete behavior is:

- hard delete for tables, fields, links, and pivots
- dry-run conflict reporting before destructive execution
- internal conflict checks only

Current internal conflicts include cases such as:

- reserved fields
- fields referenced by links
- fields referenced by pivots
- links referenced by pivot paths
- tables still referenced by links or pivots

Archive or soft-delete semantics are not part of the current V1 contract. They should only be added if the downstream product requires history-preserving destructive changes.

## Schema reconciliation

The service includes a reconciliation CLI and admin API that compare:

- metadata tables in `core.*`
- physical tenant schemas and tables in PostgreSQL

This is intended to detect drift such as:

- metadata table exists but physical table is missing
- physical table exists but metadata does not
- expected columns are missing
- unexpected columns exist

CLI:

```bash
go run ./cmd/reconcile
```

HTTP:

- `GET /v1/admin/reconcile`

## Tenant schema migration history

Physical-schema-affecting operations record a tenant migration-history row in `core.tenant_schema_migrations`.

Examples:

- `create_tenant:tenant`
- `provision_tenant_schema:tenant`
- `create_table:table`
- `create_field:field`
- `update_field:field`
- `delete_field:field`
- `delete_table:table`

HTTP:

- `GET /v1/tenants/:tenantId/schema-migrations`

## Logging

The service emits:

- structured startup logs
- structured request logs
- request IDs via `X-Request-ID`

If the client does not provide `X-Request-ID`, the service generates one.

## Data types

Current supported field data types:

- `bool`
- `int`
- `float`
- `string`
- `timestamp`
- `ip_address`

Not yet supported in the current implementation:

- `coords`

## Current operational limits

- Dockerized local PostgreSQL works, but local service process detachment may behave differently on Windows shells
- no external dependency registry for downstream workflows/rules/scenarios
- no background worker for `core.index_jobs`
- no metrics/tracing yet
- auth is static bearer-token based, not user/tenant aware
- broader request/response integration coverage is still pending

## Development workflow

Typical local flow:

1. Start PostgreSQL
2. Run metadata migrations
3. Start the API
4. Call tenant create + provision
5. Create tables/fields/links/pivots
6. Run reconcile if needed

If you prefer `mise`, the equivalent command style is:

```bash
mise exec -- go run ./cmd/migrate up
mise exec -- go run ./cmd/server
```

Detailed setup steps are in [SETUP_AND_RUN_GUIDE.md](./SETUP_AND_RUN_GUIDE.md).

## Key docs

- [SETUP_AND_RUN_GUIDE.md](./SETUP_AND_RUN_GUIDE.md)
- [DATA_MODEL_SERVICE_EXTRACTION_DESIGN.md](./DATA_MODEL_SERVICE_EXTRACTION_DESIGN.md)
- [DATA_MODEL_SERVICE_IMPLEMENTATION_BLUEPRINT.md](./DATA_MODEL_SERVICE_IMPLEMENTATION_BLUEPRINT.md)
- [IMPLEMENTATION_TODO.md](./IMPLEMENTATION_TODO.md)
