# Local ingestion on MySQL

The ingestion API runs at **http://localhost:8081/docs**. Data-model remains at
**http://localhost:8080/docs**. Both use the existing shared `fraud` database on
MySQL 8.4.8 and the `fraud-mysql-dev` Compose project and persistent volumes.

From `backend/ingestion-service`, with Docker Desktop running Linux containers:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/setup-mysql.ps1
```

The script starts data-model, creates or reuses the tenant whose external key is
`local-ingestion-development`, provisions it through data-model, and creates a
`transactions` table with nullable `amount` (float) and `currency` (string) fields.
It prints the tenant ID. Existing custom tables are preserved. For a different
tenant/table, pass `-TenantId <uuid> -ObjectType <name>`; a missing table is created.
Pending tenants are provisioned. Retained scope settings are reused on later runs.

The installer applies ingestion's owner migrations after data-model, creates
separate restricted API and worker accounts, grants access to the selected
physical table, and writes the runtime configuration. MySQL v006 contract plans
and the complete v007 worker-index upgrade are supported; checksums, grants and
publication readiness remain mandatory. The ordinary PostgreSQL entrypoint and
its Compose file are separate.

See [endpoint status](MYSQL_ENDPOINT_STATUS.md), [worker status](MYSQL_WORKER_STATUS.md)
and [runtime boundaries](MYSQL_RUNTIME.md) for the integration audit, reproduction
commands and remaining gaps. Local API setup permits four concurrent synchronous
writes on its eight-connection pool so deferred admission and reads retain
connection capacity during lock contention.

This is a local development API with login disabled, published only on loopback.
Foreign browser origins are rejected. The runtime supports **one configured
tenant and object type**; other scopes return 403. Swagger uses the existing
shared specification. All 15 documented operations, including
`GET /v1/admin/deferred-ingest-metrics`, are mounted by this MySQL runtime.
Swagger assets require internet access.

Try a request in Swagger, replacing `<tenant-id>` with the printed ID:

```http
POST /v1/tenants/<tenant-id>/ingest/transactions
Content-Type: application/json
Idempotency-Key: local-first-transaction-v1

{"object_id":"transaction-001","amount":125.50,"currency":"USD"}
```

Read it with `GET /v1/tenants/<tenant-id>/records/transactions/transaction-001`.
The API also exposes `/healthz`, `/readyz`, `/openapi.yaml` and `/redoc`. The
separate worker processes CSV uploads and deferred ingestion. Outbox consumers
for monitoring/scoring are not configured by this setup.

Run the retained-data smoke check (requires a table without additional required
business fields, such as the default demo table):

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/mysql-local-smoke.ps1
```

It checks documentation, readiness, record creation/PATCH/read, idempotency
replay/conflict, tenant and browser-origin rejection, and actual CSV processing.
It retains two smoke records and an upload history entry per run.

For subsequent startup and inspection from this directory:

```powershell
docker compose --env-file ../../.local/mysql-dev/compose.env --env-file ../../.local/mysql-dev/ingestion.env -f docker-compose-mysql.yml up -d --wait
docker compose --env-file ../../.local/mysql-dev/compose.env --env-file ../../.local/mysql-dev/ingestion.env -f docker-compose-mysql.yml ps
docker compose --env-file ../../.local/mysql-dev/compose.env --env-file ../../.local/mysql-dev/ingestion.env -f docker-compose-mysql.yml logs ingestion-service ingestion-worker migrate-ingestion
```

Rerun the setup script after code changes, migration changes or scope changes.
It stops ingestion before refreshing account grants, builds the image, runs the
installer to completion and waits for both containers to be healthy. Drain work
before selecting a different tenant/table: existing queued work is retained and
the worker only serves the selected scope.

To stop ingestion while retaining the shared services and all data:

```powershell
docker compose --env-file ../../.local/mysql-dev/compose.env --env-file ../../.local/mysql-dev/ingestion.env -f docker-compose-mysql.yml stop ingestion-service ingestion-worker
```

Generated credentials, configuration, migration plans and scope settings are in
the ignored `.local/mysql-dev` directory. Preserve them with the database volume.
Do not delete volumes or use `--remove-orphans` when switching shared Compose
files. Neither the API nor worker receives the root secret or administrative
socket. Each runtime has an eight-connection pool; transient worker health checks
also open a separate pool. Worker health checks verify database prerequisites,
not processing latency or an external delivery system. This local verification
does not establish production rollout or load acceptance.

