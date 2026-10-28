# Frontend, HTTP integrations and validation tooling

Status: **NEEDS_REVIEW** (design); implementation **NOT_STARTED**. Primary task: V-01; prerequisites: DM-01, IN-01, DE-01, SR-01, CM-01, AU-01, OP-01. Findings: PG-28/29/31/32/33/34.

## Verified execution path

[Source entry point](../../../backend/stress-tests/DATABASE_SCALE_RESULTS_ANALYSIS.md#L157). Frontend and HTTP integration tests reach databases indirectly through APIs. The audit found no frontend runtime database driver. Stress scripts include direct PostgreSQL clients and database observation. Existing unit/fake tests do not establish MySQL SQL/lock behavior. Full source occurrences are in the [audit catalog](../../11_source_evidence_catalog.md).

## Target implementation

Preserve HTTP status/error mapping, identifiers, cursors, JSON shape, manual privacy, role capability and review/closure version semantics. Add client handling only if an explicit API decision changes schema-operation pending behavior; do not redesign UI incidentally. Real-engine suites are mandatory and skipped tests are failures in the migration gate. Retain a PostgreSQL characterization lane for differential tests until cutover decisions are settled.

Use the common [schema contract](../04_schema_and_data_access_design.md), [security requirements](../05_security_and_integrity_requirements.md) and [transaction protocols](../07_transaction_and_recovery_protocols.md). These are required acceptance inputs, not already implemented helpers.

## Implementation checklist

- [ ] **V-01.1 ? NOT_STARTED:** Inventory API snapshots/fixtures, browser journeys and integration expectations; link every changed storage behavior to an observable assertion.
- [ ] **V-01.2 ? NOT_STARTED:** Port disposable MySQL fixtures and SQL seeds with safe target checks, migration ordering and full owner grants.
- [ ] **V-01.3 ? NOT_STARTED:** Run all module and cross-service suites, including direct_db/ingestion_http modes, async workers, provider auth and negative tenant tests.
- [ ] **V-01.4 ? NOT_STARTED:** PF-01: pair representative PostgreSQL/MySQL workload runs, preserve complete attempt accounting and verify business outcomes/side effects.

## Tests and acceptance

V-01/PF-01; use module-linked tests, backend/integration-tests and frontend suites present in the audit manifest. Record discovered/executed/passed/failed/skipped totals. Add stale review UI, pagination ties, expired sessions and malformed provider/manual history fixtures.

No claim of parity based on HTTP 200 alone or mocked repositories. No application tests or benchmark executed in this documentation phase. Exact CI commands are produced against the implementation revision, not guessed here.

## Delivery and rollback dependency

Implement and validate in disposable environments. All shared consumers must be compatible before live activation; an individual module passing its suite does not authorize a partial cross-engine production cutover. Preserve old adapters/compatible API artifacts through rehearsal as needed, with a single chosen runtime authority. The [roadmap](../10_implementation_roadmap.md) owns task dependencies and [cutover plan](../11_data_transfer_and_cutover.md) owns data rollback. Module schema rollback cannot undo externally delivered effects.
