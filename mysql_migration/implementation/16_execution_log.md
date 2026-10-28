# Implementation execution log

## 2026-10-22: DB-01 / PX-01 foundation slice

User authorization: "start implementing" supersedes the earlier documentation-only
phase. This slice implements independent foundation mechanics; it does not accept
the remaining architecture proposals or switch any application database.

Implemented [backend/mysqlstore](../../backend/mysqlstore/README.md):

- Pinned Go MySQL driver v1.9.3 with checksums and an independent Go module.
- Typed, validated configuration; bounded pools/timeouts; verified TLS by default;
  UTC, strict SQL and READ COMMITTED session settings.
- Explicit mutation and read-only snapshot transaction helpers, rollback on
  callback errors/panics/cancellation, and a distinct commit-outcome error requiring
  reconciliation rather than automatic retry.
- Mechanical MySQL error classification, restricted physical identifier quoting
  and standard-byte-order BINARY(16) UUID codec.
- Isolated MySQL 8.4.8 / Go 1.25.8 Compose fixture with tmpfs, no published ports,
  no application volumes/networks and mandatory integration configuration.

Verification command from repository root:

```powershell
docker compose -p migration-foundation-test -f backend/mysqlstore/compose.test.yaml up --abort-on-container-exit --exit-code-from test --attach test
```

Final result: `go vet ./...` passed; `go test -mod=readonly -race -count=1 -v ./...`
passed all four top-level tests, with zero skips. Reported test duration: 1.272s.
Server reported MySQL 8.4.8, UTC session timezone and READ-COMMITTED isolation.

Real-engine assertions cover commit/rollback, callback panic, cancellation before
commit, interruption during SQL execution and subsequent pool use, snapshot
consistency during concurrent writes, read-only enforcement, concurrent locked
increments, tenant-composite foreign keys, duplicate/check constraints and no-op
changed-row behavior. Configuration/codec tests cover TLS policy, pool validation,
identifier rejection, binary UUID copy semantics and conservative commit-error
classification. Missing integration configuration fails the Compose gate.

The 1,668 existing audited source hashes still match. Existing user changes were
preserved. No service imports the new package, no original application dependency
was modified, and PostgreSQL remains the configured application database.

## Remaining work and status

DB-01 and PX-01 are IN_PROGRESS, not completed. POC-01 is only partially verified.
TLS certificate/hostname integration, actual deadlock/lock-timeout schedules,
ambiguous COMMIT fault injection, failover, remaining scalar codecs and service
port extraction remain outstanding. The test network explicitly permits plaintext;
this is not production TLS validation. No throughput or capacity claim is made.

B-01 live deployment/data/grant profiling and SLO/recovery decisions remain open;
this independent foundation slice does not require or fabricate them. Schema,
DDL publication, queue, authorization and service conversions remain unimplemented.
Next acceptance work is completing POC-01 and the runtime-DDL/work-engine proofs
before integrating the data-model service. All unrelated ADRs remain proposed.

The earlier documentation validation report is a snapshot of the design phase,
not a statement that implementation is still forbidden or that these tests ran
during that earlier phase.

## 2026-10-22: Foundation fault, TLS and scalar validation

Continued under the user's instruction to continue and tick completed work.
Added UTC DATETIME(6) codec with explicit range/precision validation and six new
unit/integration tests. The parent DB-01/PX-01 tasks remain IN_PROGRESS; completed
subtasks DB-01.1a through DB-01.1f are checked in the shared-module plan.

Implemented/verified:

- Real two-transaction deadlock: one winner, one classified victim, no partial
  victim writes.
- Real one-second lock timeout after an earlier successful write: full rollback,
  then a successful subsequent transaction through the same pool.
- Real COMMIT acknowledgement loss: a test-only connection wrapper consumes the
  server OK packet, hides it from the driver, and confirms the committed receipt
  through a separate healthy connection. Exactly one callback invocation; error
  remains CommitError, not a retryable deadlock classification.
- Ephemeral test CA/server certificates, successful encrypted connection, wrong
  hostname rejection and untrusted-CA rejection. Certificates are test-volume
  contents and never enter the repository.
- DATETIME range/precision/UTC boundaries, real binary UUID/time round trips,
  preservation of a JSON integer above the exact float64 range, and distinction
  between JSON null and SQL NULL.

An intermediate run found fixture certificate rotation on repeated Compose
one-shot dependency startup; its TLS test failed correctly. The certificate
setup now validates/reuses existing material and rejects incomplete/expired
material. That run also overlapped an edit to the fixture generator and failed
its compilation; it is not counted as a passing result. The subsequent clean
full run passed, including repeated certificate-service startup.

Final validation: MySQL 8.4.8, Go 1.25.8, driver v1.9.3; go vet passed and all
**10 tests passed with race detection, zero skips**, reported 2.330s. The expected
EOF/closed-connection diagnostics belong to deliberate COMMIT fault injection.
The certificate utility compiles and runs in the fixture; it has no standalone
unit tests. No performance or production readiness claim follows from this run.

Reproduction uses the current commands in [mysqlstore README](../../backend/mysqlstore/README.md):
start the isolated mysql service with --wait, run the test service, then remove
only that Compose project's containers/network/certificate volume. No application
service imports this foundation yet; PostgreSQL remains configured. Remaining
work includes owner-specific codecs/error mappings, service ports, observability,
failover and runtime DDL/durable work proofs.

## 2026-10-22: Ingestion transactional queue boundary

Implemented IN-01.2a/b/c and DB-01.1h.1; parent IN-01 and DB-01 remain IN_PROGRESS.

- `internal/ports/repositories.go`: typed EnqueueUploadLog/EnqueueDeferredIngest replace RawTx; no pgx import in the domain port.
- `internal/store/postgres/transaction_manager.go`: bind River producers to the adapter transaction, fail on unavailable producers, and use bounded independent rollback cleanup after cancellation.
- `internal/service/{upload_log_service,deferred_ingest_service}.go`: require an atomic transaction for creation; store and enqueue together. Retryable failures persist reset domain state and return an error to the existing River job. Terminal attempts persist failure. Removed nontransactional creation fallbacks and no-op producers.
- `internal/riverjobs/enqueuer.go`, `internal/httpapi/router.go`, `internal/app/app.go`, `cmd/worker/main.go`: explicit producer setup errors and updated construction. PostgreSQL and River remain the runtime adapters.
- `internal/store/postgres/transaction_queue_test.go`: eight real PostgreSQL scenarios across both job types: commit, rollback after enqueue, cancellation and unavailable producer; verifies scheduled time and row/job counts.
- `internal/service/queue_boundary_test.go`: missing transaction managers, retry signals, terminal status and repository failure propagation. Existing service integration tests now use actual River producers/migrations and assert retryable errors.

Paths above are relative to `backend/ingestion-service/`. Existing audited source hashes are a historical baseline: this slice intentionally changes 15 pre-existing ingestion source files, plus adds tests/isolated fixture files. Other audited source files were unchanged when cross-checked. No production schema, dependency manifest or database configuration changed.

The first full race run exposed existing parallel tests mutating Gin global mode. TestMain now sets it once in both HTTP test packages; parallel execution and race detection remain enabled. An earlier compilation error from an obsolete test stub was corrected. Neither failed run counts as verification.

Final validation: Go 1.25.8, PostgreSQL 16 image, `go vet ./...` and `go test -mod=readonly -race -count=1 -timeout=180s -v ./...` passed: **79 top-level tests, 53 subtests, 7 tested packages, zero failures and zero skips**. The PostgreSQL image uses its existing major-version tag; capture an immutable digest before formal cross-engine benchmark comparisons. No latency or throughput claim is made.

Reproduce from the repository root:

```powershell
docker compose -p migration-ingestion-test -f backend/ingestion-service/compose.migration-test.yaml up --abort-on-container-exit --exit-code-from test --attach test
docker compose -p migration-ingestion-test -f backend/ingestion-service/compose.migration-test.yaml down
```

The fixture uses tmpfs, an isolated network, no published ports and test-only credentials. Database-reset helpers require local test database names; contract tests create disposable databases. Do not override these URLs with application credentials. The fixture command runs vet/tests and does not format source files (formatting was completed before the verified run).

Remaining limitations: no MySQL ingestion repository or queue executor, no cross-engine parity claim, and no end-to-end worker retry scheduling/rescue validation. The tests establish durable PostgreSQL enqueue atomicity and retry error propagation. Runtime remains PostgreSQL; shared MySQL foundation has no application consumers yet. All production cutover, capacity, security acceptance and pending ADR gates remain open.

## 2026-10-22: Nullable identifier and timestamp foundation

Completed DB-01.1g.1; parent DB-01.1g and DB-01 remain IN_PROGRESS.
Added `backend/mysqlstore/nullable.go` with NullUUID and NullDateTime scanner/value
contracts. Valid=false binds SQL NULL; Valid=true preserves strict binary UUID
and UTC DATETIME(6) validation. An all-zero UUID is distinct from NULL. Both
scanners clear old state before NULL/error handling, reject nil receivers, and
return errors rather than treating invalid input as successful absence.

Source requirements include ingestion optional StartedAt/CompletedAt fields
(`backend/ingestion-service/internal/domain/ingestion/deferred_ingest.go:25-26`)
and data-model index-job optional table ID and lifecycle timestamps
(`backend/data-model-service/internal/store/postgres/index_job_repository.go:169-177`).
These existing domain contracts are evidence for nullable storage support; their
repositories have not been converted to these new codecs.

Added `nullable_test.go`: reused scan destinations, invalid binary/text/time
inputs, precision/zero-date rejection, nil receivers, NULL binding, real MySQL
round trips, UTC preservation and NULL versus all-zero UUID distinction.
The full disposable MySQL 8.4.8 / Go 1.25.8 foundation suite passed `go vet ./...`
and `go test -mod=readonly -race -count=1 -timeout=90s -v ./...`:
**12 tests passed, zero skips/failures**, package duration 2.563s. Expected
EOF diagnostics come from the existing deliberate lost-COMMIT-acknowledgement
test. The certificate generator compiled and executed; it has no standalone tests.
Reproduction uses [mysqlstore README](../../backend/mysqlstore/README.md).

No application service or production configuration changed. Remaining scalar
contracts, owner-specific nullable mappings/error translation, observability,
queue/DDL proofs and service adoption remain open. This validates codec mechanics,
not domain parity, production capacity or migration completion.

## 2026-10-22: Pool saturation and session lifecycle verification

Completed DB-01.1i.1 and DB-01.1i.2; parent DB-01.1i remains IN_PROGRESS.
Added `backend/mysqlstore/pool_test.go` to verify the configured foundation's
bounded acquisition and session lifecycle contracts. This slice adds tests and
documentation; it does not change runtime pool settings or service wiring.
Requirements are the bounded shared-server budget, UTC/strict sessions and
transaction isolation described in target architecture sections 3-4 and the
performance plan. Production budgets still require measured workload evidence.

The saturation test holds both connections in a two-connection pool and starts
sixteen mutation callers. It waits for the pool WaitCount acquisition barrier,
cancels the callers, and verifies context errors, zero callback execution,
positive wait accounting and unchanged held-connection counts. After releasing
the holders, a new mutation succeeds and no connection lease remains in use.
No scheduler sleep is used as evidence that all callers reached the barrier.

The session test checks connection IDs to establish reuse, verifies UTC, all six
required strict SQL modes, READ COMMITTED and read-write defaults after both
successful and failed Snapshot callbacks, then evicts only its own idle
connection and confirms a new physical connection receives the same settings.
This complements the existing snapshot isolation and read-only write rejection
tests; it does not establish safety for arbitrary caller-issued SET statements,
which the foundation contract prohibits.

Final validation: MySQL 8.4.8, Go 1.25.8; `go vet ./...` passed and the full
`go test -mod=readonly -race -count=1 -timeout=90s -v ./...` suite passed:
**14 tests, zero failures/skips**, package duration 2.354s. The certificate utility
compiled/executed without standalone tests. Expected EOF logs remain deliberate
COMMIT-acknowledgement fault injection. Reproduce using the mysqlstore README's
disposable Compose commands.

Remaining: actual failover, all-service connection accounting, production load
and SLO acceptance, pool metric export and operational alert integration. These
small deterministic fixtures make no throughput, latency or production capacity
claim. PostgreSQL remains the application runtime; no service adopts MySQL here.

## 2026-10-22: Data-model transaction cleanup preparation

Completed DM-01.2a; parent DM-01 and DM-01.2 are IN_PROGRESS. Updated
`backend/data-model-service/internal/store/postgres/transaction_manager.go`:
reject nil pool/callback, use one deferred rollback with an independent five-second
cleanup context, preserve the callback error via errors.Join if cleanup also
fails, and check request cancellation before commit. Previously a rollback using
the canceled request context could replace the original callback error; the
fallback background rollback had no deadline. Existing authorization checks and
transaction-scoped repository/DDL construction remain intact.

Added `internal/store/postgres/transaction_cleanup_test.go`: dependency validation
plus six real PostgreSQL cases (commit, callback failure, canceled callback failure,
canceled callback success, panic, read-only grant). Tests assert persisted row
counts and preserved error/panic identity, then execute another transaction using
a single-slot pool. The read-only case confirms no mutation callback execution.
This is not a full identity authorization or tenant-isolation suite.

Final validation with Go 1.25.8 and PostgreSQL 16 image: module-wide `go vet ./...`
passed; focused race-enabled tests passed **2 top-level tests and 6 subtests,
zero skips/failures**, package duration 1.069s. The broader service/HTTP/repository
integration suite was not executed in this slice. No claim of full data-model
migration acceptance follows. Cleanup deadline behavior under a stalled server
and rollback network failure has not been fault-injected here.

Reproduction from repository root:

```powershell
docker compose -p migration-data-model-test -f backend/data-model-service/compose.migration-test.yaml up --abort-on-container-exit --exit-code-from test --attach test
docker compose -p migration-data-model-test -f backend/data-model-service/compose.migration-test.yaml down
```

The fixture uses tmpfs, no published ports and no application network/volume.
A separate transaction-test URL is required; the test rejects targets outside
localhost/127.0.0.1 and the data_model_tx_test_* database-name prefix. It creates
a unique fixture table, never resets application schemas. The fixture runs vet
and the focused test selection; formatting was completed before verification.
Production remains PostgreSQL; no schema, dependency or deployment configuration
was changed. Data-model RawTx/queue extraction, MySQL DDL publication/recovery,
all-service integration and production acceptance remain open.

## 2026-10-22: Data-model index queue boundary and full owner regression

Completed DM-01.2b/c/d and DB-01.1h.2; parent DM-01/DB-01 remain IN_PROGRESS.

- `backend/data-model-service/internal/ports/transactions.go`: replace RawTx with typed EnqueueIndexJob and an explicit unavailable-queue error.
- `internal/store/postgres/transaction_manager.go`: bind the River producer to the transaction adapter. Run is driver-free; RunWithTx is an explicit PostgreSQL-only adapter operation for existing display persistence, using the same authorization and cleanup path.
- `internal/service/index_job_service.go`, `managed_index_job_helper.go`, `table_service.go`, `navigation_option_service.go`, `display_caption.go`: index request/retry and managed search/navigation operations use store.EnqueueIndexJob. Schema-history insert failure now propagates before enqueue. Production service code and ports contain no pgx/River imports.
- `internal/riverjobs/enqueuer.go`: remove NoopIndexJobEnqueuer and reject missing clients/transactions. Existing River kind, arguments, scheduling, queue, attempts and ByArgs policy are preserved.
- `internal/httpapi/router.go` and `internal/app/app.go`: bind the real producer centrally, handle initialization errors and close the pool on construction failure.
- `internal/display/store.go`: keep PostgreSQL display reads/writes, caption updates and history in the same authorized transaction through RunWithTx. This is an adapter contract, not a completed MySQL display port.

Paths after the first bullet are relative to backend/data-model-service.

New real-engine transaction_queue_test.go verifies domain index row plus River
job atomic commit/rollback/cancel, unavailable producer/client rollback, scheduled
time and same-transaction repeated enqueue deduplication. Service tests assert
history failures stop enqueue. Existing tests use the new interfaces; service
integration uses real producers except its existing explicit recording-producer
assertion. Gin mode is set once per HTTP test package in TestMain instead of
mutating globals in parallel tests. Resetting integration helpers now require
localhost/127.0.0.1 and data_model_tx_test_* database names.

The first compile revealed display's additional RawTx dependency; its explicit
PostgreSQL adapter transaction contract was preserved. Subsequent compile checks
found a test import and a test fake's pointer-receiver wiring; these were corrected.
Those failed runs are not counted as verification.

Final validation: Go 1.25.8, PostgreSQL 16 image, module-wide `go vet ./...` and
`go test -mod=readonly -race -p 1 -count=1 -timeout=180s -v ./...` passed:
**73 top-level tests, 29 subtests, 11 tested packages, zero skips/failures**.
Includes existing display concurrency/rollback, platform authorization, HTTP,
service/metadata/DDL integration, reconciliation and worker tests. Packages are
serialized because some existing fixtures reset shared disposable core schemas;
parallel tests within packages remain enabled. PostgreSQL query-plan output from
display tests is fixture evidence only, not a MySQL performance comparison.

Reproduce using the preceding entry's Compose commands. The fixture now sets all
three test database environment variables and runs the entire module suite.
AUTH_PREFLIGHT fixtures create/drop their own uniquely named disposable databases.
No application database/network/volume is used. Formatting was completed before
verification; the reproducible fixture only downloads, vets and runs tests.

Remaining: MySQL owner repositories, runtime DDL intent/verification/publication,
display SQL conversion, reconciliation enqueue atomicity review, durable MySQL
worker execution and cross-service/production gates. Real worker process crash,
lease recovery and concurrent duplicate enqueue are not proved by the new queue
fixture. No schema or application dependency changes were made; PostgreSQL remains
the configured runtime. Existing audit source hashes describe the historical
baseline and do not describe these subsequently authorized source changes.

## 2026-10-22: Atomic reconciliation repair scheduling

Completed DM-01.2e and DB-01.1h.3. Parent migration tasks remain IN_PROGRESS.
Added explicit pending follow-up DM-01.2f for legacy stranded repair jobs.

`backend/data-model-service/internal/reconcile/service.go` now performs each repair
create/retry, schema-history write and typed enqueue in one owner transaction.
Physical inspection remains outside those transactions. Errors from history and
queue writes propagate; no fallback empty history payload or best-effort history
write remains in this path. Constructor errors propagate through HTTP setup and
the reconcile CLI. Earlier committed gaps remain committed if a later gap fails;
the whole reconciliation run is not one transaction and reports an error.

The prior tenant-wide index-job history load and stale in-memory status lookup
were removed from scheduling. `ports.IndexJobRepository.GetByDedupeKeyForUpdate`
and the PostgreSQL implementation lock an existing exact-key row until commit.
Pending/running jobs are skipped based on the current locked state. Missing keys
are not locked: the existing partial unique index in metadata migration 000003
(`index_jobs_dedupe_key_idx`, nonempty dedupe key) rejects competing inserts.
The losing attempt returns the wrapped constraint error; no implicit retry or
success is fabricated. A later reconciliation observes the winner's pending job.
No global/tenant/table-parent lock was added, and no external delivery occurs
while the transaction is open. This query-shape change is not a measured
throughput claim.

New `internal/reconcile/repair_integration_test.go` creates uniquely named disposable
databases, applies current metadata and River migrations, and verifies:

- New creation and existing failed-job retry: successful commit, error after a real
  history insert, error after a real River insert, and cancellation after enqueue
  (eight subtests). Row status, history, queue rows and orphan work are checked.
- Eight concurrent attempts to retry one failed job produce exactly one reported
  schedule, one durable River job and one history entry.
- A deterministic missing-key barrier makes two transactions observe absence
  before insert. Exactly one commits and one returns PostgreSQL 23505 for the
  intended dedupe constraint. Only one row/job/history exists; follow-up
  reconciliation succeeds without scheduling duplicate work.

The full module gate passed before the final missing-key race test was added;
that additional test justified a final repeat. Final Go 1.25.8 / PostgreSQL 16
validation: `go vet ./...` and
`go test -mod=readonly -race -p 1 -count=1 -timeout=180s -v ./...` passed:
**76 top-level tests, 37 subtests, 11 tested packages, zero skips/failures**.
Final reconciliation package duration was 1.701s. Counts were taken from the last
container start only, excluding the preceding successful run. Formatting was
completed before verification and removed from the reproducible test command.
Use the existing data-model Compose commands documented above. Temporary databases
are dropped by test cleanup; the stack is separate from application resources.

Limitations: no automatic recovery of historical pending rows that lack queue
work; DM-01.2f tracks this. Existing worker fencing/lease behavior and other
index-request concurrency paths were not redesigned. No MySQL repository,
queue executor, DDL publication/recovery or production cutover was implemented.
No schema/dependency changes were needed. PostgreSQL remains the runtime database.

## 2026-10-22 — DB-01.1j: pinned MySQL DDL lock lifecycle

Implemented `WithDDLLock` in `backend/mysqlstore/ddl_lock.go` with behavioral tests
in `ddl_lock_test.go`. P-01 requires lock ownership and DDL on the same physical
connection. Parameterized `GET_LOCK(?, 0)` makes one fail-fast attempt; contention
returns `ErrDDLLockBusy` without running the callback. Names are validated ASCII
identifiers. The callback receives the pinned connection and is never retried.

Cleanup explicitly releases the lock with an independent five-second deadline,
then discards the physical connection on every exit, including success. This
isolates DDL session state at the cost of connection setup per operation; no
production performance improvement is claimed. `DDLLockCleanupError` preserves
causes, and `errors.go` classifies that uncertainty as `Other` even when joined
with a deadlock error. Callers must reconcile physical DDL outcomes before retry.

