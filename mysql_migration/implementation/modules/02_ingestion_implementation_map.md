# Ingestion implementation coverage and behavior map

Status: IN_PROGRESS source review, 2026-10-28. This is a plan, not a verified
target implementation or completed schema manifest. Task ownership remains in
[IN-01.1/2/3/4](02_ingestion_service.md); no new completion IDs are introduced.
Paths below are relative to `backend/ingestion-service/` unless stated otherwise.
PostgreSQL/River remain the configured runtime. Existing dirty work is preserved.

IN-01.1 source characterization is now complete; the
[closure contract](02_ingestion_characterization_contract.md) supersedes the
initial source-only status and partial evidence below. It records 29 scenarios,
the five-table/53-column source inventory, target compatibility decisions and
repository consumers. IN-01.2/3/4 target implementation and acceptance remain open.

## Source behavior requiring executable characterization (IN-01.1)

| Source | Observed behavior | Required decision/test |
|---|---|---|
| `internal/service/ingest_service.go`: Ingest | Reads model and validates before receipt lookup. SHA-1 input is JSON of normalized record; receipt lookup is tenant plus key. Response kind is checked separately. | Pin normalization/null/numeric/time behavior and replay after model changes. Mode and object type are not explicit hash inputs; test same key/body across modes and tables. Define versioned target identity and legacy-receipt handling rather than silently changing stored hashes. |
| Same file: BatchIngest | Hashes original input records, not normalized records; accepts 1..500 records; rejects duplicate normalized lookup IDs before mutation; stores ordered results and sets Replayed on each replayed result. | Pin raw-versus-normalized differences, list order, duplicate/error attribution and single/batch kind conflicts. Preserve rejection of duplicate lookup IDs; sorted lock order must not change logical processing order. |
| Same file: validation failures | Writes failure audit in a separate transaction and discards its error. Batch failure audits cover original inputs; empty/oversized batches return before that path. | Inject failed audit/rollback/cancellation and document current response. Target must make audit failure explicit/observable under a reviewed contract; do not claim failure audits are part of a successful data transaction. |
| `internal/store/postgres/idempotency_repository.go` | SELECT then INSERT; no explicit absent-key reservation in Get. | Concurrent identical/different commands, response-kind mismatch, receipt insertion failure, ambiguous commit and immutable result/retention policy. MySQL must serialize absent receipt keys explicitly. |
| `internal/store/postgres/tenant_data_writer.go` | PATCH first tries update; missing rows proceed to insert. INSERT conflicts target only the configured lookup field, followed by UPDATE RETURNING. Only supplied fields are patched. Alternate unique conflicts remain errors. | Missing-row patch, no-op update, alternate-unique collision, deleted/recreated row race and exact action/result parity. Never replace this with generic any-unique-key upsert. |
| `internal/service/upload_log_service.go`: RunLog | Starts attempt, decodes CSV, calls BatchIngest without a receipt key, then updates upload completion separately. | Crash after ingest commit but before completion can repeat data/audit/outbox work. Define stable execution identity and fenced completion/recovery; prove no duplicate effects after redelivery. Characterize malformed CSV, oversized batches and row counters. |
| `internal/service/deferred_ingest_service.go`: RunDeferredIngest | Starts attempt, calls Ingest with optional caller key, then updates completion separately. Retry state persists before returning a retryable error. | Cover absent caller key, duplicate deliveries, data-commit/completion gap, stale worker, exhausted attempts and cancelled updates. Queue acknowledgement alone cannot fence already-committed business writes. |
| `internal/clients/datamodel/contract_reader.go` | Table reads consult SQL contract state before using versioned cache. SQL errors propagate; legacy HTTP reader serves explicit full-model consumers. | Preserve no fallback on SQL failure; test stale token, same-name recreated table, wrong tenant, closed gate and bounded cache behavior. |

The initial review was source-only. Follow-up executable characterization now
covers twelve replay/audit/write scenarios and three worker completion-gap
scenarios in `internal/service/migration_characterization_test.go`. Full ingestion
vet/race regression passed (81 top-level tests, no skips/failures). This is
PostgreSQL evidence, not MySQL implementation or acceptance. The worker fault is
an injected completion-repository error after real ingestion commit, not a lost
server COMMIT acknowledgement. Known source weaknesses remain distinct from
intended target invariants.

## Compatibility decisions to implement and verify

The target must bind receipt identity to tenant, stable table identity, operation
kind and mode as well as versioned canonical request content. Do not silently
reinterpret legacy hashes: define a retained legacy-reader/translation policy
before importing receipts. Replays must authorize current access but must not
rerun model-dependent validation or mutations merely to reconstruct a previously
committed result. The exact encoding, error/status mapping and schema-version
preconditions still need a reviewed contract and differential tests.

Validation-failure audit persistence errors must be surfaced or durably recorded
through an explicit owner contract, rather than silently discarded. Keep this
separate from success data/audit/outbox atomicity. Worker redelivery needs a
stable owner execution identity even without a caller receipt key, plus fencing
at business commit and durable completion reconciliation. Reusing a public
idempotency namespace with a guessed CSV key is not a sufficient recovery design.

Source worker policies: both worker types declare five-minute timeouts; enqueue
uses ByArgs uniqueness, configured queues/max attempts and optional scheduled
time. StartAttempt currently updates by resource ID without a terminal-state or
lease predicate. Enumerate active/terminal redelivery, stale writes and retained
River uniqueness before choosing the target lease/retention policy. A source
search found direct core_ingestion table accesses in ingestion repositories and
migrations; the decision service's core.outbox_events is a different owner table.
External/deployment consumers still require inventory confirmation; do not assume
that a similarly named outbox worker delivers ingestion events.

