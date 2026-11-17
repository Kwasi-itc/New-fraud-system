# Internal screening provider implementation report

## Phase 8 acceptance update — 2026-11-15

The [acceptance register](INTERNAL_PROVIDER_ACCEPTANCE.md) is the definitive current gate record. All 21 browser failures are repaired: **134 full-suite passes, zero failures, one dedicated skip**, plus a separate real API/PostgreSQL/worker **1/1 pass**. Final screening regression is **138/288**, auth **33/60**, decision **260/423**, case **90/157**, investigator **1/1** (top-level / including subtests). Name quality remains unaccepted; separate identifier engineering calibration is documented. The complete sustained workload measured **87.487 decisions/s, p95 290.738 ms**, below target. Migration 22, actual restricted logins, restored-clone binding, logical restore/worker replay and operational alert contracts pass disposable checks. Source permissions, representative quality/capacity, production security/recovery/alerting and explicit activation remain open. Existing frontend lint (20 errors/74 warnings) reproduces on HEAD and is recorded separately. **No production-ready claim or activation is made.**

See [matching](INTERNAL_PROVIDER_MATCHING_EVALUATION.md), [browser investigation](INTERNAL_PROVIDER_BROWSER_REVIEW.md), [source approvals](INTERNAL_PROVIDER_SOURCE_APPROVALS.md), [performance](INTERNAL_PROVIDER_PERFORMANCE.md), [security](INTERNAL_PROVIDER_SECURITY_ACCEPTANCE.md), [recovery](INTERNAL_PROVIDER_RECOVERY.md), [activation runbook](INTERNAL_PROVIDER_ACTIVATION.md) and [Phase 8 machine manifest](evidence/internal-provider/2026-11-14/phase8/manifest.json). Earlier dated measurements below remain historical where explicitly superseded by this update.

Updated 2026-11-15 UTC, PostgreSQL main base 18c6e5d plus the uncommitted implementation. This pack supersedes the earlier Phase 1/2 status prose. PostgreSQL is the shared application database; MySQL data is preserved separately. **Development engineering is delivered across the approved workstreams; production readiness is not accepted.** No application DB migrations, production source activation, tenant grants for actual official data, credential changes, commit or push occurred.

## Completion assessment

The approved engineering workstreams are implemented and verified at their stated levels. **The provider is not production-ready and all 37 acceptance tasks are not closed.** Quality, source-use/access, capacity, deployment security/recovery/operations and baseline lint release gates remain. No new task IDs were introduced. This report and the linked machine evidence distinguish code delivery from acceptance and deployment.

## 1. Original task IDs and implementation status

Engineering-delivered IDs (26): **FND-01, FND-02, FND-03, PRV-01, DB-01, ING-01, MAT-01, API-01, RSK-01, API-02, LIF-01, OPS-01, OPS-02, OFAC-02, OFAC-04, UN-01, UK-01, EU-02, GH-02, PVL-01, PVL-02, PVL-03, INL-01, INL-02, INL-03, HARD-01**. The [plan](INTERNAL_PROVIDER_PLAN.md) retains every original dependency, verification level and open gate. Checked engineering work is not production approval. QLT-01, OFAC-01/03, EU-01, GH-01/03/04, NSDN-01, PERF-01, REL-01 and RES-01 retain explicit unmet/deferred acceptance in the ledger.

