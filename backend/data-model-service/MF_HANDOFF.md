# MF Handoff

This file is a compact handoff for the standalone data model service in `new/backend/data-model-service`.

It is intended to help a new session or a new engineer quickly understand:

- what has already been built
- what is still incomplete
- what the main design decisions are
- where to continue next

## What this project is

This is a standalone Go backend extracted from Marble’s data model area and rebuilt as an independent service.

The target purpose is:

- run in its own Docker container
- own data model metadata
- own per-tenant PostgreSQL physical schema creation and mutation
- expose an API another fraud system can call

This service is not the whole fraud platform. It is the schema/data-model subsystem.

## Current status

The core platform is implemented.

The service is usable for:

- tenant creation
- tenant provisioning
- per-tenant schema creation
- table CRUD
- field CRUD
- link CRUD
- pivot CRUD
- table options read and upsert
- assembled data model reads
- schema change logging
- tenant schema migration history logging
- schema reconciliation
- token-based service auth
- OpenAPI/Swagger documentation

The project is not fully production-complete yet. There are still hardening and lifecycle features left.

## Repo structure

```text
new/backend/data-model-service/
  cmd/
    migrate/                 metadata migration runner
    reconcile/              drift report CLI
    server/                 HTTP API entrypoint
  docs/
    openapi.yaml            main OpenAPI source
  internal/
    app/                    config and bootstrap
    domain/
      datamodel/            core domain models and validation
      tenant/               tenant model and validation
    httpapi/
      dto/                  request/response DTOs
      handlers/             Gin handlers
      docs.go               embedded docs routes
      openapi.yaml          embedded copy served by API
      router.go             route registration
    migrations/
      metadata/             SQL metadata schema migrations
    ports/                  interfaces
    reconcile/              drift detection
    service/                use cases / orchestration
    store/postgres/         metadata repositories and transactions
    tenantdb/postgres/      per-tenant physical DDL
  README.md
  SETUP_AND_RUN_GUIDE.md
  IMPLEMENTATION_TODO.md
  DATA_MODEL_SERVICE_EXTRACTION_DESIGN.md
  DATA_MODEL_SERVICE_IMPLEMENTATION_BLUEPRINT.md
```

## Main features already implemented

### 1. Service scaffold

- Gin server
- config loader
- Dockerfile
- docker-compose
- Makefile
- migration runner

### 2. Metadata schema

Metadata tables created in `core`:

- `core.tenants`
- `core.model_tables`
- `core.model_fields`
- `core.model_links`
- `core.model_pivots`
- `core.table_options`
- `core.schema_change_log`
- `core.tenant_schema_migrations`
- `core.index_jobs`

### 3. Tenant lifecycle

Implemented:

- create tenant
- get tenant
- list tenants
- provision tenant schema

Provisioning creates a physical PostgreSQL schema like:

- `tenant_<uuid-without-dashes>`

### 4. Per-tenant physical schema behavior

Implemented physical table structure currently includes:

- `id`
- `object_id`
- `updated_at`
- `valid_from`
- `valid_until`

Reserved field names are blocked in metadata so the logical model does not drift from this physical layout.

### 5. Data model operations

Implemented:

- create/update/delete table
- create/update/delete field
- create/delete link
- create/list/delete pivot
- get/upsert table options
- get assembled data model

Delete paths support dry-run conflict checks for internal dependencies.

### 6. Transactions

Important design point:

- metadata writes and physical tenant DDL now execute through a transaction manager

This means write paths are no longer best-effort split operations.

### 7. Schema audit and history

Implemented:

- schema change log recording
- `GET /v1/tenants/:tenantId/schema-change-log`

Implemented:

- tenant schema migration history recording
- `GET /v1/tenants/:tenantId/schema-migrations`

Schema migration versions currently use simple operation/resource strings like:

- `create_table:table`
- `create_field:field`
- `delete_table:table`

### 8. Reconciliation

Implemented:

- CLI: `go run ./cmd/reconcile`
- API: `GET /v1/admin/reconcile`

Purpose:

- compare metadata expectations with actual tenant schemas/tables/columns
- detect drift

### 9. Auth

Implemented:

- `SERVICE_AUTH_MODE=disabled`
- `SERVICE_AUTH_MODE=token`
- `SERVICE_AUTH_TOKEN=<secret>`

Behavior:

- health routes remain public
- `/v1/*` routes require bearer token when token mode is enabled

### 10. Docs

Implemented:

