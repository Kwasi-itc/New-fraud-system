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
- navigation option CRUD
- async index job enqueue/list/get/retry APIs
- background index job worker with retry/backoff
- assembled data model read endpoint
- schema change log recording and read endpoint
- transactional mutation execution across metadata writes and tenant DDL
- tenant schema migration history recording and read endpoint
- configurable service auth with bearer token mode
- embedded OpenAPI spec and Swagger-style docs page
- schema reconciliation CLI for metadata vs physical tenant drift
- structured request logging and request IDs
- initial unit tests for domain validation, pivot deletion behavior, handler validation, and middleware behavior
- PostgreSQL-backed integration coverage for happy-path lifecycle, delete flows, dry-run conflict reporting, and transactional rollback cases
- repository-level PostgreSQL verification for tenant, table, field, link, pivot, table options, schema change, tenant schema migration, and assembled read repositories
- repository/service/worker verification for async index jobs and reconcile-triggered repair scheduling
- V1 destructive lifecycle policy: hard delete with internal dry-run conflict checks
- ingestion compatibility updates on the assembled data-model contract:
  - top-level `revision_id`
  - tenant writability metadata
  - managed system field publication
  - archived flags in assembled tables and fields

## Next Priority

- add more rollback/failure-path coverage as new mutation behaviors are introduced
- add archival semantics only if the target product requires history-preserving deletes

## Operational Hardening

- extend auth beyond static bearer token mode
- add tenant-scoped authorization hooks
- keep the OpenAPI spec in sync as routes evolve
- add metrics and tracing hooks
- add startup migration checks and readiness assertions for metadata schema state
- make reconciliation available as an authenticated admin endpoint if needed

## Data Model Features Still Missing

- additional managed-index strategies beyond the current navigation/repair flows if the target product needs them
- support for additional data types such as `coords` if the target system needs them

## Integration Work

- define stable API versioning and error contract
- add a small client package or SDK for downstream consumers if needed
- document tenant lifecycle, provisioning order, and destructive change behavior
- decide whether delete conflicts stay internal-only or expand into an external dependency registry
