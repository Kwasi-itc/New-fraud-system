# Isolated ingestion reads and operations: next 20 existing leaves

Status: Complete for the 20 selected scoped tasks, verified 2026-10-30. Production migration remains in progress.

Selected IDs: IN-01.2.040, IN-01.2.056; IN-01.3.013f, IN-01.3.017,
IN-01.3.019, IN-01.3.022, IN-01.3.030; IN-01.4.013, IN-01.4.019,
IN-01.4.022, IN-01.4.023, IN-01.4.024, IN-01.4.025, IN-01.4.026,
IN-01.4.027, IN-01.4.028, IN-01.4.030, IN-01.4.031, IN-01.4.032,
IN-01.4.033. These are existing IDs; no new leaves replace missing work.

Independent schema review, shared Q-01 operational acceptance, live legacy
inventory/dispositions and production activation are excluded. PostgreSQL/River
remain the default application runtime. Isolated process commands now expose
existing read and aggregate transports in addition to write/work capabilities.
Production parity children remain open where full supported-type/collation/error
parity has not been established.

## Event ownership finding (IN-01.2.040)

`internal/store/mysql/record_owner.go` appends `record.ingested`, `record.updated`
and `batch.ingestion.completed` transactionally with physical rows, audits and
receipts. `internal/service/ingest_service.go` produces the corresponding source
events. UUID identity is retained in each outbox row; replay must retain existing
rows, payloads and identities. Record payloads carry tenant_id, object_type,
object_id, mode, revision_id, action, record and ingested_at. Batch payloads carry
tenant_id, object_type, mode, revision_id, count and ingested_at. Status is pending.

Repository-wide searches of event literals and `core_ingestion.outbox_events`
found ingestion's append repository, schemas and tests, but no wired ingestion
outbox delivery dispatcher. Ingestion owns delivery state; this batch does not
invent delivery or mark events delivered. The staged MySQL delivery-state schema
still awaits the independent deployment disposition in IN-01.2.016.

`decision-engine-service/internal/httpapi/handlers/decisions.go:HandleRecordIngested`
accepts an explicit trigger request through `/ingestion-events/record-ingested`.
`frontend/src/lib/decision-engine-api.ts` calls it. That request uses fields and
sync/async mode, whereas outbox mode means create/patch and values are under
record. It is not a direct event consumer and provides no outbox-event-ID delivery
acknowledgement contract. Raw forwarding would be incorrect. No direct consumers
of record.updated or batch.ingestion.completed were found. Delivery integration
is therefore an explicit remaining system gap, not a claimed capability.

The contract audit is complete when source/target event shape and replay identity
checks pass; it does not mean an absent dispatcher has been implemented.

## Implementation and test map

Paths in this section are relative to `backend/ingestion-service`. Test names
are in `internal/store/mysql` unless otherwise stated. Process acceptance uses
actual race-enabled API/worker binaries and complete owner-authored fixtures.

| Existing leaf | Implementation / acceptance |
|---|---|
| IN-01.2.040 | Event audit above; `TestProcessEventContract` verifies payload/type/identity and pending status across replay. No dispatcher is claimed. |
| IN-01.2.056 | Actual configured worker SIGINT joins inside its five-second budget; `TestProcessWorkerInterruption/shutdown` then starts another process and checks recovery. |
| IN-01.3.013f | `Service.GetRecord`, `WithRecordRead`, `ReadCurrentExact`; restricted account, 404, current publication, typed fields, concurrent writes in ReadContracts/ConcurrentReadsAndWrites/APIRuntimeGrants. |
| IN-01.3.017 | `ScalarPredicate` string comparisons and escaped literal prefix; StringPredicateContracts and live HTTP ReadContracts cover case and wildcard behavior. |
| IN-01.3.019 | `ReadCurrentPage` orders updated_at DESC then byte-exact object_id ASC; existing limit-only API, deterministic ties and bounded limits in ReadContracts. |
| IN-01.3.022 | Existing get/list/search handlers inject `IngestReader`; route paths and response wrappers are retained and exercised by ReadContracts. |
| IN-01.3.030 | Existing aggregate handler calls the typed builders for six operations; ReadContracts verifies all six and a filtered count, with tenant scope and bounded deadlines. |
| IN-01.4.013 | ReceiptCollision forces equal digests under the real shared-schema owner; distinct full-byte keys replay separately and overflow rolls back before effects. |
| IN-01.4.019 | PublicationRace closes the publication gate while an HTTP write waits; RevocationDuringReplay archives the authorized table during receipt serialization; ReadRevocation covers tenant status. |
| IN-01.4.022 | WorkerCommitLoss intercepts the actual final completion COMMIT OK, restarts the worker, and verifies one attempt/effect set. |
| IN-01.4.023 | WorkerInterruption/kill terminates the actual process with a live lease; another process rescues it and the original token is rejected. |
| IN-01.4.024 | MalformedWork covers malformed CSV, rejected malformed JSON submissions, corrupted retained JSON, trailing values and validation failures; verifies terminal codes/counters/audits. |
| IN-01.4.025 | RetryExhaustion, HandlerTimeout, WorkerInterruption and the shared IngestionWorkRecovery regression cover retries, bounded blocking, expiry, direct heartbeat and terminal reservation release. |
| IN-01.4.026 | OperatorRecovery composes trusted operator cancellation/redrive with actual worker execution; immutable resource identity, action replay and terminal cancellation are preserved. |
| IN-01.4.027 | WorkerInterruption/shutdown verifies joined cancellation and later expiry recovery under the configured policy. |
| IN-01.4.028 | DependencyOutage disconnects and rejects API/worker database transport, then restores it and reconciles accepted work. The MySQL server itself is not stopped. |
| IN-01.4.030 | MeasuredWorkloads records deterministic hot/distributed keys, concurrency 1/4, per-request latency, throughput, sampled lock waits and durable counts. |
| IN-01.4.031 | The same workloads run with/without workers; account connection sampling is checked against configured combined pools, with fixture-account overhead identified. |
| IN-01.4.032 | Two final fail-closed, race-enabled process suites retain discovered/executed/pass/fail/skip counts, source/version provenance and variability. |
| IN-01.4.033 | `reconcileProcessFixture` reports resource/current-job states and durable counts; missing jobs, terminal reservations and unexpected pending work fail acceptance. |

