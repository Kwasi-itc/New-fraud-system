# Decision evaluation, workflows and historical reads

Status: **NOT COMPLETE**. Latest verified work: cycle 16 isolated logical restore with exact schema/data verification and restricted case receipt replay, following cycle 15 scoring deferral and COMMIT-loss recovery. Evaluation throughput acceptance remains unmet. Source-inventory choices 1–9 approved by the user; cross-owner contracts are reviewed when their implementation is reached. Staged MySQL authoring, decisions/evidence, comparisons, configuration, historical reads, owner model/job endpoints, native index publication execution and screening source storage/view exist. Configured worker/decision integration, remaining cross-owner providers and runtime activation remain pending. Primary task: DE-01; prerequisites: IN-01, DB-01, Q-01, SC-01. Findings: PG-08/10/15/20/25/26/32/34.

**Cycle 10:** Completed **D19.18**: screening-owned receipt intake and local decision dispatch binding, concurrent deduplication, lost-response reconciliation, atomic source/receipt/projection persistence and preservation of a newer result when publication precedes acknowledgement. Eight top-level tests and 41 subtests passed with the race detector, no failures/skips; both vet runs passed. **125/152 complete; 27 remain.** Workflow binding remains implementation work; scoring/outbox contracts and agreed load targets are pending. See [cycle 10 evidence](../evidence/2026-11-03_decision_cycle10.json).

**Cycle 11 (partial D19.17):** Implemented the real PostgreSQL case owner receipt/reconciliation contract, opt-in HTTP routes and the decision case-workflow adapter. The actual adapter recovered a committed case after its HTTP response was lost. Nine top-level tests and 13 subtests passed with the race detector; both vet runs passed. The staged MySQL case mutation owner and external workflow receiver contracts remain open, so D19.17 is **not ticked**. At the end of cycle 11 the count remained **125/152 complete, 27 remaining**. See [cycle 11 evidence](../evidence/2026-11-03_decision_cycle11.json).

**Cycle 16:** A real isolated logical restore passed all 263 schema/data inventory checks and retained case receipt replay under restricted grants. The measured enqueue shortcut was reverted after no repeatable gain. Full provider, capacity and release acceptance remain open; counts remain **132 complete, 19 active remaining, 1 deferred**. See [cycle 16 evidence](../evidence/2026-11-04_decision_cycle16.md).

Production release work now has a [granular readiness checklist](03_decision_engine_production_readiness.md) covering migrations, grants, restore integrity and cutover. Its supporting checks do not change the 152-leaf acceptance count.

**Cycle 18:** Critical/shared-owner/recovery checks passed: 62 top-level tests and 165 subtests, race enabled, no failures/skips. Added case producer lease recovery and mixed evaluation/history load with scoring deferred. All three measured evaluation profiles failed capacity acceptance; external receiver contracts and staging allocation remain open. Counts stay **132 complete, 19 active remaining, 1 deferred**. See [cycle 18 evidence](../evidence/2026-11-04_decision_cycle18.md).

## Checklist guide and current remaining work

**Cycle 15:** Added explicit safe deferral of additional scoring and real COMMIT-loss tests for decision/effect/job persistence. Five top-level tests and 84 subtests passed with the race detector; vet passed. Full receiver/runtime/capacity/release acceptance remains open. See [cycle 15 evidence](../evidence/2026-11-03_decision_cycle15.md).

**Cycle 14:** Implemented the staged MySQL case workflow receipt owner, atomic
case mutations and notifications, restricted grants, source/target comparison,
real process-crash recovery and fresh/upgrade/repeat migration checks. Full event
receiver/runtime composition and 1,000/sec acceptance remain open. Additional
external scoring is deferred by the user: **132/152 complete, 19 active remaining,
1 deferred**. See [cycle 14 evidence](../evidence/2026-11-03_decision_cycle14.md).

**Cycle 13:** Implemented bounded required-job batching and fixture diagnostics.
Twenty-five race-enabled top-level tests and 53 subtests plus vet passed, including
PostgreSQL/MySQL effect parity and rollback. The local four-worker ramp completed
32.5% more evaluations but still fails capacity acceptance. Real receiver contracts
and dependent integration remain open: **132/152 done, 20 open**. See
[cycle 13 evidence](../evidence/2026-11-03_decision_cycle13.md).

**Cycle 12:** Completed D14.12-D14.16 and D15.17-D15.18: **132/152 done, 20 open**.
The 100,000-row history workload passed 6,000 requests (p95 21.4ms, p99 31.5ms).
Added the actual-service evaluation load fixture and fixed publication identity
loss in model copying and an unrelated-owner outbox locking deadlock. Eleven
race-enabled tests and eight subtests plus vet passed. Evaluation's 100/sec ramp
still fails capacity acceptance; 1,000/sec is a target, not an achieved result.
See [cycle 12 evidence](../evidence/2026-11-03_decision_cycle12.md).

**For the next implementation cycles, use the [D13–D20 granular subtask checklist](03_decision_engine_subtasks.md).**
It is the current working queue, with stable IDs, dependencies, owners and a
specific completion check for every unfinished item. Historical checkpoints below
remain evidence, not additional tasks to count or repeat.

| Parent | Completed subtasks | Remaining subtasks | Total |
| --- | ---: | ---: | ---: |
| D13 | 21 | 0 | 21 |
| D14 | 16 | 0 | 16 |
| D15 | 17 | 3 | 20 |
| D16 | 8 | 0 | 8 |
| D17 | 26 | 0 | 26 |
| D18 | 16 | 0 | 16 |
| D19 | 17 | 8 | 25 |
| D20 | 11 | 9 | 20 |
| **Total** | **132** | **20** | **152** |

Cycle 1 completed **11 leaves**: D13.11–D13.13, D14.09–D14.11, D15.10, D17.10, D18.09, D19.11 and D20.07. See the [integration matrix](03_decision_engine_integration_matrix.md) and [verification evidence](../evidence/2026-11-03_decision_cycle1.json). Cycle 2 added D13.14a; cycle 3 added D15.11; cycle 4 added D13.14b; cycle 5 added D19.12–D19.13. Cycle 7 below supersedes those remaining-work notes. D13, D14, D16, D17 and D18 are complete in recorded granular scope; D15, D19 and D20 remain open.

Cycle 5 (historical) implemented independently tracked screening owner migrations, exact tenant/decision/configuration/execution bindings and the bounded updatable source view. Four integration tests and 18 subtests pass with the race detector, including source-row locking and source/enqueue rollback. Full match/status mutation wiring, delivery draining, actual identities and provider receipt integration remain open. At the end of cycle 5, 2 of the selected 20 leaves were complete; cycle 6 resolves the policy dependency and completes the other 18. See [cycle 5 evidence](../evidence/2026-11-03_decision_cycle5.json).

Cycle 6 (historical) completed **D17.11–D17.22 and D18.10–D18.15**, following explicit policy approval. Real auth/case migrations, session verification and restricted grants pass; list concurrency, rollback, nine actual-service golden cases and unhinted plans over 14,080 entries pass. Exact list-name lookup now matches PostgreSQL while folded uniqueness remains. **That earlier 20-item batch completed; at that checkpoint 102/152 were checked, 50 remained.** Full browser/copy acceptance, provider composition and runtime activation remain open. See [cycle 6 evidence](../evidence/2026-11-03_decision_cycle6.json).

Cycle 7 (historical) completed **20 additional subtasks**: D13.15-D13.20, D15.12/D15.15/D15.16, D17.23-D17.26, D18.16, D19.14-D19.16 and D20.08/D20.09/D20.13. **At that checkpoint 122/152 subtasks were checked; 30 remained.** Actual owner publication/recovery/grants, 19 aggregate/navigation rules with committed evidence parity, copy/edit/race acceptance, screening versioned mutations and projection delivery, shared fixtures, a reproducible runner and a resource-budget plan are verified. Five required integration tests plus 11 subtests passed without failures/skips; cleanup passed. See [cycle 7 evidence](../evidence/2026-11-03_decision_cycle7.json) for regression results and limitations. PostgreSQL remains configured; MySQL activation and full migration acceptance remain open.

Cycle 8 completes **D15.13 and D20.11**: actual platform read/evidence parity and target SQL-failure propagation, plus browser copying through scenario publication, evaluation, committed workflow action and attributed audit. **124/152 subtasks are checked; 28 remain. The requested next 20-item batch is incomplete.** Five top-level tests and 50 subtests passed with no failures/skips. Real workflow/screening receipt bindings remain implementation work; scoring/outbox provider contracts and user-agreed load targets are pending inputs. See [cycle 8 evidence](../evidence/2026-11-03_decision_cycle8.json).

**Cycle 9 (supporting implementation):** Added a bounded offered-rate load runner and deterministic disposable history fixture, with raw request samples, nine unhinted plans and database/pool snapshots. Five race-enabled tests and vet passed. The smoke used 240 rows and 12 requests; it is not agreed-scale acceptance. No additional leaves are checked: **124/152 complete, 28 remaining**. Numeric targets and scoring/outbox provider choices remain pending; workflow/screening receipt bindings remain implementation work. See [cycle 9 evidence](../evidence/2026-11-03_decision_cycle9.json).

- [x] **T01–T50:** all five API/integration batches implemented and verified.
- [x] **D01–D13, D16–D18:** tasks completed within their recorded scope.
- [ ] **D14–D15 and D19–D25:** remaining implementation, integration or acceptance gates.
- [ ] **DE-01.1–DE-01.4:** full decision-engine migration acceptance.
- [ ] **Runtime activation:** PostgreSQL remains configured; MySQL is staged and opt-in.

**How to read the ticks:** a checked box means the specifically described local
work has implementation/verification evidence. An empty box means the described
work or acceptance gate is still open. A checked subtask does not close its parent,
approve deployment or establish full provider parity. Recorded status notes retain
those distinctions.

The detailed checkpoints below are chronological evidence. Older references to
missing routes or adapters may have been completed by a newer checkpoint; use
T01–T50, the granular subtask checklist and the current remaining-work list here for today's position. Historical
remaining-work statements are retained for traceability, not counted as new tasks.

### Remaining implementation and integration

- [ ] **Test-run comparison:** implement target evaluation and statistics API composition; list/detail/create/cancel are already completed.
- [ ] **D22 - Deployment authoring/browser wiring:** granular D17 real-owner edit/copy acceptance is complete; full runtime/provider startup acceptance remains.
- [ ] **D25 - Model/index activation:** granular D13 owner integration and metadata grants are complete; deployment supervision, full release grants and activation remain.
- [ ] **D19 - Provider acceptance:** granular D18 policy/grants/evidence review is complete; real effect/service-principal integration remains.
- [ ] **D19 / D21–D23 — External providers and workers:** finish real screening/scoring/dispatch receipt-provider integration, service identities and complete provider-backed execution acceptance.

### Remaining verification and release

