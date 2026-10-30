# Implementation roadmap and checklist

Implementation started on 2026-10-22 following the user instruction "start implementing". DB-01 and PX-01 are IN_PROGRESS for the isolated shared foundation; IN-01 is IN_PROGRESS for portable ingestion queue boundaries; DM-01 is IN_PROGRESS with staged MySQL metadata, aggregate snapshots, publication and delivery/worker metadata adapters. DE-01 has staged MySQL repositories/jobs; descriptive active, remaining-implementation, testing and dependency statuses are maintained in module 03. Previously started work is not automatically active. Runtime remains PostgreSQL/River. Other tasks have not started; production cutover is not being performed. Status vocabulary: NOT_STARTED, IN_PROGRESS, BLOCKED, COMPLETED, NEEDS_REVIEW. Design review is NEEDS_REVIEW; a document existing does not mark its implementation complete. Dependencies are acceptance dependencies; isolated development can proceed earlier with explicit interface fixtures, never partial live database cutover.

## Phases

1. G0 baseline and requirements: B-01.
2. G1 feasibility/ADRs: PX-01.
3. G2 schema and shared mechanics: SC-01, DB-01, Q-01, AU-01.
4. Module implementation in disposable environments: DM-01 -> IN-01 -> DE-01 -> SR-01 -> CM-01; AU-02 and operational preparation alongside compatible work.
5. G3/G4 parity, performance and data/recovery rehearsal: V-01 -> PF-01 -> DT-01.
6. G5 reviewed operational activation: CO-01.

Cross-owner schema dependencies may require bootstrap stubs/tables in SC-01 before module routines are installed. A stub must fail explicitly until implemented; no success for an unimplemented contract. Runtime integration tests cannot pass against placeholders. This resolves development ordering without weakening production constraints.

## Task register

