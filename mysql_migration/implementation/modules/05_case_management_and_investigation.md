# Cases and shared investigator transactions

Status: **NEEDS_REVIEW** (design); implementation **NOT_STARTED**. Primary task: CM-01; prerequisites: SC-01, DB-01, Q-01, AU-01, DE-01, SR-01. Findings: PG-11/18/19/20/25/26/33/35.

## Verified execution path

[Source entry point](../../../backend/investigator/review.go#L86). Case routes enter tenant/identity-aware UnitOfWork; intake locks logical receipt scope before hash dedupe. Shared investigator locks identity, operation, case/inbox/resource, replays or applies a review, persists immutable owner review and invokes case event/outbox contract. Assignment trigger currently guards capacity across mutations. Full source occurrences are in the [audit catalog](../../11_source_evidence_catalog.md).

## Target implementation

Keep shared review as one SQL transaction. P-05 reordered owner writes precede case event routine. Preserve case open/closed and resource-version invariants, decision membership uniqueness and atomic link transfer. Capacity uses P-06 across manual/auto/reopen/move paths with restricted bypass. Port report/evidence/upload cleanup and two periodic queues, not just CRUD. Analytics permission scope and 1001 sentinel remain; explorer selects allowed bounded fields with 128 KiB and 25+1 limits.

Use the common [schema contract](../04_schema_and_data_access_design.md), [security requirements](../05_security_and_integrity_requirements.md) and [transaction protocols](../07_transaction_and_recovery_protocols.md). These are required acceptance inputs, not already implemented helpers.

## Implementation checklist

- [ ] **CM-01.1 ? NOT_STARTED:** Port tenant-composite constraints, receipt/operation guards and link membership/transfer; characterize closure/reopen and stale version responses.
- [ ] **CM-01.2 ? NOT_STARTED:** Implement complete assignment/capacity and review contracts, restricted grants, immutable events, outbox and evidence finalization.
- [ ] **CM-01.3 ? NOT_STARTED:** Rewrite queue/workspace lists, explorer, analytics and report JSON/version with bounded snapshot queries and stable cursors.
- [ ] **CM-01.4 ? NOT_STARTED:** Port maintenance/delivery schedule uniqueness and cleanup; run shared investigator journey and concurrency/load suites.

## Tests and acceptance

POC-07/09/10; reuse store/postgres/lifecycle_integration_test.go, decision_membership_integration_test.go, phase2_intake_integration_test.go, phase5_analytics_integration_test.go, explorer_integration_test.go, phase4_evidence_integration_test.go, phase4_worker_integration_test.go and shared_load_integration_test.go. Add direct SQL bypass and two-transfer lock-order tests.

Last capacity slot has at most one winner. Review permission/closure changes cannot race past locks. Queue pages do not hydrate investigation history. Atomic upload finalization/digest and cleanup behavior preserved.

## Delivery and rollback dependency

Implement and validate in disposable environments. All shared consumers must be compatible before live activation; an individual module passing its suite does not authorize a partial cross-engine production cutover. Preserve old adapters/compatible API artifacts through rehearsal as needed, with a single chosen runtime authority. The [roadmap](../10_implementation_roadmap.md) owns task dependencies and [cutover plan](../11_data_transfer_and_cutover.md) owns data rollback. Module schema rollback cannot undo externally delivered effects.
