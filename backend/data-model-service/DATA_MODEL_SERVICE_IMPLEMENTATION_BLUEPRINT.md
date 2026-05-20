# Data Model Service Implementation Blueprint

## Purpose

This blueprint turns the extraction design into an implementation-ready plan for the standalone service under `new/backend/data-model-service`.

It defines:

- the package tree
- the initial SQL migrations
- the first REST contract
- the first internal interfaces
- the local Docker setup
- the phased implementation order

This document still stops short of code generation. Its purpose is to remove ambiguity before scaffolding begins.

## Scope of V1

V1 will deliver a standalone HTTP service that:

- manages tenants
- manages metadata for tables, fields, links, pivots, and table options
- provisions and mutates per-tenant PostgreSQL schemas
- records schema mutations
- exposes a stable REST API for another fraud system

V1 will not include:

- scenario-aware deletion conflicts
- workflow-aware deletion conflicts
- analytics-aware deletion conflicts
- host fraud engine integration logic
- shared library integration as the primary consumption path

## Recommended Tech Choices

- Go version: `1.24.x` or the repo standard if a stricter version is required
- HTTP router: `gin`
- DB driver: `pgx/v5`
- Querying:
  - use plain SQL or a light builder
  - prefer plain SQL for DDL and critical reads
- Migrations: `golang-migrate`
- Config: environment variables
- Logging: `slog`
- Containerization: Docker

Recommended decision:

- use `gin`
- use `pgxpool`
- use `golang-migrate`
- use plain SQL + small repository methods

Rationale:

- consistent with Marble
- easier handler reuse and team familiarity
- still straightforward for a standalone service

## Proposed Folder Tree

```text
new/backend/data-model-service/
  DATA_MODEL_SERVICE_EXTRACTION_DESIGN.md
  DATA_MODEL_SERVICE_IMPLEMENTATION_BLUEPRINT.md
  cmd/
    server/
      main.go
    migrate/
      main.go
    reconcile/
      main.go
  internal/
    app/
      app.go
      config.go
      wiring.go
    domain/
      tenant/
        model.go
        errors.go
        validation.go
      datamodel/
        model.go
        types.go
        errors.go
        naming.go
        validation.go
      links/
        model.go
        validation.go
      pivots/
        model.go
        validation.go
      options/
        model.go
      schemachange/
        model.go
      shared/
        ids.go
        clock.go
    service/
      tenant_service.go
      table_service.go
      field_service.go
      link_service.go
      pivot_service.go
      table_options_service.go
      schema_sync_service.go
      index_service.go
    ports/
      repositories.go
      schema_manager.go
      authorizer.go
      unit_of_work.go
      id_generator.go
      clock.go
    store/
      postgres/
        db.go
        tx.go
        tenant_repository.go
        table_repository.go
        field_repository.go
        link_repository.go
        pivot_repository.go
        table_options_repository.go
        schema_change_repository.go
        index_job_repository.go
        assembled_data_model_repository.go
    tenantdb/
      postgres/
        schema_manager.go
        ddl_planner.go
        ddl_executor.go
        index_manager.go
        identifier.go
    httpapi/
      router.go
      middleware/
        auth.go
        request_id.go
        logging.go
        recover.go
      dto/
        tenant.go
        table.go
        field.go
        link.go
        pivot.go
        options.go
        errors.go
      handlers/
        health.go
        tenants.go
        data_model.go
        tables.go
        fields.go
        links.go
        pivots.go
        options.go
        schema_change_log.go
        index_jobs.go
    migrations/
      metadata/
        000001_init.up.sql
        000001_init.down.sql
      seed/
        README.md
  pkg/
    apierrors/
      apierrors.go
  Dockerfile
  docker-compose.yml
  .env.example
  Makefile
  README.md
```

## Package Responsibilities

### `internal/domain`

Contains only business concepts and validation rules.

Must not import:

- HTTP packages
- pgx
- Docker/runtime config

Can contain:

- data types
- invariants
- domain errors
- helper methods like field sorting and path validation

### `internal/service`

Contains orchestration usecases.

Responsibilities:

- validate command input using domain rules
- load required state via ports
- invoke metadata repository writes
- invoke tenant schema DDL
- record schema changes

Must not contain:

- raw SQL
- HTTP DTOs

