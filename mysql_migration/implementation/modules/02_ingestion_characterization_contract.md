# Ingestion source characterization and target decisions

IN-01.1 source characterization completed 2026-10-28. This closes the repository
behavior/inventory task, not IN-01.2/3/4, design review, live inventory B-01 or
MySQL acceptance. PostgreSQL/River remain runtime. Tests are isolated from the
application database; no live grants or data were inspected or changed.

## Reproducible behavior evidence

Tests under `backend/ingestion-service/internal/service/`:

| Test | Evidence and interpretation |
|---|---|
| TestMigrationReplayCharacterization | Twelve cases: normalized single versus raw batch hashes; mode/table omitted from source receipt identity; model validation before replay; hash/kind conflicts; duplicate-batch rejection; swallowed single/batch failure-audit errors; alternate unique conflict and full batch SQL rollback. |
| TestMigrationWorkerCompletionGap | Three cases: interrupt completion after real business commit for CSV, unkeyed deferred and keyed deferred. Re-execution duplicates effects for the first two; retained caller receipt prevents repeat effects for the third. This is not lost server COMMIT acknowledgement. |
| TestMigrationConcurrentReceiptCharacterization | Two callers synchronize after both read an absent receipt. Identical and different commands both produce one committed caller and one 23505 loser. The loser leaves no data/audit/outbox/receipt; ordinary retry replays identical content or conflicts on changed content. Five repeated race-enabled runs passed. |
| TestMigrationCancellationAfterWrites | Single and batch requests cancel after real data/audit/outbox statements. Cancellation returns no provisional result, rolls back all effects and permits a clean later retry. |
| TestMigrationWorkerFailureCharacterization | Ten cases across CSV/deferred: decode failure, validation rejection, database failure, terminal redelivery and missing resource. Decode/database errors persist retry state until attempt exhaustion; terminal validation/exhaustion acknowledges persisted failed state. Terminal successful jobs can currently run again and duplicate effects. |
| TestMigrationOwnedSchemaInventory | Both source migrations installed on PostgreSQL; checks all five owned tables and all 53 columns in ordinal order, rejecting extra/missing columns. This is a source-column inventory, not a target schema manifest. |

Existing queue-boundary/transaction-queue tests cover missing producers, atomic
row/enqueue rollback, scheduling, cancellation cleanup and failure to persist
retry state. The full module includes service retry and contract/cache/read tests.
This combined evidence characterizes source branches; it does not establish a
running MySQL worker's rescue/backoff/lease behavior.

Full `go vet ./...` and race `go test ./...` passed: 85 top-level tests, no skips
or failures. See [focused](../evidence/2026-10-28_ingestion-closure-focused.log),
[regression](../evidence/2026-10-28_ingestion-closure-regression.log) and
[repeated receipt races](../evidence/2026-10-28_ingestion-receipt-race-repeat.log).

## Compatibility decisions for the target implementation

1. Preserve lookup-targeted create/patch semantics, supplied-field-only updates,
   alternate-unique rejection, unchanged-patch success, batch atomicity, duplicate
   lookup-ID rejection and result ordering. Sorting locks must not sort effects.
2. Do not preserve the omission of mode/table from receipt identity. Introduce an
   explicitly versioned request format binding tenant, stable table identity,
   mode, single/batch kind and canonical content. Pin encoding and normalization
   in IN-01.2 tests; do not relabel existing receipt hashes as the new format.
3. Legacy receipt translation must retain original hash/kind/result and proven
   scope. Only migrate a receipt into a scoped replay capability when attribution
   is unambiguous from source evidence. Quarantine/report unprovable scope and
   require reconciliation; never guess a mode/table or blindly repeat a write.
4. Reserve request identity before business effects. Concurrent duplicates wait
   and replay the committed result or return a typed conflict, rather than leak
   a raw duplicate-key failure as the intended target behavior. Ambiguous commit
   recovery uses the same retained identity and current authorization.
5. Historical replay must not rerun mutable model validation to reconstruct a
   result. Current authorization is mandatory; retain original response and its
   scope. New mutations require current publication/version/readiness checks.
6. Failure-audit persistence errors must be returned through an explicit owner
   error contract, retaining the original validation context; no silent discard.
   Map that error in API/worker integration tests. Successful audit/outbox remain
   in the data transaction; failed-input auditing remains a distinct transaction.
7. Every asynchronous execution needs an owner-scoped stable identity independent
   of optional caller keys. Fence at business commit, atomically retain execution
   outcome and effects, and reconcile queue acknowledgement/completion after a
   crash. Terminal redelivery must not repeat effects. Do not implement this as
   a guessed public CSV idempotency key or a last-step lease check.
8. Preserve separate domain and delivery attempt accounting. Both source worker
   types have five-minute timeouts; enqueue uses ByArgs uniqueness, configured
   queue/max attempts and optional schedule. Translate retained River work and
   retry history explicitly; scheduling/rescue/load acceptance belongs to Q-01
   and IN-01.2/4, not this characterization task.

These decisions define implementation requirements. Exact target wire/schema
formats, reviewed grants and deployed compatibility still require IN-01.2/3/4
evidence. No PostgreSQL behavior was changed by these characterization tests.

## Repository inventory and consumer boundary

Source migrations: 000001_init up/down (audit, outbox, receipts and uploads) and
000002_deferred_ingests up/down. Counts: ingestion_audit 11 columns, outbox_events
8, idempotency_keys 6, upload_logs 16, deferred_ingests 12. Existing source indexes
and reversal constraints are listed in those SQL files and must be mapped in
the target schema work under IN-01.2/SC-01.

Repository source search across backend/frontend and root Compose identifies
owner SQL in the five ingestion PostgreSQL repositories and migrations. Additional
direct SQL consumers are `backend/stress-tests/production_replay/database_reconciliation.py`,
its `tests/test_scale_sql_integration.py`, `backend/stress-tests/system_tests/service_pilot.py`
and `backend/case-manager-service/internal/store/postgres/shared_load_integration_test.go`.
The latter is test load generation, not an authorized cross-owner production
mutation path. Port these fixtures/accounting readers before claiming system
acceptance. Frontend/API readers and ingestion's data-model contract/dynamic-table
dependencies remain enumerated in the [implementation map](02_ingestion_implementation_map.md).

The decision service's core.outbox_events is distinct from
core_ingestion.outbox_events. This inventory does not establish a production
delivery consumer for ingestion's outbox. Deployment/external consumers and live
schema drift require B-01/DT-01 inventory; target installation, downstream delivery
and system validation must not silently assume a consumer exists.
