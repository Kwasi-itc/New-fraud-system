# Ingestion MySQL endpoint status

2026-11-07. A checked operation is mounted in the dedicated MySQL runtime.
Successful HTTP coverage, negative cases and isolated fault tests are separate
evidence; a checked route alone does not establish production readiness.

The shared OpenAPI specification documents **15 operations**. The MySQL runtime
mounts **all 15**. `/docs`, `/redoc` and `/openapi.yaml` are additional documentation
routes. The default PostgreSQL server remains a separate entrypoint.

| Operation | MySQL wiring | Integration coverage |
| --- | --- | --- |
| GET /healthz | [x] | Liveness without a database query |
| GET /readyz | [x] | Actual grants, schema/ledger checks and current published table; invalid startup and dependency outage in process tests |
| POST /v1/tenants/{tenantId}/ingest/{objectType} | [x] | Create, unkeyed execution, keyed replay/conflict, validation, concurrent requests, atomic audit/outbox rollback, lost commit acknowledgement |
| PATCH /v1/tenants/{tenantId}/ingest/{objectType} | [x] | Partial field preservation, ordered/concurrent updates, replay, forbidden scopes, overload admission to deferred work |
| POST /v1/tenants/{tenantId}/ingest/{objectType}/batch | [x] | Ordered results, replay, whole-batch validation rollback, empty and duplicate keys, late transactional effect failure |
| PATCH /v1/tenants/{tenantId}/ingest/{objectType}/batch | [x] | Partial updates, retained fields, duplicate-key rejection without writes, tenant isolation |
| POST /v1/tenants/{tenantId}/ingest/{objectType}/csv | [x] | Multipart admission, invalid mode/missing file, actual worker completion, duplicate headers, failure details, input bounds and foreign tenant rejection |
| GET /v1/tenants/{tenantId}/ingest/{objectType}/upload-logs | [x] | Scoped list, submitted upload present, indexed bounded repository queries and tenant isolation |
| GET /v1/upload-logs/{uploadLogId} | [x] | Pending/completed/failed state and row/attempt counts, malformed/missing IDs, another tenant's resource hidden |
| GET /v1/deferred-ingests/{deferredIngestId} | [x] | Actual overload admission, queued/completed state, keyed replay after execution, malformed/missing IDs and foreign resources |
| GET /v1/tenants/{tenantId}/records/{objectType}/{objectId} | [x] | Typed numeric/string readback, absent records, exact byte identity, tenant/authority revocation, oversized results and concurrent publication |
| GET /v1/tenants/{tenantId}/records/{objectType} | [x] | Bounded result count, deterministic ordering, invalid limits, result budgets and concurrent reads/writes |
| GET /v1/tenants/{tenantId}/records/{objectType}/search | [x] | Published-field equality, unknown/missing fields, literal text, numeric/IP normalization and scope rejection |
| POST /v1/tenants/{tenantId}/query/aggregate | [x] | COUNT/SUM/AVG/MIN/MAX/COUNT DISTINCT, filtered values, invalid operators, type and statement/result bounds, source parity |
| GET /v1/admin/deferred-ingest-metrics | [x] | Full response contract, queued-to-completed transitions, PostgreSQL parity, tenant/table isolation, authentication/revocation, missing grants, redrive lineage, bound/integrity failure, repeatable-read consistency and a 2,000-resource indexed-history plan |

## Deferred metrics contract

`GET /v1/admin/deferred-ingest-metrics` returns the existing
`deferred_ingest_metrics` envelope with queue/processing/completed/failed counts,
retry-pending and retried-execution counts, oldest queued request and age, recent
five-minute success/failure/retry counts, drain rate and database snapshot time.
The MySQL runtime restricts it to its configured tenant and stable table ID;
query parameters cannot change that scope. Missing/invalid bearer credentials
return 401 in authenticated mode; local login-free mode retains owner scope.

A repeatable-read transaction revalidates current authority and reads a bounded
indexed resource range with each resource's current job. CSV and foreign work
are excluded. Redrive's historical jobs do not duplicate resources; attempts and
recent retry counts describe the current job generation, matching resource GET.
Cancelled jobs contribute to failed counts, matching that endpoint's existing
failed DTO. Recent retry counts are execution counts, as in PostgreSQL, rather
than a fabricated history of every attempt. Drain rate is recent successes / 5.

The five-second request deadline and 10,000-retained-deferred-resource limit are
server-owned. Overflow returns 413 without partial totals; missing current jobs
or storage/grant errors return unavailable. Payloads, caller keys, result JSON,
lease tokens and error text are not selected. The existing resource history
index and runtime SELECT grants suffice; no schema mutation is performed by GET.

## Live acceptance

Run from this service directory after [local setup](MYSQL_DOCKER.md):

```powershell
node scripts/mysql-endpoint-audit.mjs
```

The runner derives its operation inventory from the shared OpenAPI file. It
records HTTP bodies/statuses, passing suites, runtime image identities and a
per-operation coverage matrix under `evidence/mysql-endpoint-audit/<timestamp>`.
Only a successful response in a passing suite counts as successful operation
coverage. An expected rejection cannot turn an incomplete operation green. The
command fails if any documented operation lacks a successful response in a
passing suite, or any assertion fails.

The [current live report](evidence/mysql-endpoint-audit/2026-11-07-throughput/report.json)
passed **9/9 suites, all 15 documented operations and 85 HTTP requests**. Metrics
were checked while a real deferred request was queued and after worker
completion, and totals were compared with SQL restricted to the configured
tenant/table and each resource's current job. Both coverage acceptance fields
are true. The adjacent trace and runtime files retain HTTP assertions and the
actual image identities.