### `internal/ports`

Defines all interfaces the services depend on.

This is the seam for testing and for future infrastructure changes.

### `internal/store/postgres`

Contains metadata persistence.

Responsibilities:

- CRUD metadata rows
- assemble full data model snapshots
- open transactions
- map DB rows to domain entities

### `internal/tenantdb/postgres`

Contains all per-tenant DDL logic.

Responsibilities:

- create schema
- create table
- alter table add column
- drop table
- drop column
- rename archived fields if needed
- create and delete indexes

This package is where Marble's `organization_schema_repository.go` behavior is conceptually reimplemented.

### `internal/httpapi`

Contains transport only.

Responsibilities:

- request parsing
- response formatting
- auth middleware
- route registration
- transport error mapping

## Initial Domain Models

### Tenant

```go
type Tenant struct {
    ID         uuid.UUID
    ExternalKey *string
    Name       string
    SchemaName string
    Status     TenantStatus
    CreatedAt  time.Time
    UpdatedAt  time.Time
}
```

### ModelTable

```go
type ModelTable struct {
    ID           uuid.UUID
    TenantID     uuid.UUID
    Name         string
    Description  string
    Alias        string
    SemanticType string
    CaptionField string
    Archived     bool
    CreatedAt    time.Time
    UpdatedAt    time.Time
}
```

### ModelField

```go
type ModelField struct {
    ID          uuid.UUID
    TenantID    uuid.UUID
    TableID     uuid.UUID
    Name        string
    Description string
    DataType    DataType
    Nullable    bool
    IsEnum      bool
    IsUnique    bool
    Archived    bool
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### ModelLink

```go
type ModelLink struct {
    ID            uuid.UUID
    TenantID      uuid.UUID
    Name          string
    ParentTableID uuid.UUID
    ParentFieldID uuid.UUID
    ChildTableID  uuid.UUID
    ChildFieldID  uuid.UUID
    CreatedAt     time.Time
}
```

### Pivot

```go
type Pivot struct {
    ID          uuid.UUID
    TenantID    uuid.UUID
    BaseTableID uuid.UUID
    FieldID     *uuid.UUID
    PathLinkIDs []uuid.UUID
    CreatedAt   time.Time
}
```

### TableOptions

```go
type TableOptions struct {
    ID              uuid.UUID
    TenantID        uuid.UUID
    TableID         uuid.UUID
    DisplayedFields []uuid.UUID
    FieldOrder      []uuid.UUID
    UpdatedAt       time.Time
}
```

### SchemaChange

```go
type SchemaChange struct {
    ID           uuid.UUID
    TenantID     uuid.UUID
    Operation    string
    ResourceType string
    ResourceID   uuid.UUID
    Status       string
    Details      []byte
    CreatedAt    time.Time
}
```

## Initial Ports

### Repositories

```go
type TenantRepository interface {
    Create(ctx context.Context, tenant Tenant) error
    GetByID(ctx context.Context, id uuid.UUID) (Tenant, error)
    List(ctx context.Context, filter TenantFilter) ([]Tenant, error)
    UpdateStatus(ctx context.Context, id uuid.UUID, status TenantStatus) error
}

type TableRepository interface {
    Create(ctx context.Context, table ModelTable) error
    GetByID(ctx context.Context, id uuid.UUID) (ModelTable, error)
    GetByName(ctx context.Context, tenantID uuid.UUID, name string) (ModelTable, error)
    Update(ctx context.Context, cmd UpdateTableCmd) error
    Delete(ctx context.Context, id uuid.UUID) error
    ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]ModelTable, error)
}

type FieldRepository interface {
    Create(ctx context.Context, field ModelField) error
    GetByID(ctx context.Context, id uuid.UUID) (ModelField, error)
    ListByTable(ctx context.Context, tableID uuid.UUID) ([]ModelField, error)
    Update(ctx context.Context, cmd UpdateFieldCmd) error
    Delete(ctx context.Context, id uuid.UUID) error
}

type LinkRepository interface {
    Create(ctx context.Context, link ModelLink) error
    GetByID(ctx context.Context, id uuid.UUID) (ModelLink, error)
    ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]ModelLink, error)
    Delete(ctx context.Context, id uuid.UUID) error
}