| ID | Delivered implementation | Current files/modules | Evidence / remaining |
| --- | --- | --- | --- |
| FND-01 | Scoped v2 requests, exact saved evidence, typed IDs and category-labelled explicit dataset refs. | internal/provideraccess, internal/sourcedata, internal/domain/screening, internal/httpapi; decision authoring DTOs | Integration-tested; Quality and deployment approval are separate. |
| FND-03 | Strict shared provider registry and named legacy compatibility; no synthetic/unknown-provider fallback. | internal/providerregistry, internal/clients/provider/configuration.go, internal/httpapi/router.go, cmd/worker | Integration-tested; Actual deployment configuration is not changed. |
| PRV-01 | Scoped Search, Enrich, Catalog, Freshness and Delta; all four entity types supported internally. | internal/internalprovider, internal/ports/scoped_provider.go | Integration-tested; Source-specific quality is unaccepted. |
| DB-01 | Owner migrations 11–22, immutable generations, artifacts, indices, leases, review, normalizer revision and inline constraints; restricted-role template tested. | internal/migrations/metadata/000011–000021, scripts/source-role-grants.sql; auth-service/internal/platform/schema_v4.sql | Integration-tested; Deployment grants, lock window and backup acceptance remain. |
| ING-01 | Bounded streaming full snapshots, original artifacts, source identity ordering, parent fencing and atomic publication/event. | internal/sourceingest, internal/sourcedata, cmd/source-import | Integration-tested; No application or production imports performed. |
| MAT-01 | Indexed name/alias plus exact typed-ID retrieval; separate explainable profiles; bounded candidates and partial overflow. | internal/matching, internal/internalprovider/fuzzy.go and identifiers.go; migrations 12/14/18/21; Phase 8 calibration tests | Integration-tested; name quality fails; separate synthetic ID calibration is complete, with independent quality acceptance still open. |
| API-01 | Manual and scenario authoring, BFF selection/capabilities, persisted execution authority and current revalidation. | internal/httpapi/manual_search.go, internal/providerregistry; decision authoring/store; frontend manual/scenario forms and BFF | Integration-tested; New features require explicit runtime configuration. |
| RSK-01 | Typed IDs retain text, type, jurisdiction and issuer; mapped scenario fields reject numbers. Categories are selected through labelled dataset refs, not inferred identity risk. | internal/sourcedata/identifiers.go; decision domain/screening/identifier.go and service/screening_dispatch_request.go; frontend identifier-fields.tsx | Integration-tested; No broad category query or automatic legal identity classification. |
| API-02 | Pinned generation/source/profile evidence, case-scoped enrichment, protected inline snapshots and private historical access. | internal/httpapi/investigator_enrichment.go, internal/providerregistry, lookup; frontend stored-evidence/screening-evidence/case evidence | Integration-tested; Published evidence has indefinite retention pending approved expiry policy. |
| QLT-01 | 4,800 labelled fictional queries, frozen family partitions, token-v2 profile, calibration/holdout and repeated deterministic report. | testdata/matching-corpus-v1.json and matching-corpus-v2.json; internal/matching; scripts corpus generators; store/postgres/matching_quality_integration_test.go | Measured; acceptance failed; Independent adjudication and source/category quality floors remain open. |
| FND-02 | Actual API, worker, history, review, case delivery/enrichment and browser monitoring journeys pass; all 21 browser failures repaired. | internal/store/postgres/internal_provider_integration_test.go and internal_scenario_journey_integration_test.go; frontend case/display/explorer/evidence tests | Full suite 134 pass/0 fail/1 dedicated skip; actual-provider 1/1 pass; quality and release acceptance remain separate. |
| LIF-01 | Diff/add/update/remove/unchanged, new-generation rollback, retained history, legal holds, bounded failed/staged cleanup and killed-process recovery. | internal/sourceingest/lifecycle.go and artifact_cleanup.go; source lifecycle/process recovery integration tests | Integration-tested; Published/quarantined/review-held evidence is intentionally retained. |
| OPS-01 | Separate source worker/CLI/profile, UTC cadence, conditional checks, fenced leases, retry cap/jitter, diagnostics and recovery. | internal/sourcecollector, cmd/source-worker, cmd/source-admin, docker-compose.screening-sources.yml, Dockerfile | Integration-tested; No source or production worker enabled; operator rollout remains required. |
| OPS-02 | Durable manual-query and live-object fanout; complete legacy delta/object pagination; 601 real rescreens and 603-object paths. | internal/sourcemonitor, internal/service/dataset_maintenance.go and continuous_worker.go, internal/store/postgres/dataset_maintenance.go; migrations 13/17/20 | Integration-tested; Enrollment/configuration is explicit; historical decisions never silently re-evaluate. |
| OFAC-01 | Official endpoints/XSD, SDN mapping, coverage and access/terms evidence documented. | INTERNAL_PROVIDER_SOURCES.md; internal/sourceconnectors registry/fixtures | Specification and schema verified; Source-specific legal/retention/purpose approval is still required. |
| OFAC-02 | Classic SDN hardened connector; all 19,565 records parsed/published, all four types searched/enriched, unchanged full snapshot verified. | internal/sourceconnectors; internal/sourcecollector/pipeline.go; testdata/source-fixtures | Live-source-tested; SDN only; not complete OFAC or ownership-derived coverage. |
| OFAC-03 | Common transactional lifecycle/review/update/removal and source fixture/actual snapshot checks pass. | internal/store/postgres/source_collector_integration_test.go and lifecycle/quality suites; cmd/source-verify | Functional lifecycle tested; gate open; Official-source matching acceptance and approved release profile remain open. |
| OFAC-04 | Complete official download/hash/XSD evidence and isolated full parse/publication/search/enrich/idempotency report. | cmd/source-verify; evidence/internal-provider/2026-11-14/official-source-verification.json | Live-source-tested; Research verification is not commercial activation or production verification. |
| UN-01 | 1,007 records; stable IDs, aliases and date precision; Person/Organization search/enrichment and unchanged publication verified. | internal/sourceconnectors; testdata/source-fixtures; cmd/source-verify | Live-source-tested; Commercial reuse/retention approval and source quality remain open. |
| UK-01 | Current UKSL, 6,440 records; organizations/persons/vessels, native names and identifiers; unchanged snapshot verified. | internal/sourceconnectors; testdata/source-fixtures; cmd/source-verify | Live-source-tested; OGL exceptions, personal/third-party data and source quality require approval. |
| EU-01 | Official FSF acquisition and token/rights requirements recorded; no borrowed/example credentials used. | INTERNAL_PROVIDER_SOURCES.md; internal/sourceconnectors registry | Externally blocked; An approved access contract/token/endpoint and rights review are absent. |
| EU-02 | Hardened FSF 1.1 parser, IDs/provenance, strict acquisition boundary and shared publication groundwork. | internal/sourceconnectors; testdata/source-fixtures | Fixture-tested; Full live schema, lifecycle and quality verification require EU-01. |
| GH-01 | FIC, BoG, SEC, OSP and ORC authority/acquisition/category limits documented separately. | INTERNAL_PROVIDER_SOURCES.md; internal/sourceconnectors/notices.go | Research/specification complete; Agency-specific acquisition, reuse, correction and retention approval required. |
| GH-02 | Global operator plus current human identity, retained attachments, two-person content review, withdrawal, audit and retention holds. | internal/reviewednotices, cmd/source-notice; migration 19; reviewed notice integration tests | Integration-tested; No actual Ghana records published; content approval never approves rights. |
| GH-03 | ORC remains a contracted corporate-information source, not an invented public bulk API. | INTERNAL_PROVIDER_SOURCES.md; reference/contract-only registry | Access blocked; Contract, price, entitlement, schema and purpose required. |
| GH-04 | No inferred PEP or complete-office-holder coverage is claimed. | INTERNAL_PROVIDER_PLAN.md and INTERNAL_PROVIDER_SOURCES.md; deferred, no production feed | Deferred by policy; Approved PEP/SOE taxonomy, provenance, purpose and retention policy required. |
| NSDN-01 | OFAC non-SDN remains separate from SDN; no implicit expansion. | INTERNAL_PROVIDER_PLAN.md and INTERNAL_PROVIDER_SOURCES.md; deferred separate coverage | Deferred coverage; Select and approve each additional list and its classification. |
| PVL-01 | Approved tenant_admin mapping, signed/current identity, bounded UTF-8 CSV, duplicates/invalid IDs denied, tenant injected. | internal/privatelists, internal/httpapi/private_lists.go, internal/app; auth-owner schema v4; backend/investigator/roles.go | Integration-tested; Explicit restricted writer connection required; no credentials provisioned. |
| PVL-02 | Distinct reviewer/hash/parent binding; snapshot, clear/archive/restore, durable audit/version history and recovery. | internal/privatelists, internal/sourceingest; migrations 15/19; private lifecycle/role/API tests | Integration-tested; Published evidence retained; no automatic expiry on private list deletion. |
| PVL-03 | Private list BFF/UI, capability gating, uploads, approval, diff, history pages and restore, isolated tenants. | frontend/src/components/screening/private-lists.tsx, frontend/src/lib/server/private-list-proxy.ts, private-list BFF/page/tests | Integration-tested + browser-tested; Full browser suite and separate real-provider journey pass; baseline lint debt remains. |
| INL-01 | Screening-owned Go facade shares matcher, current grants/exclusions and generation-pinned read; no source HTTP calls. | lookup, internal/internalprovider; decision-engine-service/go.mod, internal/service/inline_screening.go, app composition | Integration-tested; Feature remains inactive without explicit decision runtime flag. |
| INL-02 | Approved error-only failure policy, aggregate deadline and strongest forced hit outcome before workflow/scoring/effects. | decision service/authoring/error mapping, frontend scenario capabilities; inline policy tests | Integration-tested; Production quality, timeout budget and per-scenario policy activation require acceptance. |
| INL-03 | Decision/executions/evidence/effects atomic, including inline-enabled multi-scenario batches; immutable replay protected. | lookup; migration 16; decision_engine_service/internal/service/decision_service.go; inline real-binary integration tests | Integration-tested; Ordinary API retry idempotency remains the existing decision API contract; failed lookups create no successful decision. |
| HARD-01 | Signed API denial, restricted roles, malicious XML/SSRF, bounded storage/downloads, actual process kill, retry storms and patched dependency scans. | integration/fixture/API/browser tests; four participating go.mod/go.sum and Dockerfiles; source-role-grants.sql | Integration-tested + scanned; Not a production penetration test, RLS isolation or OS-image attestation. |
| PERF-01 | 40k-record/1k-query workload, real plans, concurrent import, 601 rescreens and resource observations recorded. | internal/store/postgres/source_workload_integration_test.go; dataset_maintenance/source_lifecycle integration tests; evidence workload plans/reports | Measured; acceptance open; 500 complete decision TPS, provider p95 ≤20 ms and deployment SLO acceptance are unproven. |
| REL-01 | Optional restricted worker profile, no-login role template, recovery/rollback/runbook and evidence pack prepared. | docker-compose.screening-sources.yml; scripts/source-role-grants.sql; six-document pack and evidence manifests | Release gated; Quality, legal/access, capacity, operations, baseline lint and explicit activation approval remain. |
| RES-01 | Source records remain separate, reversible linking is not implemented or needed for current provider operation. | INTERNAL_PROVIDER_PLAN.md / INTERNAL_PROVIDER_DESIGN.md; deferred, no identity-merge implementation | Deferred by policy; Separate identity-link policy approval; never merge on fuzzy names. |