- [x] **D14 / DE-01.3 — SCOPED HISTORY IMPLEMENTATION AND ACCEPTANCE PASSED:** Search/count/cursor/offset repositories, snoozes, exact IDs, Unicode search and changing-history parity are verified. Cycle 12 generated 100,000 rows across ten tenants, retained nine unhinted plans and passed 6,000 requests over five minutes (p95 21.4ms, p99 31.5ms), with 301 database/pool snapshots. Broad contains-count remains a residual scan (about 272ms in its plan) and is outside the timed six-path request mix. Full shared-stack load and runtime activation remain D20/D25. See [cycle 12 evidence](../evidence/2026-11-03_decision_cycle12.md).
- [ ] **D20 / D23 / D24 — Failure acceptance:** complete real-business crash, cancellation/finish/cleanup, uncertain-commit and all-path restricted-role acceptance.
- [ ] **D14 / D15 / D24 — Performance acceptance:** agree numeric targets, then verify representative application load, cleanup plans, connection/resource budgets and shared-database contention.
- [ ] **D25 — Production release:** freeze/promote definitions and images, approve inspection/foreign grants, and complete shared-stack upgrade/recovery/restore/fencing rehearsal.
- [ ] **D21–D25 — Coordinated activation:** complete real-provider startup/runtime rehearsal and obtain the required cutover/deployment approval.

## Verified execution path

### D13–D20 cycle 4 — leased native index publication — 2026-11-03

Completed **D13.14b**. `RunNativeIndexJob` composes typed native planning,
immutable intent, pinned DDL, publication and the native completion fence on
one owner pool/schema/tenant. It uses persisted job columns and retains job-based
operation identity across retries. No row lock spans DDL. An already published
operation reconciles its receipt; job/audit/delivery completion remains atomic.
Local authority is rechecked in start and completion transactions.

Nine race-enabled integration scenarios cover physical success, audit rollback
and replay, denied authority, expired lease, unsupported text, foreign tenant,
different pool, authority loss after publication and lease loss before publication.
Registry and publication receipt tokens agree on success. Existing completion,
delivery and endpoint regression tests pass. See [cycle 4 evidence](../evidence/2026-11-03_decision_cycle4.json).

This is staged execution of a supplied committed lease and trusted retained
reference artifact. The caller still owns supervision and failure disposition.
Fixture authority and privileged fixture execution do not establish live identity
or restricted runtime grants. D13.15–D13.20 and the broader D13–D20 gates remain
open; PostgreSQL remains configured.

### D13–D20 cycle 3 — scalar evaluation and alias fixes — 2026-11-03

Completed **D15.11**: 219 actual-service golden cases across values, boolean and
comparison operations, strings/membership, arithmetic, timestamps and local
transforms, including canonical functions and aliases. Both engines assert
expected score/outcome, every rule result, complete returned/persisted evidence,
and unchanged decision/evidence counts on invalid evaluation. The corpus exposed
and now guards arithmetic aliases rejected by the helper and StringStartsWith
incorrectly selecting suffix comparison. Operation helpers now use canonical names;
plain/evidence tests also cover whitespace normalization.

Verification: race-enabled golden corpus, evaluator suite and existing
threshold/effect parity regressions passed with no failures or skips. Reviewed
source mapping was regenerated; mapping drift tests and affected-package vet
passed. See [cycle 3 evidence](../evidence/2026-11-03_decision_cycle3.json).
Model/authority providers remain explicit fixtures. This is not physical owner,
external provider, load or runtime activation acceptance. PostgreSQL remains
the configured application database; no application database was changed.

### D13–D20 cycle 2 — native-index completion fence — 2026-11-03

Completed **D13.14a**. D13.14 was split into a verified completion guard and
remaining physical worker composition (D13.14b), increasing the leaf total to
152 without closing D13. The opt-in `WithPublishedNativeIndex` execution adapter
locks registry → job → delivery, checks the expected publication token and
actual full native index, then commits job outcome, audit and acknowledgement
together. It reads persisted job columns and rejects missing/wrong/invisible or
unique indexes, closed/stale publication, foreign tables and expired leases.
An audit failure rolls everything back. A real lock-wait test changes the token
while completion waits and proves the stale result cannot commit.

See [cycle 2 evidence](../evidence/2026-11-03_decision_cycle2.json). The fixture
creates physical objects and registry tokens explicitly; it does not prove
planner → DDL intent → publication worker composition. Native search indexes
only are covered by this guard; text indexing remains an explicit separate
strategy. Runtime configuration remains PostgreSQL. D13.14b/D13.15 and the
remaining D14–D20 requirements are still open.

### D13–D20 cycle 1 — owner endpoints and history matrix — 2026-11-03

Completed D13.11–D13.13, D14.09–D14.11, D15.10, D17.10, D18.09, D19.11
and D20.07. Seven leaves are explicit design/inventory artifacts. Four are
implemented endpoint or verified history work. The owner model endpoint uses
publication-aware snapshots and mandatory trusted read authority. Index-job
create/retry now joins owner validation, metadata/history and durable delivery;
failed enqueue rolls back and concurrent retries produce one replacement.
History differential coverage adds eight filters and pagination boundaries.

Final race-enabled suites, owner compile and affected vet checks passed. Initial
test import failure and one missing-fixture skip were corrected and retained in
the [evidence](../evidence/2026-11-03_decision_cycle1.json). See the
[integration matrix](03_decision_engine_integration_matrix.md) for remaining
provider and acceptance cases. Model publication tokens and authority callbacks
in this cycle use explicit disposable setup; real worker publication, real
permissions, grants and load acceptance are still open. PostgreSQL remains
configured. Parent D16 alone remains complete in this range.

### D13–D20 continuation — custom-list failure handling and history snapshots — 2026-11-03

- [x] **D15 / D18 — Custom-list evaluation failure handling:** an ID lookup's authorization, cancellation, timeout, projection or storage error now propagates instead of triggering a name lookup that could succeed. PostgreSQL and MySQL adapters report confirmed absence through `ErrCustomListNotFound`, preserving their original no-row errors for existing callers.
- [x] **D18 — Legacy reference compatibility:** non-UUID names use the name query directly. UUID-shaped names retain fallback only after confirmed ID absence. Evaluator tests and real PostgreSQL/MySQL comparisons cover ID/name selection, values, missing lists and tenant isolation.
- [x] **D14 — Caller-owned history snapshot parity:** explicit PostgreSQL repeatable-read and target history snapshots keep count/page results stable across separate committed writers. New snapshots see the insertion. This is not a promise that different HTTP pagination requests share a snapshot.
- [x] **D16 — Existing completion retained:** configuration repositories remain complete within their recorded scope; this continuation does not reopen that task.
- [x] **Regression evidence:** 233 distinct top-level tests and 244 subtests passed with race detection in accepted package runs. The PostgreSQL inventory was refreshed after review, and the initially skipped source-provider tests passed on rerun with their isolated database setting.

Evidence: [commands, accepted runs and limitations](../evidence/2026-11-03_decision_d13_d20_acceptance.json).

The task audit confirms that the inactive runtime already builds same-scope list
readers and composes queued effects with the decision unit of work. This is
implemented composition, not activation or evidence of production authorization.

The following parent requirements remain open and must not be replaced by fixture
providers or inferred performance targets:

| Task | Remaining completion requirement |
| --- | --- |
| D13 | Configured model/index-owner and worker integration; reviewed release grants. |
| D14 | Complete acceptance corpus and representative load against agreed numeric targets. |
| D15 | Real-provider/business and broader crash/uncertain-commit acceptance; numeric performance targets and load. |
| D17 | Real auth/case provider integration and complete cross-engine browser acceptance. |
| D18 | Real auth-owned list action policy/grants and provider-backed runtime acceptance. |
| D19 | Real screening/effect provider and lifecycle integration, including their owner contracts. |
| D20 | Full provider conformance and restricted-grant/recovery rehearsal. |

The target [auth](06_authentication_and_authorization.md),
[case](05_case_management_and_investigation.md) and
[screening](04_screening_service.md) owner modules still record implementation as
NOT_STARTED. Their decision consumers and test fixtures are not those owner
implementations. Role-to-action policy and numeric load targets have been requested;
neither is inferred from fixture permissions or exploratory throughput.


### D13–D15 continuation — owner identity and fault recovery — 2026-11-03

- [x] **D13 — Owner identity and budgets:** preserve each job's tenant/table UUID through the client and validate the table-name/UUID binding against the fresh model before mutations. Reject missing, duplicate, oversized and out-of-scope projections; reject invalid HTTP job UUIDs.
- [x] **D13 — Accepted-job recovery:** a simulated owner acceptance followed by a failed response propagates the error. A subsequent start reconciles the accepted job without issuing another create. This does not establish concurrent-create idempotency or uncertain database-commit acceptance.
- [x] **D14 — Absent cursor boundary:** PostgreSQL/MySQL keyset pagination accepts an ordering boundary without a matching anchor row. The same corpus verifies inserts ahead of and behind a cursor. Evidence deletion remains forbidden; this is not a retention/deletion acceptance test.
- [x] **D15 — Persistence failure rollback:** actual DecisionService tests inject rule-evidence, workflow, screening, scoring and outbox insert failures on both engines. Each rejects the request without returning a provisional decision, and all seven checked table counts remain unchanged. Required-job failure coverage also passes.
- [x] **Regression verification:** 184 distinct top-level tests and 238 subtests passed with race detection, with zero failures or skips in accepted runs. Full decision-module compilation and affected-package vet passed; the source manifest was regenerated and verified.
- [ ] **Remaining parent gates:** configured owner/worker/grant integration, broader real-provider and crash/uncertain-commit acceptance, numeric performance targets and representative load/soak evidence remain open. PostgreSQL remains configured.

Evidence: [fault and recovery verification](../evidence/2026-11-03_decision_d13_d15_faults.json).

### D13–D15 continuation — bounded index preparation and parity — 2026-11-03

- [x] **D13 — Preparation APIs:** staged GET/POST scenario publications/preparation routes support valid draft and committed iterations. Strict decoding, trusted publish/read authority, coherent snapshots, fresh model validation and owner IO outside database transactions are composed. Missing providers fail explicitly.
- [x] **D13 — Bounded owner contract:** the data-model scoped index-job endpoint filters by tenant and up to 100 table UUIDs, with a 1–1000 result budget. Overflow is an error, never truncated readiness. PostgreSQL and MySQL repositories implement this read; the decision HTTP client validates tenant/table identity, count and response bytes. The actual HTTP source adapter also uses scoped queries; legacy source adapters retain their old interface.
- [x] **D13 — Start/status orchestration:** missing search jobs are requested through the owner API; failed jobs are retried, then status is reread. Navigation/repair jobs cannot satisfy search requirements. Cancellation and provider failures propagate. This is not a multi-job atomic transaction; earlier accepted jobs may remain after a later failure.
- [x] **D13 — Scoped query support:** owner PostgreSQL migration 000009 adds the tenant/table/request-time index. An unhinted plan selected it with 5,000 unrelated jobs; scope limits did not admit unrelated history. Owner SQL/HTTP tests cover bounds, duplicate inputs, tenant isolation and oversized projections.
- [x] **D14 — Changing-history cursor parity:** real PostgreSQL/MySQL pagination excludes a newer insertion ahead of the cursor and includes an older insertion behind it without duplicates or omissions. Existing golden filters, exact-ID, offset/cursor and snooze-boundary checks pass.
- [x] **D15 — Snoozed-rule business parity:** actual DecisionService comparisons cover score reduction and rule outcome evidence, case sensitivity, the source API's whitespace normalization and exclusive expiry. Existing threshold/effect/required-job rollback comparisons pass.
- [x] **Verification:** 187 distinct top-level tests and 227 subtests passed with race detection; zero failures/skips in accepted runs. Both modules compiled, affected-package vet passed, and the decision source manifest was regenerated and verified.
- [ ] **Parent gates:** D13 still needs configured target owner/worker/grant integration. D14/D15 still need broader provider/failure acceptance, agreed performance targets and representative load/soak evidence. None of these parent boxes is closed by this scoped checkpoint.

