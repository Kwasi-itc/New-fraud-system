# Ingestion MySQL worker status

2026-11-07. The local `ingestion-worker` runs `cmd/mysql-worker --local` against
the same shared `fraud` database as ingestion and data-model. It executes CSV
uploads and deferred single-record requests. It has no published port and uses
its own restricted account. PostgreSQL/River deployment remains separate.

## Capability and integration matrix

The [current regression report](evidence/mysql-integration/2026-11-07T08-06-30Z/report.json)
accounts for **250 top-level tests / 645 test and subtest results**, including all
**35 process tests and 13 PostgreSQL parity tests**, with clean vet and no missing,
failed or skipped latest outcomes or race warnings. The report combines the
full-suite attempt, its unfinished shard and focused repairs; original failures
are retained. New summary tests prove atomic maintenance with record effects,
replay, corrections and rollback, including the restricted worker's actual grants.
Metrics acceptance includes queued,
running and terminal states, retry/recent-window values, configured scope,
revoked credentials/grants, redrive deduplication and concurrent completion.

The [earlier isolated acceptance report](evidence/mysql-integration/2026-11-05T11-58-31Z/report.json)
records **242 top-level tests / 631 passing test/subtest results**, clean vet,
zero failed or skipped tests and zero race warnings. All **34 `TestProcess*`
tests** executed, including shutdown/hard-kill rescue, lost COMMIT acknowledgement,
timeout, dependency outage and operator recovery. Nine packages contained tests;
six additional packages compiled without test files and are reported separately.
The adjacent `raw.jsonl` retains individual assertions and outcomes.

| Capability | Implementation and evidence |
| --- | --- |
| CSV execution | HTTP multipart request creates durable resource/job; the real worker writes records and publishes completed row/attempt counts. `TestProcessUploadLifecycle`, live CSV suite |
| Deferred single writes | Saturated synchronous path returns 202; GET polls queued/completed state; worker retains caller key and later replay. `TestProcessDeferredLifecycle`, live deferred suite |
| Scheduled availability | Future work is not claimed early. `TestProcessScheduledKinds` tests the internal owner API; HTTP scheduling is not exposed |
| Competing workers | Claims and atomic effects remain deduplicated. `TestProcessCompetingWorkers`, concurrent claim and receipt tests |
| Tenant-scoped claims | Worker does not claim another tenant's jobs. `TestProcessScopedWorkerClaims`, `TestProcessCrossTenantReadAndWork` |
| Invalid work | Duplicate CSV headers, malformed retained JSON and validation failures terminate with redacted error codes and no successful record/outbox effects. `TestProcessMalformedWork`, `TestProcessFailedWorkDetails` |
| Retry/exhaustion | Injected outbox failure rolls back records/audits/receipts and eventually discards the job without leaving a live reservation. `TestProcessRetryExhaustion` |
| Dependency outage | Disconnected storage returns unavailable; recovery drains durable work. `TestProcessDependencyOutage` |
| Graceful shutdown/hard kill | In-flight work rolls back; another process rescues the expired lease and completes once; the stale lease is rejected. `TestProcessWorkerInterruption` |
| Lost COMMIT acknowledgement | A proxy drops the actual final COMMIT OK packet. Restart retains one record, receipt, audit and event. `TestProcessWorkerCommitLoss` |
| Handler timeout/fencing | Blocked receipt work times out without business effects. `TestProcessHandlerTimeout`, heartbeat/fencing and queue recovery tests |
| Trusted cancellation/redrive | Internal operator adapter supports authorized cancellation, immutable action replay and redrive. `TestProcessOperatorRecovery`, `TestWorkOperator*`, `TestWorkRedrive*`; no application HTTP operator route |
| Inspection/metrics | Admin metrics HTTP returns the complete configured-scope snapshot, including retries, oldest age and recent drain. Current-job joins exclude historical redrives. `TestProcessDeferredMetricsEndpoint`, `TestFullSharedDeferredSnapshot*`, `TestLiveParityDeferredMetrics`, `TestDeferredSnapshotIndexedHistory`; broader operator inspection remains an internal adapter |
| Monitoring/scoring delivery | **Not implemented/configured here.** Ingestion appends durable pending outbox events; this worker does not dispatch them to external consumers |

