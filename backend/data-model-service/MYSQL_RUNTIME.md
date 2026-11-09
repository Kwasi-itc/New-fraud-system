# Local MySQL HTTP structure

The local runtime stays in one Go package, `internal/mysqlruntime`, with separate
files for composition, transport, resource handlers and operations. Keeping these
modules together avoids circular imports and unnecessary exported APIs. Public
and private routers remain separate HTTP servers with separate credentials.

| Module | Responsibility |
| --- | --- |
| `local_http.go` | Public router composition and explicit local-mode checks |
| `local_author_http.go` | Private router and the injectable authoring workflow contract |
| `local_middleware.go` | Server-owned authority, worker authentication, request deadlines and body limits |
| `local_author_client.go` | Public-to-private forwarding, credential injection, bounded responses and retry headers |
| `local_handlers_tenants.go` | Tenant HTTP parsing, validation and responses |
| `local_tenant_operations.go` | Tenant registration composition, scoped lookup and provisioning |
| `local_handlers_tables.go`, `local_handlers_fields.go` | Authoring request handlers |
| `local_handlers_updates.go` | Table/field PATCH validation and typed workflow dispatch |
| `local_updates.go` | Atomic owner publication and retained presentation results; no Gin dependency |
| `local_field_updates.go`, `local_field_patch_plan.go`, `local_field_catalog.go` | Constraint planning, physical publication and atomic enum-catalog updates |
| `local_creation.go`, `local_field_creation.go` | Physical creation workflows, reference capture, installer lifecycle and recovery |
| `local_worker.go` | MySQL queue polling, leases, managed index execution, retained-plan recovery, failure outcomes and maintenance |
| `local_handlers_*_reads.go` | Table, field/enum, relationship and history read registrations |
| `local_reads.go` | Shared read response handling and bounded tenant-scoped snapshots |
| `local_handlers_model.go` | Published model endpoint composition |
| `local_handlers_health.go`, `local_handlers_docs.go` | Health/readiness and the unchanged full OpenAPI documentation |
| `local_response.go` | Strict JSON decoding and existing mutation error mapping |

## Adding an endpoint

1. Implement business behavior through the existing service/owner and MySQL
   repository contracts. Keep DDL, transactions and publication recovery out of
   HTTP handlers. Administrative credentials remain private to the author worker.
2. Add a resource handler that validates its request, calls the operation, and
   adapts the result to the existing OpenAPI response. Put new enum mutations in
   their own resource module when their workflow is implemented.
3. Register the private handler and public forwarding route explicitly. The
   public client forwards only mounted routes, substitutes the worker credential,
   and does not forward caller identity headers. Preserve idempotency keys.
4. Preserve the two-workflow DDL concurrency limit, request deadlines, body and
   response limits, scoped reads and the shared database connection budget.
5. Add handler/contract tests, update the route inventory test intentionally, and
   exercise the actual MySQL operation before marking it complete in
   [MYSQL_ENDPOINT_STATUS.md](MYSQL_ENDPOINT_STATUS.md).

Do not introduce successful stubs to match the documentation. Swagger continues
to describe all 43 operations; this refactor does not implement missing CRUD modes.
Existing error envelopes are deliberately preserved, including their differences
between resources. Any future API-wide error normalization is a separate change.

## Regression checks

From the service directory, with Go 1.25 available:

```sh
go test -race -mod=readonly ./internal/mysqlruntime ./internal/httpapi/...
go vet -mod=readonly ./internal/mysqlruntime ./internal/httpapi/... ./cmd/mysql-api ./cmd/mysql-local-author
```

The runtime tests check route inventory, OpenAPI parity, no-auth opt-in and origin
boundaries, private credentials, handler-to-workflow arguments, invalid requests,
forwarding status/body/retry headers, redirect refusal, cancellation and response
size limits. Database-dependent PostgreSQL integration tests require their own
test configuration; an ordinary unit run does not establish that coverage.

After rebuilding the local Compose stack using [MYSQL_DOCKER.md](MYSQL_DOCKER.md):

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/mysql-local-smoke.ps1
node scripts/mysql-mutation-smoke.mjs
node scripts/mysql-partial-endpoints-smoke.mjs --physical
```

The live MySQL smoke covers all mounted operation paths, native field types,
concurrent changes, replay and conflicting keys, metadata reads and tenant-scoped
lists. Caption updates enqueue deduplicated index deliveries consumed by the
separate `cmd/mysql-worker` Compose service. Smoke tenants are retained and shared
data is never reset. See [MYSQL_WORKER_STATUS.md](MYSQL_WORKER_STATUS.md) for live
text/navigation/repair, fault injection and process-restart acceptance. Production
authorization remains separate from this local runtime.
