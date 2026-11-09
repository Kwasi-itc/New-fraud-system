# D13–D20 working subtask checklist

Updated: 2026-11-04. This is the **current execution checklist** for D13–D20.
The [main decision-engine checklist](03_decision_engine_service.md) retains the
parent tasks and historical evidence. Cycle 12 records history acceptance, the evaluation fixture and two correctness fixes; its evaluation ramp failed the rate target.

**152 subtasks: 132 checked, 19 active remaining, 1 deferred (D19.19).** These are subtask counts,
not the 25 parent D-tasks. D13, D14, D16, D17 and D18 are complete in their recorded granular scope. D15, D19 and D20 remain open. Runtime activation and full migration acceptance remain separate. Checked items describe verified local scope, including fixtures where explicitly stated.

**Cycle 9 (supporting implementation):** Added a bounded offered-rate load runner and deterministic disposable history fixture, with raw request samples, nine unhinted plans and database/pool snapshots. Five race-enabled tests and vet passed. The smoke used 240 rows and 12 requests; it is not agreed-scale acceptance. No additional leaves are checked: **124/152 complete, 28 remaining**. Numeric targets and scoring/outbox provider choices remain pending; workflow/screening receipt bindings remain implementation work. See [cycle 9 evidence](../evidence/2026-11-03_decision_cycle9.json).

**Cycle 10:** Completed **D19.18**: screening-owned receipt intake and local decision dispatch binding, concurrent deduplication, lost-response reconciliation, atomic source/receipt/projection persistence and preservation of a newer result when publication precedes acknowledgement. Eight top-level tests and 41 subtests passed with the race detector, no failures/skips; both vet runs passed. **125/152 complete; 27 remain.** Workflow binding remains implementation work; scoring/outbox contracts and agreed load targets are pending. See [cycle 10 evidence](../evidence/2026-11-03_decision_cycle10.json).

**Cycle 11 (partial D19.17):** Implemented the real PostgreSQL case owner receipt/reconciliation contract, opt-in HTTP routes and the decision case-workflow adapter. The actual adapter recovered a committed case after its HTTP response was lost. Nine top-level tests and 13 subtests passed with the race detector; both vet runs passed. The staged MySQL case mutation owner and external workflow receiver contracts remain open, so D19.17 is **not ticked**. At the end of cycle 11 the count remained **125/152 complete, 27 remaining**. See [cycle 11 evidence](../evidence/2026-11-03_decision_cycle11.json).

Production release work now has a [granular readiness checklist](03_decision_engine_production_readiness.md) covering migrations, grants, restore integrity and cutover. Its supporting checks do not change the 152-leaf acceptance count.

**Cycle 18:** Critical/shared-owner/recovery checks passed: 62 top-level tests and 165 subtests, race enabled, no failures/skips. Added case producer lease recovery and mixed evaluation/history load with scoring deferred. All three measured evaluation profiles failed capacity acceptance; external receiver contracts and staging allocation remain open. Counts stay **132 complete, 19 active remaining, 1 deferred**. See [cycle 18 evidence](../evidence/2026-11-04_decision_cycle18.md).

## Progress

**Cycle 16:** A real isolated logical restore passed all 263 schema/data inventory checks and retained case receipt replay under restricted grants. The measured enqueue shortcut was reverted after no repeatable gain. Full provider, capacity and release acceptance remain open; counts remain **132 complete, 19 active remaining, 1 deferred**. See [cycle 16 evidence](../evidence/2026-11-04_decision_cycle16.md).

**Cycle 15:** Implemented safe additional-scoring deferral and real decision/effect/job COMMIT-loss reconciliation. Five top-level tests and 84 subtests passed with race detection; vet passed. Broad leaves remain open pending full-provider acceptance. See [cycle 15 evidence](../evidence/2026-11-03_decision_cycle15.md).

**Cycle 14 (partial D19.17/D19.22/D20.15/D20.16/D20.17):** Added the real MySQL case
workflow receipt owner, atomic case/link/tag/audit/notification writes, restricted
grants, shared source/target receipt service, migration upgrade/repeat verification
and a subprocess crash after commit before acknowledgement. Real MySQL connection-loss tests cover both COMMIT rollback and lost commit acknowledgement; 26 top-level tests and 44 subtests passed with race detection, plus vet. Full provider and
shared-runtime acceptance remain open. The capacity experiment did not establish
a meaningful gain and its shortcut was removed. **132 completed; 19 active
remaining; 1 deferred.** See [cycle 14 evidence](../evidence/2026-11-03_decision_cycle14.md).

**Cycle 13 (supporting implementation):** Bounded required-job batching preserves
tenant/owner checks and transaction poisoning. Twenty-five top-level tests and
53 subtests passed with the race detector; vet passed. The four-worker local ramp
completed 2,122 evaluations versus 1,601 before batching, but still dropped 878
of 3,000 offered requests. Eight workers also exceeded the p95 limit. No additional
leaf meets acceptance: **132/152 complete; 20 remain**. See
[cycle 13 evidence](../evidence/2026-11-03_decision_cycle13.md).

**Cycle 12:** Completed D14.12-D14.16 and D15.17-D15.18. History acceptance passed
6,000 requests over 100,000 rows. Eleven race-enabled tests and eight subtests,
plus vet, passed for the load fixtures and publication-cache/owner-locking fixes.
The evaluation ramp still fails its offered-rate target: 1,529 successes and
1,471 drops at 100/sec, with zero evaluation errors after the locking fix.
**132/152 complete; 20 remain.** See [evidence and limitations](../evidence/2026-11-03_decision_cycle12.md).