## Owned persistence and shared dependencies (IN-01.1/2)

| Adapter / source objects | Target obligation |
|---|---|
| `ingestion_audit_repository.go`; core_ingestion.ingestion_audit | Preserve status/payload/error semantics, trusted tenant and revision, bounded payloads and indexed history; separate success audit atomicity from failed-validation audit policy. |
| `outbox_event_repository.go`; core_ingestion.outbox_events | Same-transaction record/event commit; preserve per-record and batch event contracts, downstream consumer ownership, delivery deduplication and retention. An outbox row is not proof of downstream delivery. |
| `idempotency_repository.go`; core_ingestion.idempotency_keys | Exact tenant/key identity with full-byte comparison for long keys, request reservation, immutable original responses, versioned fingerprints and bounded receipt reads; no digest-only equality. |
| `upload_log_repository.go`; core_ingestion.upload_logs | Binary CSV payload, row counters, timestamps, attempts/status/errors, tenant-consistent resource lookup, bounded lists and explicit payload projection. Fenced attempt/completion and durable work required. |
| `deferred_ingest_repository.go`; core_ingestion.deferred_ingests | JSON payload, optional receipt key, status/attempt counters and indexed metrics; tenant authorization for resource and admin reads, fencing and recovery. |
| `transaction_manager.go`; MutationStore | One database/transaction for local effects and typed enqueue; explicit isolation/lock order, cancellation cleanup, no network calls under locks and commit-ambiguity recovery. No leaked pgx/sql driver in domain ports. |
| `ingestion_contract_store.go`; data-model-owned read views | Read-only shared contract with stable table identity/version and registry mapping. Do not independently mutate data-model-owned metadata or reproduce private installation internals. |
| `tenant_data_writer.go`, `tenant_data_reader.go`; data-model-owned dynamic tables | Authorized mapped identifiers and compatible six-type codecs, exact lookup semantics, writer/readiness barriers and restricted record grants; no tenant CREATE DATABASE. |

Source lineage is `internal/migrations/metadata/000001_init` (four tables and
indexes) and `000002_deferred_ingests` (one table and indexes), each up/down pair.
Build a column/default/key/index/consumer manifest before freezing target SQL.
Include PostgreSQL UUID/TIMESTAMPTZ/JSONB/BYTEA transformations, unrestricted text
key domains, source-data preflight, reviewed exact-key dependencies, migration
history/checksums and restart after partial installation. Down migrations are
destructive schema operations, not an operational rollback strategy.

## Entry points and worker acceptance (IN-01.2/3/4)

| Entry point | Evidence required |
|---|---|
| Single POST/PATCH and batch POST/PATCH in `internal/httpapi/router.go` | Trusted scope, limits, typed HTTP conflicts/validation/overload errors, stable receipt transport, exact response replay and no success before commit. |
| CSV creation, upload list/detail, deferred detail | Row plus queue atomicity, bounded payload/storage, tenant-consistent ID resolution, protected status/errors and durable outcome after worker restart. |
| Record get/list/search and aggregate | Registered projections, six-type decode, temporal/current-row semantics, stable ordering/limits, parameterized values, allowed filter/operator handling and count/sum/avg/min/max/count_distinct null/type parity. |
| Read/deferred metrics and health/readiness | Explicit administrative authorization, indexed/bounded accounting, pool saturation and installed-schema/grant/dependency readiness; database ping alone is insufficient. |
| `cmd/worker`, `internal/riverjobs/upload_log_worker.go`, `deferred_ingest_worker.go` | Map upload_log_execute and deferred_ingest_execute payloads, queues, source retry/backoff/timeouts/uniqueness, lease renewal, rescue, stale-worker fencing, cancellation and joined shutdown. No external work while holding row locks. |
| App/server, migrate, Docker and configuration | Explicit adapter selection with startup errors for missing capabilities, one shared logical database, separate restricted owner/reader/worker/installer policies and an isolated runnable API stack. No production selection until all consumers are compatible. |
| Downstream record/event consumers | Source-linked consumers of records and outbox, stable event identity/schema, read/version behavior and grant compatibility. Reconcile effects beyond HTTP success counts. |

The existing `compose.migration-test.yaml` is PostgreSQL characterization only.
A MySQL test fixture must be isolated from the application database and bind the
reviewed DM-01 schema/registry contract. Fixture administrator privileges cannot
substitute for acceptance with restricted application accounts.

## Verification sequence and completion rules

First extend the existing service/integration/contract-reader/tenant-reader and
queue-boundary tests to pin the behavior above. Keep compatibility decisions
visible; do not mark IN-01.1 complete from this source review alone. Next verify
schema/transaction/keyed-write primitives against MySQL, followed by real owner
API and worker journeys. Include actual lost server commit acknowledgement,
concurrent same/different receipt keys, hot/distributed object keys, alternate
uniqueness, partial batch failure and schema change racing ingestion.

Load acceptance needs agreed payload/table/key distributions, pool and worker
budgets for the entire shared database, query plans, bounded parameter/packet
sizes and PERF-02 thresholds. Include retained receipts, large CSV payloads and
metrics reads in capacity tests. Migration acceptance needs source/target record,
audit/outbox/receipt and pending-work reconciliation, old River writer drain,
retry-history translation and a recovery/rollback rehearsal. None of those gates
are satisfied by successful isolated SQL fixtures alone.
