# Internal provider verification

## Phase 8 acceptance update — 2026-11-15

The [acceptance register](INTERNAL_PROVIDER_ACCEPTANCE.md) is the definitive current gate record. All 21 browser failures are repaired: **134 full-suite passes, zero failures, one dedicated skip**, plus a separate real API/PostgreSQL/worker **1/1 pass**. Final screening regression is **138/288**, auth **33/60**, decision **260/423**, case **90/157**, investigator **1/1** (top-level / including subtests). Name quality remains unaccepted; separate identifier engineering calibration is documented. The complete sustained workload measured **87.487 decisions/s, p95 290.738 ms**, below target. Migration 22, actual restricted logins, restored-clone binding, logical restore/worker replay and operational alert contracts pass disposable checks. Source permissions, representative quality/capacity, production security/recovery/alerting and explicit activation remain open. Existing frontend lint (20 errors/74 warnings) reproduces on HEAD and is recorded separately. **No production-ready claim or activation is made.**

See [matching](INTERNAL_PROVIDER_MATCHING_EVALUATION.md), [browser investigation](INTERNAL_PROVIDER_BROWSER_REVIEW.md), [source approvals](INTERNAL_PROVIDER_SOURCE_APPROVALS.md), [performance](INTERNAL_PROVIDER_PERFORMANCE.md), [security](INTERNAL_PROVIDER_SECURITY_ACCEPTANCE.md), [recovery](INTERNAL_PROVIDER_RECOVERY.md), [activation runbook](INTERNAL_PROVIDER_ACTIVATION.md) and [Phase 8 machine manifest](evidence/internal-provider/2026-11-14/phase8/manifest.json). Earlier dated measurements below remain historical where explicitly superseded by this update.

Updated 2026-11-15 UTC, PostgreSQL main base 18c6e5d plus the uncommitted implementation. This pack supersedes the earlier Phase 1/2 status prose. PostgreSQL is the shared application database; MySQL data is preserved separately. **Development engineering is delivered across the approved workstreams; production readiness is not accepted.** No application DB migrations, production source activation, tenant grants for actual official data, credential changes, commit or push occurred.

## Final executed regression

The [regression manifest](evidence/internal-provider/2026-11-14/regression-summary.json) lists actual executed counts, skipped tests and browser failures. Go tests use disposable databases on PostgreSQL 16.15, Go 1.27.4 and the patched dependency set. Screening/decision/case server and worker binaries were freshly built for the real integration journeys. No experimental migration targeted the application DB.

| Module | Passing top-level tests | Passing including subtests | Result |
| --- | ---: | ---: | --- |
| screening-service | 134 | 269 | Passed, plus final focused recovery/docs checks |
| auth-service | 33 | 60 | Passed |
| decision-engine-service | 254 | 409 | Passed |
| case-manager-service | 90 | 157 | Passed |
| investigator | 1 | 1 | Passed |

Screening's full-run skips are the optional live-artifact parser test, the child-process helper entry point, and the opt-in 40k workload. Actual official artifacts and the workload were executed separately; the parent killed-process test ran and passed. A helper/no-tests package is not counted as a successful behavioral test. Final focused recovery/docs checks: 7 top-level / 7 including subtests, 0 failures. The normalization-revision suite separately passed 23/23, including old-generation denial, historical enrichment, full refetch and same-content new publication. Companion-suite skips are listed individually in the manifest and are not counted as executed passes.

Frontend TypeScript and the patched production Next build pass. Final full browser suite: **134 passed, zero failed, one dedicated skip, zero flaky**; separate genuine PostgreSQL API/worker journey **1/1 passed without skip**. All 21 earlier failures are individually classified and repaired. Full npm production/all dependency audits have zero known findings. Existing 20 lint errors/74 warnings reproduce on unchanged HEAD with the patched toolchain; no lint rule was suppressed.

## Gate A — provider foundation

- [x] Scoped five-operation provider and strict named registry, no unknown/synthetic fallback.
- [x] Verified human/scenario/monitor/continuous/inline scope; current grants/roles/tenant checks; private/global isolation and cursor binding.
- [x] Unsupported fields/profiles and unauthorized/unavailable selected datasets fail closed; saved-generation evidence is explicit.

## Gate B — controlled data publication

