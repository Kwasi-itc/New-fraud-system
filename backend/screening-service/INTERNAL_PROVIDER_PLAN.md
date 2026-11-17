# Internal screening provider implementation plan

## Phase 8 acceptance update — 2026-11-15

The [acceptance register](INTERNAL_PROVIDER_ACCEPTANCE.md) is the definitive current gate record. All 21 browser failures are repaired: **134 full-suite passes, zero failures, one dedicated skip**, plus a separate real API/PostgreSQL/worker **1/1 pass**. Final screening regression is **138/288**, auth **33/60**, decision **260/423**, case **90/157**, investigator **1/1** (top-level / including subtests). Name quality remains unaccepted; separate identifier engineering calibration is documented. The complete sustained workload measured **87.487 decisions/s, p95 290.738 ms**, below target. Migration 22, actual restricted logins, restored-clone binding, logical restore/worker replay and operational alert contracts pass disposable checks. Source permissions, representative quality/capacity, production security/recovery/alerting and explicit activation remain open. Existing frontend lint (20 errors/74 warnings) reproduces on HEAD and is recorded separately. **No production-ready claim or activation is made.**

See [matching](INTERNAL_PROVIDER_MATCHING_EVALUATION.md), [browser investigation](INTERNAL_PROVIDER_BROWSER_REVIEW.md), [source approvals](INTERNAL_PROVIDER_SOURCE_APPROVALS.md), [performance](INTERNAL_PROVIDER_PERFORMANCE.md), [security](INTERNAL_PROVIDER_SECURITY_ACCEPTANCE.md), [recovery](INTERNAL_PROVIDER_RECOVERY.md), [activation runbook](INTERNAL_PROVIDER_ACTIVATION.md) and [Phase 8 machine manifest](evidence/internal-provider/2026-11-14/phase8/manifest.json). Earlier dated measurements below remain historical where explicitly superseded by this update.

Updated 2026-11-15 UTC, PostgreSQL main base 18c6e5d plus the uncommitted implementation. This pack supersedes the earlier Phase 1/2 status prose. PostgreSQL is the shared application database; MySQL data is preserved separately. **Development engineering is delivered across the approved workstreams; production readiness is not accepted.** No application DB migrations, production source activation, tenant grants for actual official data, credential changes, commit or push occurred.

## Acceptance and checkbox meaning

A checked task means its engineering implementation and named verification are delivered. It does **not** approve quality, rights or production use. A source can be live-source-tested while its commercial/release gate remains closed. Migration 21 pins Unicode 17 normalization; older published projections require a new publication, not reinterpretation. No task IDs were added; all 37 original IDs and dependencies are retained in the [machine ledger](evidence/internal-provider/2026-11-14/phase8/task-ledger.json).

| Gate | Current evidence | Acceptance |
| --- | --- | --- |
| A — provider foundation | Scoped five operations, strict registry, typed IDs and explicit dataset/category metadata | Functional integration tested |
| B — controlled publication | Immutable snapshots, artifacts, safe publication, review, fencing and recovery | Functional integration tested |
| C — existing workflows | Actual API/worker/history/review/case/browser journeys | Browser development gate passes; quality acceptance remains open |
| D — lifecycle | Add/update/remove/unchanged/rollback, legal holds, complete fanout and recovery | Development integration tested; deployment retention/restore acceptance open |
| E — inline enforcement | Error-only policy, bounded lookup, final-outcome ordering and atomic decision/effects, including batches | Functional integration tested; production quality/SLO/activation open |

## Dependency-aware delivery

- [x] Finish publication monitoring and the legacy multi-page/first-500 defect before relying on dataset updates.
- [x] Add raw artifacts, fenced staging, rollback, explicit failed/staged retention and real interrupted-process recovery.
- [x] Expand the corpus, freeze separate name scorers, measure retrieval and classification; add a separately named typed-ID profile.
- [x] Measure larger datasets and fix the observed SQL join/cardinality/GIN-pending-list costs.
- [x] Implement OFAC SDN, UN and UKSL with separate source/coverage/rights gates and complete isolated verification.
- [x] Build the EU fixture connector and Ghana reviewed-notice workflow while independent access/rights gates remain closed.
- [x] Deliver private tenant-list management with the approved administrator/two-person policy.
- [x] Deliver inline screening with the approved error-only policy and immutable atomic evidence.
- [ ] Accept matching quality and source/category coverage using independently adjudicated data.
- [ ] Obtain source-specific usage/retention approvals, EU access and any ORC/PEP agreements.
- [ ] Close the deployment SLO, restore, general frontend regression and release acceptance gates.
- [ ] Separately authorize and execute production activation.