Job status preserves the source cancelled-job semantics; it is not evidence that a
physical index is usable. Publication still validates the actual registry/version
bindings. The staged IndexPreparation dependency must be explicitly supplied by
the intended owner; it does not authorize mixed application databases. PostgreSQL
remains configured. Migration 000009 was applied only to a disposable fixture; its
ordinary index build needs a deployment lock-duration review before production.

Evidence: [commands, results and limitations](../evidence/2026-11-03_decision_d13_d15.json).

### D17 continuation — scenario graph-copy HTTP — 2026-11-02

- [x] **Scenario-copy API:** POST /v1/tenants/:tenantId/scenarios/:scenarioId/copy accepts a strict name request and returns the copied scenario. Source iterations/rules and structured workflows receive new identities; rule-hit references are remapped to copied rules.
- [x] **Preparation outside write transactions:** a bounded, coherent source snapshot closes before fresh-model IO and AST validation. Current model-read authority, case inbox/tag permissions and registry versions are checked again before destination writes, in consistent lock order.
- [x] **Live-copy atomicity and permissions:** the new live link and publication event share the graph transaction. HTTP tests cover authentication, strict requests, revocation, closed model gates and publication failure rollback. Store tests cover missing/revoked/inactive case references, tenant isolation and preparation releasing its pooled connection.
- [ ] **D17 parent acceptance:** actual identity/case providers and complete provider-backed/cross-engine acceptance remain. The case authority functions used in these tests are explicit disposable fixtures.

The full MySQL store race suite passed, including query plans. The first broader
service run found a stale source manifest. Reviewed regeneration captured the fresh
model-client contract and portable workflow validator; the full service/audit race
rerun passed. Runtime/API/execution race tests, whole-module compilation and
affected-package vet also passed.
PostgreSQL remains the configured application database. Copy is not idempotent
and must not be automatically retried after an uncertain commit.

Evidence: [runtime tests](../evidence/2026-11-02_graph-copy-runtime.jsonl),
[initial store/service run](../evidence/2026-11-02_graph-copy-store-service.jsonl),
[clean service/audit rerun](../evidence/2026-11-02_graph-copy-service-rerun.jsonl).

### D13–D20 continuation — publication delivery and browser copy — 2026-11-02

The renewed request authorizes continued dependent-service implementation and
acceptance work. The earlier scope question is superseded; no further scope
approval is required for this implementation pass. Runtime activation remains
separate. Numeric performance targets have been requested because the acceptance
plan explicitly requires approved SLOs before performance can pass.

- [x] **D13 — Owner publication-version read contract:** an explicit publication-aware data-model repository reads metadata, revisions and eligible table publication tokens in one bounded snapshot. Closed/active gates and archived tables expose no usable token; ordinary metadata readers retain their contract.
- [x] **D13 — DTO/client delivery:** optional published_version passes through the owner DTO/OpenAPI and decision HTTP adapter. It is never synthesized from the global revision. HTTP model responses have a 64 MiB hard cap.
- [x] **D13 — Fresh preparation:** the decision preparation reader uses the HTTP adapter's fresh-model capability; refresh failure cannot fall back to its advisory cache. Registry version checks remain mandatory at use.
- [x] **D17 — Browser copy API and destination validation:** POST /internal/workflow-authoring/tenants/:tenantId/scenarios/:scenarioId/copy accepts destination_scenario_id, checks the destination model and field/operator compatibility, and uses the existing case permissions, version fences, priority bounds and atomic attributed audit writes.
- [x] **D13 / D17 — Focused verification:** owner snapshot/gate/tenant/budget/concurrent-publication checks, fresh-client checks and actual browser-copy HTTP success/denial/rollback/audit tests passed with race detection. Both affected modules compiled and affected-package vet passed.

Browser copying is not idempotent and is not retried after uncertain commits.
The publication-aware owner reader is opt-in; configured PostgreSQL routes are
unchanged. Identity/case contracts in the HTTP test remain explicit fixtures.
The full store race suite also passed with query-plan tests included. This is not production throughput or SLO acceptance. Scenario graph-copy HTTP, index-job/runtime provider wiring, real auth/case/
screening/dispatch migrations and full provider/performance acceptance remain
open. These completed subtasks do not close D13–D15 or D17–D20; D16 stays checked.

**Accepted continuation evidence:** 140 distinct top-level tests and 208 subtests passed with race detection, zero failures/skips. Query-plan tests were included. Both modules compiled and affected-package vet passed. See [commands, scope and limitations](../evidence/2026-11-02_decision_d13_d20_followup.json).

### D13–D20 implementation and acceptance audit — 2026-11-02

The following additional work is implemented and verified. Parent tasks remain
open where the acceptance requirements below are outstanding; D16 stays checked.

- [x] **D17 — Browser workflow HTTP list/save/delete:** mounted in the staged application with trusted session identity, strict request limits, saved-version checks and atomic audits. Forged actor headers cannot replace the authenticated session.
- [x] **D14 — History differential corpus:** 48 matching PostgreSQL/MySQL records, 14 filter cases, counts, cursor/offset pages, exact empty/trailing-space/case object IDs, tenant isolation and snooze expiry boundaries.
- [x] **D15 / D19 — Actual-service configured effects:** compare persisted workflow/screening/scoring effects and outbox records across both engines using deterministic identities and number-preserving JSON comparisons.
- [x] **D15 / D20 — Required-job atomicity:** fail job insertion on each engine and verify the actual service returns no decision and leaves all seven affected table counts unchanged.
- [x] **D16 / D20 — Regression verification:** store correctness and runtime/API/execution/audit race suites passed; whole-module compilation and affected-package vet passed.

**171 distinct top-level tests and 214 subtests passed, zero failures or skips.**
Query-plan tests were explicitly excluded from this run; production performance
acceptance remains deferred. Explicit model/authority/foreign-contract/job fixtures
provide local evidence, not real-provider conformance. See
[commands, results and limitations](../evidence/2026-11-02_decision_d13_d20_progress.json).

Remaining parent acceptance:

- [ ] **D13:** real published-model delivery, index-preparation job integration and approved release grants. The HTTP model adapter still lacks the required published-version contract; full local publication preparation already exists.
- [ ] **D14:** complete history acceptance beyond the recorded corpus and representative application-scale load.
- [ ] **D15:** broader business/provider differential and fault acceptance; deferred performance acceptance.
- [x] **D16:** configuration repositories and atomic ordered outcomes implemented and verified within the stated task scope.
- [ ] **D17:** scenario graph-copy HTTP, destination model/browser-copy composition, actual identity/case permission providers and full provider-backed browser acceptance.
- [ ] **D18:** actual list/platform permission adapters and grants, plus complete provider-backed runtime acceptance. Local APIs and transaction composition already exist.
- [ ] **D19:** actual screening/case/scoring/receipt/dispatch providers and complete lifecycle acceptance. Local queued effects and required-job atomicity are verified.
- [ ] **D20:** real-provider conformance, all-path failure acceptance and deployment-grant rehearsal.

The identity/case SQL authorization functions and screening source-view definitions
were found only in disposable decision test fixtures, not their owning services.
Installing those fixtures as production providers would not satisfy these gates.
Cross-service implementation scope and reopening previously deferred performance
acceptance await the user's answer. PostgreSQL remains the configured shared
application database; this work does not activate MySQL.

### Ten platform API and integration tasks — 2026-11-02

**All ten implemented, verified and checked.** T41–T46 are six remaining platform
endpoints; T47–T50 are four required integration tasks under D18/D19/D22/D24.
These are scoped subtasks, not completion of parent acceptance gates.

- [x] **T41 — Legacy custom-list lookup:** explicit list_name, source-compatible exact name matching with indexed folded-hash candidate filtering and bounded tenant collection.
- [x] **T42 — Record-tag lookup:** exact object identity and authorized bounded snapshot.
- [x] **T43 — Record-tag creation:** server identity/time, validated text and atomic insert/readback.
- [x] **T44 — Risk-snapshot creation:** append-only owned record, exact object identity and atomic persistence.
- [x] **T45 — IP-flag lookup:** exact IP text and bounded ordered results.
- [x] **T46 — IP-flag creation:** server identity/time, preserved IP/flag bytes and transactional persistence.
- [x] **T47 — Platform mutation authority:** separate explicit permission callback, no evaluation-persistence fallback, missing/denied/revoked and foreign-tenant checks.
- [x] **T48 — Strict request contracts:** explicit filter keys, repeated/unknown/malformed query rejection, strict JSON, required non-null fields and preserved explicit empty strings.
- [x] **T49 — Bounded platform projections:** HTTP/text/row limits, explicit overflow and no partial success, existing indexed repository queries.
- [x] **T50 — Atomicity and evaluator integration:** post-insert readback rollback, discarded provisional results, concurrent independent inserts, snapshot consistency and evaluator visibility.

All ten named actual-HTTP checks passed. The six endpoint tests include
authentication, foreign-tenant and revoked-authority checks. Additional store
tests cover absent/denied write permission without losing read access, separate
tenant records, invalid Unicode/NUL text, accumulated byte/row overflow, all three
write/readback rollback paths and concurrent evaluator-visible writes.
**63 top-level tests and 91 subtests passed with race detection; zero failures
or skips.** Whole-module compilation and affected-package vet passed. See
[commands, task mapping and limitations](../evidence/2026-11-02_decision_platform_tasks.json).

Authorities.PlatformMutations must be supplied by the trusted permission provider;
it has no permissive default and no new production auth adapter. Missing authority
returns 503 on mutation; denied authority returns 403. Required query/body fields
must be explicit, but empty strings remain valid. Empty list_name preserves the
source all-tenant-entries behavior under the configured cap. IP/object text is
not canonicalized or trimmed. Collections have no cursors and fail with 413 on
overflow. Repeated creates are not idempotent; uncertain commits are not retried.
No schema, index, migration lineage or runtime change was made. No performance
optimization/load claim is made. Test-run comparison, screening/provider and
remaining API integration, full parity, production acceptance and activation
remain open. PostgreSQL remains configured.


### Ten custom-list API tasks — 2026-11-02

**All ten implemented, verified and checked.** T31–T40 are API subtasks under
D18/D19/D22; parent migration acceptance and activation gates remain open.

- [x] **T31 — Custom-list collection:** authorized, tenant-scoped bounded snapshot.
- [x] **T32 — Custom-list detail:** exact tenant/list identity and missing-record behavior.
- [x] **T33 — Custom-list creation:** validated text, generated identity and name-conflict handling.
- [x] **T34 — Custom-list update:** atomic parent/linked-entry rename with rollback.
- [x] **T35 — Custom-list deletion:** scoped deletion and atomic linked-entry cascade.
- [x] **T36 — Entry collection:** exact parent check and bounded ordered results.
- [x] **T37 — Entry creation:** explicit value, exact bytes and locked parent-name binding.
- [x] **T38 — CSV import:** bounded single-file parsing, request deduplication and atomic 100-row insert batches.
- [x] **T39 — Entry update:** tenant/list/entry binding and preserved identity/creation time.
- [x] **T40 — Entry deletion:** scoped mutation, missing-record handling and parent-lock coordination.