Names in the table omit `TestProcess` where clear. The only explicitly pending
resource dispositions are API-only enqueue acceptance (APIRuntimeGrants) and
intentional unselected/foreign queue ownership (ScopedWorkerClaims). These
fixtures never claim that those resources completed. Every other process
fixture must finish accepted work in a terminal state. Pending outbox rows are
expected because no delivery dispatcher is configured.

## Read contract and deliberate limits

The production handler implementation is reused in the isolated composition;
this is code-path adoption, not activation in the default deployment. Read
callbacks execute in READ COMMITTED transactions and retain the exact table's
publication barrier until results are collected. The current contract is checked
again after barrier acquisition. Exact lookup uses the fixed-domain dictionary
lookup plus indexed retained object key; no broad history scan is used to find an
identity. The list query remains the existing limit-only contract: nonpositive
limits normalize to 100 and limits above 1000 fail with 413. No offset or cursor
support is invented. Deterministic tie-breaking does not create a snapshot
across separate requests, and no indexing/performance improvement is claimed.

All active projected fields are validated (maximum 100). Transfer is guarded in
SQL before scanning: 16 KiB per text field, 2 MiB per row transfer, 1 MiB decoded
row, 4 MiB decoded page; JSON response size is checked before success. Statements
share a 2048-parameter, 2 MiB parameter-byte, 1 MiB SQL budget. Read concurrency is
four and aggregate concurrency two; aggregate deadline is five seconds. The
existing authenticated request deadline also applies. Unsupported mapped types,
filters or aggregate operations fail explicitly; no PostgreSQL/HTTP fallback is
used after a storage or authorization failure.

String equality/ranges compare full bytes, preserving case, trailing spaces and
literal values. Prefix matching is case-sensitive and escapes %, _ and the
explicit ! escape character. This intentionally fixes the source wildcard-as-
pattern behavior already identified in the query-semantics audit. Locale-specific
PostgreSQL ordering parity is not asserted. String/IP membership and full
aggregate type/precision parity remain under their existing open compatibility
leaves. Integer AVG is serialized as an exact JSON number from its validated
bounded decimal representation, not a JSON string or a float conversion.
Missing exact records return the established staged 404 error envelope. Full
source status/error parity remains a separate acceptance task.

## Recovery changes and observed failures

Receipt authorization now runs again after bucket serialization, before replay
or new effects. The table-archival race verifies the additional check. Tenant
foreign-key locking can serialize tenant revocation differently, so it is not
used to pretend a particular table-policy interleaving occurred.

MySQL client cancellation alone did not promptly release a server-side row-lock
wait: the first blocked-worker test remained running after its handler context
expired. `mysqlstore.Config.LockWaitTimeout` now supplies an optional whole-second
server bound on every new connection. The isolated runtime derives it from the
handler budget (one second below it, with a one-second minimum). This applies to
InnoDB and metadata lock waits; existing callers leaving it zero retain their
prior policy. The corrected test exhausts attempts and releases reservations
without committed effects. It does not claim arbitrary SQL statements are
server-cancelled instantly. Long-running query and deployment-wide timeout policy
remain part of broader operational review.