type PivotRepository interface {
    Create(ctx context.Context, pivot Pivot) error
    GetByID(ctx context.Context, id uuid.UUID) (Pivot, error)
    ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]Pivot, error)
    Delete(ctx context.Context, id uuid.UUID) error
}

type TableOptionsRepository interface {
    GetByTableID(ctx context.Context, tableID uuid.UUID) (TableOptions, error)
    Upsert(ctx context.Context, options TableOptions) error
}

type DataModelReadRepository interface {
    GetAssembledDataModel(ctx context.Context, tenantID uuid.UUID, opts ReadOptions) (AssembledDataModel, error)
}

type SchemaChangeRepository interface {
    Create(ctx context.Context, change SchemaChange) error
    ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]SchemaChange, error)
    MarkApplied(ctx context.Context, id uuid.UUID) error
    MarkFailed(ctx context.Context, id uuid.UUID, details []byte) error
}

type IndexJobRepository interface {
    Create(ctx context.Context, job IndexJob) error
    ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]IndexJob, error)
    UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
}
```

### Schema Manager

```go
type SchemaManager interface {
    ProvisionTenantSchema(ctx context.Context, tenant Tenant) error
    CreateTable(ctx context.Context, tenant Tenant, table ModelTable) error
    DropTable(ctx context.Context, tenant Tenant, table ModelTable) error
    AddField(ctx context.Context, tenant Tenant, table ModelTable, field ModelField) error
    DropField(ctx context.Context, tenant Tenant, table ModelTable, field ModelField) error
    ArchiveField(ctx context.Context, tenant Tenant, table ModelTable, field ModelField) error
    CreateUniqueIndex(ctx context.Context, tenant Tenant, table ModelTable, columns []string) error
    DropUniqueIndex(ctx context.Context, tenant Tenant, table ModelTable, columns []string) error
    CreateNavigationIndex(ctx context.Context, tenant Tenant, table ModelTable, columns []string) error
}
```

### Unit of Work

```go
type UnitOfWork interface {
    WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
```

### Authorizer

```go
type Authorizer interface {
    CanReadTenant(ctx context.Context, tenantID uuid.UUID) error
    CanWriteTenant(ctx context.Context, tenantID uuid.UUID) error
    CanAdminTenant(ctx context.Context, tenantID uuid.UUID) error
}
```

### Utilities

```go
type IDGenerator interface {
    New() uuid.UUID
}

type Clock interface {
    Now() time.Time
}
```

## Initial Application Services

### `TenantService`

Responsibilities:

- create tenants
- derive schema name
- provision tenant schema

Methods:

- `CreateTenant`
- `ProvisionTenant`
- `GetTenant`
- `ListTenants`

### `TableService`

Responsibilities:

- create tables
- update metadata
- delete tables

Special behavior:

- on create, also create default required fields in metadata and tenant schema:
  - `object_id`
  - `updated_at`

Recommended create flow:

1. validate name
2. create metadata table record
3. create metadata default fields
4. create physical table
5. create unique index on `object_id`
6. log schema change

### `FieldService`

Responsibilities:

- create field
- update field metadata
- enforce enum/unique/nullable rules
- add or drop physical column

### `LinkService`

Responsibilities:

- create links
- validate parent uniqueness and type compatibility
- delete links

### `PivotService`

Responsibilities:

- create pivot
- validate field-based and path-based pivots
- list pivots
- delete pivots

### `TableOptionsService`

Responsibilities:

- get table options
- upsert table options
- derive default field order when absent

### `SchemaSyncService`

Responsibilities:

- reconcile metadata and physical schema
- verify tenant consistency

This can be minimal in V1 and exposed mainly through a CLI command.

## First REST Contract

### Common Conventions

- all IDs are UUID strings
- tenant-scoped reads use `/v1/tenants/{tenantId}/...`
- updates return either the updated resource or `204`
- errors use a consistent envelope:

```json
{
  "error": {
    "code": "bad_parameter",
    "message": "field name is invalid",
    "details": {}
  }
}
```

### Tenant Endpoints

#### `POST /v1/tenants`

Request:

```json
{
  "name": "Acme Bank",
  "external_key": "acme-bank"
}
```

Response:

```json
{
  "tenant": {
    "id": "uuid",
    "name": "Acme Bank",
    "external_key": "acme-bank",
    "schema_name": "tenant_...",
    "status": "pending",
    "created_at": "..."
  }
}
```

#### `POST /v1/tenants/{tenantId}/provision`

Response:

```json
{
  "tenant": {
    "id": "uuid",
    "status": "active"
  }
}
```

### Data Model Read

#### `GET /v1/tenants/{tenantId}/data-model`

Query params:

- `include_navigation_options=true|false`
- `include_uniqueness=true|false`

Response:

```json
{
  "data_model": {
    "revision_id": "opaque-version-id",
    "ingestion_contract": {
      "tenant_status": "active",
      "writable": true,
      "managed_system_fields": ["object_id", "updated_at", "valid_from", "valid_until"],
      "record_lookup_field": "object_id",
      "partial_updates": true
    },
    "tables": {
      "transactions": {
        "id": "uuid",
        "name": "transactions",
        "description": "Transaction records",
        "fields": {},
        "links_to_single": {},
        "navigation_options": []
      }
    }
  }
}
```

Contract expectations:

- `revision_id` is opaque and must be stored by downstream ingestion as the schema snapshot identifier for the write
- the value changes when tenant published state or tenant schema migration history changes
- downstream consumers must not attempt to derive meaning from the string itself
- enum values in assembled fields are authoritative and must not be extended implicitly by ingestion writes

### Tables

#### `POST /v1/tenants/{tenantId}/tables`

Request:

```json
{
  "name": "transactions",
  "description": "Transaction records",
  "alias": "",
  "semantic_type": ""
}
```

Behavior:

- creates metadata table
- creates default fields
- creates physical table
- creates unique index on `object_id`

Response:

```json
{
  "table": {
    "id": "uuid",
    "name": "transactions"
  }
}
```

#### `PATCH /v1/tables/{tableId}`

Request:

```json
{
  "description": "Updated description",
  "alias": "Transactions",
  "semantic_type": "financial_record",
  "caption_field": "object_id"
}
```

### Fields

#### `POST /v1/tables/{tableId}/fields`

Request:

```json
{
  "name": "amount",
  "description": "Transaction amount",
  "data_type": "float",
  "nullable": false,
  "is_enum": false,
  "is_unique": false
}
```

Behavior:

- creates metadata field
- adds physical column
- optionally creates unique index

Response:

```json
{
  "field": {
    "id": "uuid",
    "name": "amount"
  }
}
```

#### `PATCH /v1/fields/{fieldId}`

Supports:

- description
- enum flag
- unique flag
- nullable flag

V1 recommendation:

- do not support renaming fields
- do not support changing field data type

### Links

#### `POST /v1/tenants/{tenantId}/links`

Request:

```json
{
  "name": "account",
  "parent_table_id": "uuid",
  "parent_field_id": "uuid",
  "child_table_id": "uuid",
  "child_field_id": "uuid"
}
```

### Pivots

#### `POST /v1/tenants/{tenantId}/pivots`

Request:

```json
{
  "base_table_id": "uuid",
  "field_id": "uuid"
}
```

or

```json
{
  "base_table_id": "uuid",
  "path_link_ids": ["uuid", "uuid"]
}
```

### Table Options

#### `PUT /v1/tables/{tableId}/options`

Request:

```json
{
  "displayed_fields": ["uuid", "uuid"],
  "field_order": ["uuid", "uuid", "uuid"]
}
```

### Deletion

V1 deletion contract:

- `DELETE /v1/tables/{tableId}?dry_run=true`
- `DELETE /v1/fields/{fieldId}?dry_run=true`
- `DELETE /v1/links/{linkId}?dry_run=true`
- `DELETE /v1/pivots/{pivotId}?dry_run=true`

Response:

```json
{
  "performed": false,
  "conflicts": {
    "links": ["uuid"],
    "pivots": ["uuid"]
  }
}
```

V1 conflicts are internal only.

## Metadata Migration Plan

Use `golang-migrate` with SQL files under:

- `internal/migrations/metadata`

### `000001_init.up.sql`

Should create:

- metadata schema, e.g. `core`
- `tenants`
- `model_tables`
- `model_fields`
- `model_links`
- `model_pivots`
- `table_options`
- `schema_change_log`
- `tenant_schema_migrations`
- `index_jobs`

Recommended constraints:

- foreign keys with `ON DELETE CASCADE` where safe
- unique constraints on tenant-local names
- check constraints on enum fields like status/type when useful

### `000001_init.down.sql`

Should drop:

- all metadata tables
- metadata schema if empty

## Draft Initial SQL Schema

### `tenants`

```sql
CREATE TABLE core.tenants (
  id UUID PRIMARY KEY,
  external_key TEXT UNIQUE,
  name TEXT NOT NULL,
  schema_name TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
```

### `model_tables`

```sql
CREATE TABLE core.model_tables (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  alias TEXT NOT NULL DEFAULT '',
  semantic_type TEXT NOT NULL DEFAULT '',
  caption_field TEXT NOT NULL DEFAULT '',
  archived BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (tenant_id, name)
);
```

### `model_fields`

```sql
CREATE TABLE core.model_fields (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  data_type TEXT NOT NULL,
  nullable BOOLEAN NOT NULL DEFAULT FALSE,
  is_enum BOOLEAN NOT NULL DEFAULT FALSE,
  is_unique BOOLEAN NOT NULL DEFAULT FALSE,
  archived BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (table_id, name)
);
```

### `model_links`

```sql
CREATE TABLE core.model_links (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  parent_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  parent_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  child_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  child_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX model_links_tenant_child_name_idx
  ON core.model_links (tenant_id, child_table_id, name);
```

### `model_pivots`

```sql
CREATE TABLE core.model_pivots (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  base_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  field_id UUID NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  path_link_ids UUID[] NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL
);
```

### `table_options`

```sql
CREATE TABLE core.table_options (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  displayed_fields UUID[] NOT NULL DEFAULT '{}',
  field_order UUID[] NOT NULL DEFAULT '{}',
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (table_id)
);
```

### `schema_change_log`

```sql
CREATE TABLE core.schema_change_log (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  operation TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id UUID NOT NULL,
  status TEXT NOT NULL,
  details JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
);
```

### `tenant_schema_migrations`

```sql
CREATE TABLE core.tenant_schema_migrations (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  version TEXT NOT NULL,
  applied_at TIMESTAMPTZ NOT NULL
);
```

### `index_jobs`

Current implementation status:

- implemented as a real metadata-backed worker flow, not just a placeholder table
- supports `navigation` and `repair` job orchestration
- supports retry scheduling, attempt counts, and dedupe keys
- used by navigation-option creation and reconcile-triggered repair

```sql
CREATE TABLE core.index_jobs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  table_id UUID NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  table_name TEXT NOT NULL,
  index_type TEXT NOT NULL,
  columns TEXT[] NOT NULL,
  status TEXT NOT NULL,
  requested_by_operation TEXT NOT NULL,
  error_message TEXT NULL,
  attempt_count INTEGER NOT NULL,
  requested_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ NULL,
  completed_at TIMESTAMPTZ NULL,
  scheduled_at TIMESTAMPTZ NULL,
  dedupe_key TEXT NOT NULL
);
```

## Tenant DDL Rules

### Schema Name Derivation

Recommended implementation:

- `tenant_` + UUID without dashes

Example:

- `tenant_6f1f4c7f3fd2443a8ccf2d5bbeb4b173`

### Create Tenant Schema

DDL:

```sql
CREATE SCHEMA IF NOT EXISTS <schema_name>;
```

### Create Physical Table

DDL target:

```sql
CREATE TABLE IF NOT EXISTS <schema>.<table_name> (
  id UUID NOT NULL PRIMARY KEY,
  object_id TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  valid_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  valid_until TIMESTAMPTZ NOT NULL DEFAULT 'INFINITY'
);
```

This mirrors Marble closely and leaves room for versioned object histories later.

### Create Field

Map domain data types to PostgreSQL:

- `bool` -> `BOOLEAN`
- `int` -> `INTEGER`
- `float` -> `DOUBLE PRECISION`
- `string` -> `TEXT`
- `timestamp` -> `TIMESTAMPTZ`
- `ip_address` -> `INET`
- `coords` -> `GEOMETRY` only if PostGIS is intentionally supported

V1 recommendation:

- defer `coords` unless PostGIS is a hard requirement
- if deferred, reject that type at API validation time

### Unique Index Rules

Always create:

- unique index on `object_id`

Conditionally create:

- unique index on user field when `is_unique=true` and type is allowed

### Navigation Index Rules

Current implementation:

- create btree index on `(filter_field, ordering_field)`
- enqueue index creation asynchronously from navigation-option creation
- detect missing managed indexes during reconcile
- schedule `repair` jobs automatically when drift is found
- retry failed worker executions with scheduled backoff before terminal failure

## Local Development Runtime

## `Dockerfile`

Expected shape:

- builder stage with Go
- final slim runtime image
- binary runs `server`

## `docker-compose.yml`

Services:

- `postgres`
- `data-model-service`

Recommended environment:

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_DB: datamodel
      POSTGRES_USER: datamodel
      POSTGRES_PASSWORD: datamodel
    ports:
      - "5434:5432"
    volumes:
      - dm_pg_data:/var/lib/postgresql/data

  data-model-service:
    build:
      context: .
      dockerfile: Dockerfile
    environment:
      DATABASE_URL: postgres://datamodel:datamodel@postgres:5432/datamodel?sslmode=disable
      PORT: 8080
      SERVICE_AUTH_MODE: disabled
      LOG_LEVEL: debug
    depends_on:
      - postgres
    ports:
      - "8088:8080"

volumes:
  dm_pg_data:
```

## `.env.example`

```env
PORT=8080
DATABASE_URL=postgres://datamodel:datamodel@localhost:5434/datamodel?sslmode=disable
SERVICE_AUTH_MODE=disabled
LOG_LEVEL=debug
DDL_LOCK_TIMEOUT=10s
STATEMENT_TIMEOUT=30s
DEFAULT_TIMEZONE=UTC
```

## `Makefile`

Recommended targets:

- `run`
- `test`
- `build`
- `migrate-up`
- `migrate-down`
- `docker-up`
- `docker-down`
- `reconcile`

## Error Model

Define a small internal error taxonomy:

- `ErrNotFound`
- `ErrConflict`
- `ErrBadParameter`
- `ErrForbidden`
- `ErrPreconditionFailed`
- `ErrInternal`

HTTP mapping:

- `ErrBadParameter` -> `400`
- `ErrForbidden` -> `403`
- `ErrNotFound` -> `404`
- `ErrConflict` -> `409`
- `ErrPreconditionFailed` -> `422`

## Testing Plan

### Unit Tests

Target:

- domain validation
- service orchestration with mocked ports

### Integration Tests

Target:

- metadata persistence
- tenant DDL execution against real PostgreSQL
- full create-table/create-field/link/pivot flows

### Golden API Tests

Target:

- request/response contract
- error payload stability

## Suggested Implementation Order

### Step 1

- scaffold the service module
- add config, server, health endpoint
- add DB connection and migration runner

### Step 2

- implement metadata migration `000001_init`
- implement `TenantRepository`
- implement `TenantService`
- implement tenant provisioning endpoint

### Step 3

- implement `TableRepository`, `FieldRepository`
- implement `SchemaManager`
- implement create-table flow

### Step 4

- implement field creation and field update flow
- implement unique index support

### Step 5

- implement links, pivots, options
- implement assembled data model read endpoint

### Step 6

- implement internal-only delete flows
- implement schema change log reads
- add reconcile CLI skeleton

## Concrete Carryovers from Marble to Preserve

Preserve behavior from Marble in the new implementation:

- deterministic strict identifier validation
- automatic default fields on table creation
- object-level tenant schema ownership
- path-based pivot validation
- field uniqueness constraints only on compatible types
- table options ordering behavior

Do not preserve as-is:

- Marble's executor abstractions
- Redis data-model cache
- direct dependency on other fraud modules
- current delete conflict scanner

## Approval Gate Before Coding

Before scaffolding code, confirm these final choices:

1. Router: `gin`
2. Migrations: `golang-migrate`
3. V1 excludes PostGIS `coords` support unless explicitly required
4. V1 delete logic is internal-only
5. V1 authentication can be disabled locally and token-based in deployed environments

## Next Step After This Blueprint

After approval of this blueprint, the next task should be:

- scaffold the Go service
- create the first metadata migration
- wire the server, config, DB, and health endpoint

No direct code should be copied from Marble without adaptation to the new ports and package boundaries.
