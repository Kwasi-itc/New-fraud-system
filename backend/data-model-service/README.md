# Data Model Service

For the service-local MySQL database and migration setup, see
[MYSQL_DOCKER.md](MYSQL_DOCKER.md) and `docker-compose-mysql.yml`.
The local MySQL API serves Swagger at http://localhost:8080/docs with tenant
registration/provisioning, physical table/native-field creation, presentation
updates and metadata reads. Remaining
mutation and worker gaps are tracked in [MYSQL_ENDPOINT_STATUS.md](MYSQL_ENDPOINT_STATUS.md).
The current queue, execution and deployment audit is in
[MYSQL_WORKER_STATUS.md](MYSQL_WORKER_STATUS.md).
The modular HTTP layout and endpoint extension workflow are documented in
[MYSQL_RUNTIME.md](MYSQL_RUNTIME.md).

Standalone Go service for tenant-aware data model management, extracted from Marble and rebuilt as an independent module.

Current location in the workspace:

- `new/backend/data-model-service`

This service is intended to be used by another fraud platform as a dedicated schema-management module. It owns metadata, per-tenant physical PostgreSQL schemas, schema mutation workflows, and supporting operational tooling.

## Purpose

Migration `000005_ingestion_contracts` publishes a compact validation contract for
each table in `core.ingestion_contracts`. Data-model-owned triggers update it in
the same transaction as table, field, and enum mutations. Only changes to the
contract payload replace its opaque UUID version. Description/display changes
do not invalidate it. Concurrent schema edits serialize on their table metadata
row; ingestion takes no such lock. Tenant-inconsistent fields/enums are rejected.

Shared-database consumers read `core.ingestion_contracts_v1` and
`core.ingestion_tenants_v1`, which expose current tenant status and schema name.
Grant consumers schema usage and SELECT on these views only; metadata and
publication writes remain owned by data-model-service. A separately configured
data-model runtime role needs permission to execute the publication functions
and modify the publication table; the migration revokes public function access.
Role provisioning remains a deployment responsibility.

Apply this migration before deploying SQL-mode ingestion readers. It backfills
existing metadata under write locks on the three source metadata tables; plan a
metadata maintenance window proportional to schema size. It does not rebuild
record indexes. Switch ingestion API and workers to HTTP mode and restart them
before reverting the migration. See the [ingestion README](../ingestion-service/README.md)
for cache, write-path, and validation details. The full-model endpoint below
retains its existing assembly behavior for compatibility.

`GET /v1/tenants/:tenantId/data-model` supports conditional GET with an `ETag`
derived from SHA-256 of the complete JSON representation. `If-None-Match` matches
return `304` without a body; other requests return the current model. Both carry
`Cache-Control: private, no-cache`, requiring revalidation before reuse. Normal
authentication and model reads happen before the validator is evaluated. The
validator includes tenant status and schema content even if the public revision
ID has not changed. This reduces transfer on unchanged responses, but does not
cache server-side queries or make the existing multi-query assembly atomic.

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
  - River-backed runtime for async index jobs
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
- `GET /redoc`

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

Data Model also accepts signed platform-session JWTs when `USER_JWT_KEYS_FILE`,
`USER_JWT_ISSUER` and `USER_JWT_AUDIENCE` are configured. This requires token mode
and auth schema version 2. Tenant-scoped platform roles grant reads; only
`platform_admin` grants writes. Ownership and current sessions are checked in the
backend, including inside mutation transactions. Global tenant administration and
reconcile remain service-only. See [permissions and coordinated rollout](../auth-service/DATA_MODEL_AUTH.md).

`DATA_MODEL_SERVICE_TOKEN` supplies the private integration credential and takes
precedence over the legacy `SERVICE_AUTH_TOKEN`. Never expose either in browser
configuration. Docker builds now use `backend` as context with
`-f data-model-service/Dockerfile`.

Supported service auth modes:

An omitted mode defaults to `token`; startup requires a nonblank private
`SERVICE_AUTH_TOKEN`. Unknown modes fail startup. `disabled` is an explicit
development opt-out. See the [legacy API rollout](../auth-service/LEGACY_API_AUTH.md)
for the remaining user/tenant authorization and deployment work.

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
- Redoc page: `GET /redoc`

The docs pages load Swagger UI and Redoc frontends against the same embedded OpenAPI document.

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

- create navigation options, set a table `caption_field`, or explicitly enqueue index jobs through the API
- run `go run ./cmd/worker` or `make run-worker`
- the worker consumes River jobs that point at `core.index_jobs`, applies managed indexes, retries transient failures, marks jobs `applied` or `failed`, and records schema-change audit rows for each transition
- `reconcile` detects missing managed indexes and schedules repair jobs automatically

Automatic behaviors in the current service:

- table creation creates the required `object_id` unique index immediately
- field create and field uniqueness changes manage unique indexes immediately
- navigation option creation auto-requests a background `navigation` index job
- setting or changing `caption_field` auto-requests a background `search` index job for that field

Current limitation:

- clearing or changing `caption_field` does not retire old search indexes yet
- reconcile does not yet infer expected search indexes from `caption_field`; it currently repairs navigation-managed indexes only

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

Service `migrate down` preserves the shared River schema: it rolls back only
data-model metadata and logs that distinction. Stop/drain the owner's consumers
before metadata rollback; shared queue rollback requires a separately reviewed
whole-stack operation. No migration-down command should be used as queue repair.

### Publication-aware assembled reads for decision preparation

