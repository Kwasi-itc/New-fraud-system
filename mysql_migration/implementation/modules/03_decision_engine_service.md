# Decision evaluation, workflows and historical reads

Status: **NOT COMPLETE**. Only the explicitly named active subtask below is currently in progress. Source-inventory choices 1–9 approved by the user; cross-owner contracts are reviewed when their implementation is reached. Staged MySQL authoring, decisions/evidence, comparisons, configuration and historical-read components exist. Full owner transaction/job composition and runtime activation remain pending. Primary task: DE-01; prerequisites: IN-01, DB-01, Q-01, SC-01. Findings: PG-08/10/15/20/25/26/32/34.

## Verified execution path

[Source entry point](../../../backend/decision-engine-service/internal/service/decision_service.go#L366). EvaluateScenario evaluates AST/rules and historical data before its persistence transaction; persistence writes decision/evidence/workflow/screening/scoring/outbox plus jobs. Direct_db and ingestion_http tenant readers must agree. Scenario/workflow authoring and screening projection are additional mutation/read paths. Full source occurrences are in the [audit catalog](../../11_source_evidence_catalog.md).

## Target implementation

Preserve this evaluation consistency contract rather than claiming a new global snapshot. Differentially test AST aggregate pushdown and HTTP fallback. Keep persistence and all seven decision job kinds atomic. Use one read snapshot and bounded ordered batch assembly for ListStructured, preserving the 500-parent cap. Replace source reconciliation with the owner routine in P-04, retaining monotonic version and relationship checks. Decision search/cursors require explicit case/null/order semantics and measured indexes.

Use the common [schema contract](../04_schema_and_data_access_design.md), [security requirements](../05_security_and_integrity_requirements.md) and [transaction protocols](../07_transaction_and_recovery_protocols.md). These are required acceptance inputs, not already implemented helpers.

## Implementation checklist

- [ ] **DE-01.1 — IMPLEMENTATION / ACCEPTANCE REMAINS:** The refreshed source drift artifact contains 166 production/migration source files under its declared inventory patterns. The separate fresh-PG catalog/proposed target mapping covers all 29 owner tables, 284 columns, 73 constraints, 71 indexes, two routines and one trigger, with source/query associations and proposed owner permissions. The user approved choices 1–9 for fresh development installation (no legacy backfill). Complete executable target definitions, deployment grants and full semantic/differential acceptance remain open; structural coverage is not release approval.
- [ ] **DE-01.2 — IMPLEMENTATION / ACCEPTANCE REMAINS:** Portable transaction/enqueue boundary covers all seven kinds, with explicit failures and tenant/resource binding. Staged MySQL authoring, decision/evidence, platform, configuration, structured workflows, custom lists, execution/outbox, durable transport and registry-reader components exist. Four-effect domain retry/redrive, approved dispatch cancellation and local owner maintenance now have scoped pinned-engine tests. Main owner/job composition, real provider/worker identities, external handlers and runtime integration remain pending. HTTP mode/runtime selection is unchanged.
- [ ] **DE-01.3 — IMPLEMENTATION / ACCEPTANCE REMAINS:** Staged scenario/rule authoring, clone/publication atomicity, history search/cursors, rule snoozes and ordered configuration arrays exist. Complete workflow authoring/audit and reconciliation remain open.
- [ ] **DE-01.4 — IMPLEMENTATION / ACCEPTANCE REMAINS:** Service baseline, PostgreSQL atomicity, scoped MySQL 8.4.8 adapter/differential/concurrency and targeted restricted-boundary tests have passed. Platform/rescue plans use 5,000 synthetic rows with hot/selective keys; the current-attempt lookup is also tested with 5,000 historical receipts. Complete cross-engine business/browser/effect corpus, application-scale plans/load and release grants remain outstanding.

## Portable preparation (2026-10-29)

- [x] Source drift inventory and grouped ownership/access contracts: [inventory and remaining semantics](03_decision_source_inventory.md).
- [x] Services/mutation ports do not import pgx/River; one typed owner `JobRequest` contract covers the seven operations. PostgreSQL stores bind tenant/resource on the caller transaction before enqueue. An AST contract test guards the boundary.
- [x] Removed required no-op producers and non-atomic retry fallbacks; queue-client errors propagate through router/app initialization. Optional-outbox disabling still creates neither event nor job and does not disable other work.
- [x] Completion/failure plus callbacks and enabled lifecycle outbox writes share a short transaction. Failed persistence recovery is tenant- and attempt-conditional and preserves newer/committed terminal states; this is not a full exactly-once/stale-worker protocol.
- [x] Added isolated real-PostgreSQL fixture, all-kind enqueue/tenant/schedule tests, owner/job rollback tests and callback failure recovery tests; baseline checks actual scores, outcomes and rule evidence.
- [x] API/worker/migrator use explicit `public` River schema selection. API and worker startup check pinned queue lineage, objects and runtime privileges; `/readyz` rechecks and fails closed, including when no pool is configured. These bounded catalog checks do not migrate/repair objects or establish complete schema-drift compatibility.

Parent tasks remain open. Existing retained-argument River deduplication needs an explicit retry/redrive identity review before target lifecycle acceptance; full queue schema-drift compatibility remains separate from readiness. Auth, ingestion/model registry and screening projection target contracts remain dependencies. No MySQL runtime switch or Aurora testing is included.

## Review checkpoint: approved sections 1 and 2

- [x] Implemented the three platform adapters and complete 17-column platform mapping; no successful placeholders for unimplemented decision repositories.
- [x] Explicit source/target risk absence contract and deterministic latest-risk tie order; arbitrary storage errors and successful nil-without-absence are not a no-risk result.
- [x] Fresh-catalog extraction and drift-checked proposed mapping for the entire decision owner, retaining full source defaults, constraints, expression/partial indexes, routine bodies and trigger definitions.
- [x] Source/query candidate associations, scalar/array/JSON/time conversion proposals, source-owner DML requirements and separate cross-owner permission/dependency contracts.
- [ ] Frozen release definitions and complete release acceptance remain open. Pinned MySQL 8.4.8 is now available and scoped suites passed; the earlier credential-helper download blocker is resolved. Older 9.7.1 evidence remains exploratory only.
- [x] User approved source-inventory choices 1–9. Cross-owner contracts/grants remain a just-in-time review gate, not permission to bypass owner mutation rules.

## Granular execution plan (five tasks per batch)

### Status rules and active work (updated 2026-10-30)

- **IMPLEMENTATION IN PROGRESS — specific work:** only work being performed now, with the exact active subtask named. Change it when switching tasks; do not leave it active when handing off.
- **IMPLEMENTATION REMAINS — not active:** some code exists, but implementation is unfinished and is not currently being worked on.
- **IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** the listed code and its scoped tests are finished. This does not imply complete runtime integration or release approval.
- **IMPLEMENTATION FINISHED; EXTENDED TESTING REMAINS:** no known feature-code gap within the listed scope, but the stated broader acceptance tests remain.
- **INTEGRATION REMAINS / TEST IMPLEMENTATION REMAINS — not active:** identify remaining wiring or test development instead of calling it generic progress.
- **AWAITING TESTING — specific suite:** use only after the relevant implementation/test code is finished and the named execution is outstanding.
- **TESTING IN PROGRESS — specific suite:** use only while that suite is actually running.
- **AWAITING DEPENDENCY — named provider/tooling:** distinguish the blocked integration/acceptance item from independent implementation that can continue.

Current active work: **None — decision-owned verification/release-preparation pass handed off**. Foreign-owner implementations and the additional auth adapters remain excluded. No EC2/application target or approved coordinated cutover is available. Actual deployment/activation remains blocked by real providers, production release/inspection approval and the shared installer's missing same-session pre-write target guard. Performance acceptance remains deferred.

### Decision-owned verification / release / activation-safeguards pass

| Subtask | Status | Scope |
|---|---|---|
| V1 | FINISHED — decision-owned release audit | Identified independent correctness/grant/recovery work; shared-owner activation and inspection-policy gates retained |
| V2 | EXPANDED CORRECTNESS / RACE SUITES PASSED; FULL BUSINESS/PROVIDER PARITY REMAINS | Actual decision thresholds/evidence/effects/rollback under API grants; 93 other owner tests plus portable/source/HTTP/application regressions. Full end-to-end provider and business differential acceptance is not claimed |
| V3 | ARTIFACT VERIFIER FINISHED; SCOPED TESTS PASSED; PRODUCTION PROMOTION REMAINS | Canonical checksummed artifact, exact source SQL/149-step lineage, pinned version and externally approved identity binding; production definitions not frozen/promoted |
| V4 | OWNER GRANT PLAN / SCOPED REHEARSAL PASSED; DEPLOYMENT POLICY REMAINS | Explicit exhaustive API/worker DML, narrow immutable-key locking privileges, four-effect dispatch and forbidden dictionary/foreign/DDL/history writes. Foreign grants, all-path restricted-role acceptance and production inspection policy remain |
| V5 | DECISION-LINEAGE DISPOSABLE REHEARSAL PASSED | Fresh/140→145→149 upgrade, dirty-before/after DDL recovery, repeat apply, checksum/drift rejection, wrong writer/unlocked definer and unrelated-owner preservation. Not complete shared-stack cutover/restore acceptance |
| V6 | READ-ONLY DEPLOYMENT PREFLIGHT / RUNBOOK FINISHED; SCOPED TESTS PASSED | Verified TLS, exact writer/version/locked definer, pinned-session inspection and build/source binding. Production apply intentionally unavailable until shared installer provides same-session pre-write guard; image build/promotion not performed |
| V7 | AWAITING DEPENDENCY — shared installer, real owners and deployment approval | No foreign implementation in this pass. Needs approved target/window, frozen/promoted release, grant/inspection policy, writer fencing/recovery evidence and coordinated activation |
| V8 | FINISHED — LOCAL VERIFICATION / SOURCE ATTESTATION | Expanded race suites, whole-module compile, executable build, affected vet and identical host/runner source hash; Docker packaging and performance excluded |

Evidence: [decision-owned verification and release rehearsal](../evidence/2026-10-30_decision_release_verification.json).
The [owner release runbook](../../../backend/decision-engine-service/README.md#decision-owned-release-verification-and-rehearsal-2026-10-30)
records exact tool capabilities, grant/inspection limits and coordinated activation gates.

### Public API / application / owner-integration pass

| Subtask | Status | Scope |
|---|---|---|
| R1 | IMPLEMENTATION FINISHED; SCOPED MYSQL RACE TESTS PASSED | Summary-only keyset pages; SQL status/attempt filters; tenant/scenario/filter-bound cursors |
| R2 | IMPLEMENTATION FINISHED; SCOPED HTTP/SQL RACE TESTS PASSED | Collection/status-summary HTTP adapters and strict query validation |
| R3 | IMPLEMENTATION FINISHED; SCOPED MYSQL RACE TESTS PASSED | Prepared recurring-schedule create/replace/disable with explicit concurrency preconditions |
| R4 | IMPLEMENTATION FINISHED; SCOPED HTTP/SQL RACE TESTS PASSED | Recurring-schedule HTTP adapters, real publication/admission and correctness tests |
| R5 | IMPLEMENTATION FINISHED; SCOPED APPLICATION TESTS PASSED — activation blocked | Explicit inactive application graph and backend fail-closed guard; no live command switch |
| R6 | IMPLEMENTATION FINISHED; SCOPED APPLICATION / SQL TESTS PASSED — activation blocked | Seven delivery kinds, scheduling, receipt-aware maintenance and joined shutdown |
| R7 | DECISION CONSUMER COMPOSITION FINISHED; OWNER INTEGRATION BLOCKED | Same-scope model registry, screening source and optional trusted browser consumers; no fabricated providers |
| R8 | AWAITING DEPENDENCY — auth/case/screening/receipt providers | Foreign MySQL implementations absent; user deferred expanded auth adapters to the auth integration pass |
| R9 | SCOPED RACE / SQL CORRECTNESS TESTS PASSED; BROADER ACCEPTANCE REMAINS | HTTP/SQL correctness, rollback, isolation, startup failure, cancellation and recovery tests |
| R10 | FINISHED — compile/vet/source attestation/evidence | Whole-module compile, affected vet, refreshed 166-source artifact and matching host/runner hash |

Evidence: [public API / inactive application verification](../evidence/2026-10-30_decision_public_api_and_runtime.json).
Implementation details and the target-only public contracts are in the
[latest README checkpoint](../../../backend/decision-engine-service/README.md#inactive-public-api-and-application-composition-2026-10-30).
Missing foreign owners, additional auth adapters (explicitly deferred), complete
fresh published-model delivery and real receipt-provider conformance remain
dependencies. Only generated private test databases/roles were changed. This
is not frozen release/grants acceptance or an approved runtime switch.

### Stages 1–2 completion pass

| Subtask | Status | Scope |
|---|---|---|
| S1.1 | IMPLEMENTATION FINISHED / SCOPED LOCAL TESTS PASSED — runtime blocked | Opt-in create/single-status/wait/cancel/redrive routes; strict decoding, trusted request factory and safe reconciliation responses. Admission is separately SQL-tested; not a full public deployment rehearsal |
| S1.2 | IMPLEMENTATION FINISHED / LOCAL TESTS PASSED — runtime blocked | Bounded calendar/interval scheduling loop, actual owner occurrence transactions, pagination, required-job rollback and concurrent ticks |
| S1.3 | IMPLEMENTATION FINISHED / LOCAL TESTS PASSED — runtime blocked | Full AST/model preparation, trigger/aggregate/related registry gates, parent/model fencing and no provider IO under locks |
| S1.4 | IMPLEMENTATION FINISHED / LOCAL TESTS PASSED — real providers blocked | Five receipt-aware dispatch kinds; approved durable intent/idempotency/reconciliation contract, no legacy send-only fallback |
| S1.5 | SCOPED CORRECTNESS TESTS PASSED — broader provider/deployment acceptance remains | HTTP, scheduling, publication, dispatch, rollback and recovery fixtures on MySQL 8.4.8 |
| S2.1 | SCOPED SOURCE/TARGET CORRECTNESS PASSED | Actual PostgreSQL/MySQL effect/browser SQL corpora plus authoring/execution/wallet-operator comparisons and portable evaluator baseline; not complete provider parity |
| S2.2 | SCOPED RECOVERY / RACE TESTS PASSED | Callback crash/rescue, cancellation/finish races, stale fences, real committed transaction with simulated lost acknowledgement, and denied reconciliation without resend |
| S2.3 | SCOPED SECURITY TESTS PASSED — production grants/owners remain | Restricted dispatch worker, HTTP path tenant checks, revocation and missing providers; fixtures are not real auth/case providers |
| S2.4 | IMPLEMENTATION FINISHED / LOCAL TESTS PASSED — deployment prerequisites blocked | Exact 149-step lineage/shape, mandatory owner health checks, actual runtime/inspection target and pool binding, shared connection budget |
| S2.5 | FINAL LOCAL VERIFICATION FINISHED | Race-enabled packages and selected SQL regressions, full compile, affected vet, refreshed 165-source artifact and matching host/runner attestation; performance excluded |

Evidence: [stages 1–2 local implementation/correctness record](../evidence/2026-10-30_decision_stage12.json).
Only generated disposable test databases/roles were changed. No application
database, EC2/Aurora target, configured backend, API route registration or live
worker activation was changed. Parent D13–D15 and D17–D25 remain unchecked where
real owner providers, remaining collection/recurring-admin adapters, full public
composition, frozen release/grants rehearsal or approved activation are needed.

### Worker integration — next five-task batch

| Subtask | Status | Scope |
|---|---|---|
| W1 | SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED | Scheduled and async worker compositions, explicit policies and shared poller/supervisor; no runtime activation |
| W2 | SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED | Exact prepared identity/lease binding and atomic whole-batch completion/result/callback |
| W3 | SCOPED IMPLEMENTATION FINISHED; PURE/MYSQL TESTS PASSED | Exact permanent receipt/owner reconciliation after ambiguous completion; no automatic replay |
| W4 | SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED | Redacted failure transitions, cancellation, domain versus transport retries; real service authorization still required |
| W5 | SCOPED TESTS PASSED; BROADER ACCEPTANCE REMAINS | Actual claim, whole-batch/evidence/effects, callback rollback, cancellation, invalid input, live selection, scope and pure uncertain-result branches |

### Admission and API integration — next five-task batch

| Subtask | Status | Scope |
|---|---|---|
| A1 | SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED | Reauthorize and fence prepared model/scenario binding in one execution/job/event transaction |
| A2 | SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED | Bounded manual scheduled and async admission; concurrent replay without duplicate jobs/events; original replay survives model changes |
| A3 | SCOPED IMPLEMENTATION FINISHED; PURE/SQL TESTS PASSED | Bounded committed-state waiting, zero-wait, cancellation and expiry without retained SQL transactions |
| A4 | SCOPED IMPLEMENTATION FINISHED; PURE/SQL TESTS PASSED | Explicit expected-generation redrive; callback-only preservation, stale/foreign/revoked authority checks |
| A5 | SCOPED TESTS PASSED; EXTENDED ACCEPTANCE REMAINS | Admission races/replay/tenant/queued-event rollback and waiting/redrive; full HTTP/provider/restricted deployment remains |

### Evaluation reader composition — next five-task batch

| Subtask | Status | Scope |
|---|---|---|
| C1 | SCOPED IMPLEMENTATION FINISHED; RACE TESTS PASSED | Narrow read-only evaluation ports; existing full source repositories remain compatible |
| C2 | SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED | Actual same-scope configuration/history/platform/list read facades, no nil mutation methods |
| C3 | SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED | Explicit target evaluator/preparer construction with validated dependencies, enabled pushdown and policy |
| C4 | SCOPED MYSQL RACE TESTS PASSED | Preparation/worker/admission fixtures now use actual read components; platform/list values, absence, foreign/revoked authority and invalid scope/policy verified |
| C5 | SCOPED VERIFICATION FINISHED; BROADER ACCEPTANCE REMAINS | Source service and selected persistence regressions, explicit manifest refresh, race/vet/whole-module compile and attested evidence passed |

### Worker/admission/composition checkpoint (2026-10-30)

W1–W5, A1–A5 and C1–C5 now have scoped implementation and correctness coverage.
`ScheduledWorker` and `AsyncWorker` use the actual owner/transport stores and
shared supervised poller. They bind prepared identity to the actual lease,
persist whole batches/result/required callbacks and enabled lifecycle outbox
events atomically, and distinguish domain retries from confirmed-persistence
transport retries. Uncertain completion consults the exact permanent receipt
and current owner; an unconfirmed outcome never authorizes automatic replay.
Actual poller completion/shutdown joining is tested. Real trusted service
identity remains a provider dependency, not inferred from receipt decoding.

Async live selection uses bounded batched scenario/iteration/rule metadata and
shared per-table registry preflight; no metadata query per scenario. Total
fan-out/output budgets are explicit. Every item's outputs commit together;
the source's final-item-only result envelope is retained, not silently changed.
The source contract still does not promise a global historical-data snapshot.

Actual admission rechecks action authority, live binding and published model
gates on the owner/job/lifecycle-event transaction, after model/payload/AST IO.
Concurrent original-key submissions create one owner and required delivery;
replay does not emit another queued event. Replays survive later model changes.
Stored-record requests retain omitted fields in durable JSON. Ambiguous creation
and waiting errors retain an opaque execution ID for authorized reconciliation,
including when the original client supplied no idempotency key.

Target submission, bounded waiting and expected-generation redrive adapters now
exist; public routes are NOT registered. Waiting holds no SQL transaction while
sleeping and reports inline completion only from an authorized committed read.
The target evaluator now uses real scoped read facades over configuration,
history, platform and list stores through narrow read-only ports. It no longer
needs the earlier test-only nil-embedded read adapters.

The remaining work is still substantive: actual auth/model/case/screening owner
providers and grants; public HTTP/runtime composition; callback and four-effect
external business dispatch; calendar-loop/runtime integration; complete business/
browser/fault differential acceptance; frozen release/readiness/grants/upgrade
rehearsal. Those are NOT completed by passing component tests. No main runtime,
public route, worker command, application database or migration SQL is switched.
Performance/load/query-plan acceptance remains deferred. Parent tasks remain
open; this checkpoint does not convert 15 scoped subtasks into 15 closed parents.
Evidence: [workers, admission and read composition](../evidence/2026-10-30_decision_workers_admission_and_readers.json).

### Request preparation and pinned scheduling — current five-task batch

| Subtask | Status | Scope |
|---|---|---|
| P1 | SCOPED IMPLEMENTATION FINISHED; PURE RACE TESTS PASSED | Count/byte/identity/type limits, detached payload/model copies, exact IDs including spaces, independent repeated-ID items, all-or-nothing validation |
| P2 | SCOPED IMPLEMENTATION FINISHED; MYSQL 8.4.8 RACE TESTS PASSED | Authorized owner/scenario/committed iteration/rule reads; scheduled running attempt/generation and pinned old iteration, never live-cache redirection |
| P3 | SCOPED IMPLEMENTATION FINISHED; MYSQL 8.4.8 RACE TESTS PASSED | Fresh bounded model, real trigger/aggregate registry checks, static AST validation and closed/stale publication rejection; real owner provider still required for activation |
| P4 | SCOPED IMPLEMENTATION FINISHED; PURE/MYSQL RACE TESTS PASSED | Actual shared evaluator and local persistence body; opaque single-use output plans before any writes; no cross-item cache contamination; caller must use fenced atomic completion |
| P5 | SCOPED CORRECTNESS VERIFICATION FINISHED; BROADER ACCEPTANCE REMAINS | Publication switch with one-connection pool, pinned score/evidence/ID, complete rollback, stored/empty/bounded candidates, authorization/tenant/model failures; source regressions/compile/vet pass; no performance suites |

### Steps 1–2 checkpoint (2026-10-30)

`internal/mysqlexecution.Preparer` composes the scoped SQL execution owner,
registry reader and real evaluator. Request preparation is read-only evidence,
not durable admission: creation must recheck identity/live/model bindings in its
own short mutation transaction. Requests without an explicit scenario validate
the payload/model now; all-live selection remains an execution-time operation.

Scheduled preparation loads the actual running owner with expected tenant,
attempt and generation, then uses its stored committed iteration and rules.
Model resolution, stored/candidate reads and AST work run outside owner write
transactions. SQL metadata preparation uses a short READ COMMITTED transaction
because request/run authority may lock identity rows; it does not misuse the
strictly nonlocking read-only Snapshot helper or promise a global data snapshot.
The selected committed iteration/rules are immutable. Registry gates are checked
again by each actual data query; these checks are not a global historical-read
snapshot. Final completion still must recheck the lease/token/expiry.

Pinning freezes the iteration's trigger formula, rules and thresholds, not every
mutable parent setting or the data model. The parent trigger object type and
scenario-level effects/routing remain current under the existing schema;
explicit item types and formulas must validate against the current published
model. The new code does not claim a historical snapshot of those other settings.

`service.PreparedDecision` shares the real live evaluator/output builder and
local persistence body, but preparation does not call a transaction manager.
`PreparedScheduled.PersistAll` returns provisional results on the caller's
transaction; it neither commits nor acknowledges the owner. A failed/uncertain
commit requires reconciliation/repreparation, not reuse of single-use plans.
Step 3 must bind the prepared identity to the actual worker lease and compose
whole-batch completion, never the legacy commit-per-item loop.

Pure and pinned MySQL correctness tests verify no preparation writes, exact
identity/model detachment, limits and invalid input, missing/revoked/foreign
authority, closed/stale registry, empty/stored/bounded candidates, old-version
score after a concurrent new publication and all-output rollback. A one-open-
connection test confirms model/provider publication does not run while a SQL
preparation transaction is retained. Actual production owner providers, worker
loops, durable admission, HTTP wiring, performance and release acceptance remain
separate. Evidence: [request and pinned scheduling](../evidence/2026-10-30_decision_request_and_pinned_preparation.json).

### Remaining completion roadmap

| Batch | Status | Scope / completion gate |
|---|---|---|
| B3 / D18,D19,D21,D22 | WORKER/ADMISSION/READ COMPOSITION SCOPED TESTS PASSED; PROVIDER/HTTP RUNTIME REMAINS | Atomic outputs/jobs/results/events, actual supervised scheduled/async workers and admission/wait/redrive components tested; live route/provider wiring and external dispatch remain |
| B4 / D13,D17,D18,D20 | REQUEST/PINNED PREPARATION AND BROWSER COPY TESTED; PUBLICATION/PROVIDERS REMAIN — not active | Fresh model/registry/request/pinned evaluation composition and audited browser copying tested; complete publication preparation, index/API activation and real provider wiring remain |
| B5 / D21–D23 | EVALUATION WORKERS SCOPED TESTS PASSED; EXTERNAL DISPATCH/PROVIDERS REMAIN — not active | Scheduled/async supervised composition exists; callback/four-effect business dispatch, calendar runtime and real trusted identities/providers remain |
| B6 / D15,D20,D24 | TEST IMPLEMENTATION / ACCEPTANCE REMAINS — not active | Full score/threshold/effect/browser comparisons and concurrent/crash/uncertain-commit acceptance |
| B7 / D14,D15,D24,D25 | SECURITY/READINESS IMPLEMENTATION REMAINS; PERFORMANCE TESTING DEFERRED BY USER | Continue restricted installation/readiness and connection-configuration safeguards; no performance acceptance, load or query-plan testing in this pass |
| B8 / D25 | RELEASE PREPARATION / ACCEPTANCE REMAINS — not active | Frozen migration references, fresh/upgrade/interrupted-step/recovery/grants rehearsal and release evidence; deployment requires separate approval |

| B3 subtask | Status | Scope |
|---|---|---|
| B3.1 | SCOPED IMPLEMENTATION FINISHED; MYSQL 8.4.8 CORRECTNESS TESTS PASSED | Same pool/schema/tenant/budgets; all requested action authorization before owner locks; no legacy unfenced execution adapter |
| B3.2 | SCOPED IMPLEMENTATION FINISHED; MYSQL 8.4.8 CORRECTNESS TESTS PASSED | Decision/effect/list and execution repositories share one transaction; actual DecisionService consumes narrow persistence port |
| B3.3 | SCOPED IMPLEMENTATION FINISHED; MYSQL 8.4.8 CORRECTNESS TESTS PASSED | Required-job/owner completion, shared error poisoning, pre/post lease and scope checks tested; full prepared request-item binding remains |
| B3.4 | SCOPED READ/CANCELLATION ADAPTERS FINISHED; PURE/MYSQL TESTS PASSED; ADMISSION/ROUTE INTEGRATION REMAINS — not active | Target generation/cancellation/replay DTOs; strict expected-generation decoding and authorized reads/cancellation; actual tenant/scenario, callback rollback and revocation tested; creation/admission, wait adapter and runtime routing remain |
| B3.5 | SCOPED SERVICE SQL/REGRESSION TESTS PASSED; FULL DIFFERENTIAL REMAINS — not active | Actual service thresholds/evidence/effects/job rollback, mixed list/execution rollback and fenced completion; no performance or query-plan suites |

### Combined persistence and browser-copy checkpoint (2026-10-30)

- The actual `DecisionService` now uses a narrow portable decision transaction
  capability. Explicit target errors never fall back to PostgreSQL. The target
  unit of work reuses existing repositories and combines required effect and
  execution jobs on one transaction; ignored job/capability failures poison it.
- MySQL 8.4.8 tests cover list/decision/job atomicity, missing capabilities,
  denied authority, atomic async result/callback, duplicate completion, owner
  binding and a lease expiring after decision writes. An actual service SQL
  corpus covers 0/9/10/20/30 score boundaries, four-rule evidence, configured
  workflow/screening/scoring effects, outbox and required-job rollback.
- `BrowserWorkflowStore.Copy` now writes attributed copy audit records in the
  same transaction as copied graphs. The approved <=500 inbox batch consumer
  fails closed without its case provider. Bounded tag batches, denied/inactive
  permissions, tenant isolation, destination caps, concurrent append priorities
  and injected audit failure are tested. Save/delete prepare permissions before
  owner locks, then reload/fence old versions; browser regression tests pass.
- Target-only execution read/cancel adapters preserve generation/cancellation
  and replay fields. Strict bounded cancellation input requires an explicit
  generation and rejects duplicate/unknown keys. Pure and actual SQL adapter
  tests pass, including tenant/scenario checks, stale generation, replay,
  revocation and callback enqueue rollback. These do not implement public
  creation/admission, inline waiting or live route activation.
- Twelve selected adapter tests passed with race detection, alongside the full
  service/ports/target-boundary packages; affected vet and whole-module
  compile-only passed. The source inventory was explicitly refreshed from a
  disposable PostgreSQL fixture: source-file hashes/references changed, not the
  source schema catalog. Performance/load/query-plan tests were not run.

Evidence: [combined persistence and browser copy](../evidence/2026-10-30_decision_composition_and_browser_copy.json).
Parent tasks remain open: prepared request-item binding and pinned scheduled
evaluation, full cross-engine business/browser comparison, real auth/case/
screening providers, external handlers and worker/runtime composition, grants,
readiness and release rehearsal are not completed by these scoped tests.

### Current five-task batch — four-effect lifecycle

| Subtask | Status | Scope |
|---|---|---|
| B2.1 / D21,D23 | SCOPED MYSQL 8.4.8 RACE TESTS PASSED | Source-state audit, bounded latest-receipt lookup, explicit owner reset and required successor job in one transaction; stale-worker fences |
| B2.2 / D21,D23 | SCOPED MYSQL 8.4.8 RACE TESTS PASSED | Separately authorized, expected-identity redrive; preserve permanent receipts, monotone counters and reject successful/foreign-owner outcomes; real trusted provider/runtime adapters remain |
| B2.3 / D23 | SCOPED MYSQL 8.4.8 RACE TESTS PASSED | Approved exact-attempt decision-owned cancellation record; atomically cancel pending/in-flight transport, preserve results, never claim to undo external delivery |
| B2.4 / D23,D24 | SCOPED MYSQL 8.4.8 RACE TESTS PASSED | Shared maintenance scheduler with real local decision-owned dispatch-exhaustion mutations and bounded progress; external handler/provider/runtime integration remains |
| B2.5 / D24,D25 | SCOPED VERIFICATION FINISHED; EXTENDED ACCEPTANCE REMAINS — not active | All 86 adapter tests, metadata/AST/identity/recurrence, exact 140→145 migration and affected vet passed; source attestation/teardown finished |

Current B2 evidence: [four-effect lifecycle and pinned-engine verification](../evidence/2026-10-30_decision_effect_lifecycle.json). The optional cancellation plan now has 145 steps and preserves the exact earlier 115/140 prefixes. Fresh/repeated and exact 140→145 disposable installations passed. This does not complete the full release/grants/runtime rehearsal or the open parent tasks.

### Latest verification checkpoint — MySQL 8.4.8 (2026-10-30)

This checkpoint supersedes earlier **AWAITING DOCKER** / exploratory-only wording for the exact tested source and suites. All 78 top-level decision MySQL adapter tests passed in four non-overlapping race-enabled groups (3 new SQL, 13 lifecycle, 3 differential and 59 regressions). Metadata, identity codec, recurrence and complete AST package race tests passed. All allocated physical-publication scenarios, publication composition, populated text-index DDL and selected contract/unit tests passed on the pinned engine. The initial text-index fixture failed because it used the old field label in its DDL plan; the fixture now matches the actual registry binding. Production checks were not weakened. Fresh/replay and scoped prefix upgrades run within these fixtures; a full release migration/privilege rehearsal is still separate.

This earlier B1 checkpoint does **not** complete D13–D15/D17–D25: real provider composition, browser-specific copy auditing, complete main transaction/worker integration, application-scale load, frozen release definitions and deployment acceptance remain. The current B2 table above supersedes its original four-effect retry/cancellation and maintenance deferrals. Evidence for the earlier source: [completion batch and pinned-engine tests](../evidence/2026-10-30_decision_completion_batch_01_and_mysql848.json).

B1.1–4 scoped implementation and pinned MySQL 8.4.8 race tests are finished locally. Real provider/main runtime composition and the other open D tasks remain unfinished. The 140-step unreleased decision plan preserves its earlier 115-step prefix; passing current scoped fixtures is not complete release acceptance. No runtime activation, EC2 access, deployment, commit or push was performed.

### Approved completion roadmap — staged implementation and local verification

| Subtask | Status | Scope |
|---|---|---|
| B1.1 / D13 | IMPLEMENTATION FINISHED; SCOPED MYSQL 8.4.8 RACE TESTS PASSED | Verified physical publication persists mapping deltas; metadata-only paths retain mappings and version/receipt semantics; API/job activation remains separate |
| B1.2 / D13 | SCOPED IMPLEMENTATION/TESTS FINISHED | Static aggregate template and caller-transaction registry capability checks passed; real full preparation/provider composition remains |
| B1.3 / D17 | SCOPED IMPLEMENTATION/TESTS FINISHED | Bounded scenario/workflow snapshot copying and guarded live publication passed; browser-specific audit/provider acceptance remains |
| B1.4 / D18 | SCOPED INTEGRATION/TESTS FINISHED | Explicit read/mutate list authority shares the staged decision/effect/job transaction; main TransactionManager remains open |
| B1.5 / D13,D17,D18 | SCOPED REGRESSION TESTS PASSED | Pinned SQL rollback/isolation/registry cases, pure tests and scoped vet passed; not full application acceptance |

`DeliveryMaintenanceStore` composes the shared bounded scheduler with authorized rescue/cleanup primitives and scoped cursors, without starting it. Pure progress, stale-conflict, partial-commit, cancellation and scope tests passed. The execution rescuer fences expired transport before interpreting changed owner state. B2 adds a real local decision-owned dispatch-exhaustion handler and actual shared-scheduler SQL recovery/cleanup coverage; real identities, external business handlers and runtime poller composition remain open.

Next groups: compose inactive decision/effect/execution adapters (D19/D22/D23); complete public DTO/source adapters and browser-copy auditing; finish business differential, concurrency, grants/readiness and load acceptance (D15/D20/D24/D25). The user has reopened local disposable Docker testing; it is not a blanket blocker. Real auth/case/screening providers remain named integration dependencies, not permission to expand into their implementations. No runtime switch, deployment, or Aurora tests.

D22 is split into these five concrete implementation tasks:

| Subtask | Status | Scope / acceptance note |
|---|---|---|
| D22.1 | IMPLEMENTATION FINISHED; EXPLORATORY DDL TESTS PASSED | Source request semantics, additive execution schema, exact-key idempotency and tenant-consistent constraints |
| D22.2 | IMPLEMENTATION FINISHED; EXPLORATORY REPLAY/ISOLATION TESTS PASSED | Typed bounded scheduled/async create, replay, read and count repositories |
| D22.3 | IMPLEMENTATION FINISHED; SCOPED LEASE/ROLLBACK/CALLBACK TESTS PASSED | Attempt-conditional lifecycle mutations and independent callback state |
| D22.4 | SCOPED CALENDAR IMPLEMENTATION/TESTS FINISHED; BROADER VERIFICATION AWAITS LOCAL ENVIRONMENT | Calendar persistence/transaction and SQL test code finished. Runtime owner adapter remains blocked. Interval schedules are not full source parity |
| D22.5 | SCOPED IMPLEMENTATION FINISHED; AFFECTED-PACKAGE VET/RACE PASSED | Disposable-engine rollback, replay, tenant, concurrency and source-parity tests; evidence/status update |

Calendar follow-up is separated before starting:

| Subtask | Status | Scope |
|---|---|---|
| D22.4-C1 | SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED | Local calendar frequencies, month-end clamp and explicit DST policy; no PostgreSQL runtime change |
| D22.4-C2 | SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED | Bounded calendar occurrence planner and missed-run policy handling |
| D22.4-C3 | SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED | Persist immutable calendar definitions and compose the transactional owner watermark path |
| D22.4-C4 | SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED | UTC/local weekday, leap/month ends, DST and tenant/rollback calendar cases |
| D22.4-C5 | PINNED SCOPED REGRESSION / VET PASSED; RUNTIME INTEGRATION REMAINS — not active | Earlier fixture-I/O failures superseded for the current scoped suites; public adapters/activation remain blocked |

D23's remaining work is separated before implementation:

| Subtask | Status | Scope |
|---|---|---|
| D23.1 | IMPLEMENTATION FINISHED; SCOPED VET/RACE TESTS PASSED | Three execution queues; mandatory tenant/kind worker authority; no handler activation |
| D23.2 | IMPLEMENTATION FINISHED; SCOPED VET/RACE TESTS PASSED | Domain-aware transport failure/exhaustion outcomes and required successor jobs |
| D23.3 | SCOPED MYSQL 8.4.8 RACE TESTS PASSED | Seven-kind expired-lease rescue, concurrency/stale fencing and 5,000-row discovery plans; real handler/runtime composition remains |
| D23.4 | SCOPED MYSQL 8.4.8 RACE TESTS PASSED; REAL PROVIDERS REMAIN | Execution and approved four-effect dispatch cancellation/transport retention tested; public adapters and business/provider composition remain |
| D23.5 | SCOPED SQL TESTS PASSED; EXTENDED ACCEPTANCE REMAINS | Seven-kind transport cases passed; full business-crash/restricted-runtime acceptance remains |

D23.3's implementation group (only the explicitly named row is active):

| Subtask | Status | Scope |
|---|---|---|
| D23.3-R1 | SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED | Bounded, indexed expired-delivery candidates for both fixed queue families, with explicit tenant/kind/rescue authority |
| D23.3-R2 | SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED | Execution resource → receipt → expired-delivery locking; transport retry versus exhaustion; required callback and owner outcome atomically |
| D23.3-R3 | SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED | Four-effect rescue with a mandatory real local owner exhaustion outcome; never invent successful business completion |
| D23.3-R4 | SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED | Heartbeat/rescue races, stale tokens, cross-tenant scope, callback-enqueue rollback and exhausted claims across all seven kinds |
| D23.3-R5 | SCOPED MYSQL 8.4.8 PLANS / RACE TESTS PASSED | Real rescue-query EXPLAIN over 5,000 rows for both families; not application EPS or complete crash/load acceptance |

D23.4's approved implementation group:

| Subtask | Status | Scope |
|---|---|---|
| D23.4-C1 | SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED | Target-only contract and schema; no PostgreSQL enum/runtime change |
| D23.4-C2 | SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED | Generation-conditional cancellation of one scheduled execution and its required active delivery |
| D23.4-C3 | SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED | Async cancellation with required cancellation callback; callback-only cancellation preserves evaluation |
| D23.4-C4 | SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED | Bounded authorized 24h/7d terminal-transport cleanup with indexes; keep permanent receipts/owners |
| D23.4-C5 | SCOPED MYSQL 8.4.8 RACE TESTS PASSED; FULL ACCEPTANCE REMAINS | Cancellation/callback/cleanup/rollback tested; full business-crash and restricted runtime acceptance remain |

### Remaining verification queue

Already passed: scoped 140- and 145-step fresh/repeated/prefix installations, exact 140→145 upgrade and metadata before/after guards; seven-kind recovery/cancellation/cleanup fixtures; both 5,000-row rescue-query plans and 5,000-receipt current-attempt lookup; all 86 existing decision-adapter tests and the scoped SQL/AST/source-target differential suites on MySQL 8.4.8. Do not repeat them merely because older evidence describes deferral; rerun for subsequent source changes.

- Complete the frozen release/shared-owner upgrade and dirty-step recovery rehearsal, including the exact prior 115-step deployment and full restricted runtime installation/grants. Scoped constructor fixtures are not this release rehearsal.
- Add/execute broader concurrent real-business cancellation/finish/cleanup and exact-boundary/crash acceptance; the existing seven-kind fixture passes are not full crash certification.
- Extend shared-scheduler acceptance to all real runtime/provider compositions, including uncertain-commit partial progress and business-crash paths. Local decision-owned dispatch exhaustion and actual scheduler SQL recovery/cleanup/cancellation now have scoped pinned-engine coverage; this is not external-handler acceptance.
- Prepare/measure representative cleanup-query plans and application-scale resource/load budgets. Existing scoped plan evidence is not an EPS or throughput claim.
- Complete missing service score/effect/browser differential coverage and real provider/startup/runtime rehearsal after their implementation. Aurora remains excluded; runtime stays inactive.

Current evidence: [B2 lifecycle and pinned MySQL tests](../evidence/2026-10-30_decision_effect_lifecycle.json); [earlier B1 completion batch](../evidence/2026-10-30_decision_completion_batch_01_and_mysql848.json). Historical evidence: [recovery implementation/native checks](../evidence/2026-10-30_decision_delivery_recovery.json), [original cancellation/retention deferral](../evidence/2026-10-30_decision_cancellation_retention.json).

Named dependencies: real auth/case/screening providers and their grants are **AWAITING DEPENDENCY** for integration/activation, not blockers for every local task. The corrected calendar recurrence contract/planning/persistence has now passed scoped MySQL 8.4.8 race tests; its public adapter/runtime composition remains blocked. Pinned MySQL 8.4.8 is installed and scoped suites passed; frozen release definitions, full grants/migration rehearsal and representative application load still remain. Runtime activation and deployment remain unapproved; Aurora testing stays excluded.

Parent DE-01.1–DE-01.4 stay open until their full acceptance criteria pass.
Groups are progress/context-management units, not stopping points. Continue through
D25 without requesting a new go-ahead after each group; retain verification and
explicit implemented-versus-pending evidence as work progresses.
Runtime remains PostgreSQL until complete composition and cutover are approved.

### Batch 1 — scenario storage foundations

- [x] **D01 / DE-01.1 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Record approved semantics and this complete task/dependency breakdown.
- [x] **D02 / DE-01.1–2 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Decision-owned exact-key dictionary and database-enforced scenario-name binding; collision-safe, tenant-scoped and restricted-definer definitions.
- [x] **D03 / DE-01.1–2 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Scenario/iteration/rule/publication schema and optional live-iteration relation, with same-tenant/same-scenario foreign keys and additive migration steps.
- [x] **D04 / DE-01.2 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Narrow per-scenario version allocation and atomic, replay-safe iteration creation; signed INT bounds and explicit rollback/uncertain-commit behaviour.
- [x] **D05 / DE-01.4 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Disposable-MySQL migration, restricted-account, collision, tenant, deletion, rollback and concurrent-allocation tests for D02–D04. Exploratory 9.7.1 passes; pinned 8.4.8 release validation remains D25.

Batch 1 is implemented locally, not selected by runtime or pushed. The foundation
plan has 30 steps (12 tables, two routines, 16 triggers) and preserves the original
three-step platform prefix. See [batch evidence and limitations](../evidence/2026-10-29_decision_batch_01.json).
Main CRUD/authoring services, real identity contracts, installation account
attestation and full target queue composition are not supplied by this batch.

### Batch 2 — authoring repositories

- [x] **D06 / DE-01.2 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Scenario create/get/update/delete and bounded list repositories, mandatory trusted authorization and exact-name conflicts.
- [x] **D07 / DE-01.2–3 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Iteration read/update/commit/clone storage; draft/shared guards serialize against commit, committed content is immutable, cloning preserves stable rule IDs and resets draft identity/version.
- [x] **D08 / DE-01.2–3 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Rule CRUD and bounded ordered batch reads/writes; same-tenant iteration binding, signed scores, formula fidelity and whole-batch rollback.
- [x] **D09 / DE-01.3 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Staged publish/unpublish/history transaction with explicit publish authorization and a mandatory local model/index-preparation guard. Live-link and event writes roll back together. A synchronized concurrent-publication test verifies same-scenario serialization, replacement history and independent publication of another scenario. Real model/index guard composition remains a runtime prerequisite, not a successful placeholder.
- [x] **D10 / DE-01.4 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Real PostgreSQL/MySQL authoring corpus plus authoring concurrency, rollback and ordered-rule tests passed in the exploratory fixture. Approved auth consumer contract is defined/tested; AU-01 provider implementation and activation deliberately remain blocked.

These checked storage/test tasks do not mark DE-01 complete or activate a service.
The authoring plan has 34 steps, preserving the 30-step foundation prefix.

### Batch 3 — decisions and historical reads

- [x] **D11 / DE-01.2 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Staged decision/evidence persistence and bounded ordered batch evidence readback. Tenant/iteration bindings derive from authoritative parent decisions; composite FKs reject foreign-iteration rules. Request/evaluation JSON, typed-nil source behaviour, rollback and tenant tests passed. Required jobs/effects are NOT yet composed with this staged store.
- [x] **D12 / DE-01.2 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Test-run, phantom-decision and phantom-evidence storage; expiry/status fidelity and same-test-run phantom iteration constraints verified. Source service comparison semantics remain unchanged.
- [ ] **D13 / DE-01.2 — SCOPED READER/REQUEST PREPARATION TESTED; PUBLICATION/PROVIDERS REMAIN — not active:** Registry-bound queries, scalar/filter validation, indexed object-ID lookup, optional collision-safe text equality and source/target/HTTP wallet comparisons passed scoped MySQL 8.4.8 race tests. The owner persists text-index mappings through verified physical publication. Target request/pinned preparation now composes fresh bounded models, item/static AST validation and actual trigger/aggregate registry checks. RegistryAggregatePublicationGuard retains its mandatory remaining full publication guard. Real model/provider wiring, complete publication preparation, index-job/API activation and release grants remain open; optional absent indexes are not a performance-readiness claim.
- [ ] **D14 / DE-01.3 — IMPLEMENTATION FINISHED; EXTENDED TESTING REMAINS:** Search/count/cursor/offset repositories and rule snoozes implemented; exact IDs remain byte-exact, including empty strings and trailing spaces. Tests verify approved Unicode case-insensitive/accent-sensitive history search separately from exact IDs. Unhinted indexed plans over 5,000 decisions and 20 scenarios passed for tenant/scenario, exact/hot object and cursor paths; contains search remains a residual filter. Application-scale load and complete parity acceptance remain open.
- [ ] **D15 / DE-01.4 — ACTUAL SERVICE MYSQL CORPUS PASSED; FULL DIFFERENTIAL REMAINS — not active:** Wallet AST/authoring differential tests and evidence rollback/isolation exist. The actual DecisionService now passes MySQL score/threshold/evidence/configured-effect and required-job rollback tests. Full PostgreSQL/MySQL business comparison and broader fault coverage remain; performance/query-plan/load testing is deferred by the user.

### Batch 4 — workflows and other effects

- [x] **D16 / DE-01.2–3 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Workflow and screening/scoring configuration repositories with ordered outcome children and atomic replacement, verified on the exploratory fixture. Storage retains NULL elements; existing public/source `[]string` ports cannot represent an element NULL and fail explicitly rather than discard/coerce it. No historical array import/backfill is needed.
- [ ] **D17 / DE-01.3 — SCOPED AUTHORING/AUDITED BROWSER COPY TESTED; REAL PROVIDERS REMAIN — not active:** Structured/browser editing, version checks, ordered snapshots and audited soft deletion are tested. Browser-specific copying now composes attributed batched audit records and graph writes on one transaction; the approved <=500 inbox write-authorization consumer and bounded tag batches precede owner locks. Permission/active/provider failures, destination caps, concurrent priorities, audit rollback and tenant isolation passed on MySQL 8.4.8. Full cross-engine browser acceptance, destination model/provider composition and real auth/case providers remain open.
- [ ] **D18 / DE-01.2 — SCOPED LIST/EFFECT INTEGRATION TESTED; MAIN RUNTIME COMPOSITION REMAINS — not active:** List storage and source-compatible name/value semantics passed pinned-engine tests. WithCustomLists adds the same transaction to staged decision/effect/job work, with distinct decision.list.read / decision.list.mutate authorization before owner locks. Read-only access cannot mutate lists; list/decision/outbox/job rollback passed. Real auth support/grants for these actions and complete evaluation/main TransactionManager composition remain open.
- [ ] **D19 / DE-01.2–3 — INTEGRATION REMAINS — not active:** Workflow and screening/scoring execution plus optional outbox repositories are implemented/tested in a staged EffectStore. Hidden authoritative iteration bindings enforce decision/scenario/config relationships with existing composite FKs, without rewriting the earlier lineage. Screening source reconciliation is monotonic and rejects legacy unversioned updates once attached. Decision/effect/tag rollback and 120-row batch readback are verified. An explicit QueuedEffectStore now composes the four effect owners with required first-attempt jobs and transaction poisoning on enqueue failure; the persistence-only store remains separately named/documented. This is NOT the live TransactionManager: complete owner/provider and seven-kind lifecycle composition remain open. Missing source providers fail explicitly.
- [ ] **D20 / DE-01.4 — EXPANDED LOCAL CORRECTNESS TESTED; PROVIDER/RELEASE ACCEPTANCE REMAINS — not active:** Actual PostgreSQL/MySQL decision/effect storage and browser save/list/update/visibility/conflict corpora now pass, alongside browser audit/copy/rollback tests. New dispatch worker restricted-grant and permanent-intent guards are separately tested. These corpora use explicit foreign-owner contract fixtures, not real auth/case/screening/dispatch implementations; complete service/provider conformance and deployment grants rehearsal remain open.

### Batch 5 — durable workers and activation prerequisites

- [ ] **D21 / DE-01.2 — SCOPED LIFECYCLE / APPLICATION COMPOSITION TESTED; REAL IDENTITIES BLOCKED — not active:** The strict seven-kind v1 codec and exact `(kind,tenant,resource,domain_attempt,redrive_generation)` reservation contract remain unchanged. Four-effect initial reservations, expected-identity domain retry and separately authorized redrive have scoped MySQL 8.4.8 race tests, including required-job rollback, retained receipts, concurrent producers and historical indexed current-attempt evidence. Both counters remain monotone; redrive does not replenish the domain budget. D22 supplies the other three kinds' lifecycle. The inactive application graph now composes all seven kinds with bounded tenant/worker inventory, receipt-aware maintenance and joined shutdown on one pool/database. Real worker identities, receipt providers and approved activation remain blocked; decoding is not authority.
- [ ] **D22 / DE-01.2 — DECISION EXECUTION APIs / INACTIVE APPLICATION COMPOSITION TESTED; OWNER ACTIVATION BLOCKED — not active:** Opt-in create/detail/wait/cancel/redrive, keyset collection/filter/status-summary and calendar recurring-administration handlers are implemented. Preparation, admission, publication and all seven delivery kinds now have a concrete same-scope application graph, cold/hot readiness and bounded shutdown. Schedule replacement/disable uses exact ID/watermark preconditions; old/new switch is atomic and tick-compatible. No network IO runs under owner locks. Real trusted identity, full published-model provider delivery, provider conformance and approved command/backend activation remain open. The user deferred new auth action adapters; PostgreSQL routes are not a fallback for missing target behavior.
- [ ] **D23 / DE-01.2 — DECISION-OWNED HANDLERS / RECOVERY TESTED; REAL PROVIDERS BLOCKED — not active:** Actual callback/workflow/screening/scoring/outbox dispatch components now require the approved attempt-aware send-intent/receipt/reconciliation contract. SQL tests cover atomic receipt/owner/queue outcomes, lost-local-ack rollback, callback crash/rescue without duplicate send, cancellation during external IO, restricted worker rights and retained proof after transport cleanup. Existing seven-kind recovery/retention and monotone domain retry/redrive remain. Source-managed screening fails closed; real provider idempotency/reconciliation/destination policy and service identities remain dependencies. No external send is claimed reversible or exactly-once.
- [ ] **D24 / DE-01.4 — EXPANDED PINNED CORRECTNESS / RESTRICTED-ACCOUNT TESTS PASSED; COMPLETE ACCEPTANCE REMAINS — not active:** 93 additional owner tests passed with query-plan cases excluded, plus real service thresholds/evidence/effects/required-job rollback under owner API grants and four-effect intent/receipt/ack under worker grants. Source/target authoring/effect/browser/execution/wallet comparisons, HTTP/application readiness, tenant/revocation, cancellation/recovery and portable/source regressions passed race-enabled suites. Full actual-service PostgreSQL/MySQL business comparison, provider conformance and all-path production grant acceptance remain open. Performance/load/query-plan acceptance remains explicitly deferred.
- [ ] **D25 / DE-01.1–4 — DECISION RELEASE TOOLING / LOCAL REHEARSAL PASSED; PRODUCTION RELEASE/ACTIVATION BLOCKED — not active:** Canonical artifact/source/version/image binding, explicit owner API/worker grant plans and read-only TLS/pinned-writer inspection are implemented. Private MySQL 8.4.8 fresh/140→145→149, interrupted-DDL before/after recovery, repeat apply, checksum/drift rejection and unrelated-owner preservation passed. Production definitions/image build/promotion, inspection/foreign grants, complete shared-stack upgrade/restore/fencing, real owners and approved activation remain. The shared runner lacks a same-session caller target guard before writes; no production apply/force/down/activate command is exposed. Aurora and performance remain excluded; no application database/runtime was changed.

The [source inventory and design review](03_decision_source_inventory.md#full-owner-mapping-and-review-checkpoint) explains the unresolved choices. The [machine manifest](../../../backend/decision-engine-service/internal/migrationaudit/testdata/decision-schema-mapping.json) is a review artifact, not a MySQL installation plan.

## Tests and acceptance

POC-10/11, PERF-01/06; reuse internal/service/rule_evidence_integration_test.go, execution_service_integration_test.go, async_validation_integration_test.go, internal/store/postgres/tenant_data_reader_test.go and runtime/ast_eval/aggregate_http_integration_test.go. Add golden full rule outcomes, not merely HTTP 200/202.

No decision threshold changes from numeric coercion. Same logical input/model/rules produces equivalent evidence under the defined read contract. Read mode and worker URL must not silently select stale/separate authority.

## Delivery and rollback dependency

Implement and validate in disposable environments. All shared consumers must be compatible before live activation; an individual module passing its suite does not authorize a partial cross-engine production cutover. Preserve old adapters/compatible API artifacts through rehearsal as needed, with a single chosen runtime authority. The [roadmap](../10_implementation_roadmap.md) owns task dependencies and [cutover plan](../11_data_transfer_and_cutover.md) owns data rollback. Module schema rollback cannot undo externally delivered effects.