Evidence and rationale: [MySQL named locks](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html)
are session-scoped and survive transaction boundaries;
[Conn.Raw](https://pkg.go.dev/database/sql#Conn.Raw) with `driver.ErrBadConn`
prevents session reuse. Runtime verification used MySQL 8.4.8, Go 1.25.8 and
mysql driver v1.9.3 in the existing isolated Compose fixture.

Verification: the first full vet/race run passed 17 top-level tests and five
subtests. After adding conservative cleanup-error classification, the final
`go vet ./...` and `go test -mod=readonly -race -count=1 -timeout=90s -v ./...`
passed **18 top-level tests and five subtests**, with zero failures/skips;
mysqlstore package time was 4.377s. Sources were formatted during verification;
the fixture command subsequently had its formatting mutation removed so future
runs only download dependencies, vet and test.

New assertions cover lock owner/connection identity, same-name exclusion before
and after implicit DDL commit, independent names progressing, fresh physical
sessions after release, and callback error/panic/cancellation. A deliberate
premature release exercises unconfirmed cleanup; an interrupted server query
exercises delayed session termination and eventual lock release with a bounded
poll. Failure tests inspect committed table existence: callback failure does not
roll back DDL. The disposable stack and certificate volume were removed after
verification; application resources were not used.

Ticked DB-01.1j only. DB-01 remains IN_PROGRESS; POC-04 is now IN_PROGRESS for this
mechanical prerequisite only. Operation intent/ledger, shape verification,
publication barriers, service adoption and full crash/failover recovery remain
unimplemented. Lost release acknowledgements, real long-running DDL, server
failure and split-brain fencing still need targeted validation. Legacy repair
recovery DM-01.2f also remains pending. PostgreSQL remains the application runtime.

## 2026-10-22 — DB-01.1g.2: lossless non-null INET storage codec

PG-24 source evidence: `backend/data-model-service/internal/tenantdb/postgres/indexing.go:113-126`
(`toPgType`) maps IP fields to PostgreSQL INET. Current ingestion validation at
`backend/ingestion-service/internal/domain/ingestion/validation.go:277-282`
accepts trimmed bare IP addresses through net.ParseIP, not prefixes. No service
validation or runtime adapter was changed.

Implemented `EncodeInet` and `InetColumns.Prefix` in `backend/mysqlstore/inet.go`
for the planned family/address/prefix representation. Four-byte IPv4 remains
distinct from sixteen-byte IPv4-mapped IPv6. Host bits survive short prefixes;
invalid families, byte lengths and prefix ranges fail explicitly. The codec
uses typed netip.Prefix input rather than introducing a different API parser.
It does not implement a single-column Valuer, nullable conversion, ordering or
containment SQL. Owner migrations must enforce the three-column invariant.

`inet_test.go` covers eight vectors including zero/max addresses, /0, short
prefixes with host bits, IPv4-mapped and compatible IPv6, malformed tuples and
buffer ownership. The disposable MySQL table has NOT NULL and enforced CHECK
constraints; direct malformed inserts fail with CheckViolation, and NULL fails.
Real-engine round trips compare complete prefixes and address bytes against
INET6_ATON. No PostgreSQL differential suite was run, so parser/output/order/
containment parity and existing data profiles remain explicit follow-up work.

Verification used MySQL 8.4.8, Go 1.25.8 and driver v1.9.3. After gofmt of the two
new files, go vet and the full race-enabled suite passed **20 top-level tests and
five subtests**, zero failures/skips; package duration 4.447s. Reproduction is the
standard mysqlstore Compose test command in its README. No performance claim is
made from this small fixture. The isolated stack and certificate volume were
removed after verification.

Ticked DB-01.1g.2 only; DB-01.1g and DB-01 remain IN_PROGRESS. POC-01 remains
partial. The production schema, API representation, nullable owner mappings,
query operators and data migration remain unimplemented. PostgreSQL remains the
application runtime. Authoritative semantics and codec usage are linked in
`backend/mysqlstore/README.md`.

## 2026-10-22 — DB-01.1e.1: explicit unconfirmed rollback outcome

Inspection of `backend/mysqlstore/transaction.go` and `errors.go` found that an
ordinary wrapped rollback error could be joined with a callback deadlock/timeout
and still classify as that ordinary callback category. Implemented RollbackError
with cause unwrapping, joined with the original callback error, and made Classify
return Other for unconfirmed cleanup. No automatic retry was introduced. This
cleanup outcome does not imply that COMMIT was sent. Panic propagation and the
existing ErrTxDone handling remain unchanged; database/sql-owned cancellation
cleanup may not expose its rollback result through the helper.

Generalized the existing test-only plaintext acknowledgement-loss connection to
select COMMIT or ROLLBACK. `rollback_fault_test.go` consumes the real server
ROLLBACK OK packet, closes the connection before the driver receives it, and
checks the typed error plus original callback cause, one callback invocation,
absence of its inserted row and no leaked pool lease. A subsequent successful
transaction uses a different physical connection and persists its own row.
The callback timeout is explicitly injected; real deadlock/timeout generation
remains covered separately in locking_test.go. Classification tests cover both
1205 and 1213 joined with cleanup failure.

Verification: MySQL 8.4.8, Go 1.25.8 and driver v1.9.3; gofmt of changed Go files,
go vet ./... and the full race suite passed **22 top-level tests plus five
subtests**, zero failures/skips, package time 4.775s. The protocol wrapper's EOF
logs are expected fault evidence. After the run, only an explanatory Go comment
and documentation were changed. Use the standard mysqlstore Compose fixture to
reproduce. The disposable stack and certificate volume were removed afterward.

Ticked DB-01.1e.1 only. This verifies lost acknowledgement after successful server
rollback, not all network partitions, rollback failure timings, panic-plus-cleanup
reporting or server failover. Full domain retry/reconciliation policies and service
adoption remain pending. DB-01 and POC-01 remain IN_PROGRESS; PostgreSQL remains
the application runtime and no application schema/configuration was changed.

## 2026-10-22 — DB-01.1g.3: nullable INET tuple integrity

The existing ingestion `normalizeFieldValue` accepts NULL only for fields marked
nullable (`backend/ingestion-service/internal/domain/ingestion/validation.go:229-235`).
The preceding INET codec covered present values only. Added `NullInet` in
`backend/mysqlstore/nullable_inet.go`, with explicit three-parameter Values and
three-column ScanColumns operations. Valid=false binds all NULL. Present values
use the strict existing codec; zero addresses are distinct from absent values.
Scanning NULL or invalid columns clears previous state. All partial-null tuples,
invalid lengths/families/prefixes, overflow and textual address data fail.
Callers must handle row Scan errors before invoking ScanColumns and must handle
its validation errors; no service input contract or application table changed.

`nullable_inet_test.go` validates reused destinations and all partial-null cases.
The isolated MySQL fixture uses an explicit all-null OR all-present-and-valid
CHECK constraint. Direct inserts prove all six partial-null combinations and
invalid non-null tuples are rejected. Round trips include NULL before and after
valid values, zero/max IPv4/IPv6, host bits and mapped IPv6. This fixture validates
the proposed invariant without applying a production migration.

Verification: MySQL 8.4.8, Go 1.25.8, driver v1.9.3; formatted the two new files,
ran go vet ./... and the full race suite: **24 top-level tests plus five subtests
passed**, zero failures/skips; package time 5.442s. Standard mysqlstore Compose
commands reproduce the run. Disposable stack and certificate volume removed
after verification. No performance claim follows from this small fixture.

Ticked DB-01.1g.3 only. The historical DB-01.1g.2 entry accurately records that
nullable mappings were pending at that point; this implements the reusable codec,
not owner mappings. DB-01.1g, DB-01 and POC-01 remain IN_PROGRESS. Service schema
and repository adoption, PostgreSQL query/format differential tests and migration
of live data remain pending. PostgreSQL remains the application runtime.

## 2026-10-23 — DB-01.1g.4: JSON and nullable JSON storage boundary

PG-20 covers JSONB persistence and query semantics. Concrete consumers include
`backend/decision-engine-service/internal/domain/execution/model.go:36,52,54`
(request/result json.RawMessage fields) and scenario formulas at
`internal/domain/scenario/model.go:47,90`. Added JSON and NullJSON codecs in
`backend/mysqlstore/json.go`; no service model/repository or runtime config changed.

The codec validates UTF-8/document syntax and binds text without parsing numbers
through float64. Scan copies data and clears previous state on errors. NullJSON
keeps SQL NULL distinct from a present JSON null literal. Invalid/empty present
documents fail. The codec does not normalize duplicate keys or enforce domain
shape, numeric range, Unicode escape or depth policies; MySQL errors propagate.

`json_test.go` covers buffer ownership, stale-state clearing, malformed documents,
invalid UTF-8, nil destinations and nullable flags. Real-engine tests cover NULL,
JSON null, boolean, positive/negative integers beyond exact float64 integer range,
Unicode, arrays and duplicate-key normalization. Direct malformed inserts also
fail when bypassing the codec. The initial run failed one expectation: MySQL
reported the positive large integer as UNSIGNED INTEGER rather than INTEGER.
Corrected the assertion to the observed type and added a negative-integer vector;
no production behavior was changed to accommodate the test.

Final verification: MySQL 8.4.8, Go 1.25.8, driver v1.9.3; go vet and the full
race suite passed **26 top-level tests plus five subtests**, zero failures/skips,
package duration 4.868s. New files were formatted before the first run; the final
change added only formatted fixture rows. Standard mysqlstore Compose commands
reproduce the tests. Disposable stack and certificate volume removed afterward.

Ticked DB-01.1g.4 only; DB-01.1g, DB-01 and POC-01 remain IN_PROGRESS. Numeric
range/precision outside these vectors, JSON query operators, ordering, canonical
hashes, payload bounds, service adoption and complete PostgreSQL differential
validation remain pending. This is not a byte-exact document archive or proof
of full JSONB compatibility. The MySQL normalization reference and usage contract
are linked in backend/mysqlstore/README.md. PostgreSQL remains the runtime database.

## 2026-10-23 — DB-01.1a.1: explicit session integrity defaults

Inspection of `backend/mysqlstore/db.go` showed UTC/strict/isolation settings were
explicit but integrity and transaction completion settings inherited server
defaults. The connector now initializes autocommit=1, completion_type=0,
foreign_key_checks=1 and unique_checks=1 on every new physical connection.
Explicit transaction helpers continue to own their commit/rollback boundaries.
No global variable, application configuration or service schema was changed.

Extended pool_test.go to verify these settings initially, after successful and
failed snapshots on the same session, and after physical connection replacement.
Added integrity_test.go with disposable parent/child tables using composite
(tenant,id) keys. Direct SQL verifies valid tenant relationships and same external
key across tenants, rejection of a parent belonging to another tenant, secondary
unique enforcement and rejection of referenced-parent deletion. An FK failure
rolls back an earlier write; a later successful transaction is visible from a
separate pool. This fixture is not a migrated application schema or authorization
test and does not establish whole-system tenant isolation.

The first run exposed a test scan mismatch: completion_type returns NO_CHAIN
text, not integer 0. Corrected that assertion; final go vet and full race suite
passed **27 top-level tests and five subtests**, zero failures/skips, package
time 4.544s on MySQL 8.4.8, Go 1.25.8 and driver v1.9.3. Standard mysqlstore Compose
commands reproduce the run. Changed Go files were formatted before verification;
only the enum assertion changed before the final run. The isolated stack and test
certificate volume were removed afterward.

Ticked DB-01.1a.1 only. Initialization is not per-checkout session repair and cannot
prevent arbitrary SQL from disabling checks; callbacks must not mutate session
settings. Tests did not change server global defaults or validate historical data.
Owner grants, production constraints, live configuration/failover validation and
service adoption remain pending. DB-01 and POC-01 remain IN_PROGRESS; PostgreSQL
remains the application runtime. The MySQL system-variable reference and scope
limits are linked in backend/mysqlstore/README.md.

## 2026-10-23 — DB-01.1j.1: blocked ALTER recovery verification

Added `backend/mysqlstore/ddl_metadata_lock_test.go` to exercise the remaining
P-01 prerequisite of real DDL metadata-lock contention. No runtime helper or
application code changed. Two subtests hold a read transaction open while ALTER
TABLE waits, observe the waiting session via information_schema.PROCESSLIST,
and verify another named-lock executor cannot enter. One waits for the server
metadata-lock timeout; the other cancels the request after observing the wait.

The fixture sets session lock_wait_timeout=2 on the disposable pinned connection.
It retains the reader until the old named lock is free, inspects the table to
confirm the timed-out ALTER did not add the column, releases the reader and
executes an explicit new ALTER successfully. It also checks that no pool lease
remains. Client cancellation is not treated as proof that server work stopped;
the cancellation case took approximately two seconds including server cleanup.
This is a caller-specific test timeout, not a new production default.

Verification: MySQL 8.4.8, Go 1.25.8, driver v1.9.3; gofmt, go vet and the full
race suite passed **28 top-level tests plus seven subtests**, zero failures/skips,
package duration 8.565s. Both new subtests passed on the first run. Use the standard
mysqlstore Compose commands to reproduce. The isolated stack and certificate
volume were removed after verification; application resources were not touched.

Ticked DB-01.1j.1 only. POC-04 and DB-01 remain IN_PROGRESS. This validates blocked
DDL with a bounded server wait, not interruption during physical table rebuilding,
lost DDL success acknowledgement, server crash, primary fencing or durable schema
publication/reconciliation. No operation ledger or automatic retry was added.
Production timeout selection and service adoption remain pending; PostgreSQL
remains the application runtime.

## 2026-10-23 — DB-01.1j.2: lost DDL success acknowledgement

Added `backend/mysqlstore/ddl_ack_fault_test.go`, reusing the test-only protocol
wrapper from commit_fault_test.go. It forwards a real ALTER TABLE ADD COLUMN,
consumes its server OK packet and disconnects before the driver sees success.
The test proves the DDL callback runs once and the returned failure retains both
the DDL error and typed cleanup ambiguity, classified as Other. No runtime code,
service configuration or application schema was changed.

After bounded observation of named-lock release, recovery obtains the same lock
on a healthy physical session. It inspects the added column's type, nullability
and default, rather than relying only on the column name. The desired shape is
already present, so recovery does not replay ADD COLUMN. A subsequent insert
proves the committed default is usable. The failed pool has no outstanding lease.
This is a concrete test recovery path, not an implemented generic schema inspector
or durable operation/publication protocol.

Verification: MySQL 8.4.8, Go 1.25.8, driver v1.9.3; gofmt, go vet and full race
suite passed **29 top-level tests plus seven subtests**, zero failures/skips,
package duration 8.776s. New test passed on its first run; EOF logs are expected
fault evidence. Standard mysqlstore Compose commands reproduce verification.
The disposable stack and certificate volume were removed after the run.

Ticked DB-01.1j.2 only. DB-01 and POC-04 remain IN_PROGRESS. Tests cover one added
column with a known postcondition; other DDL forms, drift rejection, complete
shape inspection, process/server crashes, old-primary fencing, durable intent,
publication barriers and service adoption remain pending. PostgreSQL remains
the application runtime.

## 2026-10-23 — DB-01.1j.3: reusable visible-column inspection

P-01 step 3 requires physical shape evidence before metadata publication. Added
`backend/mysqlstore/columns.go` with InspectVisibleColumns and ColumnDefinition.
The query is scoped to exact validated schema/table parameters and ordered by
ordinal position on the supplied pinned connection. Results retain column type,
nullability, default, charset/collation, EXTRA and generation expression. Missing
or invisible columns return ErrNoVisibleColumns; query/scan/cancellation failures
return no partial list. The caller still owns authorization and DDL lock scope.

`columns_test.go` verifies invalid input, missing-table behavior, physical order,
empty-string versus absent/default-NULL metadata, text collation, generated
expression metadata and same-name changes from INT NOT NULL DEFAULT 7 to BIGINT
NULL DEFAULT 9. The earlier returned snapshot remains unchanged. A canceled
inspection returns no result. The real-engine test executes within WithDDLLock.

Verification: MySQL 8.4.8, Go 1.25.8, driver v1.9.3; gofmt, go vet and full race
suite passed **31 top-level tests plus seven subtests**, zero failures/skips,
package duration 8.703s. Both new tests passed on the first run. Standard mysqlstore
Compose commands reproduce verification. Isolated stack and certificate volume
removed after the run; no application resources were used.

Ticked DB-01.1j.3 only. This is column evidence, not a full schema comparator.
Metadata visibility depends on grants/server settings, and a partial column grant
can yield incomplete metadata; restricted-account visibility was not exercised.
Default NULL and omitted default cannot be distinguished by COLUMN_DEFAULT.
Indexes, constraints, table options/base-table identity, grants, generic drift
rejection, operation ledgers and publication remain pending. Documentation links
the official COLUMNS reference and explains these limits. DB-01 and POC-04 remain
IN_PROGRESS; no service imports the helper and PostgreSQL remains the runtime.

## 2026-10-23 — DB-01.1j.4: index-definition inspection

P-01 physical verification also requires indexes, not just column presence.
Added `backend/mysqlstore/indexes.go` with InspectVisibleIndexParts and IndexPart.
It reads exact schema/table metadata with bound parameters on a supplied pinned
connection, ordered by index name and key position. It retains uniqueness,
method, optimizer visibility, column/expression, direction and prefix length.
Query/scan errors return no partial result. Empty output does not certify table
absence, existence or complete metadata privileges; the caller must establish
identity/visibility and hold the DDL lock through subsequent decisions.

`indexes_test.go` covers input validation, missing-table empty output, primary and
composite unique keys, prefix length, descending direction, invisible indexes and
functional expressions. Replacing an index under the same name with a different
prefix/direction/visibility produces changed metadata without mutating the previous
snapshot. A canceled query returns no metadata. Tests run inside WithDDLLock.

Verification: MySQL 8.4.8, Go 1.25.8, driver v1.9.3; gofmt, go vet and full race
suite passed **33 top-level tests plus seven subtests**, zero failures/skips,
package duration 8.986s. Both new tests passed on the first run. Standard mysqlstore
Compose commands reproduce verification. Disposable stack/certificate volume
removed afterward; application resources were not touched.

Ticked DB-01.1j.4 only. Prefix units depend on column type and expression strings
are server metadata, as documented in the official STATISTICS reference linked
from mysqlstore/README.md. No query-performance claim or cardinality-based shape
comparison is introduced. Restricted privileges, spatial/fulltext indexes,
constraints, table identity/options, grants, complete drift decisions, durable
intent/publication and service adoption remain pending. DB-01/POC-04 remain
IN_PROGRESS; PostgreSQL remains the application runtime.

## 2026-10-23 — DB-01.1j.5: foreign-key and check inspection

Added `backend/mysqlstore/constraints.go`: InspectVisibleForeignKeyParts reads
ordered child/parent mappings, referenced schema/table/key position and update,
delete and match rules; InspectVisibleChecks reads clause text and enforcement.
Both use exact schema/table parameters on a supplied pinned connection and return
no partial result on error. Caller authorization, DDL lock scope and sufficient
metadata privileges remain explicit preconditions.

`constraints_test.go` covers validation, missing-table empty output, composite
tenant-key mapping, changes in referential actions under the same FK name, check
enforcement drift and cancellation. A direct invalid insert confirms the newly
enforced check rejects data. The first run exposed MySQL error 1826 when DROP FK
and ADD FK reused one name within a single ALTER. The empty disposable fixture
now uses separate DDL steps under its lock. P-01 documents the observation and
requires write barriers/recovery before any production multi-step replacement;
the test is not an approved production replacement algorithm.

Final verification: MySQL 8.4.8, Go 1.25.8, driver v1.9.3; formatted new Go files,
go vet and full race suite passed **35 top-level tests plus seven subtests**,
zero failures/skips, package duration 8.751s. Standard mysqlstore Compose commands
reproduce the run. Isolated stack and certificate volume removed afterward;
application schemas and runtime configuration were not changed.

Ticked DB-01.1j.5 only. Empty metadata does not prove absence or full visibility.
Restricted-account coverage, table options/grants, complete schema comparisons,
FK replacement safety, durable intent/publication and service adoption remain
pending. Definitions do not certify effective session FK checking. Official
metadata references and limitations are linked in mysqlstore/README.md. DB-01
and POC-04 remain IN_PROGRESS; PostgreSQL remains the application runtime.

## Data-model focus and contract construction - 2026-10-23

DM-01 remains the active module until its gates are finished. Added the repository,
migration, DDL and entry-point coverage map. DM-01.1 remains IN_PROGRESS: the
column-level target manifest and exact-key enforcement are still outstanding.

Implemented internal/contract.BuildIngestion in data-model-service. It preserves
migration-000005 payload shape, archived metadata, empty objects/arrays and enum
sort-order/UUID ordering without mutating inputs. Ownership, duplicate identities
and malformed UTF-8 fail explicitly. No persistence or version allocation occurs.

Unit tests and TestPortableIngestionContractMatchesPostgres compare the builder
against the real PostgreSQL publication trigger for empty tables, all six field
types, enum ties and archives. Full go vet ./... and go test -mod=readonly -race
-p 1 -count=1 -timeout=180s -v ./... passed in the disposable PostgreSQL 16 /
Go 1.25.8 fixture. After adding UTF-8 rejection, vet and race tests passed again
for ./internal/contract and ./internal/store/postgres, including trigger parity.
Use compose.migration-test.yaml, project data-model-migration-test, run --rm test
sh -ec with the above commands (go mod download first for the full suite).

Ticked DM-01.3a only. Builder runtime wiring, authoritative version publication,
DDL readiness, MySQL repositories, grants, display cleanup, worker/reconcile
recovery and representative performance validation remain open. PostgreSQL
remains the runtime. This does not complete DM-01 or enable a MySQL cutover.

## Data-model semantic publication planning - 2026-10-23

Implemented internal/contract.PrepareIngestion and Publication. The planner uses
BuildIngestion, validates prior tenant/table/version identity, compares JSON
semantics and retains unchanged tokens. It distinguishes null/empty, missing/extra
keys, types and array order while ignoring object order, whitespace and equivalent
escaping. Numeric enum values remain strings; this is not a general JSONB numeric
comparator. Changed contracts require an error-returning fresh UUID generator;
zero and immediately reused tokens are rejected. Production integration must use
fresh random generation, not payload-derived tokens. No I/O or locks occur here.

Added three unit tests (including comparison/error subtests) and the real-engine
TestPortablePublicationVersionDecisionsMatchPostgres. Compared table presentation
edits, field/enum creation, enum sort changes with and without output reordering,
archives and JSON reserialization against migration-000005 trigger decisions.
Unit coverage includes revert-to-old-payload with a fresh token, malformed prior
JSON/UTF-8, foreign identities, alias safety and preserved generator errors.

Verification: disposable compose.migration-test.yaml PostgreSQL 16 / Go 1.25.8;
gofmt, go mod download, go vet ./... and go test -mod=readonly -race -p 1
-count=1 -timeout=180s -v ./... all passed (process exit 0). Existing authorization,
display, transaction rollback, repair concurrency, API and worker suites passed.
No application database or runtime adapter was changed. Fixture cleanup uses
project data-model-migration-test down --volumes.

Ticked DM-01.3b.1 only. DM-01.3b and DM-01 remain IN_PROGRESS. Locked snapshot
loading, atomic target persistence, concurrent publication and uncertain-commit
recovery remain explicitly unchecked as DM-01.3b.2; readiness/grants/display and
runtime entry-point wiring retain their existing separate gates. Integration
requirements are recorded in internal/contract/README.md. No other module advanced.

## Data-model MySQL publication transaction adapter - 2026-10-23

Implemented internal/store/mysql.PublicationStore for an existing model table.
It uses mysqlstore.Mutation (READ COMMITTED), locks the exact tenant/table row
before the internal owner callback, loads fields/enums in two bounded queries,
uses PrepareIngestion, and inserts or version-conditionally updates the contract
in the same transaction. A result is returned only after acknowledged commit;
CommitError is preserved without retries. Field/enum limits abort rather than
truncate. All adapter SQL is database-qualified with a validated identifier and
bound values. Every writer must follow this lock protocol; it is not authorization.

Added compose.mysql-test.yaml: MySQL 8.4.8, Go 1.25.8, tmpfs-only database,
no published ports or application networks. Fixture schema has only publication
columns and tenant-composite FKs; it is not the production SC-01 migration.
A test-only CHECK injects publication-write rejection to verify full rollback.

Validation passed: module-wide go vet ./...; contract and MySQL adapter race
suite, with all seven MySQL scenarios passing (package duration 1.722s).
Scenarios cover first/unchanged publication and enums, mutation rollback/wrong
tenant, field limits, concurrent field edits and independent same-tenant tables,
publication-write rollback, cancelled row-lock waits/recovery, enum limits and
cross-tenant FK rejection. These small fixtures do not establish performance SLOs.

Full PostgreSQL module vet/race regression also passed, exit 0. The MySQL-only
test skips in that PostgreSQL fixture intentionally; it passed separately with
DATA_MODEL_REQUIRE_MYSQL_TEST=1. New local mysqlstore dependency adds MySQL driver
v1.9.3 and edwards25519 v1.1.0 checksums. go mod tidy also correctly reclassifies
already-used JWT v5.3.0 as direct without upgrading it. Dockerfile now copies
mysqlstore before go mod download. Application constructors still use PostgreSQL.

Ticked DM-01.3b.2a only. Production metadata lineage, complete owner APIs/grants,
concurrent enum-edit coverage, operation-fenced uncertain-commit reconciliation,
DDL readiness, display cleanup and runtime adoption remain outstanding.
Reproduce MySQL checks with compose.mysql-test.yaml and PostgreSQL checks with
compose.migration-test.yaml; both stacks are disposable. Adapter README documents
schema assumptions, lock protocol, official MySQL reference and adoption gates.
The existing service-local docker-compose.yml still uses context '.' although
its Dockerfile expects the parent backend context; this pre-existing local-build
configuration mismatch remains open. Builder verification uses backend context.

Builder follow-up: docker build --target builder -f
backend/data-model-service/Dockerfile backend completed successfully (exit 0),
including server, worker and migrate binaries on the existing Go 1.25.0 image.
Both disposable MySQL/PostgreSQL stacks were removed with down --volumes, exit 0.
Tracked dependency/Dockerfile diff whitespace checks passed.

## Data-model operation receipts and uncertain-COMMIT recovery - 2026-10-23

Added PublicationOperation, MutateAndPublishOperation and RecoverPublication to
the staged MySQL owner adapter. Receipts store the operation's request fingerprint,
version and payload atomically with metadata and publication. The key is scoped
to tenant/table/operation UUID. All receipt decisions occur after acquiring the
same table guard. Matching replays bypass the mutation and return the original
result even after later publications; changed fingerprints conflict explicitly.
Recovery runs on a healthy primary connection without invoking a mutation.
Errors/cancellation are not rollback evidence; no automatic retry was added.

Caller contract: a versioned canonical request hash must cover operation kind,
expected version, all mutation inputs and trusted actor identity. Every replay
must be authorized anew. The adapter does not construct requests or implement
endpoint authorization. Production receipt immutability, retention/capacity and
table-retirement rules remain open. These receipts are metadata transaction
results, not a replacement for durable DDL intents or physical-readiness checks.

Added a receipt table only to the disposable publication fixture, with a
restrictive tenant-composite FK and a test-only CHECK to inject receipt-write
failure. Tested rollback of metadata and publication when that insert fails.
New recovery scenarios cover original-result replay/conflict/tenant rejection,
rollback and receipt rejection, concurrent duplicates executing the callback
once, concurrent enum edits without lost values, and lost COMMIT acknowledgement.
The test wire wrapper consumes a real server COMMIT OK then returns EOF. The
caller receives CommitError with no result; recovery finds the committed receipt
on a healthy connection and replay never reruns the mutation. Expected driver
EOF/closed-connection messages in this test are deliberate fault evidence.

Validation: gofmt; go mod download; go vet ./...; go test -mod=readonly -race
-count=1 -timeout=120s -v ./internal/contract ./internal/store/mysql all passed,
exit 0, using compose.mysql-test.yaml (MySQL 8.4.8 / Go 1.25.8). Existing seven
publication scenarios plus five new recovery scenarios passed; MySQL package
1.749s. No skips. This small fixture is not a performance-capacity benchmark.
PostgreSQL code and application constructors were unchanged; full PostgreSQL
regressions were not rerun for this isolated MySQL-adapter change.

Ticked DM-01.3b.2b.1 only. DM-01.3b.2b and DM-01 remain IN_PROGRESS. Production
lineage, complete owner APIs/grants, request canonicalization, service-level
recovery, DDL readiness and runtime wiring remain explicit unchecked work.

## Data-model contract preconditions and local build context - 2026-10-23

Added explicit PublicationCondition modes: absent, exact and any. Zero/unknown
modes and incompatible UUID/mode combinations fail validation. The owner must
choose policy explicitly; missing preconditions do not become unconditional.
Read the current contract and check the condition after acquiring the table lock
but before invoking mutation. Mismatches return ErrPublicationVersionConflict
without a mutation or receipt. Saved receipts now include condition mode and
expected version independently of the request hash; changed conditions conflict
even if the caller retains the original fingerprint. Valid replay returns its
original receipt before evaluating current-version conditions, so subsequent
edits do not break recovery of completed operations.

These conditions guard ingestion-contract versions only. Presentation-only
changes can retain that version. Metadata/display revision checks and the P-01
DDL operation fence remain separate unimplemented requirements. Fixture receipt
columns were expanded; no production migration or runtime activation occurred.

Verification passed: gofmt, go mod download, go vet ./..., and go test
-mod=readonly -race -count=1 -timeout=120s -v ./internal/contract
./internal/store/mysql in the isolated MySQL 8.4.8 / Go 1.25.8 fixture, exit 0.
Added validation tests plus real-engine first/exact/stale/missing conditions,
changed-condition replay conflicts and a concurrent same-version race asserting
one callback executes and one request conflicts. Prior rollback, tenant, enum,
receipt and real lost-COMMIT-acknowledgement tests also passed. MySQL package
2.292s, no skips. This does not constitute a workload performance benchmark.

Closed the previously recorded service-local Compose build-context mismatch:
context is now '..' and dockerfile is 'data-model-service/Dockerfile'. Read-only
docker compose config --format json resolved backend as context and the expected
Dockerfile path, exit 0. No application service was started; deployment database,
ports, credentials and runtime engine were not changed. The actual builder using
this backend context passed in the earlier recorded verification; not rerun here.

Ticked DM-01.3b.2b.3 and DM-01.2g only. Data-model remains active and IN_PROGRESS;
production schema, canonical request construction, authorization/grants, receipt
retention, physical readiness, display replacement and service wiring remain open.

### 2026-10-23 - DM-01.3b.2b.4 canonical owner publication requests

- Implemented dm-publication-request/v1 and SHA-256 fingerprints for UpdateTableInput, CreateFieldInput, UpdateFieldInput, CreateFieldEnumValueInput and UpdateFieldEnumValueInput. The fixed format binds tenant, table, trusted actor, explicit publication condition, operation kind and every input field, including presentation-only changes.
- Optional values preserve omitted versus explicit empty/false. Empty enum seed lists normalize consistently; nonempty ordering and all seed fields remain significant. Invalid identities, mismatched table scope, unsupported input types and invalid UTF-8 fail before hashing. Ownership, fresh authorization and business validation remain owner responsibilities.
- Moved publication conditions to internal/contract, retaining MySQL adapter aliases. Documented format evolution and remaining operation types in internal/service/publication_request.md. No application constructor or runtime route was switched to MySQL.
- Verification: disposable compose.mysql-test.yaml fixture, Go 1.25.8 and MySQL 8.4.8. go vet ./... passed. go test -mod=readonly -race -count=1 -timeout=120s -v ./internal/service -run TestPublicationRequest passed all four tests, including 28 input-field subtests. The same race-enabled command for ./internal/contract ./internal/store/mysql passed, including 14 MySQL integration scenarios for concurrency, rollback, tenant integrity, conditions and lost commit acknowledgement. Intentional EOF logs came from the commit fault test. These selected suites did not skip tests. Full PostgreSQL regression was not rerun in this step.
- Ticked only DM-01.3b.2b.4; production integration parent remains IN_PROGRESS. Remaining formats, full metadata migrations, restricted grants, durable DDL/readiness fencing, display behavior and service-level recovery still prevent runtime adoption. Data-model module remains active and incomplete.

### 2026-10-23 - DM-01.3b.2c publication byte limits

- Added a required maxBytes constructor argument to the staged MySQL publication store (supported 1..64 MiB, no runtime default). Existing fixture callers explicitly use 1 MiB. Runtime constructors still do not use this adapter.
- SQL CASE/OCTET_LENGTH projections suppress oversized table/field/enum text and prior/receipt JSON before returning it to Go. Streaming checks enforce one cumulative selected-text budget across table, fields and enums. Row limits remain independent structural bounds. Oversize fails with ErrSnapshotLimit; nothing is truncated or silently omitted.
- New publications must fit Go JSON bytes and MySQL normalized textual JSON bytes. Changed publications add one scalar measurement query before persistence so committed results remain readable under the same configured limit. This is not binary JSON storage sizing or a total heap bound. Capacity, packet-size headroom, query plans and concurrent workload benchmarks remain pending.
- Added publication_limits_test.go: constructor rejection/boundaries plus ten MySQL integration scenarios for multibyte table text, exact raw boundaries, individual/cumulative field and enum sizes, escaping expansion, atomic rollback, prior/replay/recovery limits, and recovery after restoring a compatible limit. Limit failures return no success result and prior/receipt rejection does not invoke the mutation callback.
- The first run caught SQL NULL scanning into json.RawMessage returning a generic scan error. Corrected prior and receipt reads to scan nullable byte slices before assigning JSON. Fresh-fixture rerun passed module-wide go vet ./... and go test -mod=readonly -race -count=1 -timeout=120s -v ./internal/contract ./internal/store/mysql, using Go 1.25.8 and MySQL 8.4.8. All ten new integration scenarios and fourteen existing publication scenarios passed; no selected tests skipped. Intentional EOF logs belong to the lost-COMMIT-acknowledgement test. Full PostgreSQL runtime regression was not rerun for this staged-adapter-only change.
- Updated adapter documentation with limit semantics, additional query cost, recovery configuration constraints and official MySQL normalization reference. Ticked DM-01.3b.2c only. Data-model production migration, ownership/grants, DDL readiness, display and runtime entry-point gates remain open. No other module was started and no application runtime was switched.

## 2026-10-23 - DM-01.2h: atomic worker attempt and outcome persistence

Status: COMPLETED for this bounded preparation gate; DM-01 remains IN_PROGRESS.

- Added the driver-free IndexJobExecutionStore and validated IndexJobOutcome contract. StartAttempt commits its claim before any inspection/DDL. CompleteAttempt conditionally matches tenant, job, running status and attempt count and appends history in the same short transaction.
- Implemented PostgreSQL and staged MySQL adapters. PostgreSQL cmd/worker now selects the new adapter. No MySQL runtime selection, production schema migration, durable transport or physical DDL fence is implied.
- Runner checks tenant/table identities and table name, propagates outcome/history failures, preserves original execution errors, rounds timestamps explicitly to UTC microseconds, handles terminal redelivery without DDL and recovers an already-created index after the retry budget is exhausted. Missing indexes do not trigger new DDL beyond that budget.
- PostgreSQL real-engine tests cover applied/pending/failed audit rollback, stale and wrong-tenant attempts, terminal/retry behavior and concurrent completions. Worker unit tests cover failure propagation, attempt limits and terminal redelivery.
- MySQL 8.4.8: all nine new worker integration leaf scenarios passed with race detection, alongside the existing 24 publication/byte-limit scenarios. Includes concurrent claims/completion, cancellation, invalid payload/precision/size, real server COMMIT acknowledgements hidden from claim and completion callers, and Runner -> real MySQL metadata store integration with a fake physical executor.
- Verification: isolated data-model-mysql-test compose fixture; go vet ./...; go test -mod=readonly -race -count=1 -timeout=120s -v ./internal/ports ./internal/worker ./internal/contract ./internal/store/mysql. Exit 0; selected MySQL integration scenarios executed, not skipped. Fixture removed with down --volumes.
- Limitations: metadata attempt fencing does not serialize DDL. P-01 named-lock/intent/readiness/shape verification, production exact-key/FK/grant schema, MySQL queue, legacy delivery inspection, runtime wiring and load/failover acceptance remain open. See internal/worker/execution_contract.md.

## 2026-10-23 - DM-01.2i: fail-closed service history and revision writes

Status: COMPLETED for error propagation and PostgreSQL rollback verification; DM-01 remains IN_PROGRESS.

- All 20 service history writes now propagate serialization/repository errors via recordSchemaChange. Removed the silent empty-JSON fallback. All 16 existing schema-revision paths propagate revision insertion failures. Missing audit dependencies fail explicitly.
- Preserved the existing selection of revision-producing operations; did not invent revision events for table updates, navigation or index requests. Updated unit/HTTP test fixtures to supply explicit recording revision repositories. Tenant-create HTTP regression also asserts its revision identity.
- Added 36 real-engine rollback cases across all nine owner services. Each injected failure occurs after a successful audit/revision INSERT, proving callback errors abort the transaction without relying on PostgreSQL SQL-error poisoning. Before/after snapshots compare all 12 metadata tables, publication payload/version, physical tenant schemas/columns/indexes and River rows.
- Verified helper fail-closed behavior and the full module suite: docker compose -p data-model-tx-test -f backend/data-model-service/compose.migration-test.yaml run --rm test sh -ec 'go vet ./... && go test -mod=readonly -race -p 1 -count=1 -timeout=180s ./...'. Exit 0. Includes real PostgreSQL/River service, display, HTTP authorization, import/export, reconciliation and repository regressions. MySQL integration is tested separately above; a PostgreSQL-only full-module invocation is not evidence of MySQL integration execution.
- Scoped git diff --check passed after gofmt, with only existing Windows line-ending warnings.
- Limitation: PostgreSQL transactional DDL rollback is not a MySQL DDL protocol. Production services continue on PostgreSQL; staged MySQL components are not an end-to-end migration. No other module was advanced. See internal/service/audit_contract.md and the unchecked parent/module gates.

## 2026-10-23 - DM-01.2f: legacy repair delivery inspection and recovery

Status: COMPLETED for recovery tooling and verification. DM-01 and all parent MySQL conversion gates remain IN_PROGRESS.

- Added internal/reconcile/delivery_recovery.go and cmd/recover-delivery. Default is inspect-only; apply requires an exact tenant/job and operator reference/reason. Only pending reconcile_repair jobs with valid table ownership and no retained matching River delivery are eligible.
- Locks one owner row, checks every queue and delivery state including legacy rows without unique metadata, and inserts delivery plus recovery history in one transaction. Preserves owner state, original schedule and attempt count. No physical DDL or network work under the lock. Wrong tenant fails without an unscoped fallback.
- Records database session principal separately from asserted operator identity. Uses configured queue/max attempts in the command. CLI apply refusal exits 2, failures exit nonzero, and execution has a 30-second deadline. Added command binary build/copy to Dockerfile; the full image was not built or deployed in this pass.
- Added operator runbook modules/01_data_model_legacy_delivery_recovery.md, including retained-terminal evidence, ambiguous acknowledgement handling, query-cost limits and no automatic broad scan. Production inventory and repair were not performed.
- Verified 23 real PostgreSQL/River cases with race detection: inspect/apply/replay, audit attribution, custom queue/attempt configuration, concurrent recovery, all eight River states with legacy uniqueness metadata absent, wrong tenant/invalid request, four non-pending owner states, operation/type/table identity rejection, history/queue failure and cancellation rollback.
- Initial integration fixture failed because one SQL parameter mixed text and the River enum; fixed the fixture with an explicit enum cast. All 23 cases passed on the subsequent run.
- Final verification: docker compose -p data-model-tx-test -f backend/data-model-service/compose.migration-test.yaml run --rm test sh -ec 'gofmt -w cmd/recover-delivery/main.go internal/reconcile/delivery_recovery.go internal/reconcile/delivery_recovery_test.go && go mod download && go vet ./... && go test -mod=readonly -race -p 1 -count=1 -timeout=180s -v ./... && CGO_ENABLED=0 go build -o /tmp/recover-delivery ./cmd/recover-delivery && /tmp/recover-delivery -help'. Exit 0, including full service/worker/HTTP/display/reconciliation/PostgreSQL regression suite and command build/help.
- TestMySQLPublication was skipped in this PostgreSQL fixture. No MySQL code changed this pass; this run is not claimed as MySQL integration evidence. Prior separately recorded MySQL results remain the evidence for staged adapters.
- Remaining module gates: production schema/metadata repositories and exact keys, P-01 physical DDL fencing/verification/readiness, complete publication/display replacements, durable MySQL queue and runtime/consumer wiring, shared-schema compatibility, and representative performance/recovery acceptance. No other module advanced.

## 2026-10-23 - DM-01.1a: exact-byte metadata key dictionary

Status: COMPLETED for definitions and the tested enforcement primitive. DM-01.1 and DM-01 remain IN_PROGRESS.

- Added internal/store/mysql/exactkeys with two InnoDB tables, a server-side digest function and a restricted resolver procedure. Fixed internal domain plus global/tenant/parent scope identifies a hash bucket; an exclusive bucket lock and full binary-value current read allocate/reuse an internal UUID. Hash collisions never substitute for equality.
- Definitions preserve unbounded key values without a prefix unique key or digest-only uniqueness. Runtime callers cannot supply the digest. Representative consumer BEFORE triggers derive keys, including global nullable keys; the fixture definer is locked and retains only required table/routine privileges. Runtime test credentials cannot read/write dictionaries, call the resolver or drop triggers.
- Added guards against disabling session unique/FK checks. Dictionary lookup uses FOR SHARE under the exclusive bucket lock, avoiding UPDATE privileges on immutable dictionary rows while seeing data committed after an older RR snapshot.
- Verified ten real MySQL scenarios: full bytes/long text/Unicode/case/trailing spaces and scopes; update/forged key; rollback and failed owner insertion; concurrent duplicate; independent tenants; runtime bypass denial; old RR snapshot; disabled unique checks; disabled FK checks; and forced digest collisions in a separate isolated database. Invalid physical schema identifiers are also rejected by a unit test.
- Initial tests exposed binary-log installation privileges and FOR UPDATE privilege requirements. Kept binary-log safeguards enabled, used privileged installation with a restricted definer, and changed only the immutable dictionary read to FOR SHARE. Final tests passed without granting runtime dictionary access or dictionary UPDATE to the definer.
- Deployment acceptance still requires every owner binding/grant, migration lifecycle, retention policy, representative plans/load tests and source collation/data profiling. No production definer or runtime switch was installed. See the package README for primary MySQL references and the exact invariant/privilege contract.

## 2026-10-23 - DM-01.1b: structural metadata schema and adapter integration

Status: COMPLETED for structural definitions, metadata column mapping and targeted integration. Parent migration gates remain open.

- Added internal/store/mysql/metadata: mappings for all twelve source metadata tables, three ordered child relations, publication receipts and 32 insert/update guards. Together with the two private dictionary tables, the fixture installs 18 tables plus routines/guards. Definitions are individual server statements; no multi-statement SQL parsing or IF NOT EXISTS drift masking.
- Tenant-composite FKs reject cross-tenant and wrong-table field references. Ordered children preserve ordinal identity, options prevent duplicate field references, and owner identity/event guards reject rewrites. Nine exact-text domains preserve scoped/global and conditional nonempty dedupe uniqueness. Session integrity guards also cover nullable branches with no dictionary call.
- Added the source-column manifest at modules/01_data_model_metadata_schema.md, including every source column, type/default transformation, relationship/key/index mapping and explicit remaining enforcement/data-profile gaps. Source finite-domain checks are stronger than old TEXT columns and require drift preflight; they do not silently coerce historical values.
- Full-schema tests revealed the earlier publication subset omitted source table_name. PublicationStore now writes table_name alongside payload/version on insert and changed publication; tests verify rename publication and historical receipt recovery. Updated the fault-injection subset accordingly.
- Seven full-schema MySQL integration scenarios cover source-column/ordered replacement coverage, metadata uniqueness, all six field types and nullable integrity guards, tenant/table relationships, ordered children/pivots, publication/receipt immutability, and worker outcomes/terminal redelivery/conditional dedupe. The full-schema fixture uses a privileged installer for structural validation; it is not a complete production privilege matrix.
- Final combined verification on fresh isolated MySQL 8.4.8: docker compose -p data-model-mysql-test -f backend/data-model-service/compose.mysql-test.yaml run --rm test sh -ec 'gofmt -w internal/store/mysql/metadata/schema_test.go && go mod download && go vet ./... && go test -mod=readonly -race -count=1 -timeout=120s -v ./internal/contract ./internal/ports ./internal/worker ./internal/store/mysql/...'. Exit 0. All 50 real-engine leaf scenarios passed (33 existing publication/worker/limit cases, 10 dictionary cases, 7 metadata cases), plus selected unit tests. No selected MySQL fixture was skipped. Intentional wire-fault EOF messages remain expected test evidence.
- PostgreSQL runtime behavior was not changed by this pass, and its database suite was not rerun; module-wide vet compiled all packages. Scoped diff whitespace checks passed. Corrected and verified data-model documentation source links.
- Compose fixture now supplies an isolated test-root account solely for ephemeral database/user/grant tests, with no published ports or application networks/volumes. This does not change application credentials or deployment configuration.
- Not complete: production migration execution/dirty recovery, full repository layer, physical mappings and P-01 DDL/readiness, display/publication owner closure, durable MySQL queue, runtime/consumer wiring, import/export and performance/recovery/data acceptance. Runtime remains PostgreSQL. No other service module was advanced.

## 2026-10-23 - DM-01.1c: tenant-bound table, field and enum repositories

Implemented NewMetadataRepositories over a caller-owned sql.Tx and authorized
nonzero tenant UUID. Table/field/enum ports now have staged MySQL implementations
for every method. Scoped predicates, composite foreign keys and exact-parent row
locks prevent cross-tenant and wrong-parent mutations. Missing updates/deletes
return sql.ErrNoRows; unchanged updates succeed without affected-row assumptions.
Reads bound row counts and text sizes without returning partial models. Writes
validate UTF-8, NUL, byte budgets, identifiers and signed enum sort order. Added
ordered access indexes to the target structural definitions, not production DDL.

Eight real MySQL integration scenarios passed with race detection and module-wide
vet: round trips/all field types/mutable columns/no-op updates; tenant/parent
isolation; ordered archived-inclusive lists; deletion/missing rows; rollback after
exact-key conflict; row/multibyte/cumulative limits; lock cancellation and unrelated
tenant progress; invalid constructor/read-only rejection. New code remains staged.

The initial cancellation test incorrectly required a successful rollback after the
driver discarded a cancelled connection. Corrected the test to recognize the
explicit context deadline cleanup error and verify a fresh transaction works.
The shared Mutation helper continues to propagate unconfirmed rollback errors.
A subsequent full-suite run encountered existing tables in the older publication
fixture because the same disposable server was reused. The new repository and
structural suites passed; the complete suite is being rerun after removing and
recreating only the isolated data-model-mysql-test Compose project.

DM-01.1c is ticked for the three repository adapters and their tests only. The
parent module remains open for remaining repositories, owner publication/display
closure, physical registry/DDL recovery, durable transport, migration/grants,
runtime and consumers, full acceptance and representative query-plan benchmarks.
Runtime remains PostgreSQL/River; no application database was modified.

Final clean-fixture validation for DM-01.1c passed (exit 0):
  docker compose -p data-model-mysql-test -f backend/data-model-service/compose.mysql-test.yaml run --rm test sh -ec 'go mod download && go vet ./... && go test -mod=readonly -race -count=1 -timeout=120s -v ./internal/contract ./internal/ports ./internal/worker ./internal/store/mysql/...'
All 58 real MySQL leaf scenarios passed (33 publication/worker/limits, 10 exact-key,
7 structural metadata, 8 repository scenarios), plus selected unit tests. No tests
in this selection skipped. Expected fault-injection EOF logs were present. This
is not a full PostgreSQL integration rerun or a representative performance result.
Touched-document relative links and scoped git diff whitespace checks passed.

## 2026-10-23 - DM-01.1d/e: link, navigation and pivot repositories

Implemented every method of LinkRepository, NavigationOptionRepository and
PivotRepository on the existing tenant-bound, caller-transaction MySQL factory.
Navigation resolves all five names through one tenant/table-consistent joined
query. Links retain exact child-table/name uniqueness and scoped deletion. Pivot
paths preserve order and repeated links, write in batches of at most 1,000 rows,
and load parents/children in one coherent statement. Joined-row overflow and
invalid/gapped paths return errors without partial models. Added tenant/created/id
access indexes to staged link/navigation/pivot definitions.

Five relationship scenarios passed: round-trip/order/delete and parameterized names;
tenant/wrong-table endpoints; byte/row limits; rollback and pivot delete barriers;
concurrent exact-name duplicate creation. Four pivot scenarios passed: 1,001-entry
paths and boundaries; tenant/shape rejection; complete owner rollback after child
failure; corrupt ordinal rejection. These supplement the prior eight repository
scenarios. The final clean fixture passed all 67 MySQL leaf scenarios (33 existing
publication/worker/limits, 10 exact-key, 7 structural metadata, 17 repositories),
selected unit tests, race detection and module-wide vet. No selected tests skipped.

Final command (exit 0):
  docker compose -p data-model-mysql-test -f backend/data-model-service/compose.mysql-test.yaml run --rm test sh -ec 'gofmt -w internal/store/mysql/metadata_repositories.go internal/store/mysql/pivot_repository.go internal/store/mysql/metadata/repositories_test.go && go mod download && go vet ./... && go test -mod=readonly -race -count=1 -timeout=120s -v ./internal/contract ./internal/ports ./internal/worker ./internal/store/mysql/...'

DM-01.1d/e ticked for implemented repository ports and their tests. Parent DM-01
remains open. Endpoint suitability/path traversal, display cleanup, managed-index
scheduling, remaining repositories, authoritative publication, durable transport,
DDL recovery/registry, production migration/grants, runtime/consumer wiring and
representative query plans remain pending. Runtime is still PostgreSQL/River.
No application database was changed. PostgreSQL integration suites were not rerun.

## 2026-10-23 - DM-01.1f: audit history and tenant revision repositories

Implemented SchemaChangeRepository and TenantSchemaMigrationRepository on the
same authorized-tenant/caller-transaction MySQL factory. All existing port methods
are supported. Concrete ListPage methods use tenant/stream-bound time/UUID keyset
cursors and existing indexes, without OFFSET or whole-history COUNT. Equal-time
records use binary UUID tie breaking; byte overflow returns no partial page/cursor.
History input rejects oversized JSON before the shared valuer allocates a string.
JSON normalization is explicit; these are not byte-exact audit archives.

Seven event scenarios passed: JSON/numeric round trip and immutable events; equal-
timestamp pages; tenant/cursor scope; malformed input and limits; exact input/stored
JSON boundaries and cumulative limits; complete owner rollback after audit failure;
complete owner rollback after revision failure. Existing transaction cleanup and
commit uncertainty contracts are unchanged. No automatic mutation retries added.

Validation:
- Initial clean-fixture run passed 73 real MySQL leaf scenarios, selected unit
  tests, race detection and go vet ./... (exit 0).
- After reviewing JSON allocation limits, added the early byte guard and boundary
  scenario; the affected metadata suite passed all 31 scenarios with race detection
  and module-wide vet (exit 0). No selected tests skipped in either run.
- Scoped whitespace checks and touched-document link validation passed.

Final command:
  docker compose -p data-model-mysql-test -f backend/data-model-service/compose.mysql-test.yaml run --rm test sh -ec 'gofmt -w internal/store/mysql/event_repository.go internal/store/mysql/metadata/event_repositories_test.go && go mod download && go vet ./... && go test -mod=readonly -race -count=1 -timeout=120s -v ./internal/store/mysql/metadata'

DM-01.1f ticked for the repository methods and verified behavior only. Parent DM-01
remains open: tenant/options/job/assembled repositories, owner publication/display,
physical registry and DDL recovery, durable queue, production migration/grants,
API/import/export/worker/reconcile and consumer wiring, representative performance
and complete end-to-end acceptance. HTTP cursor adoption and production retention
permissions are pending. Runtime remains PostgreSQL/River. No application database
was changed; PostgreSQL integration tests were not rerun this increment.

## 2026-10-23 - DM-01.1g: tenant persistence and platform catalogue

Implemented tenant-bound create/get/list/status methods on the MySQL metadata
factory and an explicit read-only TenantCatalog with bounded global lists and
keyset pages. Null external keys remain distinct from empty strings; global exact
external/schema uniqueness uses existing guards. Status updates lock only the
exact tenant and use UTC microsecond statement time. No provisioning/DDL occurs.
Added created_at/id catalogue index to the staged structural definitions.

The scoped List intentionally differs from PostgreSQL's global repository List.
Future TenantService platform listing must explicitly use the authorized catalogue;
zero tenant identity is not a global-access sentinel. Constructors do not replace
request authorization. Registration allocation, status readiness and HTTP error/
cursor integration remain open. Runtime remains PostgreSQL/River.

Seven real MySQL scenarios passed: nullable/empty/Unicode round trips; tenant scope
and status updates; catalogue/keyset scope separation; exact global uniqueness;
registration/history/revision rollback; row/byte/configuration/cursor limits;
concurrent duplicate external-key registration. Full selected regression passed
81 MySQL scenarios, selected unit tests, race detection and module-wide vet (exit 0).
No application database was modified. PostgreSQL integration tests were not rerun.

Command:
  docker compose -p data-model-mysql-test -f backend/data-model-service/compose.mysql-test.yaml run --rm test sh -ec 'gofmt -w internal/store/mysql/metadata_repositories.go internal/store/mysql/tenant_repository.go internal/store/mysql/metadata/tenant_repository_test.go && go mod download && go vet ./... && go test -mod=readonly -race -count=1 -timeout=120s -v ./internal/contract ./internal/ports ./internal/worker ./internal/store/mysql/...'

DM-01.1g ticked for the implemented adapters/tests. Module stays open for options,
job and assembled repositories; authoritative owner publication/display; physical
registry and DDL recovery; durable queue; migration/grants; API/import/export/worker/
reconcile and consumer wiring; full end-to-end and representative performance
acceptance. No completion of these parent requirements is implied.

### 2026-10-23 - DM-01.1h ordered table-options persistence

Implemented the staged MySQL TableOptionsRepository and factory port binding.
Use one bounded statement for ordered reads, exact-table locking for first/updating
writes, shared locks for active same-table field validation, 1,000-row insert batches,
identity/JSON retention and atomic display-version increments. Reject invalid lists,
corrupt ordinals, bounds violations and exhausted versions explicitly.

Verification: isolated MySQL 8.4.8 fixture; Go 1.25.8 gofmt, module-wide go vet,
and go test -mod=readonly -race -count=1 -timeout=120s -v ./internal/contract
./internal/ports ./internal/worker ./internal/store/mysql/... all passed (exit 0).
Six new options scenarios exercise round trips, replacement/JSON/identity/version,
tenant/parent/active/distinct rules, owner-audit rollback, limits/gaps/overflow,
1,001-field batching and concurrent first upserts. No production DB was accessed.

Ticked DM-01.1h only. Full display JSON validation, archive/link cleanup, job and
assembled repositories, owner protocols, DDL recovery, durable MySQL delivery,
migrations/grants and runtime wiring remain open. Runtime remains PostgreSQL/River.

### 2026-10-26 - DM-01.1i index-job metadata repository

Implemented every IndexJobRepository port on the tenant-bound caller transaction:
create/get/list/exact dedupe and locked dedupe, start attempt, applied/failed/pending
outcomes and operator retry. Preserve nullable fields, ordered/repeated columns,
schedules and attempt counts; reject malformed input, bounds and attempt exhaustion.
Caller owns rollback, authorization, audit and durable enqueue. Legacy outcome ports
are unfenced and must not replace IndexJobExecutionStore.CompleteAttempt.

An added old-repeatable-read test exposed a stale dictionary subquery during locked
dedupe lookup. Replaced it with a single-table current read using a generated
nonunique tenant/hash index plus full binary equality. This also avoids granting
read access to private dictionary data. Existing guards still enforce exact global
nonempty dedupe uniqueness. Forced generated-hash collision tests verify that hash
equality cannot return a different job. Representative plans/load remain open.

Initial verification found a cancellation-test cleanup assertion that rejected the
expected driver deadline during rollback. Corrected it and verified the preceding
write was not committed. The added snapshot regression initially failed, then passed
with the single-table lookup. Final fresh-fixture verification passed (exit 0):
Go 1.25.8 gofmt, module-wide go vet, and race-enabled selected regression covering
contract, ports, worker and recursive MySQL packages. Nine new job scenarios passed;
no selected tests skipped. MySQL fixture version: 8.4.8.

Command:
  docker compose -p data-model-mysql-test -f backend/data-model-service/compose.mysql-test.yaml run --rm test sh -ec 'gofmt -w internal/store/mysql/metadata_repositories.go internal/store/mysql/index_job_repository.go internal/store/mysql/metadata/job_repository_test.go && go mod download && go vet ./... && go test -mod=readonly -race -count=1 -timeout=120s -v ./internal/contract ./internal/ports ./internal/worker ./internal/store/mysql/... > /backend/data-model-service/mysql-job-verification.log 2>&1'

Ticked DM-01.1i only. Parent tasks remain unchecked. Runtime stays PostgreSQL/River;
no application database was accessed and no MySQL runtime was activated. Remaining:
assembled-model repository, transaction-manager/owner integration, complete display
and publication protocols, physical registry/DDL recovery, durable queue/River drain,
production migration/grants, consumer/entry-point wiring and full acceptance/load
qualification. PostgreSQL integration tests were not rerun for this staged increment.

Final cleanup succeeded: isolated Compose container/network/volumes removed.
Retained raw test evidence: evidence/2026-10-26_dm-01.1i_tests.log.
Touched-file whitespace and local Markdown link checks passed.

### 2026-10-26 - DM-01.1j and DM-01.2j.1/.2 staged model reads and delivery

Implemented tenant-bound assembled snapshots with nine SELECTs independent of
model cardinality, shared row/text budgets, map-based reference assembly and
explicit rejection of missing references/corrupt ordinals. Archived data and empty
collection shapes are preserved. Added a driver-free model/tenant/revision snapshot
port and snapshot-aware owner service constructor; public revision hashing is
unchanged. Eight MySQL scenarios passed, including concurrent writer consistency,
exact aggregate budgets, cancellation/pool reuse, owner integration and a measured
constant nine SELECTs at both two and 102 tables. This is not a latency benchmark.

Implemented staged index-delivery definitions and persistence: atomic owner enqueue,
cross-queue job reservations, scheduled SKIP LOCKED claims, random lease tokens,
database-clock heartbeat/expiry, fenced local completion/retry, cancellation and
retention. Inspected pinned River v0.31.0 sources: ByArgs without ByQueue includes
completed work in dedupe; preserve completed reservations until cleanup. Retention
defaults are 24h completed/cancelled and 7d discarded. Maintenance cursor pages move
past locked source prefixes; selected-row writes include tenant predicates.

Added the existing worker execution port bound to one committed delivery lease.
Job outcome, audit and delivery completion/retry commit atomically. A shared helper
preserves the standalone execution store's attempt/history implementation. Terminal
redelivery does not restart the domain attempt. Queue and domain counters remain
distinct; delivery exhaustion is not physical readiness or a domain success.

Verification:
- Eight assembled/published snapshot MySQL scenarios passed.
- Nine delivery lifecycle scenarios passed, including scheduling, dedupe, competing
  claims, fencing, callback/expiry rollback, reaper/retention, cancellation and
  locked-prefix fairness.
- Three actual lost-COMMIT-acknowledgement scenarios passed for enqueue, claim and
  completion. Fault consumes server COMMIT OK before dropping the acknowledgement.
- Seven leased worker outcome scenarios passed: applied/pending/failed, audit
  rollback, expired lease, stale domain attempt and terminal redelivery.
- Snapshot-owner service and existing revision-token tests passed with race detection.
- Final fresh MySQL 8.4.8 fixture: Go 1.25.8 gofmt, module-wide go vet, selected
  contract/ports/worker/recursive-MySQL regression passed with race detection,
  no selected tests skipped, exit 0. Existing publication and standalone execution
  commit-fault regressions passed after the shared outcome helper change.

The initial snapshot-service command encountered shell pipe quoting; corrected
commands ran both service test groups successfully. No Go edits occurred while
compiler/test processes read the source tree. A disposable long-running verifier
reused the Go cache; the MySQL service was recreated for the full publication run.

Final command in the isolated verifier:
  docker exec data-model-mysql-verifier sh -ec 'go vet ./... && go test -mod=readonly -race -count=1 -timeout=120s -v ./internal/service -run TestSnapshotDataModelReadService && go test -mod=readonly -race -count=1 -timeout=120s -v ./internal/service -run TestBuildRevisionID && go test -mod=readonly -race -count=1 -timeout=120s -v ./internal/contract ./internal/ports ./internal/worker ./internal/store/mysql/... > /backend/data-model-service/mysql-data-model-progress-verification.log 2>&1'

Ticked only DM-01.1j and DM-01.2j.1/.2. Added unticked DM-01.2j for complete transport
and worker integration. All module parents stay open. Runtime remains PostgreSQL/
River; no application DB changed. PostgreSQL integration suites were not rerun.

Remaining module requirements: physical registry/DDL intent/readiness/reconciliation;
full display validation/invalidation/cleanup; authoritative owner transactions and
publication/replay authorization; production migration lineage/dirty-DDL recovery/
grants; polling/heartbeat supervision and complete retry policy; operator inspection,
redrive/audit and River drain/translation; server/API/import/export/reconcile/worker/
migrate and consumer wiring; representative query plans/load, resource budgets,
failover and complete authorization/end-to-end acceptance. Performance workload/SLO
inputs were requested; no unprovided acceptance thresholds are fabricated.

Final cleanup confirmed: disposable Go verifier stopped/removed, MySQL container
removed, project network removed and fixture volumes removed. Touched-file
whitespace checks, local Markdown links and retained test-result checks passed.
Final selected regression evidence:
  evidence/2026-10-26_mysql-data-model-progress-verification.log
Additional staged verification logs retained alongside it. No module-completion
or production-readiness claim is made.

## 2026-10-26 - DM-01.2j.3 staged polling and lease supervision

Implemented driver-free LeaseSupervisor and DeliveryPoller. Explicit lease,
heartbeat, claim and execution durations; initial renewal before execution;
cancellation on heartbeat failure; fixed concurrency without claim prefetch;
joined shutdown/panic cleanup; mandatory execution-error reporting; fatal claim
and reporting failures propagate after all executors exit. Durable transitions
remain exclusively in the leased execution adapter. No executable was changed.

Verification: Go 1.25.8 gofmt, module-wide go vet, selected contract/ports/worker/
recursive MySQL regression passed with race detection on disposable MySQL 8.4.8.
After adding final tests, worker plus all MySQL metadata tests passed again with
race detection, no skipped tests. Nineteen deterministic lifecycle scenarios and
one real-engine test (two slots, three deliveries, periodic heartbeat, atomic
job/history/delivery completion) passed. No Go edits while compiler/tests ran.
Evidence:
  evidence/2026-10-26_mysql-worker-supervision-verification.log
  evidence/2026-10-26_mysql-worker-supervision-final-verification.log

Inspected pinned River v0.31.0 retry_policy.go in dependency cache. Default delay
uses (len(Errors)+1)^4 seconds with +/-10 percent jitter, not simply claim attempt.
Recorded outstanding error-count/rescue/import mapping and 24-hour adapter-delay
bound compatibility. No source-compatible retry policy claim is made.

Ticked only DM-01.2j.3. Parent DM-01.2j and all module parents remain open.
Runtime stays PostgreSQL/River; application database unchanged. Maintenance
scheduler, retry policy, operations/River drain, migration/grants, full display,
owner/publication/receipt authorization, physical DDL intent/readiness,
executable/consumer integration and complete acceptance remain outstanding.

Cleanup confirmed: disposable verifier removed and isolated MySQL compose project
removed with --volumes. No application database was used. Scoped diff whitespace
check passed; existing CRLF conversion warnings were informational.

## 2026-10-26 - DM-01.3d.1 MySQL display persistence and cleanup

Audited the module checklist and current runtime. The module is not complete:
PostgreSQL/River is still the configured authority. No parent tasks were ticked.

Implemented strict display validation, tenant-bound bounded settings snapshots,
optimistic saves, atomic owner effects/audit, exact-table and dependency locks,
parent-caption invalidation/clearing, and field archive/delete and link-delete
cleanup. Added three tenant-consistent reverse dependency relations with indexed
referenced-identity lookups and immutable tuple guards. Cleanup verifies that its
owner callback actually archives/deletes the selected resource; unfinished work
returns an error and rolls back. Parent sets for surviving relationships are
included in ordered locks and rechecked before reference rebuilding.

Settings fields/enums are batched by distinct parent table, not loaded per link.
Measured five SELECTs with one and 101 links sharing a parent. This is query-count
evidence, not representative performance acceptance. All new code is staged;
existing PostgreSQL display behavior and executable wiring remain unchanged.

Verification on disposable MySQL 8.4.8 / Go 1.25.8:
- Eighteen real-engine display scenarios passed with race detection: atomic save,
  strict target/legacy validation, tenant/row/byte bounds, optimistic concurrency,
  owner/audit rollback, dependent caption invalidation and legacy-caption removal,
  field/link cleanup, unfinished-owner rejection, cancelled competing locks,
  dependent version exhaustion, actual lost-COMMIT-acknowledgement recovery,
  query-count scaling and single-slot pool reuse.
- Five strict-validator scenarios passed with race detection.
- Module-wide go vet and the full selected contract/ports/worker/recursive MySQL
  regression passed, no selected tests skipped.
- After final lock-order refinement, module-wide vet and all MySQL metadata tests
  passed again. gofmt applied; no source edits while compiler/tests read the tree.

The initial test fixture had incorrect domain Link field names; corrected before
execution. Cleanup testing then exposed MySQL JSON_TABLE over a filtered derived
source. Replaced that source with direct owner-table joins and tenant/bounded-ID
predicates in every branch. All corrected cleanup cases and regressions passed.
No SQL failures were ignored or converted to success.

Evidence:
  evidence/2026-10-26_mysql-display-validation-verification.log
  evidence/2026-10-26_mysql-display-regression-verification.log
  evidence/2026-10-26_mysql-display-final-verification.log
Contract: modules/01_data_model_display_contract.md
Ticked only DM-01.3d.1. DM-01.3d is IN_PROGRESS, not complete.

Outstanding module gates remain substantive: physical registry/DDL intent/shape/
readiness/reconcile; production checksummed lineage, dirty-DDL recovery and grants;
complete owner transaction/publication/receipt/replay authorization; all display
owner entry points, table lifecycle, validated dependency backfill and physical
record readers; worker maintenance/retry/operations/River drain; server/import/
export/worker/reconcile/migrate and consumer wiring; full end-to-end authorization,
rollback/failover, representative plans/load and cutover acceptance. Current tests
use privileged disposable fixtures and do not establish production readiness.
No application database was modified. PostgreSQL integration suites were not rerun.

Cleanup confirmed: disposable Go verifier stopped/removed; isolated MySQL
container and project network removed with compose down --volumes. Scoped
whitespace checks passed. Application runtime and database remain unchanged.

### 2026-10-26 - DM-01.2j.4 delivery maintenance scheduler
Implemented a driver-free maintenance port and staged scheduler with independent
expiry/retention cursors across paced bounded cycles, alternating pages, explicit
per-page deadlines and joined cancellation. Errors report stream and acknowledged
changes without hiding partial commits or commit ambiguity. No retry is inferred.
Verified Go 1.25.8 formatting, module-wide vet, six deterministic scenarios and a
MySQL 8.4.8 locked-prefix/restart/reservation-cleanup integration scenario. Full
selected contract/ports/worker/recursive MySQL race regression passed. Evidence:
evidence/2026-10-26_mysql-maintenance-verification.log.
Ticked only DM-01.2j.4. Runtime remains PostgreSQL/River. Retry policy, operational
supervision/metrics, physical DDL fencing, migration/grants, executable wiring,
River drain and representative acceptance remain open. Disposable fixture only;
no application database accessed. Fixture remains running for subsequent work.

DM-01.2j.4 follow-up: added actual lost-COMMIT-acknowledgement tests for expiry
and terminal cleanup. Both consume server COMMIT OK before hiding it. Scheduler
propagates typed commit ambiguity and acknowledged_changes=0; healthy reads prove
the committed effect and rescans do not repeat it. Module vet and all worker and
metadata race tests passed after this addition. Evidence:
evidence/2026-10-26_mysql-maintenance-final-verification.log.
Pinned River v0.31.0 rescue source review confirms a remaining gap: rescue appends
an error and applies error-count backoff, unlike the staged immediate MySQL reap.
Runner terminal validation vs retry exhaustion also yield different River transport
outcomes despite both producing failed domain state. Delivery contract records
these requirements explicitly; retry/transport parity is NOT checked complete.
Disposable verification cleanup completed: verifier stopped and compose down
--volumes removed the fixture container/network. No application database changed.

### 2026-10-26 - DM-01.1k.1 ordered MySQL migration runner and command
Implemented a data-model-owned migration ledger in the selected shared database,
ordered SHA-256 checksums binding every step input, exact before/after SHOW CREATE
JSON verification, pinned installer locking, dirty intent before DDL and inspected
restart recovery. No transactional-DDL assumption, forced clean, destructive down,
untracked object adoption or automatic compensation. Errors remain explicit.
Added separate cmd/migrate-mysql with validate/status/up, bounded reviewed JSON
plans, environment password, verified TLS, explicit deadlines and a one-connection
installer pool. Dockerfile packages the executable; image deployment is not claimed.
The existing PostgreSQL migration executable and application runtime are unchanged.
Eleven table/history/recovery scenarios, four view/routine/trigger scenarios and
one real lost-DDL-acknowledgement scenario passed on disposable MySQL 8.4.8. The
wire fault consumed server DDL OK; recovery inspected and finalized without
replaying CREATE. Plan/CLI tests, module-wide vet, full selected race regression,
command build and help passed. Additional concurrency/ledger/executable-object
tests then passed with all metadata regression and vet. Go 1.25.8 formatting done.
Evidence: evidence/2026-10-26_mysql-migration-runner-verification.log,
evidence/2026-10-26_mysql-migration-regression-verification.log,
evidence/2026-10-26_mysql-migration-final-verification.log.
Ticked DM-01.1k.1 only; parent DM-01.1k and module gates remain open. A complete
reviewed production manifest with reference definitions and restricted definers/
grants is NOT supplied by this runner. Physical registry, model DDL intent/barrier/
publication/recovery, owner transaction manager and business-service integration
remain outstanding. No application database accessed or migrated.
Verification cleanup completed: stopped the disposable verifier and removed the
isolated MySQL fixture/network with compose down --volumes. No application data
was changed. Production remains PostgreSQL/River.

### 2026-10-26 - DM-01.1l physical registry and durable intent preparation
Added structural definitions for dm_physical_tables, dm_physical_fields and
immutable-request dm_ddl_intents, plus the separately tracked active-operation FK.
Physical table and field-component names derive from immutable UUIDs in the shared
application database; no per-service or per-tenant application database added.
Implemented transaction-bound tenant-scoped registration and bounded mapping reads,
including complete IP family/address/prefix components, batched writes and corrupt
mapping rejection. LockPublished holds a shared exact-table registry lock through
the caller transaction and rejects closed/retired/stale-version models.
Implemented short atomic intent/barrier preparation, actor/request/expectation-bound
replay, monotonic preparation epochs, normalized plan limits, bounded internal plan
reads and tenant/state/UUID keyset recovery discovery. No physical DDL, readiness
shortcut or public model-success response is implemented by preparation.
Eleven MySQL 8.4.8 integration scenarios passed, including 1,000 IP fields/3,000
component rows, complete rollback, tenant/FK rejection, immutable replay identity,
corrupt components, stale/retired gates, previous-writer blocking, independent
tables, competing intents, failure after intent insertion, normalized byte limits,
paging and actual lost-COMMIT acknowledgement. Module-wide vet and full selected
race regression (including migration command tests) passed; Go 1.25.8 gofmt done.
Evidence: evidence/2026-10-26_mysql-physical-registry-verification.log and
evidence/2026-10-26_mysql-physical-registry-regression-verification.log.
Ticked DM-01.1l only. Production manifest/definers/grants, physical SQL/index/base
column planning, pinned per-step DDL execution/recovery, shape/grant verification,
atomic publication/barrier release, owner authorization/transaction-manager and
consumer integration remain open. Readiness in tests uses privileged fixture setup,
not a production publisher. Runtime remains PostgreSQL/River. No application
DB accessed; disposable fixture only. No parent migration task marked complete.
Cleanup verified: disposable verifier stopped; compose down --volumes removed the
isolated MySQL container and network. No application data or runtime changed.

### 2026-10-26 - DM-01.1m descriptive staged MySQL naming
- Replaced abbreviated SQL object names in data-model adapters, structural definitions, fixtures and current contracts with data_model_ ownership names. Canonical request and exact-key protocol namespaces remain stable.
- Physical table mappings now use tenant_<tenant label>_<table label>_<full UUID>; columns use field_<field label>_<full UUID>_<component>. Sanitized immutable labels are hints; tenant IDs/FKs and authorization remain authoritative. One logical application database; no per-tenant application databases.
- Full selected CLI/contract/ports/worker/MySQL race regression and module-wide vet passed on the isolated MySQL 8.4.8 fixture. Final metadata race regression, vet and migration-command build also passed after strengthening invalid-label guards. Evidence: evidence/2026-10-26_mysql-descriptive-naming-verification.log and evidence/2026-10-26_mysql-descriptive-naming-final-verification.log.
- Added meaningful tests for readable names, tenant separation, stability after logical renames, maximum name length, Unicode fallback and malformed labels including trailing newlines. Checked only DM-01.1m; parent gates remain open.
- Runtime remains PostgreSQL/River. No application database changed. This does not establish production manifest/grants, physical DDL readiness/recovery, owner transaction integration or complete migration acceptance. Previously installed artifacts require reviewed upgrades; never rewrite installed checksums.
- Cleanup confirmed: stopped the disposable verifier and removed the isolated MySQL compose fixture with down --volumes; application databases were untouched.

### 2026-10-26 - DM-01.1k.2 installation artifacts and DM-01.2k.1 physical step execution
- Added ordered staged structural installation steps, explicit dedicated definers, exact binding to reference definitions, locked-account provisioning SQL, object-specific definer grants and fail-closed grant/lock inspection. Verified fresh installation, prefix restart at the cyclic active-intent FK, replay and restricted-account behavior. This is not a deployment-specific reviewed production artifact or a complete runtime privilege policy.
- Added data_model_schema_change_steps with immutable tenant/table/operation/ordinal/checksum identity and indexed bounded recovery. PhysicalExecutor holds the same table-specific named lock and pinned connection for journal/inspection/DDL, uses short registry-then-intent transactions, and leaves uncertain DDL dirty. Unexpected shape blocks the operation. Registry remains closed; execution ends at verifying, not published or ready.
- Nine MySQL physical recovery scenarios passed, including actual successful CREATE/ALTER acknowledgements hidden from the driver, followed by healthy recovery without blind DDL replay. Added strict plan boundary tests. Full selected cmd/migrate-mysql, contract, ports, worker and recursive MySQL race regression passed without skips; module-wide go vet passed. Evidence: evidence/2026-10-26_mysql-installation-physical-final.log (initial regression also retained).
- Initial fixture run exposed timestamp nanosecond precision and cleanup using a canceled context; corrected both and reran successfully. One test command had PowerShell/container quoting issues; reran the complete packages successfully without the problematic regex argument.
- Checked only DM-01.1k.2 and DM-01.2k.1. Production manifest/upgrades, preflight/business invariants, full roles, operation-specific planners, physical grants, publication, runtime/reconciliation integration and broader acceptance remain open. Runtime still PostgreSQL/River; no application database changed.
- Cleanup confirmed: stopped the disposable verifier and removed the MySQL compose fixture with down --volumes, including all generated test databases/accounts. No application database changed.

### 2026-10-26 - DM-01.1k.3, DM-01.2k.2 and DM-01.2k.3
- Added typed, scoped six-type field additions and native index creation/removal, strict reference binding, Boolean/IP checks and explicit nullability. NOT NULL additions carry durable empty-table preconditions; populated tables block before journaled DDL rather than receiving synthesized values. Text indexes, enum enforcement and remaining table/type-change/backfill/retirement plans fail or remain explicitly unimplemented.
- Added final physical verification under the same pinned DDL lock through an owner callback: operation/epoch/version, full applied journal/checksums, exact schema, unexpected-trigger rejection and strict physical account policies. Writer/DDL privileges are separated; broad/extra/missing grants, roles and mandatory server roles are rejected. This helper does not open the registry or implement owner publication.
- Added fixture-only build-mysql-reference and a frozen 80-step fraud artifact (root Compose shared database name) with locked-definer SQL and provenance. Plan SHA-256: 0b5e2223ed0805e8ff6b6e185eabf30816ba643d723ca530a10826e19c74a841. Artifact remains needs_review. Added append-only upgrade binding, stable Git checkout bytes and read-only migrate-mysql verify under the migration lock.
- Actual CLI rehearsal in the disposable fixture applied the frozen artifact, rejected verification before grants, passed after the exact definer allowlist and replayed successfully. No application database was contacted. The generated reference database/account were cleaned before artifact output. An initially missing output parent caused an explicit error; created the repository output parent and regenerated successfully.
- An initial full regression exposed asynchronous old-session lock release in the lost-DDL-ack fixture. Production correctly returned lock-busy. Updated the fixture to wait for confirmed server-side lock release, not a blind elapsed-time DDL retry. Ten repeated physical recovery runs passed. Retained the initial failure log separately.
- Final fresh-fixture full selected race regression passed without skips: cmd/build-mysql-reference, cmd/migrate-mysql, contract, ports, worker and recursive MySQL adapters. Module-wide go vet and both command builds passed. Main evidence: evidence/2026-10-26_data-model-plans-readiness-regression.log; CLI/recovery/focused evidence retained alongside it.
- Checked only DM-01.1k.3, DM-01.2k.2 and DM-01.2k.3. Both production installation and full physical lifecycle parent tasks remain open. Remaining invariants, reviewed production policies, table lifecycle/text/enum plans, owner publication, runtime/reconciliation wiring and broader recovery/load acceptance are not claimed complete. Runtime remains PostgreSQL/River. Unrelated workspace changes were preserved.
- Cleanup confirmed: stopped the disposable verifier and removed the isolated MySQL compose fixture with down --volumes. All temporary reference/rehearsal/test databases and accounts were confined to that fixture. No application database changed.

## 2026-10-26 - DM-01.2k.4 bounded physical reconciliation

Implemented and verified:
- PhysicalExecutor.ReconcilePage selects a bounded tenant/state/operation page,
  projects routing IDs without loading each plan, closes rows before DDL and
  attempts operations sequentially with independent deadlines and existing locks.
- Partial failures remain joined errors with redacted per-operation outcome codes.
  Cursors advance over attempted work, including failures; new sweeps revisit
  failed/busy operations. Exhaustion is only a page snapshot, never readiness.
- Added TLS-enabled reconcile-physical-mysql with explicit tenant/state, bounded
  limits, password-env credentials, JSON partial reports and nonzero failures.
  Packaged the binary without changing server entrypoint or application runtime.
- Native physical role policies reject direct routine EXECUTE capabilities, which
  could otherwise bypass DML/DDL separation through definer routines.
- Real MySQL tests passed mixed busy/drift pages, continuation, restart, one-slot
  pools, tenant isolation, closed gates, command success/drift/error output, dirty
  recovery and actual lost CREATE/ALTER acknowledgements via reconciliation.
- Unit tests passed cancellation/cursor preservation, deadline isolation, joined
  error retention, uncertainty classification and invalid command/page inputs.
- Selected race regression and module-wide go vet passed. Subsequent targeted race
  tests covered final routine-policy and recovery-fixture changes; command build
  and help passed. Changed Go source was formatted; no source edits overlapped
  compiler/test execution. Evidence files use 2026-10-26_physical-* names.

Checklist: ticked DM-01.2k.4 only. DM-01.2k, DM-01.1k and overall DM-01 remain open.
Configured runtime remains PostgreSQL/River. No application database was changed.
Outstanding: full physical lifecycle/type/backfill/text-uniqueness planners,
reviewed production grants/install acceptance, owner authorization/publication,
transaction/service and queue/runtime wiring, operator audit/scheduling, former-
primary fencing, representative load/query plans and complete cutover acceptance.
- Cleanup confirmed: stopped data-model-mysql-verifier and removed the isolated
  data-model-mysql-test MySQL container/network with compose down --volumes.

## 2026-10-27 - DM-01.2j.5 explicit worker transport outcome

Implemented and verified:
- IndexJobOutcome now carries RetryDelivery for execution-error transport intent.
  Applied outcomes cannot retry. Pending outcomes continue to retry. Direct
  terminal validation outcomes acknowledge; failed domain outcomes caused by an
  execution error still request delivery retry, matching the source error path.
- The runner preserves its original returned execution error. PostgreSQL/River
  still observes that error; the staged MySQL adapter now uses explicit transport
  intent rather than assuming every failed domain outcome should be acknowledged.
- The stored delivery budget stays authoritative after process configuration
  changes. Exhausted retries discard transport and release reservations with
  delivery_attempts_exhausted, preserving domain status and immutable audit.
  Remaining retries of terminal jobs acknowledge without a new domain attempt.
- Four worker branch tests and thirteen real-MySQL leased scenarios passed with
  race detection, covering independent attempt budgets, last-attempt success,
  terminal redelivery, audit rollback, stale attempts and lost COMMIT ACK.
- Full selected command/contract/ports/worker/recursive MySQL race regression,
  module-wide go vet, worker build and recovery-command build passed. No skips
  or failures were found in the final regression. Changed Go files were formatted.
- Corrected a stale abbreviated physical-name entry in the implementation map.

Evidence: 2026-10-27_worker-transport-focused.log and
2026-10-27_worker-transport-regression.log. The separately retained
worker-exhaustion-superseded.log covers an abandoned intermediate experiment,
NOT the accepted contract: final code does not convert pending domain jobs to
failed solely because transport exhausted. Source/design review confirmed the
counters must remain separate; operator inspection/redrive must expose divergence.

Ticked only DM-01.2j.5. DM-01.2j and the overall module remain IN_PROGRESS.
Configured runtime remains PostgreSQL/River; the MySQL adapter is not activated.
No application database was changed. Backoff/error-count parity, rescue scheduling,
operator redrive/audit, River drain, physical lifecycle/planners, production roles,
owner publication, transaction/service/runtime wiring and full acceptance remain.
- Cleanup confirmed: disposable Go verifier stopped; isolated MySQL container
  and compose network removed with down --volumes. No application DB touched.

## 2026-10-27 - DM-01.2j.6 bounded operator delivery inspection

Implemented and verified:
- Added explicitly tenant-bound DeliveryInspector page/detail reads. One bounded
  statement joins selected deliveries, source jobs and matching reservations;
  reports retain domain/transport divergence and omit lease tokens, raw error
  text, unbounded job definitions and full histories. No writes or locks added.
- Keyset cursors validate tenant, UUID and microsecond timestamp. Separate bounded
  covering ranges for equal-time/lower-ID and older-time rows are merged before
  bounded detail lookups. The existing tenant/created/id index is reused; no schema
  artifact or installed migration checksum was changed.
- Tests passed with 1,003 tenant rows, two queues, five delivery states, a second
  tenant, a 1,000-row equal-time prefix and multi-megabyte omitted job fields.
  Stable cross-page order, no omissions/duplicates, cancellation, detail isolation
  and cursor rejection passed. One SELECT was measured at page sizes 1 and 1000.
- EXPLAIN exposed a table scan in the initial broad projection. Narrow covering
  selection removed the table scan, but an OR cursor still filtered the already-
  seen tenant prefix. Final disjoint predicates both use covering index ranges.
  Plans and intermediate evidence are retained; this is not a load benchmark.
- Added and packaged inspect-deliveries-mysql for explicit operator invocation:
  verified TLS outside the guarded fixture, password-env credentials, mandatory
  tenant, bounded page/deadline, cursor or detail modes and nonzero failures.
  Real command tests used SELECT-only credentials and exposed a pending domain
  job with discarded transport. Foreign detail access and invalid cursor/flags
  failed. The fixture reader was denied history SELECT and delivery UPDATE.
- Full selected command/contract/ports/worker/recursive MySQL race tests, module-
  wide go vet, command build/help and formatting passed. Final regression had no
  failures or skips. No Go edits overlapped compiler/test execution.

Evidence: 2026-10-27_delivery-inspection-* files. Initial fixture failure correctly
rejected changing immutable job definition fields; final fixture inserts large
values at creation. Initial plan-failure and covering-prefix-plan evidence are
superseded by final range-page and regression evidence, not hidden or overwritten.

Ticked DM-01.2j.6 only. DM-01.2j and the overall module remain IN_PROGRESS.
Configured runtime remains PostgreSQL/River; no server/worker entrypoint changed.
The operator tenant flag scopes reads, not authorization. Production operator
access/credentials, inspection audit, audited redrive, retry/rescue parity, River
translation/drain, physical lifecycle and publication, owner/runtime integration,
shared resource budgets and full acceptance remain outstanding.
No application database was changed.
- Cleanup confirmed: disposable verifier stopped and isolated MySQL container and
  compose network removed with down --volumes. No application DB touched.

### 2026-10-27 - DM-01.2j.7 audited discarded-delivery redrive

Implemented IndexDeliveryStore.RedriveDiscarded and the explicit privileged
redrive-delivery-mysql command. Only discarded deliveries with pending domain
jobs and no cross-queue reservation are eligible. Preserve domain attempts/status;
create new transport delivery/reservation/history atomically with explicit schedule
and transport budget. Source UUID keys immutable history to permit exactly one
redrive per source, with exact-input/principal replay after uncertain commit or
source retention cleanup. The history payload records request, actor/reason,
CURRENT_USER(), configuration and new delivery identity. Caller authorization is
mandatory on replay as well as initial apply; command actor is an attestation by
an already privileged operator, not an end-user authentication mechanism.

Ten metadata integration scenarios passed, including eight concurrent same-request
calls, rejected running/terminal jobs, tenant/source isolation, another queue's
reservation, forced audit failure rollback, retained-history replay and a real
server COMMIT acknowledgement hidden by the wire-fault fixture. Validation rejects
missing identities, unbounded/invalid UTF-8 input and non-microsecond schedules.

The new command uses verified TLS and password environment input, explicit apply,
all recovery identities and positive attempt budget, bounded execution and no
automatic retry. It is built/copied in Dockerfile; server entrypoint is unchanged.
Command integration passed with SELECT/INSERT grants plus UPDATE restricted to
immutable identity columns, required for MySQL FOR UPDATE. Job-status updates and
audit deletion were denied; actual identity changes failed the immutable guard.
Initial SELECT/INSERT-only policy failed with MySQL 1142; evidence retained. Initial
focused shell invocation also failed Windows regex quoting; corrected invocation
and all subsequent focused tests passed. No source edits occurred during compilation.

Final verification on a refreshed disposable MySQL 8.4.8 fixture / Go 1.25.8:
- gofmt on all new/changed Go files.
- go vet ./... passed.
- go test -mod=readonly -race -count=1 -timeout=120s -v on
  ./cmd/redrive-delivery-mysql ./cmd/inspect-deliveries-mysql
  ./cmd/reconcile-physical-mysql ./cmd/build-mysql-reference ./cmd/migrate-mysql
  ./internal/contract ./internal/ports ./internal/worker ./internal/store/mysql/...
  passed with no skipped tests.
- redrive-delivery-mysql build and help passed.
Evidence: evidence/2026-10-27_delivery-redrive-final-regression.log and associated
focused, restricted-command, superseded failure and help logs.

Ticked DM-01.2j.7 only. Runtime remains PostgreSQL/River; no application database
was changed. Production AccountPolicy currently lacks column-grant manifest and
verification support, so production role installation is not complete. Operator
authorization/credentials, physical recovery for running jobs, owner terminal
retry, backoff/rescue parity, River drain and full runtime/acceptance remain open.
Cleanup confirmed: stopped data-model-mysql-verifier and ran the scoped disposable
Compose down --volumes; MySQL container and project network were removed.

### 2026-10-27 - DM-01.3b.2d owner-transaction publication composition

User scoped this pass to DM-01.3a through DM-01.3e. Re-read AGENTS.md and the
checklist, inspected publication/request/display/metadata and physical verification
contracts. DM-01.3a was already complete; DM-01.3b/c/d/e require additional owner,
physical and runtime integration. Do not mark these parents complete.

Added PublicationStore.MutateAndPublishOperationTx, extracting the shared table
lock/snapshot/version/receipt implementation used by standalone publication.
The new method joins a caller-owned READ COMMITTED transaction, returns only a
provisional result and requires full rollback on any error. This makes publication
composable in existing display/cleanup callbacks and owner transactions without a
nested connection. Replay skips only its callback, not earlier outer effects;
owner authorization and receipt recovery before outer replay remain mandatory.

Six new full-metadata MySQL integration scenarios verify all six field types,
archived fields/enums, exact-key and immutable receipt guards, retention FK
protection, metadata/history/tenant revision/durable delivery atomicity, rollback
after provisional success, display rollback on receipt failure, combined display
commit, archive cleanup with published snapshot, real lost outer-COMMIT recovery,
replay without duplicated history/delivery, and foreign tenant rejection. A
single-connection pool verifies no nested transaction/pool acquisition. Added
invalid-input tests for the caller-owned entry point.

Initial display fixture reads failed because tenant.New creates a pending tenant;
corrected only the disposable fixture to activate it, matching the existing display
contract. The initial failure log is retained. All six corrected scenarios passed.

Verification:
- gofmt on the four new/changed Go files; go vet ./... passed.
- MySQL 8.4.8 / Go 1.25.8 race regression passed for contract, ports, worker,
  display and recursive mysql adapters, including prior publication receipt,
  concurrency, byte-limit and commit-fault scenarios.
- Canonical owner request regression passed with race detection.
- Three PostgreSQL-backed display scenarios skipped in the MySQL-only run; then
  all display and PostgreSQL repository tests passed on a separate disposable
  PostgreSQL 16 fixture with race detection. This includes both portable ingestion
  payload and semantic publication-version parity tests against PostgreSQL.
Evidence: evidence/2026-10-27_publication-composition-regression.log,
2026-10-27_publication-request-regression.log and
2026-10-27_publication-display-postgres-regression.log, plus focused/failure logs.

Ticked only DM-01.3b.2d; 1.3b/c/d/e remain incomplete. Production authorized owner
paths, verified physical gate publication, outer-operation replay, full display
lifecycle/backfill and API/import/export/worker/reconcile/consumer integration are
not completed by these adapter tests. Runtime remains PostgreSQL/River. No
application database or production configuration was changed. No subagents used.
Cleanup confirmed: stopped the MySQL verifier and removed both scoped disposable
Compose projects with down --volumes (MySQL and PostgreSQL containers/networks).

### 2026-10-27 - DM-01.3c.1 verified physical publication commit

Continued the user's DM-01.3a-e scope. Re-read AGENTS.md and inspected intent,
registry, publication, physical verification and access contracts. Added
PhysicalExecutor.PublishVerifiedOperation as the staged single-table owner commit
boundary; extracted the existing physical verifier for reuse on the pinned
connection without reacquiring or releasing the named execution lock.

Every call/replay requires an authorization callback before DB connection/locks;
it must authenticate the real caller, bind the requested actor and authorize the
tenant/table. The adapter matches immutable intent actor/hash and absent/exact
version conditions (any-version is rejected). Unpublished work must pass complete
journal/shape/trigger/grant verification, then recheck model/registry/intent state
under ordered transaction locks. Owner effects, ingestion contract, receipt,
published intent and open registry version commit atomically on that connection.
Archive cannot reopen writes; a receipt without published intent is not adopted.
Owner semantic plan/metadata correspondence remains an explicit callback precondition.

Historical published replay returns the original receipt after current authorization
without replaying effects or reopening a newer barrier. It does not attest to current
schema/grants or physical availability. Commit/rollback/lock-cleanup errors propagate
and no successful publication result is returned on uncertainty. No automatic retry.

Fourteen real-engine scenarios passed on MySQL 8.4.8 / Go 1.25.8: initial publication,
exact-version follow-up, historical replay over a new barrier, revoked authorization
including replay, actor/hash conflict, unverified intent, schema/grant drift, orphan
receipt, archive rejection, owner/receipt/gate failure rollback and actual lost-COMMIT
acknowledgement. Single-connection execution and IS_USED_LOCK checks prove the owner
transaction retains the pinned lock. Fault recovery waits for observed lock release.
Unit tests reject missing callbacks, cross-database composition, invalid actors and
invalid identities/conditions before mutation.

Verification: gofmt; go vet ./...; full selected race-enabled tests for reconcile-
physical, redrive, inspect, migrate and reference-builder commands, contract, ports,
worker and recursive MySQL adapters. All passed, with no skipped tests. Evidence:
evidence/2026-10-27_physical-publication-regression.log plus focused/expanded logs.
No Go edits occurred while compilers/tests read the tree.

Ticked DM-01.3c.1 only. DM-01.3b/c/d/e parents remain incomplete. Concrete platform
authorization/business-service callbacks, complete canonical/semantic planner
coverage, multi-table display lifecycle, production role policies, runtime consumer
wiring, failover fencing and load/resource acceptance remain open. Tests use fixture
metadata credentials and explicit physical-role policies; they are not production
role deployment. Runtime remains PostgreSQL/River; no application DB was changed.
Cleanup confirmed: stopped data-model-mysql-verifier and removed the disposable
MySQL Compose container and project network with down --volumes.

## 2026-10-27 - DM-01.3d.2 authorized display publication replay

Implemented staged DisplayStore.SavePublished and pinned dm-display-save/v1 canonical requests. Trusted tenant/table/actor, ingestion condition, optimistic display version and all display inputs are hash-bound. Mandatory authorization runs before locks on every call/replay. Receipt recovery precedes current topology/version checks and is rechecked under ordered model-table locks before effects. New saves require the matching open physical registry gate held through commit. Display settings/references/caption invalidation, owner SQL effects, audit and publication receipt commit together; semantic ingestion-token changes roll back. Historical replay returns the original ingestion receipt without repeating effects or reopening a newer barrier; it is not a saved-settings API response or current readiness attestation.

Verified: canonical wire/hash unit tests; seven real MySQL scenarios covering replay, eight concurrent duplicates, owner and receipt rollback, closed gate, semantic mutation rollback and actual lost COMMIT acknowledgement. Additional assertions cover changed actor/request/condition, revoked authorization, audit deduplication and replay during a newer closed barrier. Module-wide go vet and selected command/contract/ports/worker/recursive MySQL race regression passed with no skips. Go 1.25.8 / isolated MySQL 8.4.8. Evidence: evidence/2026-10-27_display-publication-hash.log and evidence/2026-10-27_display-publication-regression.log. Changed Go sources formatted before compilation.

Only DM-01.3d.2 ticked. DM-01.3b/c/d/e remain incomplete: concrete owner authorization/business callbacks, complete semantic DDL planners, cleanup/table lifecycle, production grants/backfill, API/import/export/worker/reconcile/reader integration and full acceptance/load/cutover requirements remain. Runtime still PostgreSQL/River. No application database changed. Disposable verifier stopped and fixture compose project removed with --volumes after verification.

## 2026-10-27 - DM-01.1k.4 restricted column policies and installer verification

Implemented ObjectGrant.Columns for explicit SELECT/INSERT/UPDATE/REFERENCES columns with canonical ordering, identifier validation, no overlap with table-wide privileges, per-object column bounds and a 1 MiB rendered-policy bound. Complete-account verification normalizes only column-list order and continues rejecting additional/missing privileges, wider scopes, roles and grant options. Restricted redrive integration now provisions and verifies one typed policy rather than adding manual out-of-policy UPDATE grants.

Added migrate-mysql -account-policies for verify/validate. The dm-account-policies/v1 document binds the migration database and 1..64 explicit accounts; bounded decoding rejects duplicate keys/accounts, unknown fields, trailing documents, unsupported grants, excess nesting and oversized input/rendered grants. Verify checks additional accounts on the pinned connection under the existing migration lock after schema lineage and definer checks; other actions reject this flag. Output identifies how many additional accounts were verified. No account provisioning, credential storage or automatic grant repair was introduced. External privilege administration still requires operational exclusion.

Verified with Go 1.25.8/MySQL 8.4.8 disposable fixtures: column grant rendering and policy bounds; five real drift scenarios (extra/missing columns, table-wide UPDATE, unrelated object access, grant option); actual column-only reads and locking permissions with denied status/read access; restricted redrive/replay and immutable-identity enforcement; complete-schema CLI installation/verification rejecting missing/excess grants and wrong account lock, then passing after correction; malformed policy document cases. The first drift test demonstrated that REVOKE table-wide UPDATE also removes its column grants; fixture restoration now explicitly reapplies the reviewed policy, and this operational behavior is documented. Final module-wide vet and selected command/contract/ports/worker/recursive MySQL race regression passed with no skips. Evidence: evidence/2026-10-27_column-policy-regression.log, evidence/2026-10-27_account-policy-command.log and evidence/2026-10-27_account-policy-final-regression.log.

Only DM-01.1k.4 ticked. Production account policies/installation/credential lifecycle, remaining invariants/DDL planners, concrete owner authorization and publication/display callbacks, table lifecycle, runtime/consumer/worker wiring and full acceptance remain incomplete. PostgreSQL/River runtime remains configured; no application database or production grant was changed.

Cleanup for DM-01.1k.4: stopped data-model-mysql-verifier and removed the data-model-mysql-test compose project with --volumes after the final regression. No application database was accessed.

## 2026-10-27 - DM-01.2k.5 and DM-01.2k.6 native field uniqueness and physical grant separation

Found and fixed a staged compatibility gap after ObjectGrant.Columns was introduced: physical policy mode separation inspected only table-wide privileges. It now examines column privileges on every object and rejects any target-column grants outside the exact native table-wide role contract. This prevents reader column DML, consumer REFERENCES elsewhere, and DDL target column INSERT/UPDATE from reaching publication even when those grants are accurately listed in AccountPolicy. Reviewed writer metadata-column rights remain permitted. Tests keep writer/DDL accounts present while checking an independent reader, avoiding a false pass from a missing mandatory writer. Real MySQL tests install unsafe allowlisted grants, assert the owner callback is not invoked and verify recovery after safe grants are restored.

Added CreateFieldUniqueIndex and DropFieldUniqueIndex to the typed planner. They use the same record_unique_<field UUID> name as AddFields, resolve authorized mapped fields and retain full native key semantics, including IP family/address/prefix. Text uniqueness remains explicitly unsupported. Five real-engine scenarios verify bool/int/float/timestamp/IP duplicate rejection, multiple NULLs, drop, failed recreate with duplicate data, explicit fixture repair/recreate and preserved rows. Existing typed integration now binds and executes a six-step durable reference chain including field unique creation/removal, verifies replay and retains NOT NULL preflight behavior. Owner relationship validation, metadata publication, production collision handling and resource/load acceptance remain open.

Verification: full selected command/contract/ports/worker/recursive MySQL race regression and module-wide vet passed after the role fix; subsequent focused planner/durable integration, final role unit checks, five value lifecycle scenarios and module-wide vet passed after the planner additions. No final tests skipped. Go 1.25.8/MySQL 8.4.8 disposable fixture only. Evidence: evidence/2026-10-27_physical-column-role-regression.log, evidence/2026-10-27_field-unique-planner-regression.log, evidence/2026-10-27_physical-role-unit-final.log, evidence/2026-10-27_field-unique-values.log.

Only DM-01.2k.5 and DM-01.2k.6 ticked. No parent completion claimed. Runtime remains PostgreSQL/River. Full table creation, text exact uniqueness, enum/type/backfill/retirement plans, owner-service and worker wiring, production grants and full acceptance remain outstanding. No application database changed.

Cleanup for DM-01.2k.5/6: stopped the disposable verifier and removed data-model-mysql-test with --volumes after verification. No application database accessed.

## 2026-10-27 - DM-01.3b.2b.5 creation request canonicalization

Extended the pinned dm-publication-request/v1 helper from five to nine existing owner input types: table/link/pivot/index-job creation now encode every mutation field. Tenant scopes must match; links bind all endpoints and use the child table, pivots use the base table and index jobs use their requested table. Table creation binds the stable owner-allocated table identity in context. Pivot path/index column order and nil/empty distinctions remain significant. Optional scheduled instants canonicalize to UTC with nanosecond precision, preserving nil versus explicit time without mutating caller values. Existing supported v1 encodings are unchanged.

Verified: original golden encoding; reflection-based every-input-field coverage across nine types; four additional wire-format cases; scope and endpoint rejection; invalid UTF-8; nil/empty and optional identity distinctions; path/column order; equivalent timezone instants and nanosecond differences. The obsolete assertion that table creation was unsupported was replaced with the still-unsupported pointer form. Canonical request race tests, module-wide vet and the full selected command/contract/ports/worker/recursive MySQL race regression passed with no final skips on isolated Go 1.25.8/MySQL 8.4.8. Evidence: evidence/2026-10-27_creation-request-regression.log and evidence/2026-10-27_creation-request-mysql-regression.log.

Only DM-01.3b.2b.5 ticked. Hashing does not implement authorization, stable ID allocation/recovery, original creation-result receipts, business validation, multi-table locking or runtime owner integration. Deletes/archives, navigation, legacy options and imports still require explicit owner request contracts. DM-01.3b/c/d/e remain incomplete; PostgreSQL/River remain configured. No application database changed.

Cleanup for DM-01.3b.2b.5: stopped the disposable verifier and removed the data-model-mysql-test compose project with --volumes after successful verification. No application database accessed.

## 2026-10-27 - DM-01.2h.1 worker index postcondition

The shared index runner no longer records applied solely because CreateManagedIndex returned nil. Initial inspection must supply a nonempty name; post-create inspection must confirm existence and the same name. Missing/changed identity returns ErrManagedIndexUnverified. Inspection failures use normal retry/exhaustion and preserve the original error alongside failed outcome persistence. Existing terminal redelivery and recovery at attempt exhaustion are preserved.

Eight new unit scenarios cover creation, missing result, changed/empty identity, inspection errors, exhaustion, outcome persistence errors and empty existing identity. Three isolated PostgreSQL scenarios execute real index DDL/inspection, remove the created index before inspection, and recover committed DDL after an injected inspection failure without creating twice. Integration outcome persistence is a validating fake; real atomic outcome/history behavior remains covered by the existing PostgreSQL/MySQL adapter regressions. These tests prove existence/identity only, not full index shape or MySQL readiness.

Verification: gofmt; module-wide go vet; selected MySQL command/contract/ports/worker/recursive adapter race suite; PostgreSQL tenantdb/worker/store/service/reconcile/display/httpapi/cmd-worker race suite. Both final suites exited zero without skips. Go 1.25.8, disposable MySQL 8.4.8 and PostgreSQL 16 only. Evidence: evidence/2026-10-27_worker-postcondition-unit.log, evidence/2026-10-27_worker-postcondition-mysql-regression.log and evidence/2026-10-27_worker-postcondition-pg-regression.log.

Only DM-01.2h.1 ticked. No migration parent completed. Configured runtime remains PostgreSQL/River; the shared runner source guard also applies to that backend. MySQL full physical verification, owner-service/queue/runtime integration, production deployment grants and acceptance remain outstanding. No application database accessed.

Cleanup for DM-01.2h.1: stopped data-model-mysql-verifier and removed data-model-mysql-test and data-model-worker-pg-test with --volumes after successful verification. No application database accessed.

## 2026-10-27 - DM-01.3b.2b.6 navigation request canonicalization

Added CreateNavigationOptionInput to the pinned dm-publication-request/v1 format. The source table is the receipt scope; tenant, source table/field, target table, filter field and ordering field are all bound. Missing endpoint identities and mismatched tenant/source scope fail explicitly. Field roles remain significant and self-table navigation remains representable. Existing nine operation encodings are unchanged.

Verified the complete navigation wire and SHA-256 value, every-input-field coverage across ten supported input types, six missing identity cases, tenant/source mismatch, swapped filter/ordering roles, self-table navigation and unsupported pointer input. Canonical request race tests, module-wide vet and selected command/contract/ports/worker/recursive MySQL race regressions exited zero with no skips. Go 1.25.8/MySQL 8.4.8 disposable fixture only. Evidence: evidence/2026-10-27_navigation-request-regression.log and evidence/2026-10-27_navigation-request-mysql-regression.log.

Only DM-01.3b.2b.6 ticked; no parent completed. This helper does not authorize either table, check field/link/pivot suitability, allocate creation identities, retain original creation results or schedule target indexes. Those owner transactions, remaining request formats, production deployment and runtime acceptance are still outstanding. Runtime remains PostgreSQL/River and no application database was changed.

Cleanup for DM-01.3b.2b.6: stopped data-model-mysql-verifier and removed data-model-mysql-test with --volumes after successful verification. No application database accessed.

## 2026-10-27 - DM-01.2k.7 native field nullability planning

Added PhysicalPlanner.SetFieldNullable for authorized registered field snapshots. It preserves native types, LONGTEXT collation, DATETIME(6) precision and unsigned IP tuple components; all IP components change in one ALTER. Existing Boolean/IP checks and native unique indexes remain. Foreign fields, enums and unique-text enforcement paths fail closed. Requiring NOT NULL binds an immutable empty-table precondition rather than allowing implicit data synthesis. Populated-table tightening, including tables whose current values happen to be non-NULL, still requires a separate validation/backfill protocol.

Six real-MySQL scenarios verify populated value preservation across bool/int/float/string/timestamp/IP, multiple NULLs, retained uniqueness and retained Boolean/IP checks. Extended typed integration binds and executes an eighteen-step reference chain (native fields/indexes plus six tighten/six relax steps) and replays it. A separate populated-table tightening case blocks before any DDL journal step, preserves the closed protocol, and resumes after explicit deletion of the isolated fixture row. Unit tests verify all type components, distinct statement identities, empty-table requirements and unsupported/foreign rejection. Module-wide vet, recursive MySQL race regression and selected command/contract/ports/worker race regression all exited zero without skips. Evidence: evidence/2026-10-27_nullability-regression.log and evidence/2026-10-27_nullability-command-regression.log. Go 1.25.8/MySQL 8.4.8 disposable fixture only.

Only DM-01.2k.7 ticked. The physical planner does not update metadata, captions, audit or publication and does not authorize requests. Owner integration, other physical operation types, populated-table validation/backfill, production grants, runtime wiring and full resource/load acceptance remain open. PostgreSQL/River remain configured; no application database accessed.

Cleanup for DM-01.2k.7: stopped data-model-mysql-verifier and removed data-model-mysql-test with --volumes after successful verification. No application database accessed.

## 2026-10-27 - DM-01.2l staged MySQL service transaction manager

Added a tenant-bound implementation of ports.TransactionManager using one READ COMMITTED mysqlstore transaction and bounded metadata repositories. A mandatory MutationAuthorizer runs before every owner callback on that exact transaction. The authorizer contract requires trusted identity, current write permission and resource ownership using local SQL; concrete production identity SQL is still outstanding. Queue submission requires the same database pool and database, participates in the transaction and fails explicitly when absent. RunWithTx supports explicit owner SQL/publication composition without a second connection. No automatic retry occurs; uncertain commits preserve mysqlstore.CommitError.

All legacy SchemaManager methods explicitly reject with ErrPhysicalProtocolRequired, including readiness inspection. The adapter cannot execute DDL through the legacy metadata transaction path. Owners must adopt durable physical planning/execution and verified publication. Existing callbacks must not retain the transaction/store, commit/rollback themselves, issue DDL/session changes or call networks.

Verified configuration limits, absent authorizer, foreign queue pool/database, missing callbacks, every physical-method rejection and missing queue. Eleven MySQL scenarios cover atomic job/history/revision/delivery work and dedupe, owner rollback, missing queue, rejected physical DDL rollback, cancellation, panic cleanup, foreign tenant rejection, actual lost-COMMIT acknowledgement without retry, authorization denial before callback, and publication commit/rollback on the exact transaction. A one-connection pool proves no nested acquisition, and escaped stores reject use after completion. The existing TenantService.Create path creates pending tenant metadata/audit/revision using a deliberately preallocated matching tenant scope. This is not registration endpoint wiring or durable registration recovery.

The initial regression exposed a test-only shared error variable: the expected missing-queue error caused later subtests to stop before executing. Corrected its per-subtest scope and added publication composition checks. The focused final suite passed, then the disposable fixture was refreshed and module-wide vet plus the full selected command/contract/ports/worker/recursive MySQL race suite passed with no skips. Evidence retains the initial diagnostic log as well as final success: evidence/2026-10-27_transaction-manager-regression.log, evidence/2026-10-27_transaction-manager-command-regression.log, evidence/2026-10-27_transaction-manager-final.log and evidence/2026-10-27_transaction-manager-full-final.log.

Only DM-01.2l ticked for the staged metadata transaction adapter. No parent completed. Concrete identity authorization, authorized platform catalogue routing, registration allocation/recovery integration, all owner publication/display/DDL protocols, runtime/worker wiring, production deployment grants and full acceptance remain outstanding. PostgreSQL/River remain configured. Go 1.25.8/MySQL 8.4.8 disposable fixtures only; no application database accessed.

Cleanup for DM-01.2l: stopped data-model-mysql-verifier and removed data-model-mysql-test with --volumes after successful final verification. No application database accessed.

## 2026-10-27 - DM-01.2m explicit authorized tenant catalogue service path

Added ports.TenantCatalog (List only) and TenantService.WithTenantCatalog. Default construction retains PostgreSQL repository listing. Selecting a catalogue affects only List, never scoped Get/Create/Provision; catalogue errors do not fall back to scoped repository reads. Added staged MySQL TenantCatalogReader with explicit database/row/byte bounds and a mandatory CatalogAuthorizer on every read-only snapshot. It returns no partial result on authorization, bounds, cancellation or transaction errors. Production identity SQL and endpoint wiring are not supplied by fixture authorization callbacks. Snapshot authorization does not promise cancellation of reads already in flight when permissions change.

Verified service routing, foreign lookup rejection, no fallback after catalogue denial, default behavior, nil/configuration rejection and required authorizer. Five MySQL scenarios plus empty-list setup exercise real TenantService global listing, authorization on each call/revocation, row/byte overflow, MySQL read-only transaction write rejection, cancellation and pool reuse, and eight concurrent calls through one connection. Module-wide vet, selected command/contract/ports/worker/recursive MySQL race regression and PostgreSQL service/httpapi race regression all exited zero without skips. Go 1.25.8/MySQL 8.4.8/PostgreSQL 16 disposable fixtures only. Evidence: evidence/2026-10-27_tenant-catalog-routing.log, evidence/2026-10-27_tenant-catalog-mysql-regression.log and evidence/2026-10-27_tenant-catalog-pg-regression.log.

Only DM-01.2m ticked. The staged service catalogue path is implemented, but concrete production platform authorization, MySQL API wiring, paging/export consistency and registration allocation/recovery remain outstanding. Full owner/worker physical/publication integration, production deployment and acceptance gates also remain open. PostgreSQL/River remain configured and no application database was accessed.

Cleanup for DM-01.2m: stopped data-model-mysql-verifier and removed data-model-mysql-test and data-model-catalog-pg-test with --volumes after successful verification. No application database accessed.

## 2026-10-27 - DM-01.2n tenant registration scope allocation

Added ports.TenantRegistrationManager and explicit TenantService.WithTenantRegistrationManager selection for Create only. The owner validates input/dependencies, allocates one nonzero tenant ID and passes that exact scope into registration before repository access. Default PostgreSQL creation retains its existing transaction path. MySQL RegistrationTransactionManager copies immutable transaction configuration per call, requires platform registration authorization for the new identity, rejects existing scopes before callbacks, and checks that the callback persisted a pending tenant before commit. It has no queue binding and inherits explicit physical-operation rejection. Zero/uninitialized manager scopes fail before authorization or database access.

Seven real-MySQL scenarios verify eight concurrent owner allocations with distinct scopes and one tenant/history/revision each; authorization on registration and existing-scope rejection; denied/foreign/missing/premature-active registrations; owner-error rollback; and a real lost-COMMIT acknowledgement with exactly one attempt and durable tenant/history/revision while the service returns an error and no result. Unit tests cover owner allocation order, zero IDs, input validation before allocation, capability selection and invalid boundaries. Module-wide vet, full selected command/contract/ports/worker/recursive MySQL race regression and PostgreSQL service/API race regression exited zero without skips. Go 1.25.8/MySQL 8.4.8/PostgreSQL 16 disposable fixtures only. Evidence: evidence/2026-10-27_registration-scope-unit.log, evidence/2026-10-27_registration-scope-mysql.log and evidence/2026-10-27_registration-scope-pg.log.

Only DM-01.2n ticked for registration scope allocation. Existing-ID rejection is not replay recovery. Stable operation/actor/request receipts, original-result recovery, concrete platform identity authorization, production timestamp/credential wiring and endpoint activation remain open. No migration parent completed. PostgreSQL/River remain configured; no application database accessed.

Cleanup for DM-01.2n: stopped data-model-mysql-verifier and removed data-model-mysql-test and data-model-registration-pg-test with --volumes after successful verification. No application database accessed.

## 2026-10-27 - DM-01.2n.1 canonical tenant registration requests

Added RegistrationRequestHash with a separate pinned dm-tenant-registration/v1 wire format. Registration has no table publication scope: the fingerprint binds the stable allocated tenant UUID, trusted actor UUID, tenant.create operation kind and original typed name/external-key inputs. Explicit wire fields are independent of service model tags. Nil, empty and whitespace-only keys, case and Unicode normalization differences remain distinct even when tenant.New produces identical normalized metadata. Zero identities and invalid UTF-8 fail without a partial hash or input mutation. Operation keys, generated timestamps and session/authz tokens are excluded; current authorization remains mandatory for initial calls and replay.

Verified golden encoding and SHA-256, every input field, actor/tenant binding, ten original-input distinctions, model-normalization characterization and invalid identity/encoding rejection. Module-wide vet, canonical registration race tests and full selected command/contract/ports/worker/recursive MySQL race regression exited zero without skips. The frozen installation lineage was not changed. Go 1.25.8/MySQL 8.4.8 disposable fixture only. Evidence: evidence/2026-10-27_registration-request-unit.log and evidence/2026-10-27_registration-request-mysql.log.

Only DM-01.2n.1 ticked for fingerprint construction. Durable registration receipts, stable operation-to-tenant identity recovery, original-result replay, append-only receipt schema upgrades, retention/grants and concrete endpoint authorization are still outstanding. The helper is not invoked by runtime registration. PostgreSQL/River remain configured; no application database accessed and no migration parent completed.

Cleanup for DM-01.2n.1: stopped data-model-mysql-verifier and removed data-model-mysql-test with --volumes after successful verification. No application database accessed.

### 2026-10-27 - DM-01.1l.1 archived physical planner snapshots

Audited DM-01.1/DM-01.2 boundaries and corrected the physical planner rejecting
complete snapshots merely because a field was archived. Constructor validation
now retains archived fields with the same tenant, table, component and identifier
checks; typed field selection rejects archived targets before returning SQL.
Active-field planning remains possible without discarding historical mappings.

Verification: thirty archived-target rejection cases, snapshot-copy isolation,
and malformed/foreign archived mapping rejection; real MySQL integration checks
renamed logical/archived visibility, unchanged component mappings and preserved
text/IP values. Module-wide go vet and selected contract, ports, worker and
recursive MySQL adapter tests passed with race detection and no skips. Changed
Go files were formatted before compiling. Evidence:
implementation/evidence/2026-10-27_archived-planner-regression.log

Corrected stale manifest prose that described implemented migration/registry/
repository/delivery foundations as missing. Ticked only DM-01.1l.1. DM-01.1 and
DM-01.2 remain open for full schema/physical invariants, reviewed production
installation/grants, concrete authorized owner publication/replay/archive/display
integration, worker policy/recovery, executable adoption and acceptance tests.
No runtime selection changed: PostgreSQL/River remain configured. No application
database was accessed. Disposable verifier and MySQL Compose fixture were removed
successfully, including volumes. Unrelated workspace changes were preserved.

### 2026-10-27 - DM-01.1k.5 locked account grant installation

Implemented InstallLockedAccountPolicies and explicit migrate-mysql install-grants.
The command verifies the complete schema lineage and dedicated locked definer
under the pinned migration lock before account changes. Validate all policy
inputs and preflight all accounts before the first GRANT. Require locked existing
accounts without sessions; reject missing accounts, mandatory/inherited roles,
unexpected grants, partial-object grant drift and unlocked policies. Keep accounts
locked and never change credentials, revoke rights or activate runtime.

GRANT is nontransactional. Errors identify the account/statement and may leave a
prefix applied; exact complete object grants are accepted on explicit replay.
Nine real-MySQL scenarios passed including an actual lost server GRANT response,
resumption, current sessions after locking, unsafe later accounts causing no
earlier changes and cancellation. CLI installation/replay, grant drift, unlocked
account and schema drift rejection also passed. Module-wide go vet and selected
migrate command/contract/ports/worker/recursive MySQL race regression passed with
no skips. Evidence: implementation/evidence/2026-10-27_grant-install-regression.log.

### 2026-10-27 - DM-01.2o driver-free verified publication owner capability

Added ports.PhysicalPublicationManager and a tenant/table-bound MySQL adapter.
Owners receive MutationStore repositories and atomic delivery submission without
a SQL transaction handle. Require same pool/database, copy reviewed grant policies
including nested column lists, authorize actual caller/request identity before
locks on every invocation/replay, and recheck local permission in the owner
transaction. Retain the existing pinned DDL lock, shape/grant verification,
contract/receipt publication and readiness commit boundary. Legacy DDL through
the metadata store fails explicitly.

Six real-engine scenarios passed: atomic metadata/audit/revision/job/delivery/
publication commit and replay, current permission denial, owner failure after
queue submission, missing queue, foreign scope and legacy DDL rejection. Checked
expired stores, immutable configuration, single-connection composition and denied
replay. Constructor/dependency checks passed. Module-wide go vet and the selected
migrate command/contract/ports/worker/recursive MySQL race suite passed on a fresh
fixture without skips. Evidence:
implementation/evidence/2026-10-27_publication-port-regression.log.

Ticked only DM-01.1k.5 and DM-01.2o. DM-01.1/DM-01.2 remain open: full physical
base-table creation and exact text-key enforcement, remaining DDL operations,
concrete owner integration, reviewed production deployment policies and remaining
acceptance gates are not completed by these adapters. PostgreSQL/River remain
configured. No application database was accessed or changed. Both runs used
isolated fixtures; the verifier and MySQL Compose resources were removed with
volumes. Preserve unrelated workspace changes. Go edits preceded compilation.
The final rg failure-marker search returned 1 because no markers matched; both
test command sessions themselves exited 0. git diff --check found no whitespace
errors (only preexisting checkout line-ending warnings).

### 2026-10-27 - DM-01.1k.6 / DM-01.2j.8 delivery error-count upgrade and retry policy

Inspected pinned River v0.31.0 retry_policy.go and JobRescuer source. Retry uses
prior recorded errors plus one, fourth-power seconds, +/-10% jitter and duration
saturation; rescue records an error too. Added a driver-free local retry policy
and an opt-in MySQL delivery configuration. New enqueue/redrive initializes zero;
claim does not increment errors. Retry completion and rescue increment errors
atomically with state/schedule, preserving existing attempt-budget exhaustion.
Fixed delays cannot be supplied alongside a configured policy. Unknown histories
fail before policy-aware claim or rescue. Legacy fixed-delay configuration and
PostgreSQL/River runtime selection remain unchanged.

Added a source-bound v002 ALTER with nullable error_count and range/attempt CHECK.
NULL preserves unknown histories for existing rows; no inferred backfill. The
reference builder optionally appends this upgrade. Generated a separate 81-step
fraud v002 artifact, leaving the frozen 80-step v001 prefix intact. Prefix identity,
source binding, checksum provenance and full-schema runner apply/verify/replay
passed. Artifact remains needs_review. No production deployment is claimed.

Verification: module-wide go vet and selected reference/migrate command, contract,
ports, worker and recursive MySQL adapter race regression passed with no skips.
Expanded focused coverage then passed eleven MySQL scenarios: claim/error-count
differences from translated fixture history, delays beyond 24 hours, rescue plus
subsequent execution errors, unknown history before claim/rescue, exhaustion,
owner rollback, actual lost COMMIT responses for finish and rescue, successful
acknowledgement and redrive replay with a fresh counter. Unit policy vectors cover
jitter bounds, microsecond precision, saturation and invalid values. Source edits
were completed before compiler/test access. Evidence:
implementation/evidence/2026-10-27_delivery-retry-upgrade-regression.log
implementation/evidence/2026-10-27_delivery-retry-final.log

Ticked DM-01.1k.6 and DM-01.2j.8 only. Parents DM-01.1k, DM-01.2j and DM-01.2k
remain open. Production preflight/policy review, actual River history translation
and writer drain, per-kind rescue/timeout behavior, all-producer policy selection,
column grants, executable wiring, physical DDL fencing/planners and failure/load
acceptance remain required. Mixed legacy/policy writers are not a supported
rollout. No application database was accessed. Disposable verifier and MySQL
Compose resources were removed with volumes. Unrelated changes were preserved.

## 2026-10-27 - Initial physical table creation reference and exact object keys

- Implemented CreateTableReference for the complete mandatory object_id and updated_at snapshot. Generated descriptive registry-backed names, UUID record identity, microsecond validity envelope, exact-key FK/unique constraint and insert/update guards. Returned statements are reference-only; the existing v1 physical executor rejects trigger plans.
- Guards derive identity from complete bytes and fixed tenant/table UUID scope, overwrite forged keys and reject disabled integrity checks. Generated a dedicated locked trigger-definer policy; no dictionary or standalone resolver grants are given to writers.
- Fifteen invalid-input cases and normal/forced-collision MySQL scenarios passed: long, case, Unicode, trailing and NUL values; duplicate updates; key-only forgery; independent table scopes; transaction rollback; concurrent duplicates; restricted writer bypass denial. Reference SHOW CREATE observations and exact locked-account grants were verified on the disposable fixture.
- Verification: gofmt; module-wide go vet ./...; selected command/contract/ports/worker/recursive MySQL go test -race regression, all passing without skips. Evidence: evidence/2026-10-27_physical-creation-focused.log and evidence/2026-10-27_physical-creation-regression.log. git diff --check passed for the touched module/checklist scope.
- Checked only DM-01.2k.8 for reference generation/enforcement tests. Added explicit open DM-01.2k.9 for multi-object durable execution/recovery, complete trigger/resolver/definer readiness and owner publication. DM-01.1, DM-01.2 and their production/physical/delivery parents remain incomplete.
- Runtime remains PostgreSQL/River. No application database or production grants changed. Disposable verifier and MySQL Compose fixture were removed after verification. Preserve shared-database ownership and do not treat direct reference fixture execution as an activated production path.

## 2026-10-27 - Versioned initial table and trigger execution/recovery

- Implemented PhysicalTableCreation.Bind and v2 creation identity with complete reference binding. Decode regenerates the exact typed SQL and checks object/order/identity/definer/mappings; execution separately rejects tenant/table scope mismatch. V1 remains table-only and cannot carry a creation identity.
- Extended the pinned executor to inspect and journal table and trigger objects independently. V2 checks every applied/dirty/unjournaled object before resuming, rejects extra triggers, and checks all three final definitions before entering verifying. Unknown DDL outcomes remain dirty; drift remains blocked; the registry never opens through execution.
- Native readiness explicitly rejects v2 before its publication callback. Shared resolver/dictionary lineage, locked definer readiness, reviewed restricted privileges for definer installation and concrete owner publication remain open. Administrator-backed disposable DDL tests are not production restricted-account acceptance.
- Fifteen invalid-plan cases and thirteen real-MySQL recovery scenarios passed: fresh/replay, dirty-before/after each trigger, unrecorded trigger, earlier table/trigger drift, unexpected trigger, actual lost insert/update-trigger acknowledgements, applied trigger drift and tenant isolation. Reconciliation and closed/unpublished registry assertions passed. Corrected fixture seeding to include mandatory applied_at timestamps after the initial run exposed the journal CHECK constraint.
- Verification passed: gofmt, module-wide go vet ./..., selected build-reference/migrate/reconcile commands, contract, ports, worker and recursive MySQL adapter tests with race detection and no skips. Evidence: evidence/2026-10-27_physical-creation-recovery-focused.log and evidence/2026-10-27_physical-creation-recovery-regression.log. Scoped git diff --check passed.
- Checked DM-01.2k.9a only for binding and durable execution/recovery. DM-01.2k.9 remains IN_PROGRESS; DM-01.1, DM-01.2 and their production/physical/delivery parents remain incomplete. Runtime remains PostgreSQL/River. No application database changed; disposable verifier and MySQL Compose fixture were removed after checks.

## 2026-10-27 - DM-01.2k.9 shared exact-key dependency inspection prerequisite

- Added installation.NewExactKeyReadiness with source binding for the complete frozen v001 lineage and optional shipped v002 upgrade. Partial/unknown/substituted lineages fail construction; owned copies prevent caller mutation. No reference definition is learned from the application target.
- Added migrations.Runner.VerifyOnConn and reused it from existing locked Verify. Shared dependency inspection uses the caller-owned pinned connection, verifies complete applied checksummed history and all final installed shapes, then exact locked resolver-definer grants. It rejects unexpected dictionary triggers, mandatory roles, missing/excess grants and existing authenticated definer sessions. Distinct record/resolver principals account for case-insensitive host identity. No DDL, repairs, grant changes or registry publication occur.
- Verified thirteen negative scenarios on each of v001 and v002 (26 subtests): wrong database, shared definer, dirty history, checksum drift, missing ledger, dictionary shape drift, extra dictionary trigger, digest/resolver drift, missing/excess grants, unlocked account and a still-connected locked account. Also checked repeated verification, cancellation, immutable configuration and named-lock ownership with a one-slot pool. Eight invalid artifact/dependency cases and case-only host alias checks passed.
- The first focused run exposed fixture cleanup that did not restore a routine's original SHOW CREATE definition after ALTER COMMENT. Corrected disposable cleanup by recreating the original fixture SQL and restoring its EXECUTE grant. Verification continued to reject drift; no verifier exception was added. The initial failed evidence is retained.
- Verification: changed Go files formatted before compilation; module-wide go vet ./... and selected build-reference/migrate/reconcile commands, contract, ports, worker and recursive MySQL race regression passed without skips. After final host-identity hardening, module-wide vet and affected installation/migrations race packages passed again. Evidence: evidence/2026-10-27_creation-dependencies-initial.log, evidence/2026-10-27_creation-dependencies-focused.log, evidence/2026-10-27_creation-dependencies-session.log, evidence/2026-10-27_creation-dependencies-regression.log and evidence/2026-10-27_creation-dependencies-final.log.
- DM-01.2k.9 remains IN_PROGRESS; no checklist items were ticked. This is a verified shared dependency primitive, not integration into the actual v2 publication path. Complete record-trigger/definer verification, reviewed restricted DDL installation, authorized atomic owner publication, failure/replay/rollback acceptance and production policy review remain outstanding. The explicit plan.Version != 1 publication rejection is unchanged.
- Documented MySQL 8.4 TRIGGER/SET_ANY_DEFINER and binary-logging installation requirements from primary documentation. Generic object-only AccountPolicy was not broadened. No global settings, application database, production grants or runtime selection changed. Runtime remains PostgreSQL/River. No performance improvement or production readiness is claimed.
- All edits stayed within the data-model module and its migration documentation/evidence. Unrelated changes were preserved. The disposable verifier was stopped and the data-model-mysql-test Compose container/network/volumes removed after verification. No tests remain running.

### 2026-10-27 - DM-01.2k.9 configured creation readiness and owner finalization

- Scope: data-model service and its disposable MySQL fixture only. Preserved unrelated dirty/untracked work; no subagents. Host Go still unavailable; used Docker Go 1.25.8 / MySQL 8.4.8. PostgreSQL/River runtime remains configured. No application database, production grants or production server settings changed.
- Added RecordInstallerPolicy with exact temporary target CREATE/SELECT/TRIGGER, dictionary REFERENCES and narrowly scoped registry/intent/journal grants plus separate global SET_ANY_DEFINER. Generic AccountPolicy remains object-only. Active verification requires reviewed ROW/trusted-creators settings when binary logging is enabled. Helpers never provision/unlock/repair accounts or change server settings. Publication requires installer locked, disconnected and global definer capability revoked. Production isolation, privilege administration exclusion and replication/restore review remain required.
- Initial restricted test failed with MySQL 1142 using column REFERENCES(id); corrected to table REFERENCES and retained initial failure evidence. Disposable Compose alone now explicitly sets trusted creators and ROW logging. Actual restricted execution/replay and denial/recovery for missing REFERENCES, SET_ANY_DEFINER and TRIGGER pass; record/dictionary/routine bypasses are denied.
- WithCreationReadiness explicitly opts an executor/publication manager into v2 readiness. Complete applied table/trigger progress, exact trigger set, source-bound shared installation lineage, locked shared and record-definer grants/sessions, inactive installer and exact separate consumer policies are inspected on the pinned execution connection. Unconfigured v2 publication still returns unsupported. Operational exclusion of shared-schema/privilege changes is required; the table named lock cannot fence administrators.
- Owner effects are followed by bounded initial metadata/mapping postcondition verification inside the same atomic publication transaction. Added TableService.PublishPreparedCreation for an already prepared/executed command: canonical original API identity, active tenant, original table attributes and mandatory fields, atomic create audit and tenant revision. Authorization runs on every invocation/replay. This returns the original ingestion publication, not a fabricated original Table result. Full allocation/preparation and original-result receipt orchestration remain unfinished.
- Verification: gofmt changed sources; module-wide go vet ./...; selected race regression across build-mysql-reference, migrate-mysql, reconcile-physical-mysql, contract, ports, worker and recursive store/mysql packages passed without skips/failures. Final focused suite passed 30 creation scenarios, including concrete service commit/replay/revocation, audit/revision rollback, metadata mismatch, trigger/journal/resolver/grant drift, installer sessions/capability, grant-denial recovery and actual lost publication COMMIT.
- Added actual lost INSERT-trigger and UPDATE-trigger acknowledgements using restricted installer credentials, waited for observed old-session lock release, recovered and published. Published restricted writer accepted long/NUL-containing object IDs, overwrote a forged derived key and rejected exact duplicate with 1062. Re-ran module-wide vet and all TestPhysical metadata tests after extending the fault helper: passed without skips/failures. Scoped tracked and new-file whitespace checks passed (no-index exit 1 for added files is expected).
- Evidence: evidence/2026-10-27_creation-publication-focused.log (initial REFERENCES failure); -focused-second.log; -expanded.log; -owner.log; -regression.log; -final.log; -recovery-regression.log (all with the same 2026-10-27_creation-publication prefix). Final metadata physical regression: 28.301 seconds; final focused creation suite: 30 scenarios, 17.22 seconds.
- Stopped verifier and removed the disposable Compose project/volumes after all tests completed. No test processes remain running.
- DM-01.2k.9 and all existing incomplete parents remain IN_PROGRESS/unchecked. Remaining: full creation preparation/stable identity/original Table-result recovery; reviewed production installer/definer/replication deployment; other lifecycle planners and owner/display paths; delivery translation and executable/consumer integration; operational, failover and representative load acceptance. Disposable acceptance is not migration completion.

### 2026-10-27 - DM-01.2k.9 durable owner allocation and original-result recovery checkpoint

- Continued within data-model only, preserving unrelated changes. PostgreSQL/River remain configured. No application DB, production account or production server setting changed. Disposable verifier/MySQL remain running for continued module work; no compiler/test remains running at this checkpoint.
- Added DDLIntentStore.PrepareTx so owner metadata/mappings and intent/barrier work can share a transaction. Five real-engine cases verify rollback, replay, tenant isolation, invalid input and expired transaction rejection with one connection.
- Added immutable original creation receipts as append-only v003: four steps after the frozen 81-step v002 prefix, separate locked SELECT/TRIGGER definer, bounded original JSON, tenant/operation and tenant/table keys, immutable update and denied delete. No prior source/checksum changed. Fixture builder emits new 85-step artifact with SHA256 417aa18a279265f40189b15c2b9127e55fbc6ced98d07de8f4109a0259c9a419 and needs_review provenance. Extra receipt-definer provisioning/grant files are included. Production deployment remains unapproved.
- Added source-bound v003 shared dependency constructor; requires complete lineage, exact receipt-definer grants/lock/no sessions and exactly the three expected receipt triggers. Older constructors reject unconfigured v003. Ordinary receipt writer needs SELECT/INSERT only; actual restricted reserve/replay passed and UPDATE/DELETE were denied. Existing v002 tenant rows survived partial v003 installation and guard-resumption/replay.
- Added dm-table-creation/v1 canonical pre-allocation hash, distinct from allocated-table publication hash. It binds original actor/tenant/name/description/alias/semantic type without generated IDs/time; preserves normalization distinctions and rejects invalid UTF8/oversize input. Golden and every-field tests passed.
- TableService.PrepareCreation now allocates original table/two fields once through authorized TableCreationManager, retaining the snapshot/hashes atomically with metadata and closed physical mappings. Matching retries skip allocation and effects. BindPlan checks retained scope/field identities and registered names against complete typed v2 source, then prepares the intent/barrier on the owner transaction. Reference capture stays outside row locks and away from application-target drift adoption.
- RecoverCreation returns the retained original Table only after matching published intent and publication receipt. End-to-end owner tests execute restricted DDL, publish audit/revision/contract under the execution lock, recover after actual lost publication COMMIT and preserve original result after a later alias edit. Eight concurrent initial allocations converge; actual lost allocation COMMIT recovers the same IDs without regenerating them. Authorization/revocation, changed request, local denial and owner rollback pass.
- Initial end-to-end recovery failed because the new reader assumed NULL for publication receipt expected_version. Existing publication receipts use zero UUID for absent conditions; DDL intents use NULL. Corrected the query to honor both established representations; retained failure evidence and subsequent passing runs.
- Verification: gofmt; module-wide vet; full selected race suite (reference/migration/reconcile commands, contract, ports, worker and recursive mysql packages) passed without skips/failures. Metadata package 94.928s. Final focused receipt acceptance (including 12 malformed snapshots and extra trigger rejection) passed 10.453s; owner allocated normal/lost-publication-COMMIT acceptance passed 7.839s; canonical service test passed. Frozen v003 test proves exact v002 prefix/provenance/source binding and rejects altered upgrade steps.
- Evidence: 2026-10-27_creation-allocation-full.log, creation-allocation-regression, creation-command-unit, creation-owner-acceptance, creation-owner-allocation-focused (initial failure), creation-owner-allocation-second, creation-preparation-focused, creation-receipt-artifact, creation-receipts-acceptance, creation-receipts-final and creation-receipts-focused (same date prefix and .log suffix), under implementation/evidence.
- Updated workflow/install/publication/request docs and DM-01.2k.9 status. No parent completion boxes ticked. Still outstanding: operational reference/installer lifecycle, production principal/replication review, HTTP/worker/consumer integration, remaining physical lifecycle/display work, retention/retirement, failover and load acceptance. Internal allocation is never an API create success or approval of new pending-response semantics.

### 2026-10-27 - guarded native record-table changes checkpoint

- Added v3 source-bound native changes over existing v2/v3 record tables. Complete retained guard identity/definitions, regenerated typed statements and empty-table preconditions are mandatory. V1/v2 reject the new fields; mandatory record columns/envelope/key constraints cannot be changed. No runtime activation or application DB/grant changes.
- Execution checks both guards and the exact trigger set before ALTER/recovery and before verification. Publication checks shared lineage, locked definers, inactive installer and complete desired field/mapping snapshot under the execution lock through atomic owner effects/contract/receipt/gate commit.
- Initial restricted test failed with MySQL 1227 because the long-lived native DDL role cannot inspect triggers. Added a separate temporary NativeChanges installer profile with exact target SELECT/CREATE/ALTER/INDEX/TRIGGER, narrow journal rights and dictionary REFERENCES, without SET_ANY_DEFINER. Actual restricted two-step column/index execution and replay pass. Existing runtime roles remain unchanged.
- Twelve real-engine allocated creation/native scenarios pass, including missing ALTER recovery, retained guard drift before/after DDL, active installer, owner rollback, lost ALTER/publication COMMIT acknowledgements, altered metadata nullability, extra field and missing mapping. Rollback preserves a closed gate. Exact object ID duplicate enforcement survives the native change. Unit codec tests cover all six supported native operation kinds and reject substituted source/preconditions/mandatory fields.
- Verification: gofmt changed files before compilation; module-wide go vet; full selected command/contract/ports/worker/recursive mysql race regression passed without skips/failures. Metadata package 104.159s. Final focused two-step acceptance passed 25.014s. Evidence: evidence/2026-10-27_guarded-native-*.log, including the initial grant failure and subsequent passing runs; full suite is guarded-native-full.log.
- No parent checklist completed. Concrete field/index owner integration, enum/unique-text/type-change/backfill/retirement, production reviewed deployment, runtime/consumer integration and operational/load acceptance remain unfinished. Disposable verifier/MySQL remain available for continued work; all tests completed at this checkpoint.

### 2026-10-27 - concrete native field creation publication checkpoint

- Added FieldService.PublishPreparedCreation for previously prepared/executed native field creation. It requires exact prior publication, active tenant/table, original field attributes and a trusted API actor. It writes create-field audit and tenant revision through the table-bound manager in the same verified publication transaction. Enum/unique-text creation remains rejected; this does not activate runtime or return an original Field result.
- Added dm-prepared-field-creation/v1 canonical fingerprint binding the retained allocated field UUID plus the existing complete canonical owner command. Changed field identity, actor, version or original attributes cannot replay the receipt. Durable pre-allocation identity/original Field-result receipts remain unfinished.
- Six concrete owner acceptance cases extend the allocated creation suite to eighteen scenarios: commit/replay/revocation, audit failure, revision failure, prepared attribute drift, inactive tenant and actual lost publication COMMIT. Ambiguous recovery waits for observed old-session named-lock release and does not repeat audit/revision effects. Publication failures leave readiness closed and roll back owner effects.
- Initial acceptance failed only in the new assertion query, which named data_model_schema_changes instead of the installed data_model_schema_change_log. Corrected the test; preserved the failed log and subsequent passing evidence.
- Verification: gofmt; module-wide go vet; focused canonical request race test; eighteen real-engine allocated scenarios; final full selected race suite across commands, contract, ports, workers and recursive MySQL packages. No final failures or skips. Final metadata package 92.270s. Scoped source/documentation whitespace checks passed. An overly broad tracked check also reported existing CRLF whitespace in unrelated dirty files; those files were preserved, and checks were narrowed with CR-at-EOL handling.
- Evidence: evidence/2026-10-27_field-owner-focused.log (initial assertion failure), field-owner-hash.log, field-owner-acceptance.log, field-owner-final.log and field-owner-full.log (same date prefix). Updated execution/planning/install/request docs, module checklist and roadmap. No incomplete parent checklist was ticked.
- PostgreSQL/River remain configured; no application database, production principal or production grant/server setting changed. Full module remains unfinished: durable field allocation/result recovery, remaining field/index/display owner paths, enum/text-key/type-change/backfill/retirement protocols, delivery translation, executable/consumer integration, reviewed deployment and operational/load acceptance. Disposable fixtures are not evidence of completed migration.
- Cleanup completed after final verification: stopped data-model-mysql-verifier and removed the disposable Compose project/volumes. No compiler or tests remain running from this work.

### 2026-10-28 - DM-01.3d / DM-01.3c / DM-01.3b.2b.2 concrete display owner publication

- Read AGENTS.md and current display/publication contracts; preserved preexisting dirty changes. Runtime remains PostgreSQL/River. No application database, production account/grant or server setting changed.
- Added display.PublicationManager and same-pool/tenant-bound MySQL adapter. Mandatory trusted-identity authorization precedes locks on every call/replay; local authorization runs inside the actual display transaction. Owner effects use transaction-bound repositories and queue, without nested transactions, DDL or network calls.
- Added TableService.PublishDisplaySettings: validates canonical original request, active tenant/table and persisted validated caption; joins managed caption search-index job/delivery and tenant revision with settings, dependency maintenance, invalidation, display audit and immutable publication receipt. Replay returns the original ingestion publication, never current/saved settings, and does not repeat effects. Existing HTTP/runtime behavior is unchanged; actual MySQL search-index execution remains a separate unfinished worker capability.
- Added exact staged DisplayOwnerPolicy: object/column grants for display mutation, producer work and receipt insertion, excluding physical DDL, dictionary access, physical gate/contract mutation and receipt update/delete. Initial real restricted-login test failed with MySQL 1142 for a reservation locking SELECT. Added UPDATE only on immutable reservation tenant_id, with installed identity guards mandatory; no delivery lifecycle UPDATE or reservation DELETE. Restricted execution now passes; an actual tenant-identity substitution fails with 1644. Production complete-account review/deployment remains open.
- Seven concrete owner scenarios pass in the display suite: normal/replay, eight concurrent duplicates, missing queue, revision failure, local authorization denial, lost COMMIT and closed gate. Replays reject changed input/tenant/revoked identity; successive saves reuse the caption job/delivery, clearing creates no job, and historical replay does not restore old settings.
- Four further journeys use complete v003 installation and actual restricted table/trigger creation plus verified TableService publication before concrete display save: commit, lost commit, missing queue and restricted display principal. These do not seed an open physical gate. Restricted grant-denial acceptance revokes revision INSERT and proves settings rollback; dictionary/gate/receipt-delete/field-mutation/DDL bypasses are denied. These isolated journeys are not production/runtime acceptance.
- Verification: gofmt changed files; module-wide go vet ./...; full selected race suite across build-mysql-reference/migrate-mysql/reconcile-physical-mysql, contract, ports, worker and recursive MySQL packages passed without skips/failures. Metadata package 92.868s. Constructor tests reject missing authorization, foreign tenant/pool/schema and missing effects. Scoped source/documentation whitespace checks passed.
- Evidence: evidence/2026-10-28_display-owner-focused.log, display-owner-physical.log, display-owner-restricted.log (initial locking-grant failure), display-owner-restricted-second.log and display-owner-regression.log (all same date prefix). Updated display contract, module checklist and adapter/install README. All three requested parent items remain unchecked.
- Outstanding: remaining field/link/type/enum/alias owner paths and table lifecycle, dependency backfill and reviewed production grants, authorized physical record readers, original response/result recovery and retention/retirement, complete worker/consumer/API/import/export integration and operational/load acceptance. No claim of module/migration completion.
- Cleanup completed: verifier stopped and disposable Compose database/project/volumes removed after successful verification. No tests/compiler remain running from this work.

### 2026-10-28 - DM-01.3b owner coverage audit and table presentation recovery

- Read AGENTS.md and reconstructed the publication checklist, owner request contracts, receipt persistence and existing owner wiring. Preserved unrelated changes and PostgreSQL/River configuration. No application database or production grants changed.
- Added ports.MetadataPublicationManager and same-pool tenant-bound MySQL implementation for presentation-only operations on an existing exact publication. Current authorization runs before database locks on every invocation/replay; new mutations reauthorize locally, lock the matching open physical gate after the model-table lock, and commit owner effects with the publication receipt. Semantic token changes roll back all effects/contract/receipt writes. Historical replay skips effects and does not reopen a later barrier.
- Added concrete TableService.PublishPresentationUpdate for description/alias/semantic-type updates, canonical original input, active tenant/table validation and atomic audit/revision. Existing caption managed-index behavior is preserved. Caption input is rejected in favor of display coordination. The manager separately rejects changed caption or archived table postconditions, because unchanged ingestion JSON alone cannot authorize these lifecycle mutations. The return value is an original ingestion publication, not an original Table API response.
- Eight real-engine scenarios run after complete v003 installation and actual restricted physical creation/verified TableService publication: commit, eight concurrent duplicate requests, actual lost COMMIT acknowledgement, revision rollback, closed gate, semantic mutation rejection, caption bypass rejection and archive bypass rejection. Replayed requests require current authorization, reject changed input, preserve later presentation edits with the same semantic token and leave newer closed barriers intact. Boundary tests reject foreign pool/database/tenant, missing actor/table/op/hash/effects and non-exact conditions before database work.
- The first full regression failed an existing delivery-inspection EXPLAIN assertion: a fixture with 1003 rows for one tenant and one for another yielded nearly tied primary/covering-index costs. Refreshing statistics alone reproduced the failure. Changed only the fixture to populate both tenants equally and explicitly refresh statistics before its plan assertion; application SQL is unchanged. Three consecutive real-engine runs passed with covering range plans. This is a representative-fixture correction, not a production performance claim. Preserved failed regression/statistics-only evidence.
- Verification: gofmt; module-wide go vet ./...; clean-fixture full selected race regression across reference/migration/reconcile commands, contract, ports, worker and recursive MySQL packages passed with no skips/failures. Final metadata package 102.174s. Scoped source/documentation whitespace checks passed.
- Evidence under implementation/evidence: 2026-10-28_presentation-owner-focused.log, presentation-owner-guards.log, presentation-owner-regression.log (initial inspection-plan failure), presentation-inspection-statistics.log (stats alone insufficient), presentation-inspection-balanced.log (three passes), and presentation-owner-final.log (full pass), all with the same date prefix.
- Updated DM-01.3b status with an explicit owner completion matrix, request contract and adapter README. DM-01.3b remains unchecked: native field allocation/results, field/enum/link/navigation/pivot/index owners, deletion/archive/import formats and lifecycle, complete original API-result recovery, retention/retirement and production entry-point integration are still unfinished. This audit does not narrow the parent or count staged adapters as complete service behavior.
- Cleanup completed after final verification: disposable verifier stopped and Compose database/project/volumes removed. No compiler/tests remain running from this work.

### 2026-10-28 - Field presentation owner publication
- Added FieldService.PublishPresentationUpdate using the canonical field.update request and existing exact-version metadata publication manager.
- Validates active tenant/table/field ownership in the transaction; preserves alias trimming and atomically writes owner audit/revision with the publication receipt. Rejects any enforcement flag and mismatched canonical input before effects.
- Reused complete-v003 verified physical creation journeys for field commit, eight concurrent duplicate callers, lost COMMIT acknowledgement, revision rollback and closed readiness. Historical authorized replay preserves later edits and closed barriers; revoked replay and changed requests fail.
- Seven additional owner scope tests reject inactive tenants, foreign/archived tables, foreign tenant/table fields, archived and missing fields before writes.
- Verification: gofmt; scope race tests; focused real-engine race tests; go vet ./...; full selected command/contract/ports/worker/recursive MySQL race suite. All passed without skips/failures; metadata regression 99.155s. Scoped whitespace checks passed.
- Evidence: 2026-10-28_field-presentation-focused.log, 2026-10-28_field-presentation-scope.log, 2026-10-28_field-presentation-regression.log under implementation/evidence.
- Updated coverage matrix and request contract. No incomplete parent was ticked: original Field results, restricted field policy, other owner/enforcement/lifecycle operations, runtime and operational acceptance remain outstanding.
- Runtime remains PostgreSQL/River. No application database or production grants changed. Unrelated workspace changes preserved.
- Cleanup completed: disposable verifier stopped and compose MySQL/network removed with down --volumes. No test/compiler sessions remain running.

### 2026-10-28 - DM-01.1 metadata manifest and structural capacity verification
- Added testdata/metadata-column-manifest.json: 101 source columns / 12 tables, explicit source declaration and target conversion, 24 named index/function/trigger definitions including replacements, and 50 LF-normalized source SHA-256 bindings covering migrations, structural/guard definitions and append-only v002/v003 builders.
- Added tests rejecting missing/duplicate mappings, source or target drift, newly unbound SQL, unsupported conversions and missing replacement references. Named function/trigger entries explicitly retain incomplete owner integration status.
- Installed-schema verification uses one bounded information-schema query for all mapped types/nullability. A separate bounded index inventory rejects prefix keys, unknown encodings, oversized keys or too many parts; accounts conservatively for encoding, nullable/variable-width overhead and primary suffixes.
- Verified 88 indexes / 205 components; maximum conservative key bytes 221 and key-plus-primary bytes 237, excluding record/page overhead. This is structural evidence, not performance/load acceptance.
- Passed gofmt, focused and expanded metadata race tests, go vet ./..., full selected command/contract/ports/worker/recursive MySQL race suite (metadata 107.358s), with no skips/failures. Scoped whitespace checks passed.
- Evidence: implementation/evidence/2026-10-28_column-manifest-focused.log, -expanded.log and -regression.log.
- DM-01.1 remains unchecked. Manifest is needs_review; dynamic field exact-key/enum enforcement, source-data preflight/import, production review/policies and owner/API acceptance remain outstanding. No runtime activation, application DB or production grant changes; unrelated edits preserved.
- Cleanup completed: verifier stopped and disposable MySQL/network removed with down --volumes. No tests or compilers remain running.

### 2026-10-28 - Prepared native field nullability owner finalization
- Added FieldService.PublishPreparedNullabilityUpdate using canonical field.update, exact prior publication and trusted API actor. Requires completed typed physical work; verifies active scope and reserved/enum restrictions, then updates field/audit/revision atomically with verified physical publication.
- Added six complete-v003 owner journeys following actual verified table and native-field creation: tighten, populated relaxation, owner rollback, drift, lost publication COMMIT and populated-tightening rejection. Restricted native installer executes the second v3 operation, then disconnects and locks before publication. Existing record preservation and actual SQL NULL enforcement are checked.
- Lost-commit recovery waits for actual old-session execution-lock release. Authorized replay deduplicates effects; revoked replay and changed canonical commands fail. Failed publication leaves readiness closed and rolls back owner changes. Populated tightening creates no journal entry and preserves records/metadata.
- Twelve owner scope/input rejection cases passed. Initial focused run failed because the new fixture login omitted required connection lifetimes; corrected fixture configuration and preserved failure evidence.
- Passed gofmt, focused/expanded real-engine race tests, owner-boundary race tests, go vet ./..., and full selected command/contract/ports/worker/recursive MySQL race suite. No final skips/failures; metadata regression 104.814s. Scoped whitespace checks passed.
- Evidence: implementation/evidence/2026-10-28_nullability-owner-fixture-failure.log, -focused.log, -expanded.log, -scope.log and -regression.log.
- Checked only the explicitly scoped new DM-01.2k.10 finalization subtask. Parent tasks remain unchecked: preparation/API integration, original Field receipts, populated tightening/backfill, enum/unique-text enforcement and production/operational acceptance remain open.
- Runtime remains PostgreSQL/River. No application database or production grants changed; unrelated workspace changes preserved.
- Cleanup completed: disposable verifier stopped and compose MySQL/network removed with down --volumes. No tests/compiler sessions remain running.

### 2026-10-28 - Prepared native numeric uniqueness owner finalization
- Added FieldService.PublishPreparedUniqueEnable with canonical field.update, exact version and trusted actor. Integer/float enabling only; rejects enum fields, unsupported types, uniqueness removal and mixed nullable/enum flags. Shared native-owner validation preserves nullability behavior.
- Complete-v003 fixtures create and publish a native field, then use the restricted installer for a second v3 operation. Integer and float publication, owner rollback, drift, lost COMMIT, duplicate data rejection and explicit fixture correction/retry passed. No automatic application data repair is introduced.
- Actual SQL confirms multiple NULLs remain allowed and duplicate INSERT/UPDATE fail; failed updates retain both NULL rows. Failed installation leaves metadata unmodified and readiness closed. Lost publication recovery does not repeat audit/revision; revoked replay is denied.
- Seventeen unique-owner boundary cases and twelve existing nullability boundary cases passed with race detection. Gofmt, module-wide go vet, full selected command/contract/ports/worker/recursive MySQL race suite and scoped whitespace checks passed. Final regression has no skips/failures; metadata 133.504s.
- Evidence: implementation/evidence/2026-10-28_unique-owner-scope.log, -focused.log, -nullability-scope.log and -regression.log.
- Checked only the scoped new DM-01.2k.11 finalization subtask. Updated request contract and owner coverage matrix. Parents remain incomplete: preparation/API wiring, original Field receipts, relationship-aware removal, text/enum protocols, production policies and operational acceptance remain open.
- Runtime remains PostgreSQL/River; no application database or production grant changes. Preserved unrelated workspace work.
- Cleanup completed: verifier stopped and disposable compose MySQL/network removed with down --volumes. No test/compiler sessions remain running.

2026-10-28 - DM-01.3d.3 original display settings results (staged)
- Added versioned immutable owner results and TableService.PublishDisplaySettingsResult.
  Settings/dependencies/caption jobs/delivery/audit/revision/publication/result commit
  atomically. Every replay reauthorizes; legacy missing results fail closed.
- Append-only v004 has 89 source-bound steps, preserving the frozen v003 prefix.
  Dedicated locked result definer, exact guards/grants/session checks, retained
  tenant/table/publication FK and bounded JSON. Source manifest now binds 51 files.
- Reference artifact SHA256:
  2d47513c9b9dc9ca472ec8ad405be5afe2b41656da0e33ed080d6685cbd554a5
  Generated only in disposable MySQL 8.4.8; status remains needs_review.
- Ten focused scenarios passed after actual verified physical creation and full
  v004 installation: owner commit/concurrency/lost commit/restricted grant denial/
  revision rollback, adapter historical replay/concurrency/rollback/lost commit/
  legacy rejection. Extra checks cover bounds, tenant FK, immutable/integrity
  guards, partial migration/replay, source substitutions and grant/trigger drift.
- Initial focused test assumed display version increments exactly once; caption
  invalidation legitimately advances it further. Corrected assertion to compare
  the complete committed Settings. Preserved initial failure evidence.
- gofmt, module-wide go vet and selected command/contract/display/ports/service/
  worker/recursive MySQL race regression passed. Metadata package 145.569s.
  No MySQL skips/failures. Thirteen PostgreSQL-dependent legacy display/service
  tests skipped because this disposable fixture supplies MySQL only.
- Evidence: 2026-10-28_display-result-{focused,lineage,owner,expanded,builder,
  artifact,regression}.log. Only DM-01.3d.3 checked. All parents remain incomplete:
  remaining owner results/mutations, lifecycle/retention/retirement, API/runtime,
  production review/grants/backfill and operational/load acceptance outstanding.
- PostgreSQL/River remain configured. No application database/grants changed.
- Cleanup completed: stopped data-model-mysql-verifier and removed disposable
  data-model-mysql-test Compose resources/volumes. No tests remain running.
  Scoped no-index whitespace checks passed for all changed Go/manifest files.

2026-10-28 - Existing DM-01.3b/3b.2/3b.2b.2/3c presentation owner work
- Added typed original Table/Field response recovery to concrete presentation
  owners. Existing canonical validation/effects are reused. Owner metadata,
  audit/revision/publication and v004 result receipt commit together. Ambiguous
  commit returns no successful resource; same-command replay reauthorizes and
  returns the immutable original response. Legacy missing results fail closed.
- Added narrow presentation UPDATE repositories and PresentationOwnerPolicy.
  Description/alias/updated_at and table semantic_type are allowed; identity,
  enforcement, caption, archive, DDL, gate/contract and dictionary mutation are
  excluded. Verified restricted principal and rollback on revoked result INSERT.
- Extracted common bounded owner-result persistence for display and metadata.
  Frozen migration/artifact history is unchanged. Pinned Table/Field v1 wire
  contracts and scope/kind validation prevent silent response substitutions.
- Twelve real-engine table/field scenarios passed on full v004 after verified
  physical creation. Both original-result concurrent retries also passed with
  eight DB connections and an authorization start barrier, not pool serialization.
- gofmt, module-wide vet and selected command/contract/ports/worker/recursive
  MySQL race regression passed without skips/failures. Metadata 152.195s.
  Evidence: 2026-10-28_presentation-result-{table,field,regression,concurrent}.log.
- Updated existing unchecked requirements and owner coverage matrix. No new
  checklist IDs and no parent completion ticks. Remaining owner/lifecycle/
  retention/retirement, API/runtime and production acceptance remain outstanding.
- PostgreSQL/River remain configured; no application database or grants changed.
- Cleanup completed: verifier stopped and disposable Compose containers/network/
  volumes removed. No tests remain running. Scoped Go whitespace checks passed.

2026-10-28 - Existing DM-01.3b/c physical Field-result requirements
- Added typed original Field results for prepared native creation, nullability
  and numeric uniqueness owners. Reuses canonical owner validation/effects and
  v004 immutable Field format. No frozen schema/artifact changes.
- Internal result hook runs after publication receipt insertion and before gate
  opening on the same transaction/pinned execution connection. Receipt failure
  rolls back owner/publication/readiness while retaining applied DDL evidence.
- Historical replay reauthorizes, checks durable physical command identity and
  reads the original Field under the execution lock. Missing legacy receipts,
  scope/kind changes and bounds fail closed; commit ambiguity returns no Field.
- Twelve focused real-engine cases and recovery rerun passed. Receipt-only
  retries successfully publish without reexecuting DDL. Original responses
  survive later aliases and newer closed gates without repeated effects.
- Eight-connection test initially assumed all concurrent calls succeed. Existing
  GET_LOCK(name,0) deliberately returns ErrDDLLockBusy. Corrected the test to
  require explicit failure/no success response for busy calls and same-operation
  recovery after contention. Focused corrected race test passed; failure log kept.
- gofmt, module-wide go vet and selected command/contract/ports/worker/recursive
  MySQL race regression passed without skips/failures. Metadata 170.218s.
  Final concurrency-only test changes verified separately with race detection.
- Evidence: 2026-10-28_physical-field-result-{focused,recovery,regression,
  concurrent,contention}.log. Parent requirements and matrix updated; no new IDs
  or completion ticks. Remaining implementation and acceptance are still open.
- Found concrete runtime dependency: internal/access/platform.go invokes auth-
  owned PostgreSQL identity.authorize_data_model_v1. AU-01.2/3 remain NOT_STARTED.
  Asked whether scope may extend to that auth dependency; no auth edits made.
- No application database or production grants changed. PostgreSQL/River remain
  configured. Scope decision does not authorize production activation.
- Cleanup completed: verifier stopped, disposable Compose containers/network/
  volumes removed, no tests running. Scoped changed-Go whitespace checks passed.

2026-10-28 - Data-model implementation-plan audit (documentation only).
Compared the checklist/coverage map with API routes, access/platform.go and repository/display/delivery/validation contracts. Added explicit owner-audit coverage for tenant registration/provisioning, options replacement, enum/pivot/navigation deletion and index-job retry/admin reconcile. Recorded HTTP request-key/result/error acceptance, auth-owned AU-01.2/3 dependency, physical readers/consumers, isolated API/frontend stack requirements and operational/data acceptance under existing task IDs. Corrected stale current coverage-map wording for registration allocation and supported physical execution/publication. Identified default Compose 120s test timeout versus recorded 170.218s metadata regression; no new test failure is claimed. No code/runtime/grants/database changes, test runs or completion checkbox changes. No new checklist IDs.
