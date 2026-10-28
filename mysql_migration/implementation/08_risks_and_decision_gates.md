# Risks, dependencies and decision gates

Likelihood is not statistically known without deployment/workload inspection. Severity reflects impact if the failure occurs. References PG-xx link through the [replacement inventory](02_postgresql_feature_replacements.md). Every risk has a blocking acceptance gate, not a promise that it has been solved.

| Risk | Severity / likelihood | Evidence and failure | Mitigation / owner task / gate |
|---|---|---|---|
| RK-01 Premature model publication | Critical / mechanism certain for naive translation | PG-05/06; MySQL DDL implicit commits | P-01 state machine, crash recovery; DM-01/POC-04 |
| RK-02 Lost or duplicate work | Critical / unmeasured | PG-10/14; River transactional enqueue | P-03, receiver dedupe, all 15 kinds; Q-01/POC-02 |
| RK-03 Long-key identity collision | Critical / key-size exposure verified, data distribution unknown | PG-16/25; long TEXT tuples | Exact dictionary/native bound, forced collision; SC-01/POC-03 |
| RK-04 Partial exclusion/review ledger | Critical / naive write order fails | PG-16/18; deferred FK graph | Parent-first transition plan; SR-01/POC-07 |
| RK-05 Privilege or tenant regression | Critical / unmeasured | PG-17/18/35; shared contracts and ID-only gaps | Narrow grants, tenant FKs, bypass tests; AU-01/CM-01/POC-08 |
| RK-06 Stale workers overwrite current state | Critical / race-dependent | PG-13/15; claims and monotonic versions | Fence business publication, source->queue order; SR-01/POC-06 |
| RK-07 Assignment/admin invariant lost | High / race-dependent | PG-17/19; trigger/advisory guards | Complete restricted owner mutation paths; CM-01/AU-01 |
| RK-08 Wrong conflict/no-op interpretation | High / likely under literal port | PG-08/28; multiple unique keys and row counts | Typed outcomes and scoped guards; IN-01/POC-05 |
| RK-09 Rule/search/order changes | High / data-dependent | PG-20/23/25/26/34 | Differential edge corpus, snapshots and plan tests; DE-01/PF-01 |
| RK-10 Provider state loss/storage conflict | Critical / unresolved | PG-29; provider-owned schema and export omissions | Pinned release migration/state plan and storage decision; AU-02 |
| RK-11 Cutover loses target writes | Critical / unresolved until rehearsal | PG-18/30; shared atomic contracts | Quiesce all writers and explicit post-write rollback policy; DT-01 |
| RK-12 Performance not representative | High / evidence limitation confirmed | Historical report excludes worker saturation and has ingestion failures | Paired full-system workload, approved SLOs; PF-01 |
| RK-13 Live drift or hidden objects | High / unknown | Audit excludes live schema/dependency-owned DDL | Read-only object/grant/data profile; B-01 |
| RK-14 Queue/dictionary maintenance burden | High / staffing unknown | New proposed mechanisms | Named maintainer, lifecycle tooling, soak, or validated alternative; Q-01/SC-01 |
| RK-15 Dynamic DDL exceeds engine limits | High / actual field/table distribution unknown | PG-05; row/key/column/index limits | Profile widest models and counts, fail safely before publication; SC-01/DM-01 |

## Decisions needed before implementation acceptance

G0: pin MySQL patch/driver/provider releases; approve architecture owners; choose deployment/recovery model; establish RPO/RTO, SLOs, retention, downtime and data scale. B-01 profiles live applied migrations, extensions, permissions, sequences/provider/River objects, key lengths, floats/timestamps/INET and outstanding work. No credentials or live data were inspected here.

G1: accept or reject ADR-03/04/05/07 only after POCs show recoverable DDL, complete durable work, exact-key enforcement and safe owner routines. A design-level recommendation is not permission to skip these gates. Failure requires revising the ADR and affected module plans, not weakening the invariant.

G2: approve the schema transform manifest and public API compatibility, including pending schema operations, timestamp/version encoding, manual actor migration and any length changes. No arbitrary new length limits are approved.

G3: all modules and real-MySQL tests pass; grants and shared migration order verified. G4: performance/recovery/data rehearsal passes agreed objectives. G5: explicit operational cutover approval with named rollback authority and evidence bundle. This phase completes design documents only; G0-G5 remain unpassed.

## Existing gaps versus migration obligations

Current source includes ignored schema errors, optional no-op enqueue paths, ID-only relationships and API-level tests that do not fully validate outcomes. Record these precisely in characterization tests. Do not accidentally carry silent success into the target design, and do not label unrelated security redesign as already authorized implementation. Required fixes that enable the migration must have distinct acceptance cases and an owner. No claim is made that the present PostgreSQL deployment already satisfies every target control.
