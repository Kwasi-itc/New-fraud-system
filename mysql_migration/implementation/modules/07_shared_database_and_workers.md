# Shared driver, transactions, jobs and migration contracts

Status: **NEEDS_REVIEW** (design); implementation **IN_PROGRESS**. Primary task: DB-01; prerequisites: B-01, PX-01. Findings: PG-01/02/04/10/11/12/14/28.

## Verified execution path

[Source entry point](../../../backend/ingestion-service/internal/store/postgres/transaction_manager.go#L66). At the audit baseline, driver types escaped mutation ports. Ingestion now hides its transaction behind typed operations; River still requires pgx transactions inside the adapter. Shared investigator calls owner SQL functions. Every API/worker/migrate/reconcile executable constructs database dependencies; separate processes multiply the connection budget. Full source occurrences are in the [audit catalog](../../11_source_evidence_catalog.md).

## Target implementation

Provide a small shared SQL foundation for typed config, codecs, error taxonomy, transaction boundaries and observability. Domain ports expose business operations, not a universal SQL dialect abstraction. Q-01 implements P-03 with owner schemas/privileges and one maintained executor. Separate session-bound DDL locks from business row locks. Migration ledgers remain independently owned with explicit cross-owner ordering. No missing queue may quietly succeed.

Use the common [schema contract](../04_schema_and_data_access_design.md), [security requirements](../05_security_and_integrity_requirements.md) and [transaction protocols](../07_transaction_and_recovery_protocols.md). These are required acceptance inputs, not already implemented helpers.

## Implementation checklist

- [ ] **DB-01.1 ? IN_PROGRESS:** DB-01: implement driver/codecs/transaction/error contract and contract tests, then migrate ports without leaking sql.Tx into unrelated domain logic.
- [ ] **DB-01.2 ? NOT_STARTED:** Q-01: inventory every kind, payload, retry, unique scope, schedule, timeout, cancellation and domain effect; implement claims/fencing/retention/operator visibility.
- [ ] **DB-01.3 ? NOT_STARTED:** SC-01/OP-01: implement ledger/checksum, fixed routine deployment/privileges and schema compatibility checks; coordinate bootstrap dependency passes.
- [ ] **DB-01.4 ? NOT_STARTED:** V-01: test pool reuse/session settings, cancellation, cleanup, enqueue rollback, lost acknowledgement, reaper races and versioned job migration.

## Verified foundation subtasks

Parent DB-01 remains IN_PROGRESS: these ticks cover the reusable foundation only,
not service conversion or production acceptance. Evidence: [execution log](../16_execution_log.md).

- [x] **DB-01.1a - COMPLETED:** Typed connection settings, bounded pools, strict UTC sessions and verified TLS configuration.
- [x] **DB-01.1a.1 - COMPLETED:** Explicit FK/unique checks, autocommit and NO_CHAIN initialization; session reuse/replacement assertions and composite tenant-FK/secondary-unique/rollback fixture passed. Production owner constraints, grants and adoption remain pending.
- [x] **DB-01.1b - COMPLETED:** Mutation/snapshot boundaries with rollback on callback error, panic and cancellation; read-only and snapshot consistency tests.
- [x] **DB-01.1c - COMPLETED:** Binary UUID and UTC DATETIME(6) codecs; precision/range rejection and real-engine round trips.
- [x] **DB-01.1d - COMPLETED:** Real deadlock and lock-timeout classification; whole-transaction rollback of preceding writes and pool reuse.
- [x] **DB-01.1e - COMPLETED:** Lost COMMIT acknowledgement fault test; no callback retry and committed operation receipt reconciliation.
- [x] **DB-01.1e.1 - COMPLETED:** Unconfirmed rollback is a typed cleanup error and cannot classify as an ordinary deadlock/timeout. Real lost-ROLLBACK-acknowledgement test preserves the callback cause, verifies rolled-back state and successful replacement-session use without retry.
- [x] **DB-01.1f - COMPLETED:** Real TLS connection plus wrong-hostname/untrusted-CA rejection; ephemeral fixture certificate reuse.
- [ ] **DB-01.1g - IN_PROGRESS:** Remaining domain scalar/nullable codecs and error-to-domain mappings in owner repositories.
- [x] **DB-01.1g.1 - COMPLETED:** Nullable binary UUID and UTC DATETIME(6) codecs; strict validation, stale destination clearing and real-MySQL NULL/zero-value round trips. Owner mappings remain pending.
- [x] **DB-01.1g.2 - COMPLETED:** Non-null INET storage codec preserves address family, host bits and prefix length; MySQL round trips and direct invalid-insert constraint tests passed. API parsing/formatting, nullable owner mappings, query parity and production schema adoption remain pending (PG-24).
- [x] **DB-01.1g.3 - COMPLETED:** Nullable INET codec binds all-null or validated present tuples, rejects partial NULL and clears reused destinations. Real MySQL constraints reject all six partial-null combinations; owner schema/repository adoption and query parity remain pending.
- [x] **DB-01.1g.4 - COMPLETED:** JSON/NullJSON storage codecs validate UTF-8/document syntax without float64 decoding, preserve SQL NULL versus JSON null, and clear stale scan state. Real MySQL scalar/large integer/Unicode/duplicate-key tests passed; full JSONB parity and owner adoption remain pending (PG-20).
- [ ] **DB-01.1h - IN_PROGRESS:** Driver-independent service-port extraction, observability and service adoption.
- [x] **DB-01.1h.1 - COMPLETED:** Ingestion transaction ports no longer expose pgx.Tx; PostgreSQL adapter preserves atomic typed enqueue with real-engine tests. Other services and MySQL adoption remain pending.
- [x] **DB-01.1h.2 - COMPLETED:** Data-model service ports use typed index enqueue without pgx/River imports; PostgreSQL display adapter retains an explicit transaction contract pending its own conversion. Full owner suite passed.
- [x] **DB-01.1h.3 - COMPLETED:** Data-model reconciliation uses the typed transaction/enqueue boundary; repair history and queue mutations roll back together, with exact-key contention tests.
- [ ] **DB-01.1i - IN_PROGRESS:** Failover/resource-budget and representative load validation with deployment settings.

- [x] **DB-01.1i.1 - COMPLETED:** Disposable two-connection pool with sixteen waiting mutations: cancellation before callbacks, observable wait accounting, recovery and no leaked leases.
- [x] **DB-01.1i.2 - COMPLETED:** UTC/strict/default transaction session settings after successful/failed snapshots on the same connection and after physical connection replacement. Deployment failover and capacity remain pending.

## Tests and acceptance

- [x] **DB-01.1j - COMPLETED:** MySQL pinned-connection DDL lock lifecycle: fail-fast contention, explicit release, physical session disposal and real-engine failure tests. This is a P-01 prerequisite; operation ledgers, publication barriers, recovery and service adoption remain pending. POC-04 is only partially exercised.
- [x] **DB-01.1j.1 - COMPLETED:** Actual ALTER TABLE metadata-lock wait behind a reader: observed contention, server timeout/client cancellation, named-lock cleanup, unchanged shape and explicit successful retry tested. Production DDL recovery protocol and timeout selection remain pending.
- [x] **DB-01.1j.2 - COMPLETED:** Lost real ALTER success acknowledgement: callback runs once, cleanup ambiguity remains explicit, and a fresh lock owner verifies type/nullability/default before using the already-applied column without replay. Full schema inspector/publication recovery remains pending.
- [x] **DB-01.1j.3 - COMPLETED:** Reusable pinned-connection visible-column inspector with ordered definitions, explicit empty-result error, generated/collation/default metadata and real-engine drift/cancellation tests. Complete schema/grant inspection and publication decisions remain pending.
- [x] **DB-01.1j.4 - COMPLETED:** Reusable visible-index-part inspection preserves uniqueness, order, prefix, direction, expressions and optimizer visibility; MySQL same-name drift and cancellation tests passed. Complete constraint/grant inspection and publication decisions remain pending.
- [x] **DB-01.1j.5 - COMPLETED:** Visible FK/check inspection preserves composite mappings, referential actions and check enforcement; real MySQL drift, invalid-insert and cancellation tests passed. Table/grant verification, safe constraint replacement and publication remain pending.

POC-01/02; reuse existing owner transaction/integration tests and case maintenance/delivery tests. New real-engine tests must prove stale worker cannot commit business effects, not only that stale acknowledgement fails. Test all 15 kinds and negative configuration paths.

No global transaction mutex or one giant service account. Executor and exact-key/DDL mechanisms need named maintenance ownership. Shared code owns mechanics; services retain business transitions.

## Delivery and rollback dependency

Implement and validate in disposable environments. All shared consumers must be compatible before live activation; an individual module passing its suite does not authorize a partial cross-engine production cutover. Preserve old adapters/compatible API artifacts through rehearsal as needed, with a single chosen runtime authority. The [roadmap](../10_implementation_roadmap.md) owns task dependencies and [cutover plan](../11_data_transfer_and_cutover.md) owns data rollback. Module schema rollback cannot undo externally delivered effects.

Initial connection/transaction foundation is implemented in [mysqlstore](../../../backend/mysqlstore/README.md); ingestion ports now expose typed enqueue operations, backed by the existing PostgreSQL/River adapter. No service uses MySQL yet. See the [execution log](../16_execution_log.md).