The verifier originally hit its seven-minute envelope as the suite grew. It now
budgets suite time from discovered test count, retains per-request/worker bounds,
and kills the entire test process group on timeout in the required Linux test
container. A regression proves spawned children terminate. Partial, timed-out,
failed or skipped suites remain rejected evidence.

## Measurement interpretation

Each configuration has 32 closed-loop HTTP requests, concurrency 1 or 4, with
one repeated key or 32 distributed identities. Worker interference adds eight
accepted deferred jobs per configuration. There is no offered fixed-rate claim,
capacity estimate, production SLO or cold-cache claim. Raw per-request latencies,
counts, account connection peaks and 10 ms lock-wait samples are retained. The
sampling query and race detector add overhead. Generator and application share
the isolated verifier environment; this is a small comparison, not a validated
load-generator capacity test. Repeated results report variability rather than
an optimization claim. No production schema, data or shared load generator was
used.

## Remaining gates

Independent deployment review, the shared Q-01 gate, retained legacy inventory,
reviewed exception dispositions and transfer rehearsal remain open. Parent
migration completion is not implied by this batch. PostgreSQL/River remains the
default application authority; no production cutover, dependency addition or
uncontrolled workload was performed.


Readiness acceptance also exposed an empty-file publication race. Both isolated
commands now share `mysqlruntime.PublishReady`: write and close a bounded private
temporary file, then atomically link it to the exclusive marker path. Competing
publishers cannot replace an existing marker or expose a partial address. The
concurrent publication regression and repeated process suites verify this change.
A rejected run using the former create-then-write marker is retained.

Measurement collection is fail-closed: a resource-query error or zero samples
rejects the run. Throughput is labeled requests per second, and final report
extraction independently checks durable record/audit/outbox totals against public
and private execution receipts. This prevents a successful HTTP-only measurement
from being presented as a correct ingestion workload.


Result/parameter budgets bound transfer and application allocation, not database
scan cost. The stable updated_at/object_id list order can require a filesort when
no matching owner-managed index exists. This batch adds no speculative physical
index and makes no query-plan improvement claim; representative scale/index
review remains required before deployment. Client deadlines likewise are not a
claim of immediate server-side cancellation of arbitrary long-running SELECTs.


## Accepted results and reproduction

Both final race-enabled process runs passed **32/32**, with zero failures/skips:
[first run](../evidence/2026-10-30_ingestion-operations20/accepted-1/summary.json),
[second run](../evidence/2026-10-30_ingestion-operations20/accepted-2/summary.json).
The full-schema suite passed 13/13; component regressions passed 21 tests,
handler/runtime/verifier regressions passed 28, and connector regression passed 1.
Scoped vet passed. The DTO package has no standalone test files; no required test
was skipped. See [verification](../evidence/2026-10-30_ingestion-operations20/verification.json),
[measurements and reconciliation](../evidence/2026-10-30_ingestion-operations20/report.md)
and [source/binary provenance](../evidence/2026-10-30_ingestion-operations20/provenance.json).
Rejected and superseded runs are retained separately and excluded from acceptance.

Use the existing isolated Linux Compose verifier with its disposable MySQL
endpoint and owner exporter, following the [fixture setup](02_ingestion_process_batch20.md#reproduction-and-evidence).
From `/backend/ingestion-service`, build and run twice using distinct new output
directories (the verifier rejects existing directories):

```sh
go build -race -mod=readonly -o /tmp/ingestion-mysql-api ./cmd/mysql-api
go build -race -mod=readonly -o /tmp/ingestion-mysql-worker ./cmd/mysql-worker
INGESTION_DATA_MODEL_FIXTURE_EXPORTER=/tmp/ingestion-dm-fixture INGESTION_VERIFY_SUITE=process go run ./cmd/verify-mysql-fixture testdata/new-operations-run-1
INGESTION_DATA_MODEL_FIXTURE_EXPORTER=/tmp/ingestion-dm-fixture INGESTION_VERIFY_SUITE=process go run ./cmd/verify-mysql-fixture testdata/new-operations-run-2
go vet -mod=readonly ./internal/mysqlruntime ./internal/httpapi/... ./internal/store/mysql ./cmd/mysql-api ./cmd/mysql-worker ./cmd/verify-mysql-fixture
```

Exactly **20 existing leaves** were ticked. Current totals are **190 complete /
26 open / 216 total**, excluding parent tasks with child IDs. The exact-read
parent is also complete because all its children now pass; it adds no extra leaf.
[Completed IDs](../evidence/2026-10-30_ingestion-operations20/completed_tasks.json)
retain the exact batch membership. No commit, push, deployment, dependency
addition or production workload was performed.