The [earlier live report](evidence/mysql-endpoint-audit/2026-11-05/report.json)
recorded 14/15 operations before metrics implementation. It is historical,
including its expected metrics 404. The updated runner checks the full metrics
contract, queued/completed transitions, caller scope overrides and actual
resource/current-job counts in SQL. Each run retains `http.jsonl`, `runtime.json`
and the per-operation `report.json` identifying the tested image IDs.

The [initial report](evidence/mysql-endpoint-audit/2026-11-05-initial/report.json)
retains the failed pool-saturation result. Its duplicate-batch expectation was
also corrected from 409 to the established 422 validation contract. The final
rerun verifies both duplicate rejection without writes and deferred admission
with reserved connection capacity.

The audit uses the saved local tenant/object type and expects the default demo
model with nullable `amount` and `currency`. Every record/key has a fresh UUID
prefix. Records, receipt buckets, audits, outbox events and upload history are
retained; shared data and volumes are never reset. To prove overload admission,
the test briefly pauses the worker, blocks only its fresh receipt bucket,
occupies the configured synchronous slots, polls a real deferred request, then
releases the transaction and restores the worker in cleanup. Avoid running it
concurrently with another operational audit of this same local worker.

## Isolated integration and fault acceptance

The [current regression report](evidence/mysql-integration/2026-11-07T08-06-30Z/report.json)
accounts for **250 top-level tests / 645 passing test and subtest results across
nine tested packages**, including **35 process tests and 13 PostgreSQL parity
tests**. An independent 250-test inventory has zero missing outcomes. This combines
the full-suite attempt, completion of its unfinished shard, and focused repairs;
it is not a single uninterrupted run. The original timeout and failed attempts
remain in the evidence. Latest complete outcomes have zero failures, skips or
race warnings, and vet passed. Six command/DTO packages compile without tests.

New velocity acceptance covers exact integer sums/counts, late events, nullable
amounts, account/time/amount corrections, replay, rollback, bounded atomic
backfill, tenant scope and coherent reads during concurrent raw-edge/bucket moves.
The restricted worker maintains active summaries while definition mutation and
bucket deletion remain forbidden. The [local upgrade proof](evidence/mysql-integration/2026-11-07T08-06-30Z/local-upgrade.json)
records the unchanged 23-step prefix, two appended ingestion v006 steps and two
successful installer runs. The [fresh smoke](evidence/mysql-integration/2026-11-07T08-06-30Z/local-smoke.log)
passes documentation, readiness, record/replay/isolation and CSV worker checks.

The new metrics tests cover every response field, empty snapshots, scheduled and
retry-pending work, terminal/cancelled states, current-job redrive deduplication,
tenant/table isolation, revoked authority, missing grants/jobs, exact limit
rejection, concurrent completion in a repeatable-read snapshot and a 2,000-row
mixed-history query plan. The parity test calls the existing PostgreSQL metrics
repository; it exposed and fixed its ambiguous timestamp parameter by casting
the five-minute-window input explicitly to `timestamptz`.

The [earlier isolated report](evidence/mysql-integration/2026-11-05T11-58-31Z/report.json)
passed **242 top-level tests / 631 test and subtest results across nine tested
packages**, including **34 process tests, 13 shared-owner tests and 12 source
parity tests**. Vet passed; there were no failed or skipped tests and no race
warnings. Six additional packages compiled with no test files; they are listed
separately and are not counted as skipped tests. The adjacent `raw.jsonl` and
`vet.log` retain complete evidence. MySQL store tests took 552.379 seconds with
the race detector, so the former 180-second whole-package budget was insufficient.

The isolated data-model exporter supplies v006 contract fixtures. The live suite
separately exercises the installed v007 lineage; unit tests reject partial,
reordered or changed v007 suffixes. This run does not establish an isolated
v006-to-v007 migration upgrade or production cutover acceptance.

```powershell
docker compose -p ingestion-mysql-tests -f compose.mysql-test.yaml up --abort-on-container-exit --exit-code-from test
node scripts/summarize-mysql-tests.mjs evidence/mysql-integration/<printed-timestamp>
docker compose -p ingestion-mysql-tests -f compose.mysql-test.yaml down
```

The test project publishes no ports and shares no application database/network
or data volume. It builds the data-model source exporter, race-enabled ingestion
API/worker and legacy-receipt importer before executing the MySQL suite.
PostgreSQL is a disposable source-parity fixture, not ingestion's runtime store.
Every test database/account is generated by the fixture and removed by its owner.
The summary rejects failed, skipped or empty test runs and race warnings; inspect
`raw.jsonl`, `stderr.log` and `vet.log` for the exact results.

The suite includes the domain, all HTTP handlers, MySQL runtime, MySQL store and
CLI packages. `TestProcess*` starts real API/worker processes with restricted
accounts. `TestFullShared*` uses owner-authored shared schema/contract fixtures;
receipt, writer, audit, query, permissions, installation recovery and queue
tests exercise MySQL directly. `TestLiveParity*` compares PostgreSQL behavior.

## Limits and findings

- Local mode is login-free and serves one configured tenant/object type.
  Token-authenticated isolated-process tests do not establish production identity.
- Deferred metrics count at most 10,000 retained resources in the configured
  scope. Larger exact snapshots return 413; no truncated counts are reported.
- The upload list currently returns at most 100 items without exposing a cursor;
  the repository has cursor support, but HTTP pagination is not wired.
- Saturation testing found eight synchronous slots competing for an
  eight-connection pool. Local setup now uses four write slots, reserving
  connections for deferred admission, reads and readiness. The live deferred
  test verifies admission while those four writes remain blocked.
- External monitoring/scoring outbox delivery, production cutover and sustained
  representative load remain separate work. See [worker status](MYSQL_WORKER_STATUS.md).