## 2. New task IDs

None. The work uses the original 37 IDs. [Machine task ledger](evidence/internal-provider/2026-11-14/phase8/task-ledger.json) also preserves prior scope/dependencies and current files/verification.

## 3. Workstreams delivered

- [x] Automatic saved-query and live-object rescreening, plus complete checkpointed legacy maintenance.
- [x] Immutable publication, rollback, original artifacts, explicit retention/holds and interrupted-process recovery.
- [x] Matching investigation, expanded frozen corpus, separate token and exact-ID evidence profiles, measured accuracy and reproducibility.
- [x] Larger concurrent PostgreSQL workload, actual query plans, bounded pools/retries and operator diagnostics.
- [x] OFAC SDN, UN and UKSL connectors with complete actual-snapshot verification.
- [x] EU fixture groundwork and controlled Ghana notice review/withdrawal framework.
- [x] Private tenant administration/review/version management with BFF/UI integration.
- [x] Error-only inline decision enforcement and atomic evidence/effects, including multi-scenario batches.
- [x] Security fixes, regression/scan evidence, release/runbook documentation.
- [ ] Matching/source-specific quality acceptance, external rights/access and production operational approval.

## 4. Architecture implemented

One shared PostgreSQL DB; screening-owned source/version/projection/artifact/publication/review tables; strict scoped read registry; a separate source executable and existing screening queues/workers. Tenant-private writes use a separate restricted pool and auth-owner locking contract. Decision evaluation uses a screening-owned Go facade and transactional persistence, not source HTTP. [Design](INTERNAL_PROVIDER_DESIGN.md) records contracts, limits, ownership and trust boundaries.

