# Isolated write/work process acceptance batch

Selected existing leaf tasks (20): IN-01.2.023, .027, .028, .029, .030,
.031, .033, .034, .036, .038, .039; IN-01.4.003, .011, .012, .014,
.015, .016, .017, .020, .021.

Status: Complete for these 20 existing scoped leaves, 2026-10-30. The main
tracker is ticked and acceptance evidence is retained.

The explicit MySQL composition exposes the existing write, CSV, upload-list,
upload-detail and deferred-detail transports in an isolated generated database.
It advertises only write/work capabilities. Record/aggregate/operator endpoints
and production cutover are excluded; those existing tasks remain open. Default
PostgreSQL/River entry points remain unchanged. Dedicated API and worker commands
load one explicit MySQL authority, validate lineage, required table definitions,
lookup capabilities, current publication and restricted grants before readiness.
No service creates a database or executes a migration at startup.

API acceptance must use real configured processes and the complete owner-authored
shared-schema fixture. Worker acceptance must start the actual configured worker,
use scoped claims/reaping and reconcile durable effects. Component tests alone
cannot close these selected process-level cases. Independent schema/grants review,
Q-01 acceptance, read parity and legacy source inventory are separate open gates.

## Implementation and acceptance mapping

Paths below are relative to `backend/ingestion-service`. Process tests live in
`internal/store/mysql/process_*_test.go`; each starts built API/worker binaries,
not an in-process replacement for HTTP handlers. Fixtures install 91 data-model
steps and 23 ingestion steps on MySQL 8.4.8 in generated disposable databases.

| Existing task | Implemented boundary | Acceptance tests |
|---|---|---|
| IN-01.2.023 | `internal/mysqlruntime/readiness.go`: lineage, table definitions, lookup execution, publication and direct grants | StartupChecks, APIRuntimeGrants |
| IN-01.2.027 | `Service.Ingest`, existing POST handler, trusted tenant/table resolution | WriteBaseline, IdenticalReceipts |
| IN-01.2.028 | Existing PATCH handler and owner supplied-field writes | WriteBaseline, OrderedPatches |
| IN-01.2.029 | `Service.BatchIngest`, ordered atomic batch POST, 500-record canonical bound | WriteBaseline, AlternateUniqueRace, LimitsAndAuthentication |
| IN-01.2.030 | Existing batch PATCH handler and omission semantics | OrderedPatches |
| IN-01.2.031 | Typed, sanitized boundary errors and existing HTTP error envelope | WriteBaseline, LimitsAndAuthentication, ConflictingReceipts, AlternateUniqueRace, EffectGrantRollback, LostCommitAcknowledgement |
| IN-01.2.033 | `Uploads.Create` through `WorkOwner.Create`, existing CSV handler | UploadLifecycle, APIRuntimeGrants |
| IN-01.2.034 | `Deferred.Create` from actual write-slot overload, retained public key, atomic resource/job | DeferredLifecycle, ScheduledKinds |
| IN-01.2.036 | `WorkOwner.Detail`, authorized bounded metadata, existing DTO adapters | UploadLifecycle, FailedWorkDetails, DeferredLifecycle |
| IN-01.2.038 | `cmd/mysql-worker`, two configured queue runners, owner handlers | ScheduledKinds, CompetingWorkers, ScopedWorkerClaims, StartupChecks |
| IN-01.2.039 | `Config.Validate`, explicit isolated authority, no PG/River fallback | StartupChecks, APIRuntimeGrants |
| IN-01.4.003 | `cmd/mysql-api` / `cmd/mysql-worker` process fixture and readiness files | All TestProcess cases |
| IN-01.4.011 | Twelve simultaneous identical HTTP submissions; one physical/evidence/receipt set | IdenticalReceipts |
| IN-01.4.012 | Conflicting contents/mode/kind; tenant/table authorization checked before replay | ConflictingReceipts |
| IN-01.4.014 | Alternate unique race and late batch collision rollback, no unrelated-row upsert | AlternateUniqueRace |
| IN-01.4.015 | Client cancellation during isolated outbox delay, no effects, same-key retry releases lock | CancellationBeforeCommit |
| IN-01.4.016 | TCP proxy consumes actual COMMIT OK then disconnects API driver; same-key HTTP replay | LostCommitAcknowledgement |
| IN-01.4.017 | Revoke audit/outbox/public and private receipt INSERT after startup; 503 and zero effects | EffectGrantRollback |
| IN-01.4.020 | Both kinds scheduled using database time, attempts policy, configured queues | ScheduledKinds, ScopedWorkerClaims |
| IN-01.4.021 | Two worker processes ready before duplicate-key jobs become eligible; one attempt each, one effect set, no live reservations | CompetingWorkers |

Test names in the table have the `TestProcess` prefix. No additional task IDs
were created or substituted. Parent IN-01.2 and IN-01.4 remain in progress.

## Runtime contracts and limits

- New composition: `internal/httpapi/write_work_router.go`,
  `internal/mysqlruntime/{config,readiness,service,run}.go`, `cmd/mysql-api`,
  `cmd/mysql-worker`. Existing handlers use narrow injectable write/work
  interfaces; their default concrete PostgreSQL service remains the fallback.
- Configuration requires authority `isolated-mysql`, host `mysql` or loopback,
  a generated `ingestion_full_test_<32 hex>` database, non-root direct account,
  one tenant/object type, two distinct queues, explicit pool/concurrency, payload,
  receipt/effect and timing budgets, plus both owner installation artifacts.
  Password and service token are environment-only. No startup DDL or repair.