Verified on 2026-11-05: image build; Go tests and vet for `internal/mysqlruntime`,
`internal/httpapi` and the three MySQL commands; the live smoke script; numeric
and string field readback; and a second setup run preserving tenant identity,
credentials and previously ingested records. Both ingestion containers reported
healthy against the existing shared MySQL installation with data-model v007.

The November 5 metrics completion rerun verified all 15 operations (nine live suites / 83
HTTP requests) and the full race-enabled regression (249 top-level tests / 638
test and subtest results), with clean vet and no skipped tests or race warnings.
See the endpoint status above for reports, scope and retained-history limits.

The November 7 optimization rollout passes all 15 operations again (nine suites /
85 HTTP requests). Its combined regression inventory accounts for 250 top-level
tests and 645 passing test/subtest results, clean vet, no missing outcomes and no
current failures, skips or race warnings. The full-suite attempt, completion
shard and focused repairs are retained in the endpoint/worker status reports.

## Exact velocity summaries

Ingestion v006 adds `ingestion_velocity_definitions` and
`ingestion_velocity_buckets` in the same `fraud` database. Normal API and worker
writes check the installed definition inside their authorized record transaction.
Record changes, summary changes, audit/outbox effects and replay receipts commit
together. Replays do not increment counters. Account, amount and timestamp
corrections subtract the old contribution before adding the new one; late events
use their actual event-time minute.

The installer supports an append-only upgrade from the retained 23-step plan to
25 steps. It checks every original source step and the independent reference
definitions, retains an exclusive backup of the old plan, and adds only the new
suffix. The normal target lineage runner still rejects application schema drift.
An absent retained plan is not reconstructed from an existing application schema.
Stop ingestion API/worker, run `migrate-ingestion` with the upgraded image, then
start both upgraded containers. Refresh the decision installer grants before
restarting its API, worker and local providers. No volumes or business rows are
reset. The local 23-to-25 upgrade and a second installer run passed on 2026-11-07.

Summaries are opt-in per tenant/table/publication version. The group must be a
string, the time a timestamp, and the amount an integer. Store money in integer
minor units for this path. Nullable amounts preserve `COUNT(amount)` and SQL
`SUM` semantics. The default demo `amount` is a float and remains on ordinary
SQL; setup does not create a summary definition automatically.

After upgrading both ingestion containers, edit the example to match existing
published fields and place it in the generated runtime directory:

```powershell
Copy-Item testdata/velocity-definition.example.json ../../.local/mysql-dev/ingestion-runtime/velocity-definition.json
docker compose --env-file ../../.local/mysql-dev/compose.env --env-file ../../.local/mysql-dev/ingestion.env -f docker-compose-mysql.yml run --rm --no-deps migrate-ingestion /app/install-ingestion --velocity
```

This is an explicit installer maintenance operation for the already configured
tenant/table. It rechecks current authorization and publication, takes the physical
registry exclusively, drains in-flight writes, and atomically replaces the
definition and its complete backfill. No partial summary becomes visible.
Backfill and replacement are limited to 100,000 rows/buckets and ten seconds;
larger histories need a separate staged activation protocol. Normal runtime
accounts can maintain buckets but cannot edit definitions or delete summaries.

The decision reader uses summaries only for exact group equality plus a
half-open timestamp window of at most 24 hours. Complete minute buckets and raw
boundary records are combined in one SQL statement, preserving a coherent data
snapshot. Floats, distinct counts, other aggregate shapes, extra predicates,
groups over 256 bytes, stale publication versions and windows without a full
minute use ordinary SQL. A new publication requires an explicit rebuild before
its summaries become eligible again.

All record mutations must use the updated ingestion owner. Direct SQL edits
require rebuilding before summary use; record deletion is not an ingestion API
operation. Buckets are retained without an automatic TTL so historical queries
do not silently lose coverage. The native owner tests cover replay, late events,
NULL transitions, account/time/amount corrections, atomic rollback, partial
backfill rejection, tenant scope and concurrent moves between summary interiors
and raw window boundaries.
