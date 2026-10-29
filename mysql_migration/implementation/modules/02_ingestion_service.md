# Ingestion, upload/deferred work and tenant reads

Status: **NEEDS_REVIEW** (design); implementation **IN_PROGRESS** (portable queue boundary; MySQL adapter pending). Primary task: IN-01; prerequisites: DM-01, DB-01, Q-01. Findings: PG-08/09/20/24/28/32/34.

## Verified execution path

[Source entry point](../../../backend/ingestion-service/internal/service/ingest_service.go#L112). Ingest/BatchIngest validate a model contract, enter a transaction, check receipt/hash/kind, write tenant records and persist audit/outbox/idempotency response. Upload/deferred creation also enqueues work transactionally. SQL model reader and HTTP model fallback plus aggregate endpoints are part of the migration. Full source occurrences are in the [audit catalog](../../11_source_evidence_catalog.md).

## Target implementation

Implement P-02: request-scoped idempotency reservation, published schema barrier check and narrow lookup-key guards. Preserve lookup-field conflict targeting, patch-only supplied columns, alternate uniqueness errors and all-or-nothing batch behavior. Keep audit/outbox/receipt/enqueue in the owner transaction. Remove success-through-noop queue wiring from the target design, with explicit startup/configuration failures. Typed aggregate builders retain count/sum/avg/min/max/count_distinct, null/filter behavior and tenant mapping.

Use the common [schema contract](../04_schema_and_data_access_design.md), [security requirements](../05_security_and_integrity_requirements.md) and [transaction protocols](../07_transaction_and_recovery_protocols.md). These are required acceptance inputs, not already implemented helpers.

## Implementation checklist

- [ ] **IN-01.1 ? NOT_STARTED:** Characterize current replay/hash/kind and failure-audit behavior; enumerate upload/deferred failure and retry paths.
- [ ] **IN-01.2 ? IN_PROGRESS:** Replace RawTx exposure with typed enqueue/mutation context; implement keyed object write/readback and errors without generic any-unique upsert.
- [ ] **IN-01.3 ? NOT_STARTED:** Port SQL/HTTP contract readers, dynamic projection/filter/aggregate builders and schema-version coordination; bound batch parameters and payloads.
- [ ] **IN-01.4 ? NOT_STARTED:** Port integration fixtures and add concurrent replay/unique collision/cancellation/ambiguous commit and worker completion tests.

## Tests and acceptance

POC-05, PERF-02; reuse internal/service/integration_test.go, internal/service/table_contract_test.go, internal/store/postgres/contract_integration_test.go, tenant_data_reader_test.go and clients/datamodel/contract_reader_test.go. Add record accounting beyond successful-response counts.

Every accepted input has a reconciled durable outcome. No audit/outbox without its successful data transaction. Batch lock sorting must not reorder patch semantics; benchmark guard cost under hot and distributed keys.

## Delivery and rollback dependency

Implement and validate in disposable environments. All shared consumers must be compatible before live activation; an individual module passing its suite does not authorize a partial cross-engine production cutover. Preserve old adapters/compatible API artifacts through rehearsal as needed, with a single chosen runtime authority. The [roadmap](../10_implementation_roadmap.md) owns task dependencies and [cutover plan](../11_data_transfer_and_cutover.md) owns data rollback. Module schema rollback cannot undo externally delivered effects.

## Verified preparation (source and real-test recheck 2026-10-29)

- [x] **IN-01.2a - COMPLETED:** Replace ingestion RawTx with typed transactional enqueue methods; keep pgx and River inside PostgreSQL adapters.
- [x] **IN-01.2b - COMPLETED:** Verify upload/deferred row and job commit/rollback together, tenant binding, callback/cancellation/panic cleanup, scheduled times and missing-schema/producer rollback against disposable PostgreSQL.
- [x] **IN-01.2c - COMPLETED:** Remove no-op queue success and require transaction managers on creation. Domain retries atomically update owner state and enqueue a version-1 per-attempt delivery; a running/retained prior delivery cannot silently absorb the new retry. Repeated insertion for the same attempt remains deduplicated; legacy v0 payloads remain accepted.
- [x] **IN-01.2d - COMPLETED:** API/worker startup and API readiness validate explicit public River schema lineage, required objects and role access. Queue-client construction errors propagate; missing database is not ready. Restricted-role, unknown-version and removed-table tests fail closed without installing anything.

The parent IN-01.2 remains open: MySQL keyed writes, target queue and errors are not implemented. These tests verify atomic enqueue/retry identity and readiness, not a full running worker's scheduling/rescue lifecycle, stale-effect fencing or exactly-once external effects. Migration metadata down preserves shared River objects; coordinated queue rollback is a separate operation, not a repair command. Full scope and reproduction: [dated evidence](../evidence/2026-10-29_dependency_free_shared_work.json).