All ten named actual-HTTP tests passed, including authentication, foreign-tenant
and revoked-authority checks. **58 top-level tests and 61 subtests passed with
race detection; no failures or skips.** Whole-module compilation and affected
package vet passed. Additional checks cover second-batch rollback, CSV/BOM/header/
quoted fields, byte/row limits, empty-import authorization, rename/import lock
coordination, unrelated-list progress and evaluator visibility after rename.
See [commands, task mapping and limits](../evidence/2026-11-02_decision_custom_list_tasks.json).

Collections fail with 413 on configured overflow and do not supply cursors.
Imports trim and deduplicate within one request; existing equal entries remain
allowed, so repeated imports are not idempotent. Individual entry writes preserve
whitespace and explicit empty values; missing/null values are rejected. Uploads
are parsed before mutation locks. Uncertain commits are not automatically retried.
The 256-entry query-plan fixture is scoped evidence, not production performance
acceptance. Legacy by-name and other remaining APIs, real authority/provider
integration, full parity, release and activation remain open. No schema migration
or configured runtime change: PostgreSQL remains configured.


### Ten administration API tasks — 2026-11-02

**All ten implemented, verified and checked.** T21–T30 are new API subtasks
under D12/D14/D16/D19/D22, not completion of the parent migration gates.

- [x] **T21 — Workflow configuration list:** bounded ordered snapshots, workflow-specific read authority and case-inbox permission checks.
- [x] **T22 — Workflow configuration detail:** exact tenant/scenario binding and destination authorization.
- [x] **T23 — Workflow configuration create:** validated configuration/outcomes, atomic insertion and guarded display-order allocation.
- [x] **T24 — Workflow configuration update:** atomic outcome replacement, old/new destination checks and stale-write rejection.
- [x] **T25 — Workflow configuration delete:** exact owner binding, current inbox permission, dependency conflicts and atomic deletion.
- [x] **T26 — Workflow reorder:** source-compatible UUID array, complete unique membership, guarded concurrency and transactional reorder.
- [x] **T27 — Test-run creation:** pin the live publication, enforce distinct same-scenario phantom iteration, validate expiry and persist atomically.
- [x] **T28 — Test-run cancellation:** authorized, row-locked status transition with rollback and repeat cancellation behavior.
- [x] **T29 — Rule-snooze creation:** strict identity/expiry validation, exact object bytes, tenant/scenario scope and atomic persistence.
- [x] **T30 — Outbox history:** authorized bounded latest-event window, strict limit parsing and existing payload DTO semantics.

Each task has a named actual-HTTP test including authentication, foreign tenant
and revoked-authority checks. Additional tests cover configuration/outcome and
test-run/snooze rollback, concurrent order allocation, independent-scenario
progress, stale writers observed waiting on SQL locks, list overflow, appending
beyond the list cap, inactive/denied inboxes and missing tag providers.
**57 top-level tests and 43 subtests passed with race detection; no failures or
skips.** Module-wide compilation and affected-package vet passed. See
[commands, task mapping and limits](../evidence/2026-11-02_decision_administration_tasks.json).

Case routing consumes the approved local inbox/tag contracts before workflow
owner locks. Missing trusted sessions/providers fail closed; no provider default
or arbitrary foreign-table mutation is added. Workflow changes recheck prepared
state after locking. Test-run creation/cancellation manage metadata; target
comparison evaluation/statistics remain separate unfinished work. Outbox reads
return a latest-N window, not full-history pagination. The 33-row allocation-plan
check is correctness evidence, not production load acceptance. Real providers,
browser/API gaps, full parity, release and activation remain open. PostgreSQL
remains configured.

### Ten historical-read API tasks — 2026-11-02

**All ten implemented, verified and checked.** These are new API subtasks under
D14/D19/D22; the parent acceptance and runtime-activation gates remain open.

- [x] **T11 — Decision history list:** bounded keyset pages, strict filters, optional snapshot-consistent total, no request payloads in lists.
- [x] **T12 — Scenario decision history:** exact scenario existence/scope and filter-bound continuation.
- [x] **T13 — Filtered decision count:** authorized tenant-scoped count with strict numeric and identifier validation.
- [x] **T14 — Decision detail and rule evidence:** decision payload and ordered evidence from one read-only snapshot.
- [x] **T15 — Scenario test-run list:** bounded source-compatible collection and missing-scenario behavior.
- [x] **T16 — Test-run detail:** tenant-scoped lookup and missing-record response.
- [x] **T17 — Active rule snoozes:** exact object identity including trailing spaces, exclusive expiry and bounded results.
- [x] **T18 — Workflow execution history:** decision-owned history with tenant/parent checks, without loading the parent payload.
- [x] **T19 — Scoring-request list:** bounded history for the exact tenant/decision; no external provider operation.
- [x] **T20 — Scoring-request detail:** exact tenant/request lookup with existing request/response DTO semantics.

Each T11–T20 has a named actual-HTTP test, including unauthenticated, foreign-tenant,
revoked-authority and unexpected-query rejection. Additional checks cover cursor
scope/filter replay, tied timestamps, exact object IDs, missing parents, read-only
enforcement, row/byte overflow and snapshot consistency with a concurrent writer.
**58 distinct top-level tests and 32 subtests passed with race detection; no
failures or skips.** Full module compilation and affected-package vet passed.
See [commands, task mapping and limits](../evidence/2026-11-02_decision_history_tasks.json).

Decision lists default to 50 rows (subject to the configured cap) and use keyset
pagination only; offset is explicitly rejected. Other collections fail on configured
overflow instead of returning partial history. Filter JSON is capped at 768 bytes
to keep continuation URLs bounded. Per-request snapshots do not freeze later pages.
The varied 512-row fixture used indexed access for tenant/scenario/exact-object
pages; an all-matching object fixture chose a scan. This is scoped plan evidence,
not production-scale performance acceptance. Counts and contains-search cost remain
data-dependent. Test-run statistics/evaluation, mutations and remaining APIs are
outside this batch. Configured runtime remains PostgreSQL; real providers, release,
full parity and activation remain open.

### Ten-task API completion batch — 2026-11-02

**All ten implemented and checked; scoped verification passed.** These are ten
API subtasks under D13/D16/D19/D22, not ten completed parent acceptance gates.
This checkpoint supersedes older statements that standalone validation, scoring
configuration APIs and publication-history pagination are missing.

Cycle 7 (historical) completed **20 additional subtasks**: D13.15-D13.20, D15.12/D15.15/D15.16, D17.23-D17.26, D18.16, D19.14-D19.16 and D20.08/D20.09/D20.13. **At that checkpoint 122/152 subtasks were checked; 30 remained.** Actual owner publication/recovery/grants, 19 aggregate/navigation rules with committed evidence parity, copy/edit/race acceptance, screening versioned mutations and projection delivery, shared fixtures, a reproducible runner and a resource-budget plan are verified. Five required integration tests plus 11 subtests passed without failures/skips; cleanup passed. See [cycle 7 evidence](../evidence/2026-11-03_decision_cycle7.json) for regression results and limitations. PostgreSQL remains configured; MySQL activation and full migration acceptance remain open.

Cycle 8 completes **D15.13 and D20.11**: actual platform read/evidence parity and target SQL-failure propagation, plus browser copying through scenario publication, evaluation, committed workflow action and attributed audit. **124/152 subtasks are checked; 28 remain. The requested next 20-item batch is incomplete.** Five top-level tests and 50 subtests passed with no failures/skips. Real workflow/screening receipt bindings remain implementation work; scoring/outbox provider contracts and user-agreed load targets are pending inputs. See [cycle 8 evidence](../evidence/2026-11-03_decision_cycle8.json).

**Cycle 9 (supporting implementation):** Added a bounded offered-rate load runner and deterministic disposable history fixture, with raw request samples, nine unhinted plans and database/pool snapshots. Five race-enabled tests and vet passed. The smoke used 240 rows and 12 requests; it is not agreed-scale acceptance. No additional leaves are checked: **124/152 complete, 28 remaining**. Numeric targets and scoring/outbox provider choices remain pending; workflow/screening receipt bindings remain implementation work. See [cycle 9 evidence](../evidence/2026-11-03_decision_cycle9.json).

- [x] **T01 — Scoring configuration list:** authorized bounded collection, ordered outcomes and scenario scope.
- [x] **T02 — Scoring configuration detail:** exact tenant/scenario/config binding and missing-record behavior.
- [x] **T03 — Scoring configuration create:** strict request validation and atomic configuration/outcome insertion.
- [x] **T04 — Scoring configuration update:** immutable identity, row locking and atomic outcome replacement.
- [x] **T05 — Scoring configuration delete:** scoped deletion, dependent-reference conflicts and rollback.
- [x] **T06 — Standalone iteration validation:** bounded snapshot/model validation with detailed trigger/rule errors; no commit side effect.
- [x] **T07 — Latest rules:** live-iteration preference, otherwise indexed latest-version lookup, without scanning iteration history.
- [x] **T08 — Iteration deactivation:** authorized atomic unpublish through the existing publication owner.
- [x] **T09 — Publication-history pagination:** bounded keyset pages, strict tenant/scenario-bound cursors and stable tie ordering.
- [x] **T10 — Rule-function catalog:** authenticated source-compatible catalog from the actual evaluator metadata.

Each T01–T10 has a named actual-HTTP subtest. Additional tests cover concurrent
scoring replacement/read snapshots, configuration/outcome rollback, revocation
and edits during model retrieval, validation depth/node/byte limits, latest-rule
lookup beyond the iteration-history cap and cursor scope/ordering/budgets.
**86 top-level tests and 52 subtests passed with race detection; zero failures or
skips.** Full decision-module compilation and affected-package vet passed. Source
mapping was regenerated against disposable PostgreSQL; this batch changes only
the validation-service source hash, not the target SQL lineage.
See [commands, task-to-test mapping and limitations](../evidence/2026-11-02_decision_ten_api_tasks.json).

Configured runtime remains PostgreSQL. Scoring APIs configure decision-owned
metadata; they do not implement an external scoring provider. Validation is
advisory and revision-labelled, never a commit/publication token. Publication
pagination is opt-in with `limit`/`cursor`; no-query history retains its bounded
source shape. Real providers, graph-copy/browser and remaining configuration APIs,
index-preparation jobs, full acceptance, release and activation remain open.

### Authoring implementation checkpoint — 2026-11-02

