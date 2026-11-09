# Ingestion MySQL runtime

The dedicated `cmd/mysql-api` and `cmd/mysql-worker` entrypoints compose the MySQL
record owner, bounded record reader, CSV/deferred work owner and durable queue.
They do not construct a PostgreSQL pool or River client. The default server and
worker entrypoints still select PostgreSQL.

Local setup: [MYSQL_DOCKER.md](MYSQL_DOCKER.md).
Per-route evidence and gaps: [MYSQL_ENDPOINT_STATUS.md](MYSQL_ENDPOINT_STATUS.md).
Worker behavior and fault acceptance: [MYSQL_WORKER_STATUS.md](MYSQL_WORKER_STATUS.md).

## Shared schema and authority

All services share `fraud`. Data-model owns tenant/model metadata, physical table
publication and fixed-domain lookup routines. Ingestion reads the published
contract through those documented SQL/routine interfaces and writes authorized
records directly to the selected physical table. It owns its receipt, audit,
outbox, resource and queue tables and its independently tracked migration ledger.

Startup requires pinned MySQL 8.4.8, explicit restricted accounts, no active role,
complete owner artifacts and matching applied ledger checksums. It verifies
required table definitions, grants, contract/publication state and a nonmutating
record lookup. The data-model v006 contract lineage supports the complete
append-only v007 worker-index extension. Missing or changed artifacts, partial
upgrades, drift and missing capabilities fail startup/readiness.

Isolated process tests use generated `ingestion_full_test_*` databases and bearer
credentials. The persistent local authority requires explicit
`LOCAL_MYSQL_NO_AUTH=1`, a loopback `APP_ORIGIN`, generated Docker secrets and
`Authority=local-mysql-development`. The local HTTP wrapper binds its internal
credential on the server and rejects foreign browser origins. Tenant/object
scope is still checked by the service and revalidated in its SQL transactions.
This local authority does not implement production caller identity.

## Atomic ingestion and background work

Keyed requests canonicalize the raw typed input, reserve the tenant key, validate
the current published model, then commit record changes, audit, outbox and the
original response receipt together. Matching replays return the retained result
without repeating effects; changed commands conflict. Unkeyed executions use a
private identity namespace. Combined validation and audit failures are not
reported as clean validation successes.

CSV/deferred admission commits a resource, queue job and reservation. Worker
execution fences those effects with the current lease and commits terminal work
state alongside records, receipts, audits and outbox. Transient failures retry;
terminal payload/validation failures retain redacted errors. The worker does not
deliver pending outbox events to monitoring/scoring consumers.

Read queries use published physical mappings, explicit projections, bounded
filters/parameters/results and current authority. Supported aggregates are count,
sum, average, minimum, maximum and distinct count. The read/aggregate API does
not expose arbitrary SQL or general GROUP BY/window queries.

`GET /v1/admin/deferred-ingest-metrics` uses the existing response contract in a
read-only repeatable-read transaction. Counts cover only the configured tenant
and stable table, with one current job per deferred resource. It reads database
time, excludes payloads and historical redrive jobs, and returns 413 rather than
partial results beyond 10,000 retained resources. Missing lineage/storage fails
closed. The handler has a five-second deadline and uses existing runtime grants.

## Verification boundaries

The live endpoint runner uses retained uniquely named fixtures in the local
scope and records the tested image identities. The separate disposable Compose
suite builds all required helper binaries and tests domain/HTTP/runtime/CLI,
real MySQL transactions, process faults and PostgreSQL source parity with the
race detector. Its summary rejects skipped tests and race warnings.

The earlier 2026-11-05 [live audit](evidence/mysql-endpoint-audit/2026-11-05/report.json)
passed eight suites / 81 requests and covered all 14 mounted operations, while
flagging the missing fifteenth documented admin operation. The separate
[isolated report](evidence/mysql-integration/2026-11-05T11-58-31Z/report.json)
passed 242 top-level tests / 631 test and subtest results with clean vet, no
skipped tests and no race warnings. Source parity and worker process faults ran
on disposable fixtures; live tests used the actual local v007 installation.
Those results predate metrics implementation; the current endpoint runner
requires all 15 operations and exercises the complete metrics response.

The [metrics endpoint rerun](evidence/mysql-endpoint-audit/2026-11-05-metrics/report.json)
passed nine live suites, all 15 operations and 83 requests. It checks the metrics
contract and actual queued/completed transitions against the current local image.
The [current full regression](evidence/mysql-integration/2026-11-05T12-54-05Z/report.json)
passes 249 top-level tests / 638 test and subtest results, including 35 process
tests and 13 PostgreSQL parity tests. Vet passed with no skipped tests or race
warnings. Metrics-specific tests also verify authority, missing grants/lineage,
bounded indexed history, redrive and snapshot consistency during completion.

Upload-list HTTP cursors, multiple runtime
tenant/table scopes, production identity/cutover and external event delivery are
remaining work. Passing these suites does not establish sustained production
capacity or cover every failure mode of every operation.