- detailed README
- setup/run guide
- OpenAPI 3.0.3 spec
- raw spec route: `GET /openapi.yaml`
- Swagger-style docs page: `GET /docs`

Note:

- `/docs` currently loads Swagger UI assets from a CDN
- `/openapi.yaml` is fully local

## Test state

Implemented test coverage includes:

- domain validation tests
- middleware/auth tests
- handler validation tests
- transaction-related service tests
- reconciliation tests
- first PostgreSQL-backed integration test path

Important integration test already added:

- rollback behavior when a field uniqueness change fails during index creation

Normal suite:

```powershell
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
go test ./...
```

Integration path:

```powershell
$env:DATA_MODEL_TEST_DATABASE_URL='postgres://datamodel:datamodel@localhost:5434/datamodel?sslmode=disable'
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
go test -run Integration ./...
```

## Important operational notes

### Local runtime

On this machine, the most reliable local run mode is interactive:

1. `docker compose up -d postgres`
2. `go run ./cmd/migrate up`
3. `go run ./cmd/server`

Detached Windows background launch was inconsistent during testing. The service initializes, but staying attached in detached shell mode was not reliable from this environment.

### Docker

Local Postgres through Docker works.

Sometimes Docker access from the shell required elevated execution.

### Go cache

Use service-local `GOCACHE` to avoid Windows permission problems:

```powershell
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
```

## Major design decisions already made

### Standalone service boundary

Chosen approach:

- standalone HTTP service
- not a shared library as the primary integration model

Reason:

- cleaner ownership
- independent deployment
- simpler multi-system integration

### Framework

Chosen:

- Gin

Reason:

- consistent with Marble team familiarity

### Database

Chosen:

- PostgreSQL

Reason:

- needed for real per-tenant schema mutation behavior similar to Marble

### Scope

Included in current service:

- data model metadata
- physical tenant schemas
- schema mutation lifecycle

Explicitly not implemented as part of this service yet:

- downstream workflow/rule dependency registry
- AI features
- broader fraud-platform orchestration

## What is still left

These are the main unfinished items.

### High priority

- broader PostgreSQL integration coverage across create/delete/schema-change flows
- repository-level DB tests
- archival or soft-delete semantics for destructive changes

Why these matter:

- they increase confidence in real database behavior
- they protect lifecycle correctness when deleting or changing schemas

### Medium priority

- stronger authorization model beyond static bearer token
- startup/readiness hardening around schema state

### Platform/documentation maturity

- keep OpenAPI spec synchronized as routes evolve
- optionally make Swagger UI fully offline instead of CDN-based
- metrics
- tracing

### Product-scope expansion

- external dependency-awareness model for downstream systems
- more data types such as `coords`

## Recommended next steps

If a new session is picking this up, the best next order is:

1. Expand PostgreSQL-backed integration coverage
2. Add repository-level DB tests
3. Design and implement archival/soft-delete semantics
4. Verify async index flows with `cmd/worker` and `cmd/reconcile`
5. Improve auth/authorization if multi-user or multi-tenant security requirements are expected soon

## If the next task is “run the system”

Use:

- [SETUP_AND_RUN_GUIDE.md](./SETUP_AND_RUN_GUIDE.md)

The short version:

1. `docker compose up -d postgres`
2. set `GOCACHE`
3. set `DATABASE_URL`
4. `go run ./cmd/migrate up`
5. `go run ./cmd/server`
6. verify:
   - `GET /healthz`
   - `GET /readyz`
   - `GET /docs`

## If the next task is “understand the API”

Use:

- `GET /openapi.yaml`
- `GET /docs`
- [README.md](./README.md)

## If the next task is “understand architecture”

Use:

- [DATA_MODEL_SERVICE_EXTRACTION_DESIGN.md](./DATA_MODEL_SERVICE_EXTRACTION_DESIGN.md)
- [DATA_MODEL_SERVICE_IMPLEMENTATION_BLUEPRINT.md](./DATA_MODEL_SERVICE_IMPLEMENTATION_BLUEPRINT.md)

## If the next task is “know what is unfinished”

Use:

- [IMPLEMENTATION_TODO.md](./IMPLEMENTATION_TODO.md)

## Git context

This standalone service was initialized and pushed as its own repository, not as part of the parent Marble repo.

Remote:

- `git@github.com:Kwasi-itc/New-fraud-system.git`

Branch:

- `main`

## Final summary

The standalone data model service is past the extraction/scaffold phase.

It already supports the core data-model lifecycle and can be treated as a working standalone module.

The biggest remaining work is now hardening and lifecycle depth, not basic platform creation.
