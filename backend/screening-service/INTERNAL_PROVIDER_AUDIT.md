# Internal screening provider audit

## Phase 8 acceptance update — 2026-11-15

The [acceptance register](INTERNAL_PROVIDER_ACCEPTANCE.md) is the definitive current gate record. All 21 browser failures are repaired: **134 full-suite passes, zero failures, one dedicated skip**, plus a separate real API/PostgreSQL/worker **1/1 pass**. Final screening regression is **138/288**, auth **33/60**, decision **260/423**, case **90/157**, investigator **1/1** (top-level / including subtests). Name quality remains unaccepted; separate identifier engineering calibration is documented. The complete sustained workload measured **87.487 decisions/s, p95 290.738 ms**, below target. Migration 22, actual restricted logins, restored-clone binding, logical restore/worker replay and operational alert contracts pass disposable checks. Source permissions, representative quality/capacity, production security/recovery/alerting and explicit activation remain open. Existing frontend lint (20 errors/74 warnings) reproduces on HEAD and is recorded separately. **No production-ready claim or activation is made.**

See [matching](INTERNAL_PROVIDER_MATCHING_EVALUATION.md), [browser investigation](INTERNAL_PROVIDER_BROWSER_REVIEW.md), [source approvals](INTERNAL_PROVIDER_SOURCE_APPROVALS.md), [performance](INTERNAL_PROVIDER_PERFORMANCE.md), [security](INTERNAL_PROVIDER_SECURITY_ACCEPTANCE.md), [recovery](INTERNAL_PROVIDER_RECOVERY.md), [activation runbook](INTERNAL_PROVIDER_ACTIVATION.md) and [Phase 8 machine manifest](evidence/internal-provider/2026-11-14/phase8/manifest.json). Earlier dated measurements below remain historical where explicitly superseded by this update.

Updated 2026-11-15 UTC, PostgreSQL main base 18c6e5d plus the uncommitted implementation. This pack supersedes the earlier Phase 1/2 status prose. PostgreSQL is the shared application database; MySQL data is preserved separately. **Development engineering is delivered across the approved workstreams; production readiness is not accepted.** No application DB migrations, production source activation, tenant grants for actual official data, credential changes, commit or push occurred.

## Implemented execution paths

| Path | Current implementation | Evidence |
| --- | --- | --- |
| Manual search | Signed human session, current membership, creator-scoped history, strict provider/profile/dataset selection | Actual API + worker + browser |
| Scenario screening | Canonical v2 authored config, persisted execution/parent/request proof, current active tenant/config, queue capture and worker revalidation | Actual decision/screening workers and case delivery |
| Saved-query monitoring | Explicit creator enrollment, immutable query snapshot, selected-dataset subscriptions, paged publication fanout and durable attempts | 601 completed rescreens; old no-hits, revoke/repair/retry, 82-entry history paging |
| Continuous objects | Explicit version-2 field map, current enabled persisted config/object/tenant, live ingestion read and captured config snapshot | 603-object fanout and actual provider dispatch; revoked grants fail |
| Private maintenance | Separate restricted writer pool, current auth-owner role check, two distinct tenant administrators, version/hash/parent fence | Signed API, effective DB-role, lifecycle and browser tests |
| Global collection | Separate worker, reviewed rights/cadence, source lease/fence, bounded download/parser/staging, conditional checks and quarantine | Official fixtures, actual snapshots, retry storms and restart tests |
| Ghana notices | Separate global DB privilege plus current human identity, retained upload, distinct reviewer, fixed source category | Fictional fixture integration; no actual agency records published |
| Inline evaluation | Screening-owned shared-DB read facade; scoped evidence before final outcome; aggregate timeout; error-only failures | Real decision binary, batch rollback, timeout, partial and immutable-history tests |

## Fixed defects

