# Implementation TODO

This file tracks the remaining work for the standalone data model service after the current core slice.

## Completed

- service scaffold with Gin, Docker, Makefile, env config, and migration runner
- metadata schema bootstrap for tenants, tables, fields, links, pivots, options, schema change log, tenant schema migrations, and index jobs
- tenant CRUD read path and tenant schema provisioning
- physical per-tenant schema creation
- table create, update, delete, and dry-run delete conflict reporting
- field create, update, delete, and dry-run delete conflict reporting
- link create, delete, and dry-run delete conflict reporting
- pivot create, list, delete, and dry-run support
- table options get and upsert
- assembled data model read endpoint
- schema change log recording and read endpoint
- transactional mutation execution across metadata writes and tenant DDL
- configurable service auth with bearer token mode
- schema reconciliation CLI for metadata vs physical tenant drift
- structured request logging and request IDs
- initial unit tests for domain validation, pivot deletion behavior, handler validation, and middleware behavior
- first PostgreSQL-backed integration test for transaction rollback on unique index creation failure

## Next Priority

- expand PostgreSQL-backed integration coverage across create, delete, and schema-change flows
- expand request/response integration tests against Gin handlers and router wiring
- add repository tests against a real PostgreSQL container
- add table and field archival semantics where destructive deletes should preserve history

## Operational Hardening

- extend auth beyond static bearer token mode
- add tenant-scoped authorization hooks
- add OpenAPI or Swagger generation
- add metrics and tracing hooks
- add startup migration checks and readiness assertions for metadata schema state
- make reconciliation available as an authenticated admin endpoint if needed

## Data Model Features Still Missing

- navigation option CRUD if required by the downstream fraud platform
- physical secondary index management beyond unique field indexes
- asynchronous index job worker backed by `core.index_jobs`
- tenant schema migration history backed by `core.tenant_schema_migrations`
- support for additional data types such as `coords` if the target system needs them

## Integration Work

- define stable API versioning and error contract
- add a small client package or SDK for downstream consumers if needed
- document tenant lifecycle, provisioning order, and destructive change behavior
- decide whether delete conflicts stay internal-only or expand into an external dependency registry