- API defaults to loopback and requires bearer authentication. Pool size is
  bounded and validated against two configured worker queues. This is a local
  composition budget, not a deployment-wide capacity assessment.
- Transport bodies are bounded by configured payload bytes plus 64 KiB for
  multipart overhead. Canonical commands separately cap records at 500 and
  bytes/depth/nodes. Invalid canonical structure/count returns 400; explicit
  key/body/receipt bounds return 413. Validation returns 422, targeted uniqueness
  and public-key conflicts 409, scope rejection 403, missing detail 404, storage
  failures 503. Unknown COMMIT returns 503 `commit_outcome_unknown`; only replay
  with the same public key reconciles the retained outcome.
- Unkeyed writes receive one private execution identity for that request. No
  automatic retry manufactures another identity after uncertain commit. Clients
  cannot reconcile an unkeyed request by public key; callers needing that
  guarantee must supply a key. No generic any-unique upsert was introduced.
- Existing deferred transport has no future-schedule parameter: overload submits
  immediately with a retained optional key. Future scheduling is verified through
  the accepted WorkOwner boundary and actual workers, not an invented API field.
- Upload DTO intentionally has no timestamp fields. Deferred DTO includes them.
  `WorkOwner.Detail` provides stored timestamps for both; tests preserve the
  existing wire contract. Last-error values are bounded queue codes, not raw SQL.
- Shared `mysqlstore/work.NewScoped` filters claims and rescue by tenant; each
  runner rescues only its configured queue. Existing global constructor remains
  available to owners that intentionally manage all tenants. Tests verify foreign
  tenant and unconfigured expired jobs remain untouched.

## Reproduction and evidence

Use the existing isolated Compose fixture/verifier container only. Build the
owner exporter using its existing fixture instructions and install no application
schema. From `/backend/ingestion-service` in that container:

```sh
go build -race -mod=readonly -o /tmp/ingestion-mysql-api ./cmd/mysql-api
go build -race -mod=readonly -o /tmp/ingestion-mysql-worker ./cmd/mysql-worker
INGESTION_DATA_MODEL_FIXTURE_EXPORTER=/tmp/ingestion-dm-fixture INGESTION_VERIFY_SUITE=process go run ./cmd/verify-mysql-fixture testdata/new-process-results
```

The existing verifier now accepts only an explicit `process` suite selector or
its default full-schema suite. It discovers tests before execution, requires each
to run/pass exactly once, rejects skips/failures/empty suites and writes raw JSONL
and a summary into a new directory. Tests require the two binaries and exporter;
missing prerequisites fail instead of skipping. Child binaries and test runner
are race-enabled. Each fixture cleans up its generated database and accounts.

Regression commands:

```sh
go test -json -race -mod=readonly ./internal/httpapi/... ./internal/mysqlruntime ./cmd/mysql-api ./cmd/mysql-worker ./cmd/verify-mysql-fixture
go test -json -race -mod=readonly github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work
INGESTION_DATA_MODEL_FIXTURE_EXPORTER=/tmp/ingestion-dm-fixture INGESTION_IMPORT_TEST_BINARY=/tmp/ingestion-import-receipts go run ./cmd/verify-mysql-fixture testdata/new-full-results
go vet -mod=readonly ./internal/mysqlruntime ./internal/httpapi/... ./cmd/mysql-api ./cmd/mysql-worker ./cmd/verify-mysql-fixture
```

## Material limitations and open gates

This completes isolated write/work adoption, not the overall MySQL migration.
Production stays PostgreSQL/River. Read/aggregate/admin transport adoption,
independent deployment schema/grants review, routine-definer/body review, live
legacy inventory/reconciliation, shared Q-01 operational acceptance, production
shutdown/restart/failure campaigns and coordinated cutover remain open.

Readiness verifies required grants and rejects root/active roles; it is not a
complete audit for excess privileges. Installer review still owns least-privilege
certification. Tests include an API-only account without worker mutation rights;
other process tests use a restricted worker-capable account to exercise both
processes. Generated physical test tables/triggers use the existing shared fixture
builder, not a running data-model publication API. No throughput, query-plan or
production service-level claim follows from these correctness tests.

The standalone shared queue module's readonly test command failed on an existing
missing `google/uuid` requirement. Running that package from the ingestion
consumer module passed using already-declared dependencies; no dependency was
added. Initial rejected runs exposed a lock-observation test issue and incorrect
expectation of upload timestamps; both were corrected and retained as rejected
evidence. The acceptance tests and production code do not suppress those errors.

## Accepted results

[Process suite: 17/17](../evidence/2026-10-30_ingestion-process20/accepted/summary.json),
[full shared schema: 13/13](../evidence/2026-10-30_ingestion-process20/full-schema/summary.json),
[handler/verifier regressions: 26/26](../evidence/2026-10-30_ingestion-process20/process-handler-regressions.jsonl),
[shared queue regressions: 2/2](../evidence/2026-10-30_ingestion-process20/process-queue-consumer-regressions.jsonl).
No required test failed or skipped. The final process suite includes both the
501-record bound and private-receipt grant rollback. Scoped vet passed.
[Provenance](../evidence/2026-10-30_ingestion-process20/provenance.json) records
source/binary hashes, dirty worktree revision, Go and server versions. Rejected
runs remain in the sibling `rejected` directory. These are correctness acceptance
results, not benchmark latency or capacity measurements.

Exactly 20 checklist leaves changed to Complete: **170 complete / 46 open / 216**.
