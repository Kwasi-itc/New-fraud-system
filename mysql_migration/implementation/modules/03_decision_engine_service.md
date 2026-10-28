# Decision evaluation, workflows and historical reads

Status: **NEEDS_REVIEW** (design); implementation **NOT_STARTED**. Primary task: DE-01; prerequisites: IN-01, DB-01, Q-01, SC-01. Findings: PG-08/10/15/20/25/26/32/34.

## Verified execution path

[Source entry point](../../../backend/decision-engine-service/internal/service/decision_service.go#L366). EvaluateScenario evaluates AST/rules and historical data before its persistence transaction; persistence writes decision/evidence/workflow/screening/scoring/outbox plus jobs. Direct_db and ingestion_http tenant readers must agree. Scenario/workflow authoring and screening projection are additional mutation/read paths. Full source occurrences are in the [audit catalog](../../11_source_evidence_catalog.md).

## Target implementation

Preserve this evaluation consistency contract rather than claiming a new global snapshot. Differentially test AST aggregate pushdown and HTTP fallback. Keep persistence and all seven decision job kinds atomic. Use one read snapshot and bounded ordered batch assembly for ListStructured, preserving the 500-parent cap. Replace source reconciliation with the owner routine in P-04, retaining monotonic version and relationship checks. Decision search/cursors require explicit case/null/order semantics and measured indexes.

Use the common [schema contract](../04_schema_and_data_access_design.md), [security requirements](../05_security_and_integrity_requirements.md) and [transaction protocols](../07_transaction_and_recovery_protocols.md). These are required acceptance inputs, not already implemented helpers.

## Implementation checklist

- [ ] **DE-01.1 ? NOT_STARTED:** Inventory decision/evidence/scenario/rule/workflow/screening/scoring SQL and driver-dependent scanners against source manifest.
- [ ] **DE-01.2 ? NOT_STARTED:** Implement repositories plus atomic persistence/enqueue and bounded error/retry outcomes; port direct tenant reader and preserve HTTP contract mode.
- [ ] **DE-01.3 ? NOT_STARTED:** Port workflow/scenario authoring, list/search/cursor and reconciliation routines; preserve locks, audit and child order.
- [ ] **DE-01.4 ? NOT_STARTED:** Run differential decision/evidence corpus, concurrent authoring/snapshot tests and representative historical lookup/aggregate plans.

## Tests and acceptance

POC-10/11, PERF-01/06; reuse internal/service/rule_evidence_integration_test.go, execution_service_integration_test.go, async_validation_integration_test.go, internal/store/postgres/tenant_data_reader_test.go and runtime/ast_eval/aggregate_http_integration_test.go. Add golden full rule outcomes, not merely HTTP 200/202.

No decision threshold changes from numeric coercion. Same logical input/model/rules produces equivalent evidence under the defined read contract. Read mode and worker URL must not silently select stale/separate authority.

## Delivery and rollback dependency

Implement and validate in disposable environments. All shared consumers must be compatible before live activation; an individual module passing its suite does not authorize a partial cross-engine production cutover. Preserve old adapters/compatible API artifacts through rehearsal as needed, with a single chosen runtime authority. The [roadmap](../10_implementation_roadmap.md) owns task dependencies and [cutover plan](../11_data_transfer_and_cutover.md) owns data rollback. Module schema rollback cannot undo externally delivered effects.