Implementation order varied only where dependencies justified it: source schema/typed provenance landed before live connectors; the auth-owned locking contract supports private approval; inline evidence uses the same owned schema. Blocked EU/ORC/PEP work did not stop independent engineering.

## Original task ledger

| Engineering | ID / scope | Dependencies | Verification | Remaining acceptance |
| --- | --- | --- | --- | --- |
| [x] | FND-01 — Scoped contract and compatible DTO design | Plan approval | Integration-tested | Quality and deployment approval are separate. |
| [x] | FND-03 — Strict registry and configuration validation | FND-01 scoped subset | Integration-tested | Actual deployment configuration is not changed. |
| [x] | PRV-01 — Five-operation internal read adapter for controlled data | FND-01 scoped subset, FND-03 | Integration-tested | Source-specific quality is unaccepted. |
| [x] | DB-01 — Additive PostgreSQL source schema and grants | FND-01 | Integration-tested | Deployment grants, lock window and backup acceptance remain. |
| [x] | ING-01 — Streaming controlled import and atomic publication | DB-01, FND-01 | Integration-tested | No application or production imports performed. |
| [x] | MAT-01 — Indexed retrieval, scoring and explanations | DB-01, PRV-01 | Integration-tested | Name quality fails; separate synthetic ID calibration is complete, with independent quality acceptance still open. |
| [x] | API-01 — Registry-backed selection/capabilities and authoring | PRV-01, ING-01, MAT-01 | Integration-tested | New features require explicit runtime configuration. |
| [x] | RSK-01 — Typed query and category inputs | FND-01, API-01, MAT-01 | Integration-tested | No broad category query or automatic legal identity classification. |
| [x] | API-02 — Versioned evidence and historical enrichment | API-01, MAT-01 | Integration-tested | Published evidence has indefinite retention pending approved expiry policy. |
| [ ] | QLT-01 — Labelled corpus and threshold profiles | MAT-01, RSK-01 | Measured; acceptance failed | Independent adjudication and source/category quality floors remain open. |
| [x] | FND-02 — Actual API/worker/history/case/UI journey | API-01, API-02, RSK-01, QLT-01 | Full browser + actual PostgreSQL API/worker verified | All 21 failures closed; quality and release acceptance remain separate. |
| [x] | LIF-01 — Source diff, removals, rollback and retention | ING-01, MAT-01, FND-02 | Integration-tested | Published/quarantined/review-held evidence is intentionally retained. |
| [x] | OPS-01 — Separate source worker and private administration | LIF-01 | Integration-tested | No source or production worker enabled; operator rollout remains required. |
| [x] | OPS-02 — Publication delivery and complete tenant rescreening | LIF-01, OPS-01, FND-02 | Integration-tested | Enrollment/configuration is explicit; historical decisions never silently re-evaluate. |
| [ ] | OFAC-01 — Full schema/access/rights/coverage specification | Source inventory; may research alongside A-D | Specification and schema verified | Source-specific legal/retention/purpose approval is still required. |
| [x] | OFAC-02 — Official SDN full-snapshot connector | FND-01, OPS-01, OFAC-01 | Live-source-tested | SDN only; not complete OFAC or ownership-derived coverage. |
| [ ] | OFAC-03 — Disposable full lifecycle and quality acceptance | OFAC-02, Gates A-D, QLT-01 | Functional lifecycle tested; gate open | Official-source matching acceptance and approved release profile remain open. |
| [x] | OFAC-04 — Explicit complete live verification | OFAC-03 and approved source access/retention | Live-source-tested | Research verification is not commercial activation or production verification. |
| [x] | UN-01 — Independent UN snapshot connector | Gates A-D, OFAC-03, approved UN access/rights | Live-source-tested | Commercial reuse/retention approval and source quality remain open. |
| [x] | UK-01 — Current UKSL connector | Gates A-D, OFAC-03, approved UK terms/schema | Live-source-tested | OGL exceptions, personal/third-party data and source quality require approval. |
| [ ] | EU-01 — Establish FSF production access and permitted reuse | Source inventory; research may run earlier | Externally blocked | An approved access contract/token/endpoint and rights review are absent. |
| [x] | EU-02 — EU snapshot connector | EU-01, Gates A-D, OFAC-03 | Fixture-tested | Full live schema, lifecycle and quality verification require EU-01. |
| [ ] | GH-01 — Ghana notices access/classification/corrections | Source inventory; research may run earlier | Research/specification complete | Agency-specific acquisition, reuse, correction and retention approval required. |
| [x] | GH-02 — Controlled official-notice intake | GH-01, Gates A-D | Integration-tested | No actual Ghana records published; content approval never approves rights. |
| [ ] | GH-03 — ORC acquisition and allowed enrichment | Source inventory and agreement | Access blocked | Contract, price, entitlement, schema and purpose required. |
| [ ] | GH-04 — Curated public-office/PEP/SOE evidence | GH-01, approved coverage/retention policy | Deferred by policy | Approved PEP/SOE taxonomy, provenance, purpose and retention policy required. |
| [ ] | NSDN-01 — Separate future OFAC non-SDN coverage | OFAC-03 and source-specific scope approval | Deferred coverage | Select and approve each additional list and its classification. |
| [x] | PVL-01 — Tenant import schema and authority | Gates A-D, approved private-list permissions/retention | Integration-tested | Explicit restricted writer connection required; no credentials provisioned. |
| [x] | PVL-02 — Reviewed publication, update/removal and audit | PVL-01 | Integration-tested | Published evidence retained; no automatic expiry on private list deletion. |
| [x] | PVL-03 — Tenant management BFF/UI and investigation compatibility | PVL-02, API-02 | Integration-tested + browser-tested | Full browser suite and separate real-provider journey pass; baseline lint debt remains. |
| [x] | INL-01 — Shared screening-owned scoped read facade | Gates A-D; approved contract | Integration-tested | Feature remains inactive without explicit decision runtime flag. |
| [x] | INL-02 — Outcome policy, deadlines and evaluation ordering | INL-01, institution policy approval, relevant source/list acceptance | Integration-tested | Production quality, timeout budget and per-scenario policy activation require acceptance. |
| [x] | INL-03 — Atomic evidence, replay and projection | INL-02; owner migration review | Integration-tested | Ordinary API retry idempotency remains the existing decision API contract; failed lookups create no successful decision. |
| [x] | HARD-01 — Security/recovery matrix by gate | Applicable feature tasks | Integration-tested + scanned | Not a production penetration test, RLS isolation or OS-image attestation. |
| [ ] | PERF-01 — Source-scale and shared-DB workload evidence | MAT-01; repeat after D, each source/private path and E | Measured; acceptance open | Complete workload: 87.487 decisions/s, p95 290.738 ms; target 500/s and 50 ms plus deployment acceptance remain unmet. |
| [ ] | REL-01 — Scoped release and rollback acceptance | A-D plus source-specific/HARD/PERF gates; add PVL or E for those features | Release gated | Quality, legal/access, capacity, operations, baseline lint and explicit activation approval remain. |
| [ ] | RES-01 — Reversible cross-source identity linking | A-D, separate identity-link policy approval | Deferred by policy | Separate identity-link policy approval; never merge on fuzzy names. |

Detailed implementation/files/tests are in [Progress](INTERNAL_PROVIDER_PROGRESS.md), [Verification](INTERNAL_PROVIDER_VERIFICATION.md), and the machine ledger. Completed engineering IDs (26): FND-01, FND-02, FND-03, PRV-01, DB-01, ING-01, MAT-01, API-01, RSK-01, API-02, LIF-01, OPS-01, OPS-02, OFAC-02, OFAC-04, UN-01, UK-01, EU-02, GH-02, PVL-01, PVL-02, PVL-03, INL-01, INL-02, INL-03, HARD-01.

## Reserved decisions

The user approved tenant administrators as importers/approvers with different people required, and error-only inline failure behavior with no successful decision/effects. These are resolved decisions. Source licensing/redistribution, production retention, PEP/SOE classification, identity linking, deployment SLO acceptance and activation remain reserved. No source readiness or documentation checkbox grants production authority.