## 5. Sources and verified coverage

OFAC SDN 19,565; UKSL 6,440; UN 1,007 records. Every entity type present was searched/enriched by exact source identity and generation. Each complete artifact was restaged to verify unchanged idempotent publication. OFAC has independent full-XSD evidence. EU is fixture-tested only; Ghana actual agency data has not been imported. FIC is reference-only; ORC/PEP/non-SDN coverage is not invented. [Source inventory](INTERNAL_PROVIDER_SOURCES.md) and [machine report](evidence/internal-provider/2026-11-14/official-source-verification.json) state access/rights gaps and hashes.

## 6. Matching quality

Recall 97.12%, precision 24.27%, FPR 10.54%, FNR 2.88% at threshold 79; 60 partial queries, 0 errors. Retrieval recall 100.00%. Proposed floors fail. Independent human adjudication and representative source/type/category slices are not supplied. No thresholds were retuned from the holdout; the frozen scorer/corpus hashes are unchanged. The exact-ID profile now has separate synthetic calibration/evaluation evidence, but no independent production quality acceptance or legal identity claim. [Detailed quality](INTERNAL_PROVIDER_VERIFICATION.md#matching-quality-measurements) includes original/current reports and reproducibility limits.

## 7. Rescreening and lifecycle behavior

Explicit monitors include prior no-hits, preserve their original query and retain every attempt. Publication/currentness is immediate; queue fanout is bounded and durable. Failed/partial attempts never refresh the successful pointer. Continuous monitoring re-reads the live ingestion object under a persisted current config. Legacy maintenance no longer truncates at 500 or ignores delta pages. Tests complete 601 real provider rescreens and exercise 603-object paths, queue rollback, revoke/repair/retry and long paged history. Rollback makes a new generation/event. Legal/review holds win over cleanup; published/quarantined evidence is retained. Actual process-kill recovery and global/private recovery boundaries pass.

## 8. Decision-time behavior

The user approved **error only** for failures, timeout and incomplete screening. Those paths save no successful decision or dependent effects. Hits can force review, block-and-review or decline according to the authored rule; the strongest outcome wins and precedes workflow/scoring. Evidence includes the exact request/config/source generation/exclusion/policy snapshot. Decision/evidence/queues/outbox are atomic; an inline-enabled all-scenarios request commits none if any lookup fails. Later updates/reviews cannot rewrite original machine evidence. Feature activation remains explicit and off in the existing runtime.

## 9. Security and isolation

Current account/session/membership/version and tenant-consistent parent checks are exercised through signed APIs, restricted effective DB roles, worker ownership, forged/revoked/cross-tenant cases and BFF origin/cache rules. Downloads/XML/uploads are bounded and hardened; raw provenance is restricted and not projected into browser DTOs. Public execution of the auth-owner definer is revoked. No production credentials, role memberships or tenant grants were changed. Generated logins existed only on disposable test servers. Four patched source scans have no reachable findings; this is not RLS, a penetration test or an OS/image attestation. [Audit](INTERNAL_PROVIDER_AUDIT.md) states the trust limits.

## 10. Performance

The final 40k-record workload completed **536.96 provider searches/s**, p50/p95/p99 **13.981/22.534/28.458 ms**, with zero errors/partials/missing expected IDs during full concurrent import. Initial ingest/publish took **7840 ms**. Queries use measured bounded indexed retrieval and GIN read-latency tuning. This shared-host synthetic provider test excludes HTTP/rules/queues/decision persistence. The provider p95 ≤20 ms budget is not met; 500 completed screened decision TPS remains unproven. No infrastructure sizing or production guarantee is inferred.

## 11. Tests actually executed

The [full manifest](evidence/internal-provider/2026-11-14/regression-summary.json) gives counts and skips. Final Phase 8 screening full regression: **138 top-level / 288 including subtests passed**. Auth: **33/60**; decision: **260/423**; case: **90/157**; investigator: **1/1**, all passing. Subsequent focused recovery/docs checks passed. Actual source verification, workload, repeated quality and real browser/API/worker checks ran separately. TypeScript/build passed.

The final whole frontend browser run is **134 passed / 0 failed / 1 dedicated skip / 0 flaky**; the separate actual API/PostgreSQL/worker browser check is **1 passed / 0 skipped/failed**. All 21 original failures are classified in the Phase 8 browser report. Production readiness remains gated; existing lint debt is recorded separately.

## 12. Outstanding defects and approvals

- [ ] Accept independently adjudicated matching quality and supported source/category/type coverage; current metrics fail.
- [x] Investigate and close all 21 browser failures; final full suite and actual-provider journey pass. Existing HEAD lint remains a separate open action.
- [ ] Establish an end-to-end deployment load/SLO envelope and database-wide pool/worker/resource budget; the target is not established by the provider-only result.
- [ ] Source-specific acquisition/use/redistribution/purpose/retention approvals; UN commercial reuse remains unresolved, UK exceptions require review, OFAC SDN is not all OFAC.
- [ ] Approved EU access/token/schema/rights and any ORC contract; PEP/SOE/linking/non-SDN policy remains deferred.
- [ ] Approved published-evidence expiry/storage policy, backup/PITR/restore exercise, migration lock window, real credential provisioning and production security/OS/image acceptance.
- [ ] Separate explicit production activation and live decision-policy authorization.

The two questions answered by the user are resolved: tenant administrators import/approve with different people required; inline failures return an evaluation error with no successful decision/effects. They are not still pending approvals.

## 13. Production-readiness assessment

**Not accepted for production activation.** Functional engineering, isolated integration and selected live-source verification are delivered. Passing development tests, complete downloads and source publication do not establish permitted commercial use, matching quality, capacity or deployment readiness. The active application stack was not switched or migrated by this work. No source, private writer, inline flag or actual tenant grant was activated.

## Remaining steps before production activation

1. Approve the quality evaluation data/labels/coverage and close the quality gate for each intended source/profile; keep the unaccepted profile disabled in production authoring.
2. Resolve each intended source's usage/redistribution/purpose/retention permission. Obtain approved EU access if needed; retain ORC/PEP/non-SDN/linking as excluded until separately accepted.
3. Review the existing baseline lint debt (20 errors/74 warnings); the browser regression gate now passes. Rerun browser/security checks on the exact release artifact and record the frontend/release owner's disposition.
4. Reproduce API/worker/decision load on the intended EC2/RDS profile, with realistic sources/retained versions/backlogs/failures; approve end-to-end p50/p95/p99/errors and the shared DB budget.
5. Complete backup/PITR/restore and operational alert exercises; approve evidence storage/legal holds/retention and a source-quarantine/retry response owner.
6. Review and apply owner migrations in a maintenance-compatible order in staging: existing core/case prerequisites, auth version 4, then screening 11–22 and the reviewed role template. Migration 18 flushes GIN pending entries and disables fastupdate under a 5-second lock wait/60-second statement bound; plan that write-lock window. Migration 21 requires newly published Unicode-17 projections before older datasets become ready for current search; explicitly rebuild/review them from retained or reacquired complete artifacts and verify resulting rescreens. Do not drop published source tables for a feature rollback. An auth binary older than schema version 4 is not a valid rollback target after upgrade.
7. Provision reviewed login memberships/secrets outside the repository; the template creates NOLOGIN capabilities only. Verify reader/private-writer/collector/operator boundaries against the same logical primary DB and restrict network access. Account for every API/worker replica and administrative connection.
8. Separately authorize runtime configuration: explicitly selected provider/datasets, approved source rights/cadence/staleness, minimal tenant grants and, if needed, the private writer URL/secret file. Start the optional source profile only after review.
9. Publish one canary source/list, verify counts/provenance/readiness/currentness/queues/review and rollback/alert behavior. Validate actual credential boundaries and metrics; never treat stale/quarantined data as a clean result.
10. If inline use is approved, explicitly enable INTERNAL_SCREENING_INLINE_ENABLED with INTERNAL_SCREENING_FAILURE_POLICY=error and an accepted timeout budget, then enable only accepted scenario/profile/source policies. Canary the final outcome/evidence/effects and failure paths before wider rollout.

## Operator and test runbook

These commands are implementation/runbook references. **They were not executed against the application or production database.** Source tools read explicit SOURCE_DATABASE_URL or SOURCE_DATABASE_URL_FILE; they never select the app DB by default. Keep secret files outside tracked files and do not print tokens/DSNs in logs. Private writer config uses PRIVATE_LIST_DATABASE_URL (or its _FILE variant); import/approve roles are tenant_admin only. The app checks the same-DB identity and owner role function.

Source tools built into the screening image:

| Tool | Use |
| --- | --- |
| source-admin -action catalog | Inspect approved connector definitions, no source activation |
| source-admin -action register -source ofac-sdn -dataset ofac-sdn | Create disabled/unreviewed global metadata; no tenant grants |
| source-admin -action status -dataset ofac-sdn | Sanitized rights/freshness/error/active-generation status |
| source-admin -action diagnostics | Aggregate source/rescreen/continuous backlog, failures and ages, no screening names/queries |
| source-admin -action enable -dataset ofac-sdn -rights-review-reference APPROVED_REFERENCE -max-stale 48h -poll-interval 24h | Reserved authorized source activation; reference/cadence are deployment policy, not a publisher SLA |
| source-admin -action pause or retry -dataset ofac-sdn | Pause/fence a source or explicitly retry an approved source; a bad schema/large drop will quarantine again |
| source-admin -action approve-use -dataset REVIEWED_NOTICE_DATASET -rights-review-reference APPROVED_REFERENCE -max-stale 48h | Reserved notice-use approval; does not enable automatic collection or create tenant grants |
| source-worker -mode once / -mode poll -poll-interval 30s -batch 4 | Separate leased collection/recovery/cleanup worker, four connections |
| source-notice -action catalog / submit / inspect / approve / reject / recover / export | Controlled global notice intake; requires global role plus current human token/public verification keys; no customer API |
| source-import -rollback GENERATION -dataset DATASET | New-generation publication rollback; history retained |
| source-import -recover-interrupted | Explicit operator recovery of expired staging; never uses a default DB |
| source-import -retain-failed-days 7 -retain-staged-days 30 -legal-hold=true -dataset DATASET | Example explicit policy; keeps the hold; approval required before lifting it |
| source-import -cleanup-unpublished / -cleanup-artifacts | One bounded expired unreferenced page under explicit policies; published/review/quarantine evidence protected |

The optional docker-compose.screening-sources.yml overlay uses profile screening-sources and an explicitly provisioned SCREENING_SOURCE_DSN_FILE. It exposes no ports, runs as 65532 with read-only root/tmpfs/capability drops/resource limits, and depends on screening migrations. Merge/validate with the PostgreSQL base stack in staging before an authorized start. No compose up was issued for this profile.

Use explicit bounded pool settings across all replicas: collector 4, admin/import 2 each, private writer 2; configure API/queue pool maxima using their supported settings or DSN pool_max_conns. Reserve connections for auth/ingestion/decision/case/data-model services, migrations and operations. Do not copy the benchmark's default pool of 16 into a multi-replica deployment without budgeting. Alarm on quarantine, stale readiness, retry exhaustion, old queued targets, expired leases, continuous/publication lag, source validation failure, disk/storage growth and DB pool waits. Worker heap/pool logs are observations, not a complete monitoring/peak-resource system.

For recovery, pause/fence collection, inspect the durable attempt/artifact/diff, preserve legal holds, repair the cause and explicitly retry. Rollback uses a new immutable publication; it leaves collection paused/quarantined until reverified. For feature rollback, pause the source profile and disable the private writer/inline runtime flags using a compatible auth-v4 binary; retain schemas/evidence. Do not drop tables or reset history to restore availability.

Disposable verification uses SCREENING_TEST_DATABASE_URL, AUTH_PREFLIGHT_TEST_DATABASE_URL, DECISION_TEST_DATABASE_URL and CASE_MANAGER_TEST_DATABASE_URL, never app defaults. scripts/test-controlled-provider.sh full builds actual participating binaries and runs the screening suite. source-verify requires SOURCE_TEST_DATABASE_URL and creates/drops its own isolated DB; -fetch is explicit, -artifact uses a retained private download, and -check-unchanged verifies complete idempotency. Workload requires SCREENING_RUN_LOAD_TEST=true. Matching v2 uses MATCHING_CORPUS_VERSION=2 and MATCHING_PROFILE=name-token-v2 with an explicit report path. Real browser verification requires the Go API/worker handshake and test-only session; the fixture identities/keys are not deployment credentials.

## Phase 1 completion report

Historical controlled slice only: 111 top-level / 220 total screening tests plus later hardening, real manual API/worker/review/browser evidence; no official source or inline activation. See the immutable [Phase 1 evidence](evidence/internal-provider/2026-11-13/phase1-report.json). Those counts are historical and are not added to current totals.

## Phase 2 completion report

Historical controlled scenario/fuzzy integration: original 2,000-query corpus failed precision/FPR floors; actual scenario/worker/case/enrichment and browser checks were recorded. See [Phase 2 evidence](evidence/internal-provider/2026-11-14/phase2-report.json) and the original matching-quality report. Current ledger/source/lifecycle/inline status supersedes the earlier partial implementation prose; old measured evidence is retained rather than rewritten.