- [x] **D06–D08 / DE-01.2–3 API composition — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** The inactive MySQL application now exposes scenario create/list/get/update/delete, iteration create/list/metadata/get/update/clone/commit, rule create/list/update/delete and rule-group reads. Requests use existing DTO shapes, strict JSON/UUID/query validation, mandatory trusted authorization and bounded repositories. Duplicate-name/reference conflicts are redacted; uncertain transaction outcomes retain their separate failure response.
- [x] **Draft commit preparation — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Portable AST/type/threshold validation runs against a fresh bounded model outside database locks. The commit transaction rechecks current authority, locks the parent for sharing and the selected iteration exclusively, compares the complete prepared parent/iteration/rules, and rechecks published registry versions/gates before committing. Changed drafts, revoked authority and closed registry gates fail without committing. Target commit requires the existing target aggregate capability/registry checks; it does not create missing physical indexes or run publication jobs.
- [x] **Authoring verification — SCOPED TESTS PASSED:** Actual HTTP lifecycle, invalid inputs, cross-scenario binding, revoked access, immutable committed rules, stable-ID cloning, concurrent draft versions, failed-ID version rollback, failed-commit rollback and a rule edit during model retrieval. PostgreSQL/MySQL repository differential and preparation regressions also passed.
- [ ] **Remaining authoring/preparation integration — not active:** Scenario graph-copy and other browser/configuration HTTP composition, standalone validation/preparation responses, index-preparation job/API integration and real published-model/identity providers remain. These routes are not full source API parity. Authoring lists fail on configured overflow; cursor pagination and mutation replay after an uncertain commit are not supplied here.

Verification: **64 distinct top-level tests and 9 subtests**, race enabled, zero
failures/skips across retained accepted runs. Full decision-module compile and
affected-package vet passed. The fresh PostgreSQL schema-mapping artifact changed
only the shared validator's source hash; the 149-step MySQL SQL lineage is unchanged.
See [commands and limitations](../evidence/2026-11-02_decision_authoring.json).
No parent acceptance checkbox or production activation gate is closed. PostgreSQL
remains configured; provider, full differential, release/grants/restore and deferred
performance acceptance remain outstanding.

### Latest implementation checkpoint — 2026-11-02

This checkpoint supersedes older claims that the full prepared-publication
coordinator or the shared same-session pre-write guard is missing. It does not
close any D13/D15/D25 parent acceptance gate.

- [x] **D13 — implemented and verified in this checkpoint:** The inactive MySQL application now exposes GET/POST scenario publication routes. Publish uses the existing full AST/model/registry preparation coordinator; unpublish uses the authorized owner transition. History is tenant/scenario scoped and bounded, failing explicitly on overflow. Strict request decoding, revocation, foreign tenant rejection, closed model gates, failed-history rollback and error redaction passed actual HTTP/MySQL tests.
- [ ] **D13 — remaining acceptance at this checkpoint:** Real published-model/provider delivery, index-preparation job integration, remaining authoring/preparation APIs and approved runtime activation. Publication history has a hard bound, not cursor pagination; actions do not promise idempotent replay after uncertain commits.

- [x] **D15 / D24 — implemented and verified in this checkpoint:** Added an actual DecisionService PostgreSQL/MySQL differential corpus: non-triggering input, five score/threshold outcomes, full normalized rule evidence, persisted decision/evidence readback and invalid-input accounting. Existing authoring/browser differential and restricted-API actual-service tests passed again.
- [ ] **D15 / D24 — remaining acceptance at this checkpoint:** Complete business/effect/provider differential and fault/concurrency acceptance. The new corpus uses explicit model/authority fixtures and no external effects; it does not certify real providers.

- [x] **D25 — implemented and verified in this checkpoint:** Shared `Runner.ApplyGuarded` now runs the caller's bounded guard on the locked DDL session before ledger creation and every intent/DDL/completion write. `Release.Apply` binds the verified artifact and checks database/version/writer/locked definer through this guard. Rejection at all four write boundaries, single-connection execution, dirty-step recovery and decision fresh/upgrade/drift rehearsals passed.
- [ ] **D25 — remaining acceptance at this checkpoint:** Production release/image promotion, approved inspection/grant policies, full shared-stack fencing/restore, real owners and activation. The CLI remains artifact/grants/read-only verification; no deployment apply command or live migration was run.


Verification: 62 top-level tests and 39 subtests passed with race detection, zero
failures/skips, on disposable MySQL 8.4.8 and PostgreSQL fixtures. Whole decision
module compile, affected decision-package vet and shared migration vet passed.
See [commands, provenance and limitations](../evidence/2026-11-02_decision_implementation.json).
Existing PostgreSQL inventory/mapping inputs and target SQL lineage are unchanged.
Performance/load testing and Aurora remain excluded. PostgreSQL/River stays configured.

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

Current status: **D13–D20 continuation verified; full parent acceptance remains open**. Cross-service implementation is authorized. Numeric performance targets are still required; parent gates remain open until their documented criteria pass.

### Decision-owned verification / release / activation-safeguards pass

- [x] **V1 — local scope implemented/tested:** Identified independent correctness/grant/recovery work; shared-owner activation and inspection-policy gates retained
  **Recorded status:** FINISHED — decision-owned release audit.

- [x] **V2 — local scope implemented/tested:** Actual decision thresholds/evidence/effects/rollback under API grants; 93 other owner tests plus portable/source/HTTP/application regressions. Full end-to-end provider and business differential acceptance is not claimed
  **Recorded status:** EXPANDED CORRECTNESS / RACE SUITES PASSED; FULL BUSINESS/PROVIDER PARITY REMAINS.

- [x] **V3 — local scope implemented/tested:** Canonical checksummed artifact, exact source SQL/149-step lineage, pinned version and externally approved identity binding; production definitions not frozen/promoted
  **Recorded status:** ARTIFACT VERIFIER FINISHED; SCOPED TESTS PASSED; PRODUCTION PROMOTION REMAINS.

- [x] **V4 — local scope implemented/tested:** Explicit exhaustive API/worker DML, narrow immutable-key locking privileges, four-effect dispatch and forbidden dictionary/foreign/DDL/history writes. Foreign grants, all-path restricted-role acceptance and production inspection policy remain
  **Recorded status:** OWNER GRANT PLAN / SCOPED REHEARSAL PASSED; DEPLOYMENT POLICY REMAINS.

- [x] **V5 — local scope implemented/tested:** Fresh/140→145→149 upgrade, dirty-before/after DDL recovery, repeat apply, checksum/drift rejection, wrong writer/unlocked definer and unrelated-owner preservation. Not complete shared-stack cutover/restore acceptance
  **Recorded status:** DECISION-LINEAGE DISPOSABLE REHEARSAL PASSED.

- [x] **V6 — local scope implemented/tested:** Verified TLS, exact writer/version/locked definer, pinned-session inspection and build/source binding. Production apply intentionally unavailable until shared installer provides same-session pre-write guard; image build/promotion not performed
  **Recorded status:** READ-ONLY DEPLOYMENT PREFLIGHT / RUNBOOK FINISHED; SCOPED TESTS PASSED.

- [ ] **V7 — dependency/integration outstanding:** No foreign implementation in this pass. Needs approved target/window, frozen/promoted release, grant/inspection policy, writer fencing/recovery evidence and coordinated activation
  **Recorded status:** AWAITING DEPENDENCY — shared installer, real owners and deployment approval.

- [x] **V8 — local scope implemented/tested:** Expanded race suites, whole-module compile, executable build, affected vet and identical host/runner source hash; Docker packaging and performance excluded
  **Recorded status:** FINISHED — LOCAL VERIFICATION / SOURCE ATTESTATION.


