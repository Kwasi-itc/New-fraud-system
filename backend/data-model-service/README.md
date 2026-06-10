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
- published ingestion-safe tenant schema contracts
- links and pivots used for higher-level graph navigation
- table display/options metadata
- navigation-option metadata
- schema change audit logging
- async secondary index job orchestration
- schema drift reconciliation

The service does not yet manage:

- workflow/scenario/rule dependency registration across external systems
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
- navigation option CRUD
- async index job enqueue/list/get/retry
- assembled data model read endpoint
- schema change log endpoint
- tenant schema migration history endpoint
- transactional metadata + tenant DDL mutations
- bearer-token service auth
- embedded OpenAPI specification
- Swagger-style docs page
- request IDs and structured request logging
- schema reconciliation CLI
- background index job worker
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
    worker/                 async index job worker
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
  - table creation, column creation, unique index management, and managed secondary indexes
- `httpapi`
  - transport layer
  - auth middleware
  - health and operational routes
- `worker`
  - polling runtime for async index jobs
  - apply/fail job state transitions and audit logging
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
- `core.navigation_options`
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

The assembled data-model endpoint publishes these managed system fields as part of the ingestion contract so downstream writers know they are reserved and service-owned.

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
- `GET /v1/tables/:tableId/navigation-options`
- `POST /v1/tables/:tableId/navigation-options`
- `POST /v1/tenants/:tenantId/links`
- `DELETE /v1/links/:linkId?dry_run=true`
- `GET /v1/tenants/:tenantId/pivots`
- `POST /v1/tenants/:tenantId/pivots`
- `DELETE /v1/pivots/:pivotId?dry_run=true`
- `DELETE /v1/navigation-options/:navigationOptionId`
- `GET /v1/tables/:tableId/options`
- `PUT /v1/tables/:tableId/options`
- `GET /v1/tenants/:tenantId/schema-change-log`
- `GET /v1/tenants/:tenantId/schema-migrations`
- `POST /v1/tenants/:tenantId/index-jobs`
- `GET /v1/tenants/:tenantId/index-jobs`
- `GET /v1/index-jobs/:jobId`
- `POST /v1/index-jobs/:jobId/retry`
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

## CORS

Allowed browser origins are configured with `SERVICE_ALLOWED_ORIGINS` as a comma-separated list.

Default local value:

```env
SERVICE_ALLOWED_ORIGINS=http://localhost:3000
```

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

## Published ingestion contract

`GET /v1/tenants/:tenantId/data-model` is the canonical published contract for downstream ingestion.

The response now includes:

- `revision_id` for pinning writes to an exact published tenant schema snapshot
- `ingestion_contract.tenant_status` and `ingestion_contract.writable` so downstream services can reject writes for non-active tenants
- `ingestion_contract.managed_system_fields` so ingestion does not try to overwrite service-owned columns
- `ingestion_contract.record_lookup_field` and `ingestion_contract.partial_updates` to define baseline upsert semantics
- assembled table and field `archived` flags
- enum values inside assembled fields

This service remains the sole schema authority. It publishes the contract and manages tenant physical layout, but it does not expose a business-record write API.

### `revision_id` semantics

`revision_id` is a deterministic identifier for the published tenant schema contract returned by the assembled data-model endpoint.

Current V1 behavior:

- it changes when tenant writability state changes, such as `pending` to `active`
- it changes when the recorded tenant schema migration set changes
- it remains stable for equivalent published state, even if repository row ordering differs

Downstream ingestion should:

- fetch the contract before validating writes
- store the `revision_id` alongside ingestion logs and jobs
- treat a changed `revision_id` as a signal to refresh cached schema assumptions

The V1 contract does not guarantee any semantic decoding of the revision string. Consumers should treat it as an opaque identifier.

### Enum policy for ingestion consumers

Enum values returned in assembled fields are the canonical allowed values for managed enum fields.

Current V1 policy:

- `data-model-service` publishes enum metadata and remains the source of truth
- `ingestion-service` should validate incoming enum values against the published contract
- unseen enum values should be rejected by downstream ingestion unless a separate explicit enum-management flow adds them to the model first

This intentionally avoids Marble's current pattern where ingestion may backfill new enum values into the model asynchronously.

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

## Async index jobs

Optional or heavier secondary-index work is handled through `core.index_jobs` plus the background worker in `cmd/worker`.

Current flow:

- create navigation options or explicitly enqueue index jobs through the API
- run `go run ./cmd/worker` or `make run-worker`
- the worker claims pending jobs, applies managed indexes, retries transient failures with scheduled backoff, marks jobs `applied` or `failed`, and records schema-change audit rows for each transition
- `reconcile` detects missing managed indexes and schedules repair jobs automatically

HTTP:

- `POST /v1/tenants/:tenantId/index-jobs`
- `GET /v1/tenants/:tenantId/index-jobs`
- `GET /v1/index-jobs/:jobId`
- `POST /v1/index-jobs/:jobId/retry`

The reconcile report now includes missing managed-index details and repair-job scheduling counts when drift is detected.

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
- no metrics/tracing yet
- auth is static bearer-token based, not user/tenant aware
- broader request/response integration coverage is still pending

## Development workflow

Typical local flow:

1. Start PostgreSQL
2. Run metadata migrations
3. Start the API
4. Start the worker if you want async index execution
5. Call tenant create + provision
6. Create tables/fields/links/pivots/navigation options
7. Run reconcile if needed

If you prefer `mise`, the equivalent command style is:

```bash
mise exec -- go run ./cmd/migrate up
mise exec -- go run ./cmd/server
mise exec -- go run ./cmd/worker
```

Detailed setup steps are in [SETUP_AND_RUN_GUIDE.md](./SETUP_AND_RUN_GUIDE.md).

## Key docs

- [SETUP_AND_RUN_GUIDE.md](./SETUP_AND_RUN_GUIDE.md)
- [DATA_MODEL_SERVICE_EXTRACTION_DESIGN.md](./DATA_MODEL_SERVICE_EXTRACTION_DESIGN.md)
- [DATA_MODEL_SERVICE_IMPLEMENTATION_BLUEPRINT.md](./DATA_MODEL_SERVICE_IMPLEMENTATION_BLUEPRINT.md)
- [IMPLEMENTATION_TODO.md](./IMPLEMENTATION_TODO.md)
