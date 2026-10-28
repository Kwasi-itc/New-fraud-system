# Validation and proof-of-concept plan

POC-01 and POC-04 are **IN_PROGRESS** with verified foundation subtests recorded in [the execution log](16_execution_log.md). POC-04 currently covers only pinned DDL lock mechanics, not publication or failover. POC-02/03 and POC-05 through POC-11 remain **NOT_STARTED**. Passing isolated foundation tests does not establish service parity. Documentation research is not an experiment. Use disposable databases identified by explicit test configuration and verified target guards; do not execute destructive fixtures against the application database. Pin server/driver/revision and archive inputs, outputs and observed anomalies. The [audit testing strategy](../08_testing_and_validation_strategy.md) and module test lists remain applicable.

## Proofs before architecture acceptance

| ID | Question / setup | Failure injection and assertions | Artifact / acceptance owner |
|---|---|---|---|
| POC-01 | Driver, codecs, current locks and cleanup on exact MySQL build | UUID/time/JSON/null/binary vectors; deadlock/timeout/cancel; connection reuse; lost commit ack; TLS failure | Typed outcomes, no leaked transaction/session state; DB owner |
| POC-02 | Full durable-work lifecycle, all 15 job contracts | Crash at enqueue/claim/effect/ack; stale heartbeat; lease reclaim; active dedupe; schedule restart/catchup; poison job/redrive; queue fairness | No lost committed work, no stale local effects, external duplicates accounted for; worker/ops owners |
| POC-03 | Exact key representation and enforced allocation | Long multibyte values, equal/unequal trailing spaces, forced identical hashes, concurrent allocation, forged key IDs, dynamic unique fields | Equal values share identity, unequal values never merge, direct bypass denied, cost measured; schema/security owners |
| POC-04 | Full runtime schema publication | Crash before/after every DDL/publication step; disconnect after success; two executors; old primary fencing; field archive/display/index drift; concurrent ingestion | No incompatible published schema; deterministic repair; no second live DDL from lease timeout; model owner |
| POC-05 | Targeted ingestion plus idempotency | Same key/hash, same key/different hash/kind, alternate unique collision, no-op patch, batch duplicate object, cancel/unknown commit | Correct row targeted; all data/audit/job/receipt effects atomic; ingestion owner |
| POC-06 | Screening claim/projection delivery | Old response after reclaim; partial zero matches; new source version during projector; routine failure/savepoint; attempt fence | No stale candidate or projection overwrite; newer queue entry retained; screening/decision owners |
| POC-07 | Immediate FK review/exclusion transition | Fail each write; forge tenant/event/resource; replay/op hash conflict; immutable update/delete; accidental routine autocommit | Full review/exclusion/resource/case event atomicity and enforced privilege boundary; screening/case/security owners |
| POC-08 | Identity under concurrent change | Last-admin removals, bind issuer/subject races, revoke/disable while write waits, expired session after wait, invitation generation | Current authorization and minimum admin invariant; no cross-tenant access; identity/security owners |
| POC-09 | Case capacity and transfer | Two requests for last slot, reopen/move/worker/direct SQL bypass, concurrent decision transfer, inverse lock order | Capacity/membership invariant from all paths, unrelated cases progress; case owner |
| POC-10 | Numeric/JSON/array/aggregate/snapshot parity | Empty/null/large numeric inputs, rule threshold edges, ordered workflow children, report concurrent writes, max caps | Canonical result and evidence parity; no N+1 or cap removal; decision/case/model owners |
| POC-11 | Collations, private history and pagination | Unicode/case/accents/trailing spaces, wildcard inputs, malformed manual origin, equal time IDs, unauthorized inbox/actor | Correct equality/search domain, stable pages, private visibility; module/security owners |

A POC can fail the recommendation. Record actual failure, revise the ADR and repeat only the affected evidence. Never mark all features supported because one happy-path fixture passes. New schemas/prototypes are future implementation work requiring a separately authorized execution phase.

## Automated validation layers

- **Unit/contracts:** canonical keys, UUID/time/IP codecs, typed query construction, safe identifier allowlists, error mapping, transition planning and deterministic job/schedule identities. Avoid tests that merely restate SQL strings.
- **Real-engine repository:** every SQL/constraint/routine and migration path; fixed grants and negative direct SQL; empty/null/max-size/boundary input; transaction rollback and cancellation. Record executed vs skipped totals.
- **Service:** real owner repositories and transactional enqueuer behind HTTP entrypoints; unchanged status/response/UUID/cursor/replay behavior. Optional dependencies must fail explicitly when required.
- **Cross-service:** SQL model contracts, both decision tenant-reader modes, identity/model/case authorization, screening projection and investigator/case review in one transaction. Full shared migration graph, not isolated-owner tables only.
- **Concurrency/fault:** deterministic barriers controlling race order, many repetitions, process/connection termination and delayed provider responses. Assert committed rows and external receipts, not only response status.
- **Migration/data:** full FK/unique/tenant validation, canonical chunk checksums and ID coverage, role/array/JSON/IP/time transforms, grants/routines and pending-work mapping. Fresh boot and upgrade/recovery are different cases.
- **Performance/operations:** [PERF-01 through PERF-09](06_performance_and_scalability_design.md), paired PostgreSQL baseline, soak, failover, backup/PITR and rollback rehearsal.

## Evidence requirements

Every run records immutable test revision, server/driver configuration without secrets, fixture hash/scale, grants, migration versions, discovered/executed/passed/failed/skipped counts, elapsed times and logs with sensitive values redacted. A skipped real-DB suite blocks release. Authentication/provider tests distinguish local app identity from external OIDC state. Differential comparisons normalize representation only where the contract permits it; never normalize away a different decision, tenant, order or missing event.

Verification groups V-DM, V-IN, V-DE, V-SR, V-CM, V-AU, V-DB and V-OP refer to corresponding module suites. V-01 in the roadmap coordinates these; PF-01 is the measured performance task. Acceptance is zero unexplained integrity/security/business-result differences plus the approved G0 performance/recovery objectives. No numeric production target is invented by this plan.

## Existing reusable test locations

Exact files are linked in the generated [traceability register](12_traceability_and_coverage.md). Preserve their behavioral assertions while replacing setup/drivers. Many current tests are PostgreSQL-bound or skip without URLs; they are starting points, not evidence of MySQL readiness. Historical reverted prototype test results are not reused as current passing evidence.