Evidence: [decision-owned verification and release rehearsal](../evidence/2026-10-30_decision_release_verification.json).
The [owner release runbook](../../../backend/decision-engine-service/README.md#decision-owned-release-verification-and-rehearsal-2026-10-30)
records exact tool capabilities, grant/inspection limits and coordinated activation gates.

### Public API / application / owner-integration pass

- [x] **R1 — local scope implemented/tested:** Summary-only keyset pages; SQL status/attempt filters; tenant/scenario/filter-bound cursors
  **Recorded status:** IMPLEMENTATION FINISHED; SCOPED MYSQL RACE TESTS PASSED.

- [x] **R2 — local scope implemented/tested:** Collection/status-summary HTTP adapters and strict query validation
  **Recorded status:** IMPLEMENTATION FINISHED; SCOPED HTTP/SQL RACE TESTS PASSED.

- [x] **R3 — local scope implemented/tested:** Prepared recurring-schedule create/replace/disable with explicit concurrency preconditions
  **Recorded status:** IMPLEMENTATION FINISHED; SCOPED MYSQL RACE TESTS PASSED.

- [x] **R4 — local scope implemented/tested:** Recurring-schedule HTTP adapters, real publication/admission and correctness tests
  **Recorded status:** IMPLEMENTATION FINISHED; SCOPED HTTP/SQL RACE TESTS PASSED.

- [x] **R5 — local scope implemented/tested:** Explicit inactive application graph and backend fail-closed guard; no live command switch
  **Recorded status:** IMPLEMENTATION FINISHED; SCOPED APPLICATION TESTS PASSED — activation blocked.

- [x] **R6 — local scope implemented/tested:** Seven delivery kinds, scheduling, receipt-aware maintenance and joined shutdown
  **Recorded status:** IMPLEMENTATION FINISHED; SCOPED APPLICATION / SQL TESTS PASSED — activation blocked.

- [x] **R7 — local scope implemented/tested:** Same-scope model registry, screening source and optional trusted browser consumers; no fabricated providers
  **Recorded status:** DECISION CONSUMER COMPOSITION FINISHED; OWNER INTEGRATION BLOCKED.

- [ ] **R8 — dependency/integration outstanding:** Foreign MySQL implementations absent; user deferred expanded auth adapters to the auth integration pass
  **Recorded status:** AWAITING DEPENDENCY — auth/case/screening/receipt providers.

- [x] **R9 — local scope implemented/tested:** HTTP/SQL correctness, rollback, isolation, startup failure, cancellation and recovery tests
  **Recorded status:** SCOPED RACE / SQL CORRECTNESS TESTS PASSED; BROADER ACCEPTANCE REMAINS.

- [x] **R10 — local scope implemented/tested:** Whole-module compile, affected vet, refreshed 166-source artifact and matching host/runner hash
  **Recorded status:** FINISHED — compile/vet/source attestation/evidence.


Evidence: [public API / inactive application verification](../evidence/2026-10-30_decision_public_api_and_runtime.json).
Implementation details and the target-only public contracts are in the
[latest README checkpoint](../../../backend/decision-engine-service/README.md#inactive-public-api-and-application-composition-2026-10-30).
Missing foreign owners, additional auth adapters (explicitly deferred), complete
fresh published-model delivery and real receipt-provider conformance remain
dependencies. Only generated private test databases/roles were changed. This
is not frozen release/grants acceptance or an approved runtime switch.

### Stages 1–2 completion pass

- [x] **S1.1 — local scope implemented/tested:** Opt-in create/single-status/wait/cancel/redrive routes; strict decoding, trusted request factory and safe reconciliation responses. Admission is separately SQL-tested; not a full public deployment rehearsal
  **Recorded status:** IMPLEMENTATION FINISHED / SCOPED LOCAL TESTS PASSED — runtime blocked.

- [x] **S1.2 — local scope implemented/tested:** Bounded calendar/interval scheduling loop, actual owner occurrence transactions, pagination, required-job rollback and concurrent ticks
  **Recorded status:** IMPLEMENTATION FINISHED / LOCAL TESTS PASSED — runtime blocked.

- [x] **S1.3 — local scope implemented/tested:** Full AST/model preparation, trigger/aggregate/related registry gates, parent/model fencing and no provider IO under locks
  **Recorded status:** IMPLEMENTATION FINISHED / LOCAL TESTS PASSED — runtime blocked.

- [x] **S1.4 — local scope implemented/tested:** Five receipt-aware dispatch kinds; approved durable intent/idempotency/reconciliation contract, no legacy send-only fallback
  **Recorded status:** IMPLEMENTATION FINISHED / LOCAL TESTS PASSED — real providers blocked.

- [x] **S1.5 — local scope implemented/tested:** HTTP, scheduling, publication, dispatch, rollback and recovery fixtures on MySQL 8.4.8
  **Recorded status:** SCOPED CORRECTNESS TESTS PASSED — broader provider/deployment acceptance remains.

- [x] **S2.1 — local scope implemented/tested:** Actual PostgreSQL/MySQL effect/browser SQL corpora plus authoring/execution/wallet-operator comparisons and portable evaluator baseline; not complete provider parity
  **Recorded status:** SCOPED SOURCE/TARGET CORRECTNESS PASSED.

- [x] **S2.2 — local scope implemented/tested:** Callback crash/rescue, cancellation/finish races, stale fences, real committed transaction with simulated lost acknowledgement, and denied reconciliation without resend
  **Recorded status:** SCOPED RECOVERY / RACE TESTS PASSED.

- [x] **S2.3 — local scope implemented/tested:** Restricted dispatch worker, HTTP path tenant checks, revocation and missing providers; fixtures are not real auth/case providers
  **Recorded status:** SCOPED SECURITY TESTS PASSED — production grants/owners remain.

- [x] **S2.4 — local scope implemented/tested:** Exact 149-step lineage/shape, mandatory owner health checks, actual runtime/inspection target and pool binding, shared connection budget
  **Recorded status:** IMPLEMENTATION FINISHED / LOCAL TESTS PASSED — deployment prerequisites blocked.

- [x] **S2.5 — local scope implemented/tested:** Race-enabled packages and selected SQL regressions, full compile, affected vet, refreshed 165-source artifact and matching host/runner attestation; performance excluded
  **Recorded status:** FINAL LOCAL VERIFICATION FINISHED.


Evidence: [stages 1–2 local implementation/correctness record](../evidence/2026-10-30_decision_stage12.json).
Only generated disposable test databases/roles were changed. No application
database, EC2/Aurora target, configured backend, API route registration or live
worker activation was changed. Parent D13–D15 and D17–D25 remain unchecked where
real owner providers, remaining collection/recurring-admin adapters, full public
composition, frozen release/grants rehearsal or approved activation are needed.

### Worker integration — next five-task batch

- [x] **W1 — local scope implemented/tested:** Scheduled and async worker compositions, explicit policies and shared poller/supervisor; no runtime activation
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED.

- [x] **W2 — local scope implemented/tested:** Exact prepared identity/lease binding and atomic whole-batch completion/result/callback
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED.

- [x] **W3 — local scope implemented/tested:** Exact permanent receipt/owner reconciliation after ambiguous completion; no automatic replay
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; PURE/MYSQL TESTS PASSED.

- [x] **W4 — local scope implemented/tested:** Redacted failure transitions, cancellation, domain versus transport retries; real service authorization still required
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED.

- [x] **W5 — local scope implemented/tested:** Actual claim, whole-batch/evidence/effects, callback rollback, cancellation, invalid input, live selection, scope and pure uncertain-result branches
  **Recorded status:** SCOPED TESTS PASSED; BROADER ACCEPTANCE REMAINS.


### Admission and API integration — next five-task batch

- [x] **A1 — local scope implemented/tested:** Reauthorize and fence prepared model/scenario binding in one execution/job/event transaction
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED.

- [x] **A2 — local scope implemented/tested:** Bounded manual scheduled and async admission; concurrent replay without duplicate jobs/events; original replay survives model changes
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED.

- [x] **A3 — local scope implemented/tested:** Bounded committed-state waiting, zero-wait, cancellation and expiry without retained SQL transactions
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; PURE/SQL TESTS PASSED.

- [x] **A4 — local scope implemented/tested:** Explicit expected-generation redrive; callback-only preservation, stale/foreign/revoked authority checks
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; PURE/SQL TESTS PASSED.

- [x] **A5 — local scope implemented/tested:** Admission races/replay/tenant/queued-event rollback and waiting/redrive; full HTTP/provider/restricted deployment remains
  **Recorded status:** SCOPED TESTS PASSED; EXTENDED ACCEPTANCE REMAINS.


### Evaluation reader composition — next five-task batch

- [x] **C1 — local scope implemented/tested:** Narrow read-only evaluation ports; existing full source repositories remain compatible
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; RACE TESTS PASSED.

- [x] **C2 — local scope implemented/tested:** Actual same-scope configuration/history/platform/list read facades, no nil mutation methods
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED.

- [x] **C3 — local scope implemented/tested:** Explicit target evaluator/preparer construction with validated dependencies, enabled pushdown and policy
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; MYSQL RACE TESTS PASSED.

- [x] **C4 — local scope implemented/tested:** Preparation/worker/admission fixtures now use actual read components; platform/list values, absence, foreign/revoked authority and invalid scope/policy verified
  **Recorded status:** SCOPED MYSQL RACE TESTS PASSED.

- [x] **C5 — local scope implemented/tested:** Source service and selected persistence regressions, explicit manifest refresh, race/vet/whole-module compile and attested evidence passed
  **Recorded status:** SCOPED VERIFICATION FINISHED; BROADER ACCEPTANCE REMAINS.


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

- [x] **P1 — local scope implemented/tested:** Count/byte/identity/type limits, detached payload/model copies, exact IDs including spaces, independent repeated-ID items, all-or-nothing validation
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; PURE RACE TESTS PASSED.

- [x] **P2 — local scope implemented/tested:** Authorized owner/scenario/committed iteration/rule reads; scheduled running attempt/generation and pinned old iteration, never live-cache redirection
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; MYSQL 8.4.8 RACE TESTS PASSED.

- [x] **P3 — local scope implemented/tested:** Fresh bounded model, real trigger/aggregate registry checks, static AST validation and closed/stale publication rejection; real owner provider still required for activation
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; MYSQL 8.4.8 RACE TESTS PASSED.

- [x] **P4 — local scope implemented/tested:** Actual shared evaluator and local persistence body; opaque single-use output plans before any writes; no cross-item cache contamination; caller must use fenced atomic completion
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; PURE/MYSQL RACE TESTS PASSED.

- [x] **P5 — local scope implemented/tested:** Publication switch with one-connection pool, pinned score/evidence/ID, complete rollback, stored/empty/bounded candidates, authorization/tenant/model failures; source regressions/compile/vet pass; no performance suites
  **Recorded status:** SCOPED CORRECTNESS VERIFICATION FINISHED; BROADER ACCEPTANCE REMAINS.


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

- [ ] **B3 / D18,D19,D21,D22 — completion gate:** Atomic outputs/jobs/results/events, actual supervised scheduled/async workers and admission/wait/redrive components tested; live route/provider wiring and external dispatch remain
  **Recorded status:** WORKER/ADMISSION/READ COMPOSITION SCOPED TESTS PASSED; PROVIDER/HTTP RUNTIME REMAINS.

- [ ] **B4 / D13,D17,D18,D20 — completion gate:** Fresh model/registry/request/pinned evaluation composition and audited browser copying tested; complete publication preparation, index/API activation and real provider wiring remain
  **Recorded status:** REQUEST/PINNED PREPARATION AND BROWSER COPY TESTED; PUBLICATION/PROVIDERS REMAIN — not active.

- [ ] **B5 / D21–D23 — completion gate:** Scheduled/async supervised composition exists; callback/four-effect business dispatch, calendar runtime and real trusted identities/providers remain
  **Recorded status:** EVALUATION WORKERS SCOPED TESTS PASSED; EXTERNAL DISPATCH/PROVIDERS REMAIN — not active.

- [ ] **B6 / D15,D20,D24 — completion gate:** Full score/threshold/effect/browser comparisons and concurrent/crash/uncertain-commit acceptance
  **Recorded status:** TEST IMPLEMENTATION / ACCEPTANCE REMAINS — not active.

- [ ] **B7 / D14,D15,D24,D25 — completion gate:** Continue restricted installation/readiness and connection-configuration safeguards; no performance acceptance, load or query-plan testing in this pass
  **Recorded status:** SECURITY/READINESS IMPLEMENTATION REMAINS; PERFORMANCE TESTING DEFERRED BY USER.

- [ ] **B8 / D25 — completion gate:** Frozen migration references, fresh/upgrade/interrupted-step/recovery/grants rehearsal and release evidence; deployment requires separate approval
  **Recorded status:** RELEASE PREPARATION / ACCEPTANCE REMAINS — not active.


- [x] **B3.1 — local scope implemented/tested:** Same pool/schema/tenant/budgets; all requested action authorization before owner locks; no legacy unfenced execution adapter
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; MYSQL 8.4.8 CORRECTNESS TESTS PASSED.

- [x] **B3.2 — local scope implemented/tested:** Decision/effect/list and execution repositories share one transaction; actual DecisionService consumes narrow persistence port
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; MYSQL 8.4.8 CORRECTNESS TESTS PASSED.

- [x] **B3.3 — local scope implemented/tested:** Required-job/owner completion, shared error poisoning, pre/post lease and scope checks tested; full prepared request-item binding remains
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; MYSQL 8.4.8 CORRECTNESS TESTS PASSED.

- [x] **B3.4 — local scope implemented/tested:** Target generation/cancellation/replay DTOs; strict expected-generation decoding and authorized reads/cancellation; actual tenant/scenario, callback rollback and revocation tested; creation/admission, wait adapter and runtime routing remain
  **Recorded status:** SCOPED READ/CANCELLATION ADAPTERS FINISHED; PURE/MYSQL TESTS PASSED; ADMISSION/ROUTE INTEGRATION REMAINS — not active.

- [x] **B3.5 — local scope implemented/tested:** Actual service thresholds/evidence/effects/job rollback, mixed list/execution rollback and fenced completion; no performance or query-plan suites
  **Recorded status:** SCOPED SERVICE SQL/REGRESSION TESTS PASSED; FULL DIFFERENTIAL REMAINS — not active.


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

- [x] **B2.1 / D21,D23 — local scope implemented/tested:** Source-state audit, bounded latest-receipt lookup, explicit owner reset and required successor job in one transaction; stale-worker fences
  **Recorded status:** SCOPED MYSQL 8.4.8 RACE TESTS PASSED.

- [x] **B2.2 / D21,D23 — local scope implemented/tested:** Separately authorized, expected-identity redrive; preserve permanent receipts, monotone counters and reject successful/foreign-owner outcomes; real trusted provider/runtime adapters remain
  **Recorded status:** SCOPED MYSQL 8.4.8 RACE TESTS PASSED.

- [x] **B2.3 / D23 — local scope implemented/tested:** Approved exact-attempt decision-owned cancellation record; atomically cancel pending/in-flight transport, preserve results, never claim to undo external delivery
  **Recorded status:** SCOPED MYSQL 8.4.8 RACE TESTS PASSED.

- [x] **B2.4 / D23,D24 — local scope implemented/tested:** Shared maintenance scheduler with real local decision-owned dispatch-exhaustion mutations and bounded progress; external handler/provider/runtime integration remains
  **Recorded status:** SCOPED MYSQL 8.4.8 RACE TESTS PASSED.

- [x] **B2.5 / D24,D25 — local scope implemented/tested:** All 86 adapter tests, metadata/AST/identity/recurrence, exact 140→145 migration and affected vet passed; source attestation/teardown finished
  **Recorded status:** SCOPED VERIFICATION FINISHED; EXTENDED ACCEPTANCE REMAINS — not active.


Current B2 evidence: [four-effect lifecycle and pinned-engine verification](../evidence/2026-10-30_decision_effect_lifecycle.json). The optional cancellation plan now has 145 steps and preserves the exact earlier 115/140 prefixes. Fresh/repeated and exact 140→145 disposable installations passed. This does not complete the full release/grants/runtime rehearsal or the open parent tasks.

### Latest verification checkpoint — MySQL 8.4.8 (2026-10-30)

This checkpoint supersedes earlier **AWAITING DOCKER** / exploratory-only wording for the exact tested source and suites. All 78 top-level decision MySQL adapter tests passed in four non-overlapping race-enabled groups (3 new SQL, 13 lifecycle, 3 differential and 59 regressions). Metadata, identity codec, recurrence and complete AST package race tests passed. All allocated physical-publication scenarios, publication composition, populated text-index DDL and selected contract/unit tests passed on the pinned engine. The initial text-index fixture failed because it used the old field label in its DDL plan; the fixture now matches the actual registry binding. Production checks were not weakened. Fresh/replay and scoped prefix upgrades run within these fixtures; a full release migration/privilege rehearsal is still separate.

This earlier B1 checkpoint does **not** complete D13–D15/D17–D25: real provider composition, browser-specific copy auditing, complete main transaction/worker integration, application-scale load, frozen release definitions and deployment acceptance remain. The current B2 table above supersedes its original four-effect retry/cancellation and maintenance deferrals. Evidence for the earlier source: [completion batch and pinned-engine tests](../evidence/2026-10-30_decision_completion_batch_01_and_mysql848.json).

B1.1–4 scoped implementation and pinned MySQL 8.4.8 race tests are finished locally. Real provider/main runtime composition and the other open D tasks remain unfinished. The 140-step unreleased decision plan preserves its earlier 115-step prefix; passing current scoped fixtures is not complete release acceptance. No runtime activation, EC2 access, deployment, commit or push was performed.

### Approved completion roadmap — staged implementation and local verification

- [x] **B1.1 / D13 — local scope implemented/tested:** Verified physical publication persists mapping deltas; metadata-only paths retain mappings and version/receipt semantics; API/job activation remains separate
  **Recorded status:** IMPLEMENTATION FINISHED; SCOPED MYSQL 8.4.8 RACE TESTS PASSED.

- [x] **B1.2 / D13 — local scope implemented/tested:** Static aggregate template and caller-transaction registry capability checks passed; real full preparation/provider composition remains
  **Recorded status:** SCOPED IMPLEMENTATION/TESTS FINISHED.

- [x] **B1.3 / D17 — local scope implemented/tested:** Bounded scenario/workflow snapshot copying and guarded live publication passed; browser-specific audit/provider acceptance remains
  **Recorded status:** SCOPED IMPLEMENTATION/TESTS FINISHED.

- [x] **B1.4 / D18 — local scope implemented/tested:** Explicit read/mutate list authority shares the staged decision/effect/job transaction; main TransactionManager remains open
  **Recorded status:** SCOPED INTEGRATION/TESTS FINISHED.

- [x] **B1.5 / D13,D17,D18 — local scope implemented/tested:** Pinned SQL rollback/isolation/registry cases, pure tests and scoped vet passed; not full application acceptance
  **Recorded status:** SCOPED REGRESSION TESTS PASSED.


`DeliveryMaintenanceStore` composes the shared bounded scheduler with authorized rescue/cleanup primitives and scoped cursors, without starting it. Pure progress, stale-conflict, partial-commit, cancellation and scope tests passed. The execution rescuer fences expired transport before interpreting changed owner state. B2 adds a real local decision-owned dispatch-exhaustion handler and actual shared-scheduler SQL recovery/cleanup coverage; real identities, external business handlers and runtime poller composition remain open.

Next groups: compose inactive decision/effect/execution adapters (D19/D22/D23); complete public DTO/source adapters and browser-copy auditing; finish business differential, concurrency, grants/readiness and load acceptance (D15/D20/D24/D25). The user has reopened local disposable Docker testing; it is not a blanket blocker. Real auth/case/screening providers remain named integration dependencies, not permission to expand into their implementations. No runtime switch, deployment, or Aurora tests.

D22 is split into these five concrete implementation tasks:

- [x] **D22.1 — local scope implemented/tested:** Source request semantics, additive execution schema, exact-key idempotency and tenant-consistent constraints
  **Recorded status:** IMPLEMENTATION FINISHED; EXPLORATORY DDL TESTS PASSED.

- [x] **D22.2 — local scope implemented/tested:** Typed bounded scheduled/async create, replay, read and count repositories
  **Recorded status:** IMPLEMENTATION FINISHED; EXPLORATORY REPLAY/ISOLATION TESTS PASSED.

- [x] **D22.3 — local scope implemented/tested:** Attempt-conditional lifecycle mutations and independent callback state
  **Recorded status:** IMPLEMENTATION FINISHED; SCOPED LEASE/ROLLBACK/CALLBACK TESTS PASSED.

- [x] **D22.4 — local scope implemented/tested:** Calendar persistence/transaction and SQL test code finished. Runtime owner adapter remains blocked. Interval schedules are not full source parity
  **Recorded status:** SCOPED CALENDAR IMPLEMENTATION/TESTS FINISHED; BROADER VERIFICATION AWAITS LOCAL ENVIRONMENT.

- [x] **D22.5 — local scope implemented/tested:** Disposable-engine rollback, replay, tenant, concurrency and source-parity tests; evidence/status update
  **Recorded status:** SCOPED IMPLEMENTATION FINISHED; AFFECTED-PACKAGE VET/RACE PASSED.


Calendar follow-up is separated before starting:

- [x] **D22.4-C1 — local scope implemented/tested:** Local calendar frequencies, month-end clamp and explicit DST policy; no PostgreSQL runtime change
  **Recorded status:** SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED.

- [x] **D22.4-C2 — local scope implemented/tested:** Bounded calendar occurrence planner and missed-run policy handling
  **Recorded status:** SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED.

- [x] **D22.4-C3 — local scope implemented/tested:** Persist immutable calendar definitions and compose the transactional owner watermark path
  **Recorded status:** SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED.

- [x] **D22.4-C4 — local scope implemented/tested:** UTC/local weekday, leap/month ends, DST and tenant/rollback calendar cases
  **Recorded status:** SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED.

- [x] **D22.4-C5 — local scope implemented/tested:** Earlier fixture-I/O failures superseded for the current scoped suites; public adapters/activation remain blocked
  **Recorded status:** PINNED SCOPED REGRESSION / VET PASSED; RUNTIME INTEGRATION REMAINS — not active.


D23's remaining work is separated before implementation:

- [x] **D23.1 — local scope implemented/tested:** Three execution queues; mandatory tenant/kind worker authority; no handler activation
  **Recorded status:** IMPLEMENTATION FINISHED; SCOPED VET/RACE TESTS PASSED.

- [x] **D23.2 — local scope implemented/tested:** Domain-aware transport failure/exhaustion outcomes and required successor jobs
  **Recorded status:** IMPLEMENTATION FINISHED; SCOPED VET/RACE TESTS PASSED.

- [x] **D23.3 — local scope implemented/tested:** Seven-kind expired-lease rescue, concurrency/stale fencing and 5,000-row discovery plans; real handler/runtime composition remains
  **Recorded status:** SCOPED MYSQL 8.4.8 RACE TESTS PASSED.

- [x] **D23.4 — local scope implemented/tested:** Execution and approved four-effect dispatch cancellation/transport retention tested; public adapters and business/provider composition remain
  **Recorded status:** SCOPED MYSQL 8.4.8 RACE TESTS PASSED; REAL PROVIDERS REMAIN.

- [x] **D23.5 — local scope implemented/tested:** Seven-kind transport cases passed; full business-crash/restricted-runtime acceptance remains
  **Recorded status:** SCOPED SQL TESTS PASSED; EXTENDED ACCEPTANCE REMAINS.


D23.3's implementation group (only the explicitly named row is active):

- [x] **D23.3-R1 — local scope implemented/tested:** Bounded, indexed expired-delivery candidates for both fixed queue families, with explicit tenant/kind/rescue authority
  **Recorded status:** SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED.

- [x] **D23.3-R2 — local scope implemented/tested:** Execution resource → receipt → expired-delivery locking; transport retry versus exhaustion; required callback and owner outcome atomically
  **Recorded status:** SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED.

- [x] **D23.3-R3 — local scope implemented/tested:** Four-effect rescue with a mandatory real local owner exhaustion outcome; never invent successful business completion
  **Recorded status:** SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED.

- [x] **D23.3-R4 — local scope implemented/tested:** Heartbeat/rescue races, stale tokens, cross-tenant scope, callback-enqueue rollback and exhausted claims across all seven kinds
  **Recorded status:** SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED.

- [x] **D23.3-R5 — local scope implemented/tested:** Real rescue-query EXPLAIN over 5,000 rows for both families; not application EPS or complete crash/load acceptance
  **Recorded status:** SCOPED MYSQL 8.4.8 PLANS / RACE TESTS PASSED.


D23.4's approved implementation group:

- [x] **D23.4-C1 — local scope implemented/tested:** Target-only contract and schema; no PostgreSQL enum/runtime change
  **Recorded status:** SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED.

- [x] **D23.4-C2 — local scope implemented/tested:** Generation-conditional cancellation of one scheduled execution and its required active delivery
  **Recorded status:** SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED.

- [x] **D23.4-C3 — local scope implemented/tested:** Async cancellation with required cancellation callback; callback-only cancellation preserves evaluation
  **Recorded status:** SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED.

- [x] **D23.4-C4 — local scope implemented/tested:** Bounded authorized 24h/7d terminal-transport cleanup with indexes; keep permanent receipts/owners
  **Recorded status:** SCOPED MYSQL 8.4.8 / PURE RACE TESTS PASSED.

- [x] **D23.4-C5 — local scope implemented/tested:** Cancellation/callback/cleanup/rollback tested; full business-crash and restricted runtime acceptance remain
  **Recorded status:** SCOPED MYSQL 8.4.8 RACE TESTS PASSED; FULL ACCEPTANCE REMAINS.


### Remaining verification queue

Already passed: scoped 140- and 145-step fresh/repeated/prefix installations, exact 140→145 upgrade and metadata before/after guards; seven-kind recovery/cancellation/cleanup fixtures; both 5,000-row rescue-query plans and 5,000-receipt current-attempt lookup; all 86 existing decision-adapter tests and the scoped SQL/AST/source-target differential suites on MySQL 8.4.8. Do not repeat them merely because older evidence describes deferral; rerun for subsequent source changes.

- [ ] Complete the frozen release/shared-owner upgrade and dirty-step recovery rehearsal, including the exact prior 115-step deployment and full restricted runtime installation/grants. Scoped constructor fixtures are not this release rehearsal.
- [ ] Add/execute broader concurrent real-business cancellation/finish/cleanup and exact-boundary/crash acceptance; the existing seven-kind fixture passes are not full crash certification.
- [ ] Extend shared-scheduler acceptance to all real runtime/provider compositions, including uncertain-commit partial progress and business-crash paths. Local decision-owned dispatch exhaustion and actual scheduler SQL recovery/cleanup/cancellation now have scoped pinned-engine coverage; this is not external-handler acceptance.
- [ ] Prepare/measure representative cleanup-query plans and application-scale resource/load budgets. Existing scoped plan evidence is not an EPS or throughput claim.
- [ ] Complete missing service score/effect/browser differential coverage and real provider/startup/runtime rehearsal after their implementation. Aurora remains excluded; runtime stays inactive.

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
- [ ] **D13 / DE-01.2 — SCOPED READER/REQUEST PREPARATION TESTED; PUBLICATION/PROVIDERS REMAIN — not active:** Registry-bound queries, scalar/filter validation, indexed object-ID lookup, optional collision-safe text equality and source/target/HTTP wallet comparisons passed scoped MySQL 8.4.8 race tests. The owner persists text-index mappings through verified physical publication. Target request/pinned preparation now composes fresh bounded models, item/static AST validation and actual trigger/aggregate registry checks. RegistryAggregatePublicationGuard retains its mandatory remaining full publication guard. Full local publication preparation and the opt-in owner publication-version read contract are implemented. Draft/committed preparation status/start APIs and bounded owner index-job contracts now pass scoped tests. Configured shared-database model/index-owner/worker integration and release grants remain open; optional absent indexes are not a performance-readiness claim.
- [ ] **D14 / DE-01.3 — IMPLEMENTATION FINISHED; EXTENDED TESTING REMAINS:** Search/count/cursor/offset repositories and rule snoozes implemented; exact IDs remain byte-exact, including empty strings and trailing spaces. Tests verify approved Unicode case-insensitive/accent-sensitive history search separately from exact IDs. Unhinted indexed plans over 5,000 decisions and 20 scenarios passed for tenant/scenario, exact/hot object and cursor paths; contains search remains a residual filter. Changing-history cursor parity also passes with inserts ahead of and behind the cursor. Application-scale load and complete parity acceptance remain open.
- [ ] **D15 / DE-01.4 — ACTUAL SERVICE MYSQL CORPUS PASSED; FULL DIFFERENTIAL REMAINS — not active:** Wallet AST/authoring differential tests and evidence rollback/isolation exist. The actual DecisionService now passes MySQL score/threshold/evidence/configured-effect and required-job rollback tests. Snoozed scoring, source object-ID normalization and expiry comparisons now pass on both engines. Broader provider/fault coverage remains; performance acceptance awaits agreed numeric targets and representative load.

### Batch 4 — workflows and other effects

- [x] **D16 / DE-01.2–3 — IMPLEMENTATION FINISHED; SCOPED TESTS PASSED:** Workflow and screening/scoring configuration repositories with ordered outcome children and atomic replacement, verified on the exploratory fixture. Storage retains NULL elements; existing public/source `[]string` ports cannot represent an element NULL and fail explicitly rather than discard/coerce it. No historical array import/backfill is needed.
- [ ] **D17 / DE-01.3 — SCOPED AUTHORING/AUDITED BROWSER COPY TESTED; REAL PROVIDERS REMAIN — not active:** Structured/browser editing, version checks, ordered snapshots and audited soft deletion are tested. Browser-specific copying now composes attributed batched audit records and graph writes on one transaction; the approved <=500 inbox write-authorization consumer and bounded tag batches precede owner locks. Permission/active/provider failures, destination caps, concurrent priorities, audit rollback and tenant isolation passed on MySQL 8.4.8. Browser-copy HTTP and destination model/registry preparation now pass scoped tests. Scenario graph-copy HTTP and preflight model/case/registry preparation now pass scoped tests. Full cross-engine/provider acceptance and real auth/case providers remain open.
- [ ] **D18 / DE-01.2 — SCOPED LIST/EFFECT INTEGRATION TESTED; MAIN RUNTIME COMPOSITION REMAINS — not active:** List storage and source-compatible name/value semantics passed pinned-engine tests. WithCustomLists adds the same transaction to staged decision/effect/job work, with distinct decision.list.read / decision.list.mutate authorization before owner locks. Read-only access cannot mutate lists; list/decision/outbox/job rollback passed. Real auth support/grants for these actions and complete evaluation/main TransactionManager composition remain open.
- [ ] **D19 / DE-01.2–3 — INTEGRATION REMAINS — not active:** Workflow and screening/scoring execution plus optional outbox repositories are implemented/tested in a staged EffectStore. Hidden authoritative iteration bindings enforce decision/scenario/config relationships with existing composite FKs, without rewriting the earlier lineage. Screening source reconciliation is monotonic and rejects legacy unversioned updates once attached. Decision/effect/tag rollback and 120-row batch readback are verified. An explicit QueuedEffectStore now composes the four effect owners with required first-attempt jobs and transaction poisoning on enqueue failure; the persistence-only store remains separately named/documented. This is NOT the live TransactionManager: complete owner/provider and seven-kind lifecycle composition remain open. Missing source providers fail explicitly.
- [ ] **D20 / DE-01.4 — EXPANDED LOCAL CORRECTNESS TESTED; PROVIDER/RELEASE ACCEPTANCE REMAINS — not active:** Actual PostgreSQL/MySQL decision/effect storage and browser save/list/update/visibility/conflict corpora now pass, alongside browser audit/copy/rollback tests. New dispatch worker restricted-grant and permanent-intent guards are separately tested. These corpora use explicit foreign-owner contract fixtures, not real auth/case/screening/dispatch implementations; complete service/provider conformance and deployment grants rehearsal remain open.

### Batch 5 — durable workers and activation prerequisites

- [ ] **D21 / DE-01.2 — SCOPED LIFECYCLE / APPLICATION COMPOSITION TESTED; REAL IDENTITIES BLOCKED — not active:** The strict seven-kind v1 codec and exact `(kind,tenant,resource,domain_attempt,redrive_generation)` reservation contract remain unchanged. Four-effect initial reservations, expected-identity domain retry and separately authorized redrive have scoped MySQL 8.4.8 race tests, including required-job rollback, retained receipts, concurrent producers and historical indexed current-attempt evidence. Both counters remain monotone; redrive does not replenish the domain budget. D22 supplies the other three kinds' lifecycle. The inactive application graph now composes all seven kinds with bounded tenant/worker inventory, receipt-aware maintenance and joined shutdown on one pool/database. Real worker identities, receipt providers and approved activation remain blocked; decoding is not authority.
- [ ] **D22 / DE-01.2 — DECISION EXECUTION APIs / INACTIVE APPLICATION COMPOSITION TESTED; OWNER ACTIVATION BLOCKED — not active:** Opt-in create/detail/wait/cancel/redrive, keyset collection/filter/status-summary and calendar recurring-administration handlers are implemented. Preparation, admission, publication and all seven delivery kinds now have a concrete same-scope application graph, cold/hot readiness and bounded shutdown. Schedule replacement/disable uses exact ID/watermark preconditions; old/new switch is atomic and tick-compatible. No network IO runs under owner locks. Real trusted identity, full published-model provider delivery, provider conformance and approved command/backend activation remain open. The user deferred new auth action adapters; PostgreSQL routes are not a fallback for missing target behavior.
- [ ] **D23 / DE-01.2 — DECISION-OWNED HANDLERS / RECOVERY TESTED; REAL PROVIDERS BLOCKED — not active:** Actual callback/workflow/screening/scoring/outbox dispatch components now require the approved attempt-aware send-intent/receipt/reconciliation contract. SQL tests cover atomic receipt/owner/queue outcomes, lost-local-ack rollback, callback crash/rescue without duplicate send, cancellation during external IO, restricted worker rights and retained proof after transport cleanup. Existing seven-kind recovery/retention and monotone domain retry/redrive remain. Source-managed screening fails closed; real provider idempotency/reconciliation/destination policy and service identities remain dependencies. No external send is claimed reversible or exactly-once.
- [ ] **D24 / DE-01.4 — EXPANDED PINNED CORRECTNESS / RESTRICTED-ACCOUNT TESTS PASSED; COMPLETE ACCEPTANCE REMAINS — not active:** 93 additional owner tests passed with query-plan cases excluded, plus real service thresholds/evidence/effects/required-job rollback under owner API grants and four-effect intent/receipt/ack under worker grants. Source/target authoring/effect/browser/execution/wallet comparisons, HTTP/application readiness, tenant/revocation, cancellation/recovery and portable/source regressions passed race-enabled suites. Full actual-service PostgreSQL/MySQL business comparison, provider conformance and all-path production grant acceptance remain open. Performance/load/query-plan acceptance remains explicitly deferred.
- [ ] **D25 / DE-01.1–4 — DECISION RELEASE TOOLING / LOCAL REHEARSAL PASSED; PRODUCTION RELEASE/ACTIVATION BLOCKED — not active:** Canonical artifact/source/version/image binding, explicit owner API/worker grant plans and read-only TLS/pinned-writer inspection are implemented. Private MySQL 8.4.8 fresh/140→145→149, interrupted-DDL before/after recovery, repeat apply, checksum/drift rejection and unrelated-owner preservation passed. Production definitions/image build/promotion, inspection/foreign grants, complete shared-stack upgrade/restore/fencing, real owners and approved activation remain. The library installer now checks target identity on its locked DDL session before every write; no production apply/force/down/activate command is exposed. Aurora and performance remain excluded; no application database/runtime was changed.

The [source inventory and design review](03_decision_source_inventory.md#full-owner-mapping-and-review-checkpoint) explains the unresolved choices. The [machine manifest](../../../backend/decision-engine-service/internal/migrationaudit/testdata/decision-schema-mapping.json) is a review artifact, not a MySQL installation plan.

## Tests and acceptance

POC-10/11, PERF-01/06; reuse internal/service/rule_evidence_integration_test.go, execution_service_integration_test.go, async_validation_integration_test.go, internal/store/postgres/tenant_data_reader_test.go and runtime/ast_eval/aggregate_http_integration_test.go. Add golden full rule outcomes, not merely HTTP 200/202.

No decision threshold changes from numeric coercion. Same logical input/model/rules produces equivalent evidence under the defined read contract. Read mode and worker URL must not silently select stale/separate authority.

## Delivery and rollback dependency

Implement and validate in disposable environments. All shared consumers must be compatible before live activation; an individual module passing its suite does not authorize a partial cross-engine production cutover. Preserve old adapters/compatible API artifacts through rehearsal as needed, with a single chosen runtime authority. The [roadmap](../10_implementation_roadmap.md) owns task dependencies and [cutover plan](../11_data_transfer_and_cutover.md) owns data rollback. Module schema rollback cannot undo externally delivered effects.