- Legacy maintenance read only the first 500 objects, failed to drain delta pages and fetched config per object. Production PostgreSQL maintenance now checkpoints all bounded pages, joins configs, fences leases and rolls back queue failure at the page boundary.
- A rescreen retry reused its original idempotency key and violated the unique constraint. Each immutable attempt now has its own screening/key suffix; the target/history remains stable.
- Fuzzy SQL joined too many records before filtering and JSON cardinality estimates triggered bad plans/JIT. Bounded lateral retrieval, typed batched parameters and one per-search preparation removed those costs. GIN fastupdate is disabled with a bounded pending-list flush; ingestion is slower in exchange for predictable reads.
- Complete failed downloads without staged records could lose raw evidence. The collector now stores the original artifact before parsing; explicit GC respects quarantine/review/version holds.
- Known source validation/storage failures could leave an old clean dataset looking current. Readiness now permits only a bounded transient download failure on a still-fresh verified generation; known bad/incomplete validation is unavailable.
- A valid reviewed withdrawal was rejected by the generic empty-import guard. Empty snapshots now require an explicit guarded private clear or reviewed global withdrawal; automatic collectors still reject empty sources.
- Auth schema replay rejected its own new version 4. The current schema guard accepts version 4, remains idempotent, and rejects newer schemas.
- Case intake assumed screening candidate IDs were UUIDs. It now accepts the actual bounded composite text IDs and recognizes an authorized monitored-search origin.
- Inline multi-scenario evaluation could commit an independent successful decision before another lookup failed. Inline-enabled batches now finish all reads first, then save every decision/effect/evidence in one transaction.
- Saved inline source requests/results could be requeued or rewritten through ordinary source operations. Owner constraints now protect the original request, provider evidence, parent and immutable snapshot.
- Private recovery marked only the import receipt failed. It now also fences the expired matching source version in the same transaction; staging callbacks reject an already-cancelled receipt.
- Equal-score capped quality reports varied with randomly assigned test tenants. The test-only tenant namespace is now fixed; frozen scorers/corpora are unchanged and two repeat runs are compared explicitly.
- The text-library security update changed Unicode normalization tables from 15 to 17. Migration 21 versions the projection contract; search/currentness and authoring deny old indices, a full refetch/new publication rebuilds identical raw content, and historical enrichment stays readable.
- Reachable pgx, networking, text and auth-library vulnerabilities were patched. Four module scans emit no reachable findings on Go 1.27.4; see the scan manifest rather than relying on JSON-mode exit status.

## Security boundaries and limitations

Tenant scope is minted at trusted boundaries, not decoded from JSON, tenant headers or River arguments. Global operators can inspect global research data but gain no private-tenant read scope. Customer routes never obtain source publication authority. Private writes require a dedicated explicitly configured connection to the same logical DB; startup checks database identity/primary and the auth-owner contract.

The auth-owned role function is SECURITY DEFINER with a fixed search path, bounded role arguments, current account/membership/session/version checks and consistent locking. PUBLIC execution is revoked. The supplied SQL creates NOLOGIN capability roles only; no production login, password, membership or data grant was created.

These are service/table privilege boundaries, not PostgreSQL row-level-security claims. A compromised broadly provisioned application or writer credential is a separate security boundary requiring deployment review. Private authority comes from trusted application/session checks on every action. The no-login role fixture verifies effective permissions, not a real credential provisioning or production penetration test.

No automatic entity merge, legal identity assertion, inferred PEP/SOE family connection, complete sanctions coverage or production throughput guarantee is made. The ID method is exact-text evidence, not a probability or legal finding. Historical evidence is immutable; source/rights revocation can deny new search/enrichment without changing saved decisions.

## Remaining defects and acceptance blockers

- Matching-quality floors fail on the expanded fictional corpus; Recall 97.12%, precision 24.27%, FPR 10.54%, FNR 2.88% at threshold 79; 60 partial queries, 0 errors. Retrieval recall 100.00%.
- The identifier profile is functionally tested and explainable, but has no independently adjudicated accuracy acceptance.
- All 21 earlier browser failures are closed; full suite 134 pass/0 fail/1 dedicated skip and actual-provider 1/1 pass. Existing lint (20 errors/74 warnings) is baseline-reproduced and remains a separately recorded code-quality action.
- Provider workload p95 exceeds the proposed 20 ms budget, and 500 complete screened decision TPS is unproven. Live data distribution, cold/stale states, long retained history, deployment hardware and multi-replica interference require qualification.
- EU production access, source-specific permitted use/redistribution/retention, ORC contracts and PEP/linking policy remain unresolved external gates.
- Published/quarantined/review-held evidence is retained indefinitely. Approved published-evidence expiry, storage sizing and restore/PITR acceptance remain operational gates; no unsafe published GC was invented.
- No production rollout, actual credential boundary, image/OS attestation or deployment acceptance was performed.

The [verification record](INTERNAL_PROVIDER_VERIFICATION.md), [task ledger](INTERNAL_PROVIDER_PLAN.md#original-task-ledger) and [release steps](INTERNAL_PROVIDER_PROGRESS.md#remaining-steps-before-production-activation) distinguish these open gates from completed engineering.