- [x] Fresh/upgrade/down-up owner migration tests, duplicate/invalid/truncated/oversized input and malicious XML/download tests.
- [x] Atomic pointer + publication + review, unchanged content, stable source IDs and parent-fence conflicts.
- [x] Original artifacts, legal/review/quarantine holds, bounded concurrent cleanup and new-generation rollback.
- [x] Actual killed import process, restart/global-private recovery boundary, stale leases, retry storms and queue/storage failure retention.
- [ ] Production lock window, restricted login provisioning, backup/restore and storage/retention acceptance.

## Gate C — existing screening workflow integration

- [x] Actual manual/scenario API -> producer -> worker -> evidence/history/review and case delivery/enrichment.
- [x] Signed/current API authorization, request/role forgery, revoked access and tenant-specific private results.
- [x] Frontend private review/restore, strict authoring/BFF and real-provider monitoring browser journey.
- [ ] Matching quality acceptance and the wider frontend regression failures.

## Gate D — dataset lifecycle

- [x] New designations include previous clean results; modifications/removals/unchanged snapshots preserve historical evidence.
- [x] 601 selected saved queries complete real provider rescreens; revoked target fails, repair/retry creates a distinct attempt; 599 concurrent completions are measured on a tiny three-record fixture and are not a production throughput claim.
- [x] 603 continuous objects fan out in durable pages; queue failure rolls back a page; a restarted service resumes with no duplicate receipts.
- [x] Legacy maintenance drains three delta pages and 603 objects, checkpoints queue failure and fences expired claims.
- [x] Old clean results become stale immediately from generation/grant/readiness comparisons, not just after queue progress.
- [x] Full monitoring history paging and foreign-monitor cursor denial; failed/partial runs never replace the latest successful complete pointer.
- [ ] Production replay/restore exercise and an approved published-evidence expiry/storage policy.

## Gate E — decision-time enforcement

- [x] Screening-owned shared-DB facade, current canonical config/grants/exclusions, pinned generations and bounded aggregate budget.
- [x] User-approved error-only failure policy: failed/timed-out/incomplete lookup creates no successful decision/effects.
- [x] Strongest forced hit outcome precedes workflow/scoring; rule score is unchanged.
- [x] Decision/executions/evidence/queues/outbox are atomic, including an inline-enabled multi-scenario batch; evidence-write failure rolls back the whole batch.
- [x] Historical evidence and source attribution survive updates, revoke and ordinary review; callbacks/retries cannot rewrite/re-search saved machine evidence.
- [ ] Production source/profile quality, end-to-end decision load/SLO and explicit activation.

## Matching quality measurements

The expanded corpus has 240 fictional identity/collision families, 4,800 queries and 82 indeterminate labels. Families are separated into 1,440 development, 960 calibration and 2,400 holdout queries. The holdout has 1,182 positives, 1,176 negatives and 42 indeterminate queries. Candidate retrieval, final score, per-source/category/type/stratum results and Wilson intervals are recorded separately. Neither scorer nor corpus changed after the freeze; hashes are in the [reproducibility manifest](evidence/internal-provider/2026-11-14/matching-reproducibility.json).

| Measurement | Current frozen-name-token report |
| --- | --- |
| Calibration threshold | 79; default 85 unchanged |
| Holdout retrieval recall | 100.00% |
| Holdout scored recall | 97.12% |
| Holdout precision | 24.27% |
| Negative-query FPR / FNR | 10.54% / 2.88% |
| Partial / error | 60 / 0 |
| Default 85 scored recall / precision / FPR | 89.17% / 23.57% / 2.38% |
| Repeated fixed-namespace reports | Identical classification metrics ignoring latency: true |

The [complete current report](evidence/internal-provider/2026-11-14/matching-v2-current.json) includes slice counts, calibration sweep and actual query plans. The [original frozen report](evidence/internal-provider/2026-11-14/matching-v2-original-holdout.json) is preserved. Earlier randomized-tenant top-50 ties produced small variations in positive inclusion; the deterministic namespace fixes test reproducibility without searching for a favorable seed or changing labels/scoring. Partial results remain incomplete and cannot establish a clean decision. Their FPR denominator includes incomplete queries, so the observed unconditional FPR is a lower bound; no partial/indeterminate case is counted as a correct no-hit.