| Parent | Area | Done | Left | Total | Parent status |
| --- | --- | ---: | ---: | ---: | --- |
| [D13](#d13) | Model and index preparation | 21 | 0 | 21 | Complete in recorded scope |
| [D14](#d14) | Decision history and snoozes | 16 | 0 | 16 | Complete in recorded scope |
| [D15](#d15) | Decision evaluation parity | 17 | 3 | 20 | Open |
| [D16](#d16) | Workflow, screening and scoring configuration storage | 8 | 0 | 8 | Complete in recorded scope |
| [D17](#d17) | Authoring and browser workflow integration | 26 | 0 | 26 | Complete in recorded scope |
| [D18](#d18) | Custom lists and evaluation integration | 16 | 0 | 16 | Complete in recorded scope |
| [D19](#d19) | Workflow, screening, scoring and outbox effects | 17 | 8 | 25 | Open |
| [D20](#d20) | Integrated correctness and acceptance | 11 | 9 | 20 | Open |
| **Total** | | **132** | **20** | **152** | **5 of 8 parents complete in recorded scope** |

## How we will cycle through this

1. Work by stable ID, for example **D13.11–D13.13**, or say **“do the next ready cycle.”**
2. Select **3–5 unchecked items** whose `After` dependencies are complete. Start with the cycle below.
3. Each item has one concrete output and a `Done when` check. A code item includes its focused verification; acceptance items add the broader proof explicitly named.
4. Record the exact changed files, test/command results and evidence link next to the item, then tick it. A test fixture does not complete an item that explicitly requires a real owner/provider.
5. Update this progress table and the main checklist's summary after each cycle. Keep IDs stable; insert any further split as `D17.12a`, `D17.12b`, etc., and count only executable leaf checkboxes.
6. Mark a parent complete only after all its required leaves and final evidence review are complete. A blocked decision or missing dependency stays visible; continue other ready items.

**Types:** `PLAN` = bounded design/inventory artifact; `CODE` = implementation
with focused tests; `VERIFY` = named acceptance test or measurement;
`DECISION` = unresolved user-owned policy/target; `REVIEW` = evidence reconciliation.
A PLAN item is not provider implementation. A REVIEW item does not authorize deployment.

**Shared dependencies have one owner:** decision permissions are defined under
D17.10–D17.16 and reused by D18; history targets are D14.12; evaluation targets are
D15.17; shared resource budgets are D20.13. References to D21–D23 consume their
worker/receipt contracts; their separate parent checklists are not duplicated here.
D25 still owns production release, restore/fencing rehearsal and activation.
PostgreSQL remains configured; tests use disposable databases and target code
stays staged until the separate architecture/cutover requirements are met.

## Completed first cycle

The five artifacts below are recorded in the integration matrix. D18.09 and
D20.07 are also complete, along with D13.12/D13.13 and D14.10/D14.11. Cycle 1
evidence distinguishes implementation and tests from inventory/design work.

| Order | ID | Concrete output |
| ---: | --- | --- |
| 1 | D13.11 | Owner endpoint/producer/worker wiring map with missing adapters identified. |
| 2 | D14.09 | Finite history acceptance matrix linked to existing tests and gaps. |
| 3 | D15.10 | Finite evaluator/effect golden-case matrix linked to existing tests and gaps. |
| 4 | D17.10 | Proposed decision permission matrix for the existing roles; unresolved mappings identified. |
| 5 | D19.11 | Screening source/version/publication map with owner migration order. |

**Next ready implementation:** D19.17-D19.21 receipt-provider composition and evaluation capacity profiling, followed by dependent end-to-end acceptance. The user selected a 1,000 evaluations/second design target and reuse of PostgreSQL providers. Preserve the existing scoring HTTP and optional outbox contracts. The working acceptance profile is recorded; receiver receipt capabilities and meeting evaluation capacity remain unresolved. See the [recorded direction and implementation consequences](../evidence/2026-11-03_decision_performance_provider_direction.md).

**Cycle 8 (partial batch):** D15.13 and D20.11 are complete. Added actual-service platform read/evidence cases and SQL-failure atomicity checks, plus copied browser configuration through publication, evaluation, persisted workflow action and attributed audit. Five top-level tests and 50 subtests passed with no failures/skips. The requested next 20-item batch is **not complete**: provider receipt bindings and dependent acceptance remain; scoring/outbox contracts and user-agreed load targets were requested and remain unanswered. See [evidence](../evidence/2026-11-03_decision_cycle8.json).

**Cycle 7 (historical):** Completed 20 new leaves: D13.15-D13.20, D15.12/D15.15/D15.16, D17.23-D17.26, D18.16, D19.14-D19.16 and D20.08/D20.09/D20.13. Real-owner acceptance passed five required tests and 11 subtests with no failures/skips; cleanup passed. Existing regression coverage and limitations are recorded in [evidence](../evidence/2026-11-03_decision_cycle7.json). D13, D17 and D18 now close in recorded granular scope; no deployment or full migration acceptance is implied.

**Cycle 6 (historical):** User approved D17.11 on 2026-11-03. Completed D17.11–D17.22 and D18.10–D18.15 with real auth/case owner migrations, opaque credential verification, restricted grants, lock-wait revocation, actual-service list parity, rollback and representative indexed plans. Together with cycle 5, all 20 selected leaves are complete. No additional parent is closed. See [evidence](../evidence/2026-11-03_decision_cycle6.json).

**Cycle 5 (historical):** Completed D19.12–D19.13: independently tracked screening owner schema, strict execution bindings, bounded owner-generated projection and a lock-preserving MERGE view. Four integration tests and 18 subtests passed under the race detector, with no failures/skips. Source/enqueue rollback and repeated real-view reconciliation also pass, but full mutation and delivery-worker integration remain D19.14–D19.16. The requested 20-item batch comprises D17.11–D17.22, D18.10–D18.15 and D19.12–D19.13; only the latter two are completed. The other 18 depend directly or transitively on the unanswered D17.11 policy decision. See [evidence](../evidence/2026-11-03_decision_cycle5.json).

**Cycle 4:** Completed D13.14b. One leased native search job now goes through typed planning, immutable intent, physical DDL, publication and fenced job/audit/ack completion. Nine integration scenarios cover success, audit recovery, authority/lease loss and tenant/pool/text rejection. Retained reference inputs and authority remain fixture-supplied; caller supervision and configured decision/owner wiring remain open. See [evidence](../evidence/2026-11-03_decision_cycle4.json).

**Cycle 3:** Completed D15.11 with 219 actual-service golden cases on PostgreSQL and MySQL, including persisted evidence and invalid-evaluation row counts. Fixed arithmetic and prefix aliases that the corpus exposed. See [evidence](../evidence/2026-11-03_decision_cycle3.json). Model and authority remain explicit fixtures; this does not close D15 or any real-provider gate.

**Cycle 2:** D13.14 now has two leaves. D13.14a's completion fence is verified;
At that point D13.14b still owned physical worker composition, now verified in cycle 4. This split adds one leaf
to the total and does not close a parent. See [evidence](../evidence/2026-11-03_decision_cycle2.json).

**Target clarification:** Evaluation throughput is 1,000/second as a design target. The stated engineering defaults are the working baseline under the user's subsequent instruction to finish; D14.12/D15.17 record that direction. History's scoped benchmark passes; evaluation capacity remains unmet. PostgreSQL provider reuse is selected; durable receipt compatibility still needs verification. D17.11 was approved and implemented in cycle 6.

<a id="d13"></a>

## D13 — Model and index preparation

Owner: **Decision engine + data-model owner**. Progress: **21/21 done; 0 left**.

Existing checked scope is supported by the [main checkpoints](03_decision_engine_service.md#verified-execution-path) and [evidence 1](../evidence/2026-11-03_decision_d13_d15.json), [evidence 2](../evidence/2026-11-03_decision_d13_d15_faults.json), [evidence 3](../evidence/2026-11-02_decision_d13_d20_followup.json).

- [x] **D13.01** — Read tenant data only through published registry/version bindings; closed or stale bindings fail.
- [x] **D13.02** — Validate bounded request/pinned AST and model snapshots before evaluation.
- [x] **D13.03** — Expose owner publication versions through model DTOs and the decision HTTP client.
- [x] **D13.04** — Fetch fresh models during preparation; refresh failures do not use stale cache results.
- [x] **D13.05** — Implement preparation status/start APIs for drafts and committed iterations.
- [x] **D13.06** — Implement bounded tenant/table-scoped index-job queries on both SQL adapters and owner HTTP.
- [x] **D13.07** — Preserve and validate job tenant/table UUIDs, table-name binding, duplicates and byte limits.
- [x] **D13.08** — Create missing search jobs and retry failed jobs through the explicit owner capability.
- [x] **D13.09** — Propagate cancellation/owner errors and reconcile a previously accepted job after a simulated lost response.
- [x] **D13.10** — Verify scoped query indexing and keep physical registry validation authoritative at publication.

- [x] **D13.11 [PLAN] Map the owner integration points.** Done when: List the concrete model reader, scoped-job API, job producer, worker and publication-registry components to compose; name each missing adapter and owning service. **After:** none. Evidence: [cycle 1](../evidence/2026-11-03_decision_cycle1.json), [integration matrix](03_decision_engine_integration_matrix.md).
- [x] **D13.12 [CODE] Compose the owner model endpoint.** Done when: Wire the publication-aware reader into the intended staged owner HTTP composition; a shared-database test returns the actual eligible publication token. **After:** D13.11. Evidence: [cycle 1](../evidence/2026-11-03_decision_cycle1.json), [integration matrix](03_decision_engine_integration_matrix.md).
- [x] **D13.13 [CODE] Compose the owner index-job endpoint.** Done when: Wire scoped reads/create/retry to the owner transaction and durable producer; verify failed enqueue rolls back the owner request. **After:** D13.11. Evidence: [cycle 1](../evidence/2026-11-03_decision_cycle1.json), [integration matrix](03_decision_engine_integration_matrix.md).
**D13.14 — Index worker composition (group; complete only when both leaves pass).**

- [x] **D13.14a [CODE] Fence native-index completion against publication.** Done when: The expected registry version and actual full native index are checked in the job/audit/delivery completion transaction; stale/closed publication, wrong index shape, lease expiry and audit failure cannot commit success; a real publication lock race preserves registry-before-job ordering. **After:** D13.13. Evidence: [cycle 2](../evidence/2026-11-03_decision_cycle2.json). This is an opt-in completion guard, not physical worker wiring.
- [x] **D13.14b [CODE] Compose physical index execution and publication.** `RunNativeIndexJob` wires the actual typed planner, immutable DDL intent, pinned executor, publication manager and leased completion fence. A queued owner job physically creates its index, publishes an agreeing registry/receipt version and reaches applied/completed with one audit. Recovery after an audit failure reuses the published operation. Text/navigation/repair are explicitly unsupported. Nine scenarios verify authorization/lease boundaries, tenant/pool binding and failure states. **After:** D13.14a. Evidence: [cycle 4](../evidence/2026-11-03_decision_cycle4.json). Trusted reference inputs, lease supervision, live identity/grants and configured decision integration remain separate work; no runtime activation is claimed.
- [x] **D13.15 [CODE] Connect decision preparation to the owner.** Done when: Supply explicit Models/IndexPreparation capabilities from the intended owner composition; verify no accidental mixed application database or legacy fallback. **After:** D13.12, D13.13. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D13.16 [VERIFY] Run preparation through physical publication.** Done when: Create a required index through decision HTTP, process the actual owner job, reread status and publish the scenario; assert registry/token agreement. **After:** D13.14, D13.15. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D13.17 [VERIFY] Exercise actual owner failures.** Done when: Verify a failed job, retry, owner restart and lost HTTP response using the composed owner; retained jobs reconcile without falsely reporting physical readiness. **After:** D13.16. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D13.18 [VERIFY] Exercise simultaneous preparation starts.** Done when: Run two starts for one requirement; verify the owner's documented dedupe/replay outcome and no duplicate physical publication. **After:** D13.16. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D13.19 [VERIFY] Install and verify owner grants.** Done when: Use separate disposable API/worker accounts with reviewed owner grants; intended calls succeed and direct unauthorized DDL/foreign writes fail. **After:** D13.14, D13.15, D17.14. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D13.20 [REVIEW] Close D13 evidence.** Done when: Link the owner integration, recovery, concurrency and grant results; tick parent D13 only when D13.11–D13.19 pass. **After:** D13.11–D13.19. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).

<a id="d14"></a>

## D14 — Decision history and snoozes

Owner: **Decision engine**. Progress: **16/16 done; 0 left**.

Existing checked scope is supported by the [main checkpoints](03_decision_engine_service.md#verified-execution-path) and [evidence 1](../evidence/2026-11-02_decision_history_tasks.json), [evidence 2](../evidence/2026-11-03_decision_d13_d15.json), [evidence 3](../evidence/2026-11-03_decision_d13_d15_faults.json), [evidence 4](../evidence/2026-11-03_decision_d13_d20_acceptance.json).

- [x] **D14.01** — Implement bounded history search, count, cursor and offset repositories.
- [x] **D14.02** — Preserve byte-exact object IDs, including empty values, trailing spaces and case.
- [x] **D14.03** — Verify the recorded Unicode case-insensitive/accent-sensitive search cases.
- [x] **D14.04** — Compare the 48-row golden corpus and its recorded filters on PostgreSQL/MySQL.
- [x] **D14.05** — Verify cursor continuation across inserts ahead of and behind the boundary.
- [x] **D14.06** — Verify a cursor boundary without a matching stored anchor.
- [x] **D14.07** — Verify count/page consistency within caller-owned snapshots across writer commits.
- [x] **D14.08** — Verify snooze tenant scope/exclusive expiry and scoped indexed history plans.

- [x] **D14.09 [PLAN] Freeze the remaining history cases.** Done when: Create a finite case matrix for combined filters, equal-time ties, null/empty inputs, Unicode and pagination; map existing tests and identify only uncovered cases. **After:** none. Evidence: [cycle 1](../evidence/2026-11-03_decision_cycle1.json), [integration matrix](03_decision_engine_integration_matrix.md).
- [x] **D14.10 [VERIFY] Fill filter/search parity gaps.** Done when: Implement every uncovered filter/search case in D14.09 on both engines; compare complete normalized results and counts. **After:** D14.09. Evidence: [cycle 1](../evidence/2026-11-03_decision_cycle1.json), [integration matrix](03_decision_engine_integration_matrix.md).
- [x] **D14.11 [VERIFY] Fill pagination parity gaps.** Done when: Implement uncovered tie, end-of-stream, changed-filter and concurrent-write cases from D14.09; assert no unauthorized or duplicate rows. **After:** D14.09. Evidence: [cycle 1](../evidence/2026-11-03_decision_cycle1.json), [integration matrix](03_decision_engine_integration_matrix.md).
- [x] **D14.12 [DECISION] Record numeric history targets.** Done when: Record user-agreed retained rows, tenant/skew profile, request rate, p95/p99 latency, error rate and test duration; exploratory figures are not targets. **After:** user input. **Evidence:** [cycle 12](../evidence/2026-11-03_decision_cycle12.md).
- [x] **D14.13 [CODE] Build the history load fixture.** Done when: Generate the D14.12 distribution reproducibly with bounded seeding and a disposable-database guard; record seed and row counts. **After:** D14.12. **Evidence:** [cycle 12](../evidence/2026-11-03_decision_cycle12.md).
- [x] **D14.14 [VERIFY] Capture representative query plans.** Done when: Capture unhinted filter/count/cursor plans at the agreed scale; record examined rows, residual contains-search cost and any required indexed-query fix. **After:** D14.13. **Evidence:** [cycle 12](../evidence/2026-11-03_decision_cycle12.md).
- [x] **D14.15 [VERIFY] Run history load acceptance.** Done when: Run the agreed workload, compare all recorded metrics with D14.12, and retain raw measurements including shared-resource usage. **After:** D14.14. **Evidence:** [cycle 12](../evidence/2026-11-03_decision_cycle12.md).
- [x] **D14.16 [REVIEW] Close D14 evidence.** Done when: Link the completed case matrix, plans and measured load result; tick parent D14 only when these pass. **After:** D14.10, D14.11, D14.15. **Evidence:** [cycle 12](../evidence/2026-11-03_decision_cycle12.md).

<a id="d15"></a>

## D15 — Decision evaluation parity

Owner: **Decision engine**. Progress: **17/20 done; 3 left**.

Existing checked scope is supported by the [main checkpoints](03_decision_engine_service.md#verified-execution-path) and [evidence 1](../evidence/2026-11-02_decision_d13_d20_progress.json), [evidence 2](../evidence/2026-11-03_decision_d13_d15.json), [evidence 3](../evidence/2026-11-03_decision_d13_d15_faults.json), [evidence 4](../evidence/2026-11-03_decision_d13_d20_acceptance.json).

- [x] **D15.01** — Compare actual-service trigger/non-trigger behavior on both engines.
- [x] **D15.02** — Compare recorded score thresholds and approve/review/block-and-review/decline outcomes.
- [x] **D15.03** — Compare complete normalized rule evidence and persisted decision/evidence readback.
- [x] **D15.04** — Compare configured workflow/screening/scoring effects and outbox records.
- [x] **D15.05** — Verify required-job enqueue failure rolls back decision/effect writes.
- [x] **D15.06** — Verify rule-evidence/workflow/screening/scoring/outbox insert failures roll back all seven checked tables.
- [x] **D15.07** — Compare snoozed scoring and rule evidence, including exclusive expiry.
- [x] **D15.08** — Compare source object-ID normalization and case behavior.
- [x] **D15.09** — Verify custom-list lookup failures propagate and legacy references remain compatible.

- [x] **D15.10 [PLAN] Freeze the remaining business corpus.** Done when: Enumerate supported evaluator branches, aggregate forms and effect combinations; map each to existing evidence or one missing golden case. **After:** none. Evidence: [cycle 1](../evidence/2026-11-03_decision_cycle1.json), [integration matrix](03_decision_engine_integration_matrix.md).
- [x] **D15.11 [VERIFY] Complete scalar/value golden cases.** Verified 219 cases across E01–E06 and public aliases: null/false/zero/missing values, boolean/comparison boundaries, strings/Unicode/membership, arithmetic, clock/timezone boundaries and local transforms. Both actual services compare complete returned and persisted evidence; invalid evaluation leaves decision/evidence counts unchanged. Arithmetic/prefix alias bugs fixed; focused normalization and existing threshold/effect regressions pass. **After:** D15.10. Evidence: [cycle 3](../evidence/2026-11-03_decision_cycle3.json). Real model/authority/provider and performance gates remain separate.
- [x] **D15.12 [VERIFY] Complete aggregate golden cases.** Done when: Add missing aggregate/filter/navigation cases against actual published model tables; compare values and rule evidence on both engines. **After:** D15.10, D13.16. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D15.13 [VERIFY] Complete platform-read golden cases.** Done when: Cover missing list/tag/risk/IP cases through actual evaluation readers, including absence and failure propagation. **After:** D15.10, D18.13. **Evidence:** [cycle 8](../evidence/2026-11-03_decision_cycle8.json).
- [ ] **D15.14 [VERIFY] Complete effect-combination golden cases.** Done when: Compare all missing workflow/screening/scoring/outbox combinations from D15.10 through the composed owner transactions. **After:** D15.10, D19.21.
- [x] **D15.15 [VERIFY] Exercise revocation during evaluation.** Done when: Revoke real permission between preparation and persistence; assert denial and no committed decision/effect/job output. **After:** D17.16, D18.13. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D15.16 [VERIFY] Exercise publication/model changes.** Done when: Change a publication or model gate between preparation and persistence; verify the documented fence or pinned-version result with actual owner composition. **After:** D13.16. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D15.17 [DECISION] Record numeric evaluation targets.** Done when: Record user-agreed decisions/second, p95/p99 latency, error rate, rule/aggregate mix, tenant distribution and sustained-run duration. **After:** user input. **Evidence:** [cycle 12](../evidence/2026-11-03_decision_cycle12.md).
- [x] **D15.18 [CODE] Build the evaluation load fixture.** Done when: Seed the agreed rule/model/effect mix reproducibly and measure decision/effect/job counts alongside latency and errors. **After:** D15.17. **Evidence:** [cycle 12](../evidence/2026-11-03_decision_cycle12.md).
- [ ] **D15.19 [VERIFY] Run evaluation load acceptance.** Done when: Meet the recorded targets using composed owners and the shared connection/resource budget; retain raw results and bottleneck findings. **After:** D15.18, D19.21, D20.13.
- [ ] **D15.20 [REVIEW] Close D15 evidence.** Done when: Link every case in D15.10, revocation/model checks, load results and applicable crash/uncertain-commit results; tick parent only after all pass. **After:** D15.11–D15.16, D15.19, D20.15, D20.16.

<a id="d16"></a>

## D16 — Workflow, screening and scoring configuration storage

Owner: **Decision engine**. Progress: **8/8 done; 0 left**.

Existing checked scope is supported by the [main checkpoints](03_decision_engine_service.md#verified-execution-path) and [evidence 1](../evidence/2026-10-30_decision_completion_batch_01_and_mysql848.json), [evidence 2](../evidence/2026-11-02_decision_d13_d20_progress.json).

- [x] **D16.01** — Implement workflow configuration repository operations.
- [x] **D16.02** — Implement screening configuration repository operations.
- [x] **D16.03** — Implement scoring configuration repository operations.
- [x] **D16.04** — Persist ordered outcome children and read them in stable order.
- [x] **D16.05** — Replace configuration children atomically with their parent changes.
- [x] **D16.06** — Enforce tenant/scenario/iteration relationships in the staged schema.
- [x] **D16.07** — Preserve NULL storage semantics and reject unsupported NULL conversion through public string-slice ports.
- [x] **D16.08** — Record passing scoped configuration tests and retain the completed D16 parent status.

D16 gains no artificial outstanding work. Provider/runtime acceptance involving
these configuration repositories belongs to D17/D19/D20 below.

<a id="d17"></a>

## D17 — Authoring and browser workflow integration

Owner: **Decision engine; auth owner for identity; case owner for inbox/tag policy**. Progress: **26/26 done; 0 left**.

Existing checked scope is supported by the [main checkpoints](03_decision_engine_service.md#verified-execution-path) and [evidence 1](../evidence/2026-10-30_decision_composition_and_browser_copy.json), [evidence 2](../evidence/2026-11-02_decision_d13_d20_followup.json), [evidence 3](../evidence/2026-11-03_decision_d13_d20_acceptance.json).

- [x] **D17.01** — Implement structured/browser workflow save, list and update operations.
- [x] **D17.02** — Fence stale edits with workflow versions and serialize same-scenario priority changes.
- [x] **D17.03** — Persist attributed authoring audit records atomically with workflow changes.
- [x] **D17.04** — Implement audited soft deletion.
- [x] **D17.05** — Implement browser workflow graph copy with rule-reference remapping.
- [x] **D17.06** — Implement bounded batch inbox/tag authorization consumers and inbox visibility filtering.
- [x] **D17.07** — Validate destination model/registry bindings before browser-copy writes.
- [x] **D17.08** — Expose strict browser-copy and scenario graph-copy HTTP routes.
- [x] **D17.09** — Verify recorded fixture permission, tenant, concurrency and audit/publication rollback cases.

- [x] **D17.10 [PLAN] Draft the complete decision permission matrix.** Done when: List read/author/publish/workflow/list actions and existing roles; distinguish trusted user sessions from worker identities and preserve browser platform-admin AND case access. **After:** none; owns the policy shared with D18. Evidence: [cycle 1](../evidence/2026-11-03_decision_cycle1.json), [integration matrix](03_decision_engine_integration_matrix.md).
- [x] **D17.11 [DECISION] Confirm the decision permission matrix.** Done when: Record user-approved role-to-action mappings from D17.10; do not infer grants from test fixtures. **After:** D17.10, user input. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D17.12 [CODE] Implement auth-owned membership storage prerequisite.** Done when: Add independently tracked target user/membership/role definitions with tenant-consistent keys and version checks; test scoped reads and invalid relationships. **After:** D17.11; auth owner. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D17.13 [CODE] Implement auth-owned session storage prerequisite.** Done when: Add target session identity, digest, expiry, revocation and membership bindings; test invalid/cross-tenant references and expiry boundaries. **After:** D17.12; auth owner. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D17.14 [CODE] Implement decision session authorization provider.** Done when: Implement identity_authorize_decision_v1 over real owner data with the approved action mapping, current active tenant/user/membership/session checks and no transaction control/network calls. **After:** D17.11–D17.13; auth owner. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D17.15 [VERIFY] Test every role/action boundary.** Done when: Run allow/deny tests for each approved role/action, stale membership version, wrong session/tenant and read-only session; unknown actions deny. **After:** D17.14. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D17.16 [VERIFY] Test revocation and expiry while waiting.** Done when: Verify current expiry/revocation after lock waits and consistent user→membership→session lock ordering before decision writes. **After:** D17.14. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D17.17 [CODE] Implement case inbox authorization prerequisite.** Done when: Implement the real case-owned inbox membership/active-state read contract, including required target ownership tables/migrations and the single-inbox function. **After:** D17.14; case owner. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D17.18 [CODE] Implement case inbox batch authorization.** Done when: Implement the real <=500 inbox batch function, sorted narrow locking and all-requested-inboxes authorization; test mixed allowed/denied inputs. **After:** D17.17; case owner. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D17.19 [CODE] Implement case tag batch authorization.** Done when: Implement the real bounded tag function and required target tag ownership definitions; test tenant, active-state and permission checks. **After:** D17.14; case owner. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D17.20 [CODE] Implement case inbox visibility provider.** Done when: Implement the real bounded visibility function; return only requested authorized IDs, with explicit denial/unavailability semantics. **After:** D17.17; case owner. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D17.21 [CODE] Wire trusted authoring identity and providers.** Done when: Connect the established credential verifier to TrustedDecisionSession and real auth/case contracts in the staged composition; reject forged/missing identity. **After:** D17.15, D17.16, D17.18–D17.20. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D17.22 [VERIFY] Verify authoring provider grants.** Done when: Install only required auth/case execution/read grants for disposable authoring accounts; verify intended calls and denied cross-owner writes. **After:** D17.21. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D17.23 [VERIFY] Complete real-provider browser edit parity.** Done when: Compare save/list/update/delete, visibility and version-conflict results on both engines with actual owner implementations. **After:** D17.21. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D17.24 [VERIFY] Complete real-provider copy parity.** Done when: Compare browser/scenario graph copy, rule remapping, audit attribution, destination validation and rollback across both engines. **After:** D17.21, D13.16. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D17.25 [VERIFY] Exercise real-provider edit/copy races.** Done when: Test concurrent priorities, membership changes, inbox deactivation and audit failures; verify no partial graph or unauthorized result. **After:** D17.23, D17.24. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D17.26 [REVIEW] Close D17 evidence.** Done when: Link policy, owner migration order, grants and browser/copy results; tick parent only after D17.10–D17.25 pass. **After:** D17.10–D17.25. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).

<a id="d18"></a>

## D18 — Custom lists and evaluation integration

Owner: **Decision engine + auth owner**. Progress: **16/16 done; 0 left**.

Existing checked scope is supported by the [main checkpoints](03_decision_engine_service.md#verified-execution-path) and [evidence 1](../evidence/2026-11-02_decision_custom_list_tasks.json), [evidence 2](../evidence/2026-10-30_decision_composition_and_browser_copy.json), [evidence 3](../evidence/2026-11-03_decision_d13_d20_acceptance.json).

- [x] **D18.01** — Implement custom-list parent CRUD with tenant-scoped name semantics.
- [x] **D18.02** — Implement list-entry CRUD and bounded list/entry reads.
- [x] **D18.03** — Expose the recorded custom-list API/import/export operations.
- [x] **D18.04** — Compose explicit custom-list access with the decision/effect/job transaction.
- [x] **D18.05** — Keep list-read permission distinct from list-mutation permission.
- [x] **D18.06** — Verify list/decision/job rollback and reject mutations through read-only capability.
- [x] **D18.07** — Compose actual bounded custom-list readers into the inactive evaluator/runtime.
- [x] **D18.08** — Propagate lookup failures and verify ID/name/UUID-shaped-name compatibility across engines.

- [x] **D18.09 [PLAN] Map list action consumers.** Done when: List each list API/evaluator/worker call and its required read or mutate action; map it to D17.10 without creating a second role policy. **After:** none. Evidence: [cycle 1](../evidence/2026-11-03_decision_cycle1.json), [integration matrix](03_decision_engine_integration_matrix.md).
- [x] **D18.10 [CODE] Bind list actions to real auth.** Done when: Use the approved auth provider for decision.list.read and decision.list.mutate; remove fixture authority from the integration composition. **After:** D18.09, D17.14, D17.21. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D18.11 [VERIFY] Verify list-specific grants.** Done when: Run intended list operations with a restricted account; show list-read cannot mutate and generic decision persistence cannot grant list writes. **After:** D18.10. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D18.12 [VERIFY] Exercise list changes and revocation.** Done when: Race list rename/delete/update and real authority revocation against evaluation; assert the documented read/fence outcome without hidden errors. **After:** D18.10. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D18.13 [VERIFY] Run actual-service list decision parity.** Done when: Evaluate list-based rules through source/target services with real authority; compare decisions, rule evidence and tenant isolation. **After:** D18.10. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D18.14 [VERIFY] Exercise list/effect/job failure atomicity with real auth.** Done when: Inject a late required-job failure after list-related work; verify transaction rollback and no unauthorized mutations. **After:** D18.13. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D18.15 [VERIFY] Verify bounded list plans.** Done when: Capture unhinted plans for hot-name membership, ID lookup and entry pagination using a documented representative fixture; record row/byte overflow behavior. **After:** D18.10. Evidence: [cycle 6](../evidence/2026-11-03_decision_cycle6.json); scoped implementation and verification described in the approved integration matrix.
- [x] **D18.16 [REVIEW] Close D18 evidence.** Done when: Link the one shared permission policy, real-provider composition, grant checks and acceptance results; tick parent only after these pass. **After:** D18.11–D18.15. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).

<a id="d19"></a>

## D19 — Workflow, screening, scoring and outbox effects

Owner: **Decision engine + screening/provider owners; delivery prerequisites tracked under D21–D23**. Progress: **17/25 done; 8 left (including 1 deferred)**.

Existing checked scope is supported by the [main checkpoints](03_decision_engine_service.md#verified-execution-path) and [evidence 1](../evidence/2026-10-29_decision_effect_jobs.json), [evidence 2](../evidence/2026-10-30_decision_effect_lifecycle.json), [evidence 3](../evidence/2026-11-02_decision_d13_d20_progress.json), [evidence 4](../evidence/2026-11-03_decision_d13_d15_faults.json).

- [x] **D19.01** — Implement workflow execution persistence and bounded readback.
- [x] **D19.02** — Implement screening execution persistence and bounded readback.
- [x] **D19.03** — Implement scoring request persistence and bounded readback.
- [x] **D19.04** — Implement outbox event persistence.
- [x] **D19.05** — Enforce authoritative tenant/decision/scenario/config relationships.
- [x] **D19.06** — Compose effect creation with required initial jobs on one transaction.
- [x] **D19.07** — Poison the transaction when a required enqueue fails even if its error is ignored.
- [x] **D19.08** — Implement the screening source contract consumer and monotone projection reconciliation.
- [x] **D19.09** — Verify source-version/tenant bindings and reject legacy unversioned projection updates.
- [x] **D19.10** — Compare recorded actual-service effects/outbox results and injected persistence rollback.

- [x] **D19.11 [PLAN] Map screening projection ownership.** Done when: Map each source field, status transition and publication trigger to the actual screening owner; identify required target schema and migration order. **After:** none. Evidence: [cycle 1](../evidence/2026-11-03_decision_cycle1.json), [integration matrix](03_decision_engine_integration_matrix.md).
- [x] **D19.12 [CODE] Add screening publication version storage.** Done when: Add the owner-controlled positive projection version and tenant-consistent decision/config/execution bindings in target owner migrations; test invalid bindings. **After:** D19.11; screening owner. Evidence: [cycle 5](../evidence/2026-11-03_decision_cycle5.json); actual screening owner plan, replay and invalid/mismatched relationship checks.
- [x] **D19.13 [CODE] Implement the screening source view.** Done when: Publish screening_decision_source_v1 from real owner state with bounded projection fields; verify it is an updatable view whose lock reaches the intended source row. **After:** D19.12. Evidence: [cycle 5](../evidence/2026-11-03_decision_cycle5.json); actual MERGE view, bounded summary, manual/foreign exclusion and source-row lock timeout.
- [x] **D19.14 [CODE] Advance the source version on mutations.** Done when: Update each status/evidence mutation identified in D19.11 to advance the version atomically; test stale writers and exact transition behavior. **After:** D19.12. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D19.15 [CODE] Enqueue projection updates atomically.** Done when: Write the required projection delivery with each source publication; injected enqueue failure must roll back the source change. **After:** D19.14; coordinate Q-01/SR-01. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D19.16 [VERIFY] Test actual source-to-decision reconciliation.** Done when: Use the real view/version/delivery path; verify stale, repeated, foreign and out-of-order publications cannot regress or misbind the decision projection. **After:** D19.13, D19.15. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [ ] **D19.17 [CODE] Bind the real workflow receipt provider.** Done when: Connect the workflow handler to its actual owner receipt/reconciliation contract; verify durable attempt identity and lookup of an accepted result. **After:** D17.21; coordinate D23. **Partial evidence:** [cycle 14](../evidence/2026-11-03_decision_cycle14.md) adds the real MySQL case-action owner, restricted grants, source/target comparison, decision dispatch completion and process-crash reconciliation. External workflow receivers, complete case-owner/runtime composition and broader acceptance remain; [cycle 11](../evidence/2026-11-03_decision_cycle11.json) records the original PostgreSQL binding.
- [x] **D19.18 [CODE] Bind the real screening receipt provider.** Done when: Connect screening dispatch to its actual receipt/reconciliation and destination policy; verify an accepted request can be reconciled without a blind resend. **After:** D19.16; coordinate D23. **Evidence:** [cycle 10](../evidence/2026-11-03_decision_cycle10.json). Screening-owned local intake, durable acceptance/reconciliation and dispatch handoff verified; actual search worker, trusted runtime identities and full provider composition remain separate gates.
- [ ] **D19.19 [DEFERRED BY USER] Bind the additional scoring receipt provider.** The user chose to skip this integration on 2026-11-03. Internal rule scoring remains in scope and is implemented. This item is not completed. Cycle 15 implements explicit staged runtime exclusion, startup rejection of active configurations/unfinished requests/live deliveries, preparation rechecks and transaction-poisoning mutation guards. No queued work is discarded. Verified evidence: [cycle 15](../evidence/2026-11-03_decision_cycle15.md). Deployment writer fencing and complete runtime activation remain separate. Original acceptance: durable provider attempt/receipt and destination contracts with acceptance/reconciliation proof.
- [ ] **D19.20 [CODE] Bind the real outbox receipt provider.** Done when: Connect outbox delivery to its actual destination/receipt contract; verify retained acceptance proof and explicit unavailable behavior. **After:** provider contract; coordinate D23.
- [ ] **D19.21 [CODE] Compose real effect providers on the shared database.** Done when: Supply these providers and trusted worker identities to the staged application; verify same-database/pool scope and failed readiness for a missing provider. **After:** D19.17–D19.20, D17.14; coordinate D21–D23.
- [ ] **D19.22 [VERIFY] Verify effect API/worker grants.** Done when: Run actual owner transitions with separate restricted accounts; deny arbitrary source rewrites, cross-tenant access and unfenced worker completion. **After:** D19.21.
- [ ] **D19.23 [VERIFY] Exercise effect retry/redrive/cancellation.** Done when: For each effect kind, run the real provider through retry, authorized redrive and cancellation; verify attempt counters, receipts and terminal outcomes. **After:** D19.21.
- [ ] **D19.24 [VERIFY] Exercise source/decision lock ordering.** Done when: Race source publication, reconciliation and effect updates; verify narrow source→projection ordering, no stale overwrite and no network IO under locks. **After:** D19.16, D19.21.
- [ ] **D19.25 [REVIEW] Close D19 evidence.** Done when: Link actual owner/provider migrations, atomicity, grants and lifecycle results; tick parent only when D19.11–D19.24 pass. **After:** D19.11–D19.24.

<a id="d20"></a>

## D20 — Integrated correctness and acceptance

Owner: **Decision engine + participating owners; broad release remains D25**. Progress: **11/20 done; 9 left**.

Existing checked scope is supported by the [main checkpoints](03_decision_engine_service.md#verified-execution-path) and [evidence 1](../evidence/2026-11-02_decision_d13_d20_progress.json), [evidence 2](../evidence/2026-11-03_decision_d13_d15_faults.json), [evidence 3](../evidence/2026-11-03_decision_d13_d20_acceptance.json).

- [x] **D20.01** — Run the recorded source/target authoring and decision/evidence differential corpora.
- [x] **D20.02** — Run the recorded configured-effect/outbox and required-job rollback corpus.
- [x] **D20.03** — Run fixture-backed browser save/list/update/visibility/conflict/copy tests.
- [x] **D20.04** — Run recorded tenant isolation, revocation and concurrent authoring checks.
- [x] **D20.05** — Run scoped owner/worker grant and dispatch-intent tests.
- [x] **D20.06** — Retain reproducible regression commands, race results and source-inventory evidence.

- [x] **D20.07 [PLAN] Create the integrated acceptance matrix.** Done when: Map D13–D19 contracts to named end-to-end tests and expected outcomes; identify missing real-provider cases without repeating completed fixture tests. **After:** none. Evidence: [cycle 1](../evidence/2026-11-03_decision_cycle1.json), [integration matrix](03_decision_engine_integration_matrix.md).
- [x] **D20.08 [CODE] Create the real-owner integration fixture.** Done when: Install independently tracked owner migrations into one disposable logical database per engine; record exact ordering and prohibit application DB reset. **After:** D20.07, D13.12, D17.13, D17.17, D19.12. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [x] **D20.09 [CODE] Add a reproducible acceptance runner.** Done when: Start the D20.08 owners/providers and fail when required integration settings are absent; collect versions, commands and cleanup results. **After:** D20.08. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [ ] **D20.10 [VERIFY] Run end-to-end decision creation.** Done when: Exercise model publication→scenario publication→evaluation→effects/jobs→provider result; assert every owner outcome and evidence row. **After:** D20.09, D13.16, D19.21.
- [x] **D20.11 [VERIFY] Run browser-to-decision journeys.** Done when: Exercise authoring/copy with real permissions, then evaluate the resulting scenario; compare saved configuration, decision and audit evidence. **After:** D20.09, D17.23, D17.24. **Evidence:** [cycle 8](../evidence/2026-11-03_decision_cycle8.json).
- [ ] **D20.12 [VERIFY] Run restricted-account acceptance.** Done when: Apply reviewed participating-owner grants and run D20.10/D20.11 under API/worker identities, including denied foreign writes. **After:** D13.19, D17.22, D18.11, D19.22, D20.10, D20.11.
- [x] **D20.13 [PLAN] Record the shared connection/resource budget.** Done when: Inventory API/worker replicas, pools and concurrency across all participating services; show the total fits one database budget. **After:** D20.08. **Evidence:** [cycle 7](../evidence/2026-11-03_decision_cycle7.json).
- [ ] **D20.14 [VERIFY] Race cancellation, completion and cleanup.** Done when: Exercise the matrix's concurrent real-business paths; assert exactly one valid terminal result and retention of required receipts/evidence. **After:** D20.10, D19.23; coordinate D21–D23.
- [ ] **D20.15 [VERIFY] Inject process crashes at transaction boundaries.** Done when: Crash before persistence, after local commit and during provider handoff; restart and verify the matrix's recovery outcome without duplicate effects. **After:** D20.10.
- [ ] **D20.16 [VERIFY] Inject uncertain commit acknowledgements.** Done when: Lose the acknowledgement after a successful commit; reconcile the retained operation/receipt identity without blind mutation or external resend. **After:** D20.10, D19.23.
- [ ] **D20.17 [VERIFY] Verify shared migration compatibility.** Done when: Rehearse the supported installation/upgrade ordering with all participating owner objects and grants; verify unrelated owner data and no weakened constraints. **After:** D20.08, D20.12; full release/restore/fencing remains D25.
- [ ] **D20.18 [VERIFY] Run sustained shared-workload acceptance.** Done when: Run history and evaluation together using agreed targets, duration and D20.13 budgets; record latency/error/resource and lock-contention results. **After:** D14.12, D15.17, D20.10, D20.13.
- [ ] **D20.19 [REVIEW] Reconcile every acceptance result.** Done when: Mark every D20.07 case pass/fail with evidence; list any missing provider, skipped test or unmet target explicitly. **After:** D20.10–D20.12, D20.14–D20.18.
- [ ] **D20.20 [REVIEW] Close D20 evidence.** Done when: Tick parent D20 only after D20.19 has no unresolved required case; reference upstream verification rows without creating a circular dependency on parent signoffs. **After:** D20.19.
