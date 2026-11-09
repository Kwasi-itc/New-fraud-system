# MySQL data-model worker status

2026-11-05. **The local MySQL index-worker migration is implemented and running.**
`cmd/mysql-worker` consumes the same durable MySQL queue as the API. The local
Compose service `data-model-worker` executes search, navigation and repair jobs,
including string fields. PostgreSQL `cmd/worker` remains the PostgreSQL entrypoint.

## Verified behavior

The [final migration report](evidence/mysql-worker-migration/2026-11-05/report.json)
records clean `go vet`, **174 top-level tests / 954 passing test and subtest
results across 15 tested packages**, zero failures, zero skipped tests and zero
race warnings. It includes MySQL runtime transactions, the full physical recovery
matrix, schema-manifest validation, decision parity and shared worker/local-runtime
checks. [Complete Go events](evidence/mysql-worker-migration/2026-11-05/full-tests.jsonl)
retain individual results. The separate [endpoint rerun](evidence/mysql-worker-migration/2026-11-05/endpoints/report.json)
passed all 43 operations, 10 suites and 428 HTTP requests with the worker paused
for deterministic queue assertions, then resumed.

The [live acceptance report](evidence/mysql-worker-migration/2026-11-05/live-acceptance.json)
records seven passing checks against the actual MySQL 8.4.8 deployment:

| Check | Observed result |
| --- | --- |
| API enqueue through physical completion | Index exists; job applied; delivery completed atomically with audit |
| String search | Prefix index; full 40 KB values retained; colliding prefixes compare correctly; indexed lookup over 1,002 rows |
| Caption and navigation | Automatically queued work executes, including text filter plus timestamp ordering |
| Reconciliation repair | Dropped fixture index restored from retained definition; repair applied; report returns healthy |
| Duplicate enqueue | One physical effect and one apply audit |
| Injected apply-audit failure | Atomic completion rolls back; retry uses committed publication without duplicate DDL |
| Hard process kill during DDL | Expired lease rescued; immutable plan resumed; one index, open gate and one completion audit |

Only newly allocated acceptance fixtures were modified. Application data and
volumes were not reset. The earlier [worker audit](evidence/mysql-worker-audit/2026-11-05/report.json)
and failed no-consumer [probe](evidence/mysql-worker-audit/2026-11-05/runtime-probe.json)
are historical evidence of the gap this implementation closes.

## Execution and recovery

- One execution slot, one-second polling, one-minute lease, ten-second heartbeat
  and a fifty-second work deadline. Target/reference pool limits are eight/four
  connections. Shutdown cancels and joins running work.
- Maintenance runs every five seconds, at most one 100-row page per stream per
  pass. Leases fence physical execution, publication and atomic completion.
- Expected definitions come from published predecessor intents and the separate
  reference server. Retries use the saved intent and original epoch/version.
  Target drift is never accepted as the desired schema.
- A per-table lock serializes index work with field mutation. Temporary DDL
  credentials are scoped to that table and deactivated before publication.
- Execution errors commit pending/retry or failed/exhausted domain outcomes,
  delivery disposition and audit together. Retry retains error history;
  successful redelivery after publication does not repeat DDL.
- Earlier local deliveries with NULL error history are initialized with an audit
  only when never claimed and error-free. Attempted or ambiguous history is not
  guessed. Inspection/redrive CLIs remain the explicit operator recovery path.
- Repair restores only absent, known, nonunique managed indexes from the retained
  published definition. Extra columns, changed keys or trigger drift fail exact
  verification. Arbitrary schema repair is not authorized by a repair job.

The append-only v007 installation upgrade adds the scoped job-list index and
predecessor-intent lookup index. It preserves all retained earlier steps, saves
the old artifact as `data_model.json.before-upgrade`, and applies journaled ALTERs
without rebuilding application tables. The schema manifest now binds PostgreSQL
migration 000009 and its MySQL equivalent.

## Text index semantics

String lookup keys use bounded nonunique BTREE prefixes (normally 191 characters,
reduced for large composite keys). Complete values and SQL predicates/orderings
remain unchanged. Prefix collisions do not imply value equality or uniqueness;
unique text still uses the full-value exact-key protocol. This does not promise
PostgreSQL trigram/substring performance or index-only sorting of arbitrary long
strings. See MySQL's [column index documentation](https://dev.mysql.com/doc/refman/8.4/en/column-indexes.html).

## Reproduce

Start the local stack using [MYSQL_DOCKER.md](MYSQL_DOCKER.md), including installer,
API, author and `data-model-worker`. Health is private:

```sh
docker compose -f docker-compose-mysql.yml exec data-model-worker /app/mysql-worker --healthcheck
node scripts/mysql-worker-acceptance.mjs --output evidence/mysql-worker-migration/live-acceptance.json
```

Live acceptance deliberately pauses/kills/restarts the development worker and
injects a temporary failure only for its new job. Cleanup restores the worker
and removes its temporary trigger/lock.

Disposable integration, race and physical worker checks:

```sh
docker compose -p data-model-mysql-worker-tests -f compose.mysql-test.yaml -f compose.mysql-worker-test.yaml up --abort-on-container-exit --exit-code-from test
```

Or `make test-mysql-workers`. Disposable PostgreSQL supplies source parity for
the decision journey; actual execution targets MySQL. Coverage includes managed
text/navigation/repair, audit rollback, stale leases, tenant and authority
rejection, retry, maintenance, inspection/redrive and decision parity. Legacy
native-only API rejection tests remain intentional; the host uses the managed API.
The base Compose command runs the complete MySQL regression; the worker overlay
supplies the additional parity fixtures.

The endpoint audit has queue-state assertions: pause the local worker around
`node scripts/mysql-endpoint-audit.mjs` and always unpause it in cleanup. Those
queue checks complement the separate live execution report above.

## Deployment boundary

Acceptance is for the explicit local development administrator runtime
(`LOCAL_MYSQL_NO_AUTH=1`). Administrative credentials stay in private processes;
the worker has no published port, a read-only filesystem, bounded resources,
health checking and graceful shutdown. Production grants/identity, River cutover,
representative sustained load and rollout remain separate acceptance. Repeated
process deaths that exhaust delivery attempts or ambiguous historical errors
require operator inspection; the host does not fabricate an applied result to
clear recovery state.