Proposed controlled floors remain retrieval recall ≥99.5%, scored recall ≥99%, precision ≥95%, FPR/FNR ≤1%, with adequate slice coverage. **Acceptance fails.** The original 2,000-query corpus also failed precision/FPR (92.59% / 5.2% at threshold 84); the harder expanded corpus must not be presented as a comparable population improvement. Token caps reduce certain unrelated-name matches but cannot identify legal identity from overlapping common names. Exact typed IDs are a separate functionally tested profile, not a calibrated remedy or a retuned holdout scorer. Independent adjudication and representative source distributions remain required.

## Source and lifecycle verification

OFAC SDN, UKSL and UN have complete actual retrieval evidence plus retained-artifact validation on the final toolchain: full parse/stage/publish, exact source-ID/generation search and enrichment for every entity type present, and unchanged complete-snapshot idempotency. Actual record counts are 19,565 / 6,440 / 1,007. OFAC additionally has zero-error full official-XSD validation. Fixtures cover malformed/duplicate IDs, source types, alias quality, weak/approximate dates, XML/SSRF/download limits, conditional checks, quarantine, fencing and replay. EU is fixture-tested only; Ghana notice intake uses fictional uploads only. No source verification is licensing or production-quality acceptance.

## Performance and shared database workload

Final workload: 40,000 mechanical fictional names, 1,000 mixed exact/fuzzy provider searches, eight callers, default pool maximum 16, Go 1.27.4/GOMAXPROCS=2, PostgreSQL 16.15 and concurrent complete second-generation publication on a shared Docker Desktop host. Other local tooling can contend; this is not an isolated production host or real-source distribution.

- Search throughput: **536.96/s**; p50 **13.981 ms**, p95 **22.534 ms**, p99 **28.458 ms**.
- Errors/partials/missing expected IDs: **0/0/0**.
- Initial ingest/publish: **7840 ms**, **5102.01 records/s**; provider heap-in-use after search **18333696 bytes** (not peak RSS or source-worker ingestion peak).
- Recorded pre-optimization baseline: 10.52 provider searches/s, p95 1,777.79 ms on the same mechanical shape, older Go 1.25.8. Intermediate results and the [final actual EXPLAIN](evidence/internal-provider/2026-11-14/workload-final.plan.json) are retained; runtime/toolchain/host differences prevent a controlled speedup attribution to one change.
- The previous patched-toolchain run before the normalization revision measured 415.57/s, p95 31.119 ms; it is retained as [comparison evidence](evidence/internal-provider/2026-11-14/workload-patched-pre-normalizer.json). The final run is not evidence of a causal normalization speedup: host contention, cache and run variance differ. A separate 16-caller/eight-connection run was slower (293.90/s, p95 74.87 ms). Pool growth is not an automatic optimization.
- The [final resource snapshot](evidence/internal-provider/2026-11-14/workload-resource-snapshot.json) observed Go CPU 14.52%, memory 96.48 MiB and PostgreSQL CPU 99.28%, memory 1.101 GiB during the workload. PostgreSQL's cgroup includes cache from preceding tests. These are one observation, not peaks, sustained averages or a complete deployment resource budget.

These are **provider + PostgreSQL** results, excluding HTTP, queues, rule evaluation, persistence and final decision effects. The proposed provider p95 ≤20 ms budget is not met in this workload. The target of 500 completed screened decision evaluations/s with end-to-end p95 ≤50 ms and errors ≤0.1% remains unproven; no production capacity claim is made.

## Security scan and test discipline

[Four patched module scans](evidence/internal-provider/2026-11-14/security-scan-summary.json) have no reachable findings in the current database snapshot. Go is 1.27.4; affected pgx/network/text dependencies and auth go-jose were patched. Source analysis is not an image/OS attestation, penetration test or permanent guarantee against later advisories. The [official release policy](https://go.dev/doc/devel/release) and [Go vulnerability tooling](https://go.dev/doc/security/vuln/) informed this check.

Initial failures are preserved as debugging history: retry-key uniqueness, rejected reviewed withdrawal, stale auth schema guard, test-only unregistered source, a killed-process helper using the unmodified base DSN, and a callback redeclaration. They were corrected and the relevant tests rerun. An early command used SOURCE_TEST_DATABASE_URL for Go integration tests, causing skips; those results were discarded and rerun with SCREENING_TEST_DATABASE_URL. PowerShell native stderr can mark a download command failed despite tool completion; final evidence uses Go JSON events/completion markers, not that alone. Raw test/source/auth artifacts stay under ignored .local and are not published.