NewPublishedDataModelReadRepository explicitly opts into the physical registry
read contract. Use it with NewSnapshotDataModelReadService so the tenant revision,
assembled metadata and publication versions share one read-only snapshot. Existing
NewDataModelReadRepository callers keep their metadata-only behavior. The new
projection adds one bounded tenant-indexed query and consumes the same aggregate
row budget; it does not perform a query per table. It requires SELECT on the
owner's data_model_physical_tables registry in addition to existing read grants.

An optional published_version is exposed only for non-archived tables with an open
gate, no active schema operation and a nonzero published version. Consumers must
still acquire their registry binding and check that version in the data transaction;
the HTTP token is not a readiness lease. No version is synthesized from a tenant
revision. Closed/unpublished tables remain visible as metadata without a token.
Configured PostgreSQL routing is unchanged; selecting this staged reader requires
explicit composition with trusted tenant authorization and the owner read role.
### Scoped index-job preparation reads

GET /v1/tenants/:tenantId/index-jobs/scoped requires repeated table_id UUIDs
(1-100 unique values) and limit (1-1000). It returns the complete scoped set or an
error on overflow; it never reports a partial set as complete. Both PostgreSQL and
staged MySQL repositories implement tenant/table filtering. The PostgreSQL reader
bounds variable projections to 64 KiB per row and encoded results to 8 MiB; the
MySQL reader additionally obeys its configured row/byte budgets.

Metadata migration 000009 adds the PostgreSQL tenant/table/request-time index.
MySQL append-only v007 now adds its equivalent and the predecessor-intent lookup
index; the local installer preserves the retained migration prefix and backup.
An unhinted fixture plan selected it with 5,000 unrelated jobs. This is not a
production throughput result. The ordinary index build requires a lock-duration
review before deployment. No application migration or backend switch was run.

### MySQL index worker

`PhysicalPublicationManager.RunNativeIndexJob` composes one committed delivery
lease with the typed planner, immutable DDL intent, pinned physical executor,
publication receipt and native-index completion fence. Publication and completion
recheck the lease; local mutation authority is also checked in completion. The
job ID is the stable physical index and operation identity. A retry after a
committed publication reads its original receipt rather than repeating DDL.

The caller must supply retained `NativeIndexWork` from the trusted reference
builder, including original epoch/version, complete field mappings and prior
guard lineage. Never build expected definitions from application drift or accept
these inputs from HTTP clients. The caller owns lease renewal, cancellation and
failure disposition. Errors retain recovery state and never report an applied
job. Native search indexes are supported; text, navigation and repair strategies
fail explicitly until their separate protocols are composed.

The local Compose stack now runs `cmd/mysql-worker`, using
`RunManagedIndexJobWithExecutor` for numeric/text search, caption, navigation and
repair work. The native-only API above retains its restricted contract. The host
loads retained owner intents, captures references separately, supervises leases,
executes with temporary scoped credentials, retries errors and runs bounded
maintenance. Text keys use nonunique bounded prefixes; full-value constraints
remain separate. Live acceptance covers audit-failure retry and hard process
interruption during DDL. See [MySQL worker status](MYSQL_WORKER_STATUS.md) for
evidence, resource budgets and production deployment limits. The PostgreSQL
entrypoint and production configuration remain available.

### Local MySQL portable model endpoints

The local compose API exposes GET `/v1/tenants/{tenantId}/data-model/export`
and POST `/v1/tenants/{tenantId}/data-model/import` at
`http://localhost:8080/docs`. The export response can be posted unchanged to a
provisioned target tenant: both use the `{ "data_model": { ... } }` envelope.
Only schema definitions are copied; records and managed identifiers are not.

Use a UUID `Idempotency-Key` for import and keep it with the exact request body.
After cancellation or a 503, retry with that same key and body. Imports create
new tables; existing names return 409. MySQL physical DDL commits separately,
so interrupted imports may leave published progress which the retry resumes.
The final relationships/options and completion receipt commit atomically.

Local limits: 1 MiB request, 200 combined tables/fields/enum values/relationships,
and a 60-second owner attempt. The endpoint status checklist documents remaining
runtime limits. Run `node scripts/mysql-portable-smoke.mjs` for the live round-trip
and cancellation/retry checks; it creates new test tenants without resetting data.

### Local MySQL table deletion and enum creation

DELETE `/v1/tables/{tableId}?dry_run=true` previews dependencies. Omitting dry_run
removes the physical table and all its records, triggers and indexes. Dependencies
return 409. Use a stable UUID `Idempotency-Key` for retries after an interruption.
Recovery receipts and hidden metadata remain; the retired table disappears from
model/export/list results. Recreate the same name using a new explicit creation key.

POST `/v1/fields/{fieldId}/enum-values` creates a value in an existing enum field
and publishes the ingestion catalog atomically. Use a UUID `Idempotency-Key` to
retain the original response on retry. String/int/float validation and duplicate
checks apply. Run `node scripts/mysql-table-delete-enum-create-smoke.mjs` from
this service directory for acceptance using newly allocated local test fixtures.

### Remaining local MySQL API routes

All 43 checklist routes are now wired. Relationships, pivots, table options and
navigation use transactional owner rules and stable Idempotency-Key receipts.
Index-job requests create durable pending work; failed jobs can be retried, and
GET /v1/tenants/{tenantId}/index-jobs/scoped requires table_id and limit parameters.

GET /v1/admin/reconcile?tenant_id=<uuid> checks one tenant and queues missing-index
repairs. Omit tenant_id for a bounded global report. This endpoint reports observed
schema drift; it queues repairs for the separate local MySQL worker and does not
prove full schema equivalence. See
MYSQL_ENDPOINT_STATUS.md for limits and verification scope.

Run node scripts/mysql-remaining-endpoints-smoke.mjs for acceptance of these routes.
The script creates new fixtures and temporarily adds/removes a drift column only
on its own table; it does not reset the application database.