The isolated suite starts real API/worker processes, exercises physical records
and retained audit/outbox/receipt state, and injects failures only in its own
disposable databases. Those process fault tests are distinct from the live local
Compose audit, which verifies CSV and deferred processing but does not kill the
development worker or introduce failing application triggers.

The [earlier live report](evidence/mysql-endpoint-audit/2026-11-05/report.json) passed the
CSV and deferred suites, including malformed CSV termination, row/attempt counts,
actual record readback, queued-state polling and original-key replay after
deferred completion. Its historical 14/15-operation outcome predates metrics
HTTP implementation. The updated live audit additionally verifies queued and
completed counts around actual worker execution and compares the current-job
snapshot with SQL; it requires successful coverage of all 15 operations.

The [current live report](evidence/mysql-endpoint-audit/2026-11-07-throughput/report.json)
passes **9/9 suites and 15/15 operations across 85 HTTP requests**, including
the full metrics response, queued/completed transitions and scoped SQL counts.

## Local execution policy

- One runner for `local_ingestion_uploads` and one for
  `local_ingestion_deferred`, each with concurrency one: up to two active handlers
  per worker process. Claims remain tenant-scoped.
- Eight worker database connections, separate from the API's eight. Local API
  setup uses four synchronous write slots to preserve connection headroom for
  deferred admission and reads.
- One-second polling with bounded jitter, thirty-second leases, heartbeat at
  one third of the lease, ten-second handler deadlines and three maximum attempts.
  The common runner uses one-second retry base and ten-second maximum backoff.
- Each runner reaps at most 100 expired jobs per pass at half its lease interval.
  Business effects and terminal completion are protected by the current lease
  and committed together. Shutdown cancels and joins the runners; Compose grants
  thirty seconds before forceful termination.
- The read-only container receives the worker password and internal token,
  but no root password, administrative socket or schema mutation privileges.

## Reproduce

Start the stack using [MYSQL_DOCKER.md](MYSQL_DOCKER.md). Check the running
worker's database prerequisites from this directory:

```powershell
docker compose --env-file ../../.local/mysql-dev/compose.env --env-file ../../.local/mysql-dev/ingestion.env -f docker-compose-mysql.yml exec ingestion-worker /app/mysql-worker --local --healthcheck
node scripts/mysql-endpoint-audit.mjs
```

The live audit restores its brief worker pause in cleanup. It retains its new
records and queue history. Its exit code rejects failed suites and missing
documented-operation coverage, including the metrics endpoint.

For isolated retry, kill/restart, lost-commit, rollback, permissions, cancellation,
redrive, parity and race checks:

```powershell
docker compose -p ingestion-mysql-tests -f compose.mysql-test.yaml up --abort-on-container-exit --exit-code-from test
node scripts/summarize-mysql-tests.mjs evidence/mysql-integration/<printed-timestamp>
docker compose -p ingestion-mysql-tests -f compose.mysql-test.yaml down
```

## Deployment limits

The health check opens a fresh restricted pool and checks database prerequisites;
it is not a heartbeat measuring progress or processing latency. The runtime
uses the configured tenant/object type in isolated profiles. The explicit local
development profile also serves other active published owners, resolving and
revalidating each leased resource before processing. Its admin metrics retain
the configured fixture scope. Drain outstanding work before changing an isolated
runtime's scope. Failed jobs remain retained for trusted operator inspection;
there is no general operator HTTP or dead-letter service in this Compose setup.
Production authorization, River cutover, external consumers, sustained throughput
and restart faults against the exact development container image remain separate
acceptance. Isolated process tests must not be reported as live production tests.
