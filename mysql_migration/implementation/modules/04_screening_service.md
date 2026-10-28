# Screening, continuous runs, history and exclusion ledger

Status: **NEEDS_REVIEW** (design); implementation **NOT_STARTED**. Primary task: SR-01; prerequisites: SC-01, DB-01, Q-01, AU-01, DE-01. Findings: PG-13/14/15/16/20/21/25/33/35.

## Verified execution path

[Source entry point](../../../backend/screening-service/internal/service/dispatch_service.go#L75). Creation and durable work lead to token claim, provider search outside SQL, then candidate replacement/status/publication under token lock. Continuous recovery, dataset/monitored workers, case-event outbox, execution projection, manual search/history and investigator exclusions are independent paths. Full source occurrences are in the [audit catalog](../../11_source_evidence_catalog.md).

## Target implementation

Implement P-04/P-05 preserving six-minute dispatch claim, source version and case-outbox policies. Keep entity-derived match IDs textual. Migrate manual actor/origin explicitly and enforce tenant+actor+true-manual filtering. Retain exclusion key equality and immediate composite FKs with review-first ordering. Provider matching stays in existing provider/client contracts; no PostgreSQL trigram dependency was established and no new search service is justified. Candidate replacement relies on a validated locked parent and must remain in the same transaction.

Use the common [schema contract](../04_schema_and_data_access_design.md), [security requirements](../05_security_and_integrity_requirements.md) and [transaction protocols](../07_transaction_and_recovery_protocols.md). These are required acceptance inputs, not already implemented helpers.

## Implementation checklist

- [ ] **SR-01.1 ? NOT_STARTED:** Map all screening migrations and indexes, manual/continuous origin fields, review/exclusion FK graph and provider payload contracts.
- [ ] **SR-01.2 ? NOT_STARTED:** Port claim/recovery/replacement, three job kinds and fenced outbox; preserve partial zero-candidate awaiting_review behavior.
- [ ] **SR-01.3 ? NOT_STARTED:** Implement source version/enqueue enforcement, ordered projector/savepoint retry and decision owner contract; port bounded history queries.
- [ ] **SR-01.4 ? NOT_STARTED:** Implement precomputed review/exclusion transition and exact long keys; validate immutable audit, applications and case transaction joins.

## Tests and acceptance

POC-03/06/07/11; reuse store/postgres/continuous_intake_integration_test.go, case_event_outbox_integration_test.go, execution_projection_integration_test.go, object_exclusions_migration_test.go, review_consistency_integration_test.go, manual_search_integration_test.go and investigator_journey_integration_test.go. Add stale-provider-response and crash-after-review-write races.

All candidate, review, exclusion and case effects reconcile atomically. Projection retries cannot delete newer pending work. History remains actor-private. No provider call while source/case locks are held.

## Delivery and rollback dependency

Implement and validate in disposable environments. All shared consumers must be compatible before live activation; an individual module passing its suite does not authorize a partial cross-engine production cutover. Preserve old adapters/compatible API artifacts through rehearsal as needed, with a single chosen runtime authority. The [roadmap](../10_implementation_roadmap.md) owns task dependencies and [cutover plan](../11_data_transfer_and_cutover.md) owns data rollback. Module schema rollback cannot undo externally delivered effects.