| ID | Task / components and findings | Prerequisites | Expected output | Acceptance criteria | Verification | Status |
|---|---|---|---|---|---|---|
| B-01 | Baseline/live profile: All owners; PG-01?35 | ? | Read-only actual object/grant/data/work inventory against audit; collect deployment/SLO requirements. | Signed inventory resolves drift, hidden objects and data ranges; no secret payload in artifacts. | Catalog/data-profile report and G0 review | NOT_STARTED |
| PX-01 | Architecture experiments: Shared mechanisms; ADR-01?12 | B-01 | Execute POC-01?11 in isolated fixtures and revise/accept ADRs with owner review. | Every relevant POC has evidence; failures revise design; no convenience-based security acceptance. | POC report, threat/lock review and ADR decisions | IN_PROGRESS |
| SC-01 | Schema and transforms: All tables, PG-03?07/16?26/35 | PX-01 | Produce exhaustive target manifest and owner MySQL migration lineages, exact-key design and dependency graph. | Every source object/column/query consumer accounted for; key budgets and integrity enforced. | Manifest diff, fresh/upgrade/partial-DDL tests and restricted-account SQL | NOT_STARTED |
| DB-01 | Driver and transaction foundation: All Go services/investigator; PG-01/02/08/11/12/28 | PX-01 | Typed driver config/codecs/errors, mutation contexts, lock/snapshot helpers and bounded cleanup. | No driver leakage in domain ports; correct cancellation/outcomes and no session leakage. | POC-01, compile/contracts and real-engine transaction suite | IN_PROGRESS |
| Q-01 | Durable work and scheduling: Five job owners; PG-10/14 | SC-01, DB-01 | Implement P-03, map 15 kinds, schedules, dedupe, leases, recovery, metrics and redrive. | Committed enqueue survives crash; stale workers cannot publish; all source policies mapped. | POC-02 plus per-kind lifecycle matrix and soak | NOT_STARTED |
| AU-01 | Local identity/authorization: Auth + shared consumers; PG-11/17/21/23/35 | SC-01, DB-01 | Port schema/routines/roles/sessions/invitations/preflight and restricted owner contracts. | Revocation/admin/tenant invariants and session replay preserved. | POC-08 and V-AU, grant bypass tests | NOT_STARTED |
| DM-01 | Data model and publication: Data-model; PG-03?07/20/21/27/32 | SC-01, DB-01, Q-01, AU-01 | Implement full P-01 lifecycle, display/contracts, indexes and API/worker/reconcile wiring. | All physical changes verify before publication; crash repair and old-writer drain proven. | POC-04, V-DM and concurrent DDL/load | IN_PROGRESS |
| IN-01 | Ingestion and reads: Ingestion; PG-08/09/20/24/28/32/34 | DM-01, DB-01, Q-01 | Implement P-02, uploads/deferred jobs, atomic receipt/audit/outbox and query builders. | Target-specific conflicts, batch atomicity, exact replay and read-mode parity. | POC-05, V-IN and PERF-02 | IN_PROGRESS |
| DE-01 | Decisions and workflows: Decision; PG-10/15/20/25/26/32/34 | IN-01, SC-01, DB-01, Q-01 | Port evaluation readers, persistence/jobs, authoring/snapshots/cursors and projection contract. | Rule/evidence parity, bounded snapshots and monotonic tenant-bound projections. | POC-10/11, V-DE and PERF-01/06 | IMPLEMENTATION REMAINS; exact active work, scoped completion and testing/dependency states have one source of truth in [module 03](modules/03_decision_engine_service.md#granular-execution-plan-five-tasks-per-batch) |
| SR-01 | Screening and exclusions: Screening; PG-13?16/20/21/25/33/35 | DE-01, AU-01, SC-01, DB-01, Q-01 | Implement P-04/P-05, manual history, continuous/dataset work and review ledger. | No stale publication; immediate FK ledger atomicity; private history. | POC-03/06/07/11, V-SR and candidate/review races | NOT_STARTED |
| CM-01 | Cases/shared investigation: Cases/investigator; PG-11/18/19/20/25/26/33/35 | SR-01, DE-01, AU-01, SC-01, DB-01, Q-01 | Port intake, capacity, memberships/review, evidence, analytics/explorer and schedules. | All mutation paths enforce capacity/permissions; no partial review/case/outbox. | POC-07/09/10, V-CM and shared contention suite | NOT_STARTED |
| AU-02 | Provider migration decision: Keycloak/OIDC; PG-29 | B-01, PX-01, AU-01 | Pin supported release and storage topology; design supported state transfer and session policy. | No unapproved extra DB, unsupported vendor table changes or silent security-state loss. | Provider migration rehearsal and login/refresh/logout/revocation checks | NOT_STARTED |
| OP-01 | Deployment and recovery: Compose/scripts/CI/ops; PG-02/04/27/29/30/31/32 | SC-01, DB-01, Q-01, AU-02 | Port configs/tools/grants/readiness, pool budgets, protected backup/PITR and failover. | Fresh deploy/upgrade/restore succeeds; least privilege and source fencing verified. | V-OP, credential rotation and timed restore/failover | NOT_STARTED |
| V-01 | System parity validation: All modules/front end/integration; PG-01?35 | DM-01, IN-01, DE-01, SR-01, CM-01, AU-01, AU-02, OP-01 | Run mandatory real-engine and differential full-system suites. | No skipped SQL suites, unresolved tenant/integrity/business mismatches or untested changed contracts. | Signed suite totals and source-linked defect closure | NOT_STARTED |
| PF-01 | Performance qualification: All APIs/workers/reporting; PG-27/31/34 | V-01 | Paired workload/plan/soak/capacity/recovery tests at agreed scale. | Approved SLOs/regression budget achieved with zero unexplained correctness loss. | PERF-01?09 artifacts, plans and resource analysis | NOT_STARTED |
| DT-01 | Data and cutover rehearsal: All owners/provider/work; PG-03/10/18/29/30/35 | V-01, PF-01 | Implement resumable transform/reconcile, pending-work migration and rollback rehearsal. | Full logical reconciliation and restore/rollback with target writes meet downtime/RPO/RTO. | Timed production-shaped isolated rehearsal and checksums | NOT_STARTED |
| CO-01 | Controlled activation: Deployment and all owners | DT-01 | Present concrete evidence/runbook for operational approval; execute only in authorized execution phase. | Single authoritative writer, monitored acceptance and tested rollback authority; decommission after recovery window. | G5 approval plus cutover/observation record | NOT_STARTED |

## Review and ownership

DM-01 creation update (2026-10-27): explicit v2 readiness now connects shared
installation and locked-definer inspection, temporary restricted-installer
deactivation and a concrete TableService publication finalizer under the execution
lock. Disposable restricted-account recovery and atomic owner tests pass. DM-01
and DM-01.2k.9 remain IN_PROGRESS. Durable creation allocation, v003 original-result
receipts and owner result recovery now pass concurrent/lost-commit tests; operational
reference/installer orchestration, remaining lifecycle planners and owner/display integration, runtime/consumer
wiring, reviewed deployment policies and operational/load acceptance are open.
PostgreSQL/River remain configured. See the [module checklist](modules/01_data_model_service.md).

Guarded native changes now use v3 plans retaining initial triggers and regenerating
typed SQL/preconditions. A separate temporary native installer executes supported
ALTER/index changes without SET_ANY_DEFINER. Publication verifies complete desired
metadata/mappings after owner effects. FieldService's native creation finalizer
binds allocated field identity and original command, validates owner state and
writes audit/revision atomically; replay/lost-commit and rollback tests pass.
Durable field allocation/original Field-result recovery, complete index/display
owners and remaining lifecycle/runtime/deployment gates are still open.

Assign a named accountable engineering owner and security/operations reviewer per task at G0; role ownership in this register is not a fabricated staffing assignment. Each module plan supplies subordinate checklist items and test inputs. Carry task ID, PG findings, ADR and validation IDs into future PR descriptions. No estimates are invented before live inventory and POCs establish complexity.

DO-01 documentation synthesis is COMPLETED as an artifact; technical recommendations remain NEEDS_REVIEW. Documentation verification is in [14](14_documentation_validation.md). Audit tasks M-01?M-16 remain unchanged and unimplemented: B-01/PX-01 elaborate M-01?M-03; SC-01/DB-01 elaborate M-04?M-05; module tasks elaborate M-06?M-11; OP-01 elaborates M-12; DT-01 elaborates M-13/M-15/M-16; V-01/PF-01 elaborate M-14. This mapping is planning lineage, not duplicate completion credit.

Display result recovery update (2026-10-28): the staged TableService settings
response path now retains original results atomically with owner publication.
Append-only v004 supplies immutable result guards and complete 89-step dependency
verification. Deployment review, remaining owner results/lifecycle, API/runtime
binding and operational acceptance still gate DM-01.3b/c/d/e. PostgreSQL/River
remain configured; the new artifact is not production acceptance.

Presentation owner update (2026-10-28): Table/Field presentation now retains typed
original results atomically with publication and uses narrow presentation-column
updates with a tested restricted account policy. Full selected MySQL race/vet and
synchronized eight-connection replay checks pass. Existing DM-01.3b/c parents
remain unchecked pending remaining owner/lifecycle/API/runtime/deployment work.

Physical owner results update (2026-10-28): original Field responses now commit
with prepared native creation/nullability/numeric-uniqueness publication and
readiness. Lost commits, receipt failure/recovery, historical response stability
and explicit lock-busy recovery passed. Runtime authorization additionally depends
on auth-owned AU-01.2/3 (currently NOT_STARTED); data-model calls the PostgreSQL
identity.authorize_data_model_v1 routine. No runtime activation or parent
completion is implied. See the existing DM-01.3b/c coverage matrix and evidence.

Decision stages 1–2 implementation checkpoint (2026-10-30): the target-only
execution HTTP component, bounded calendar scheduler, complete prepared
publication/model binding and receipt-aware callback/four-effect workers are
implemented. The approved durable intent/provider-reconciliation contract adds
an optional 149-step append-only lineage. Local pinned-engine correctness adds
source/target effects/browser corpora, real callback crash/rescue and lost-commit
acknowledgement reconciliation, cancellation races and restricted worker checks.
Cold prerequisites use exact migration definitions, mandatory owner health
contracts, distinct inspection/runtime pool binding and the shared connection
budget. These are decision-owned components, not live provider implementations.
The module checklist records precise scoped verification and remaining public
collection/recurring-admin adapters, real providers, release/grants rehearsal and
approved activation. D13–D15 and D17–D25 remain open at those integration/release
gates; performance/load/query-plan acceptance and Aurora remain excluded.

Decision public API/application checkpoint (2026-10-30): collection/status-summary
and prepared calendar schedule administration now join the existing execution
APIs. The explicit inactive application graph composes same-scope stores,
evaluator, admission/publication, all seven delivery kinds, receipt-aware
maintenance, bounded tenant/worker inventory and joined shutdown. Cold/hot
readiness and a fail-closed backend guard prevent an accidental runtime switch.
Scoped MySQL 8.4.8 HTTP/SQL/race, PostgreSQL/source regressions, whole-module
compile and affected vet passed. The source artifact now tracks 166 files;
PostgreSQL catalog/proposed mapping are unchanged. Auth/case/screening owners,
complete fresh model delivery, receipt providers and command activation remain
blocked; the user explicitly deferred additional auth action adapters. No
foreign-owner migrations, live activation or performance acceptance occurred.
See the module's R1–R10 statuses and
[verification evidence](evidence/2026-10-30_decision_public_api_and_runtime.json).
