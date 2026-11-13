# Internal provider: verification, security and operations

Stage 1, 2026-11-13. Proposed acceptance strategy follows the [audit](INTERNAL_PROVIDER_AUDIT.md) and [design](INTERNAL_PROVIDER_DESIGN.md). No real-source provider is implemented.

## Evidence and database boundary

The files under evidence/internal-provider/2026-11-13 preserve the original migration-branch audit at HEAD 224731f. Its 29 top-level / 46 test-subtest passes ran offline without a database. They are historical evidence, not acceptance of the PostgreSQL runtime or a real-source provider. Five bounded official source probes are likewise metadata checks, not full downloads or parsing.

The PostgreSQL port is verified separately in [POSTGRESQL_PORT_STATUS.md](POSTGRESQL_PORT_STATUS.md). Its API/worker regression results do not establish that the planned internal provider exists.

- [x] Provider integration and source-access unknowns documented.
- [x] Existing PostgreSQL screening API and worker regression executed on a disposable PostgreSQL 16 server.
- [ ] Internal provider, source migrations or connector compatibility tests.
- [ ] Full-file source parsing, ingestion or live-source acceptance.
- [ ] Production rollout or representative workload acceptance.

Run the PostgreSQL suites with DECISION_TEST_DATABASE_URL, DECISION_ENGINE_TEST_DATABASE_URL, SCREENING_TEST_DATABASE_URL and CASE_MANAGER_TEST_DATABASE_URL pointing only to a disposable server with CREATE DATABASE permission. Tests create and drop their own databases. The screening journey tests also require freshly built API/worker binaries; a skip is not a pass. Never use the application DATABASE_URL for these tests.

## Source connector tests: required for each implemented connector

| Case | Expected assertion |
| --- | --- |
| Successful snapshot | Correct IDs, types, aliases, programmes, identity fields, artifact metadata and coverage counts |
| HTTP failure / DNS / timeout | Classified error, bounded retry where transient, old active generation unchanged |
| Authentication / permission denied | No secret in public response/log; visible operator action; no endless retry |
| 429 / 503 | Configured bounded backoff/jitter; validated Retry-After; no tight retry loop |
| HEAD unsupported | Bounded GET/check strategy remains possible; a HEAD 404 is not treated as proof a valid GET feed disappeared |
| Empty / truncated / HTML body | Reject or quarantine; never publish an empty replacement by accident |
| Wrong content type / unsupported format | Explicit rejection; do not parse an error page as a dataset |
| Wrong root, namespace or incompatible schema | Explicit schema error; no partial success |
| Malformed XML / DTD / external entity | Reject safely; no outbound request or local-file access during parse |
| Huge field / excessive depth / records / bytes | Bounded resource usage and explicit failure |
| Duplicate/conflicting source ID | Duplicate/conflict diagnosis; no last-record-wins corruption |
| Redirect changed to unapproved host | Reject/quarantine; no SSRF; omit signed query strings from diagnostics |
| Conditional unchanged response | Refresh successful-check time; do not invent a new content generation |

Use hand-authored schema-realistic synthetic fixtures with source/schema references, or actual public fixtures only after rights review. Do not commit full personal-data downloads as ordinary test fixtures. Parsing tests should not depend on live websites.

## Ingestion and publication tests

- [ ] Initial import creates a staged generation with complete provenance.
- [ ] Identical rerun is idempotent; byte-format-only changes are distinguished from semantic changes.
- [ ] New, changed and absent source records produce correct change entries.
- [ ] Removed aliases, identifiers, programmes and relationships do not linger in the current generation.
- [ ] Absence is recorded as removal from snapshot; an official delisting classification needs supporting evidence.
- [ ] Empty/large-drop/invalid input remains unpublished; approved valid changes can proceed.
- [ ] Batches are bounded; no entire large dataset is retained in memory.
- [ ] Failure before/after staging, validation and publication leaves the previous generation complete and searchable.
- [ ] Two workers racing on one dataset cannot both publish based on the same obsolete parent.
- [ ] Expired lease holders cannot publish or acknowledge a newer worker's run.
- [ ] Publication pointer, state and update event commit or roll back together.
- [ ] Source-specific ordering/cursors reject a missing delta and recover through snapshots.
- [ ] Crash/restart safely resumes or abandons staging; repeated events do not duplicate rescreening.
- [ ] Rollback uses an audited publication, never rewrites historical matches.
- [ ] Retention/garbage collection does not delete in-use generations or required source evidence.

## Provider and search tests

- [ ] Person and organization screening through the existing ScreeningProvider.
- [ ] Exact, normalized, alias and fuzzy names; Unicode, Ghanaian multi-part names, reorderings and false positives.
- [ ] Typed identifier match/nonmatch with issuer/type semantics; leading zeros preserved.
- [ ] Date precision/ranges, nationality/country/address compatibility and missing-value behavior.
- [ ] Dataset and category filters validated independently; no sanctions/PEP/warning conflation.
- [ ] Threshold edges 0 and 100; result bounds 1 and 50; deterministic ties.
- [ ] Candidate explanation records matched alias, differing/missing attributes, score components and algorithm version.
- [ ] Candidate generation overflow produces partial/error, never a falsely complete no-hit.
- [ ] Dataset unavailable/stale/unknown/unauthorized produces an explicit error, never implicit fallback.
- [ ] Catalog/freshness/delta payload schemas and cursor expiry are contract-tested.
- [ ] Old source/provider results remain readable and are never relabelled.
- [ ] Visibility: global published sources are usable only under deployment/tenant entitlements; private records never leak through search, catalog, enrich or explanation.

## Existing API/workflow compatibility tests

Run actual orchestration and native router tests with the new adapter, not just direct matcher calls:

- [ ] Manual create, idempotent retry, creator ownership, history paging, reopening and case creation.
- [ ] Scenario authoring save/reopen, disabled rules, unsupported capabilities and older request shapes.
- [ ] Implement and verify PostgreSQL decision-time screening/outcome escalation, bounded deadlines and provider failure before enabling its capability.
- [ ] Future decision-time worker replays immutable captured evidence after a dataset changes, without a second search.
- [ ] Continuous screening requeues eligible monitored objects after a publication using existing checkpoint logic.
- [ ] Candidate persistence, match review, refinement, actor identity and current inbox/case permissions.
- [ ] Counterparty/object exclusions preserve their provider, tenant, object and entity scope and audit receipts.
- [ ] Unresolved/partial findings still prevent approval; provider evidence does not auto-authorize reviewer actions.
- [ ] Candidate cards/details show source dataset, generation and explanations using compatible fields.
- [ ] No additional customer search endpoint or source-specific handler branch is needed.

Run the existing screening, decision, case and frontend regressions relevant to each integration change. Preserve assertions; do not make a failing real workflow pass by replacing it with a mock.

## Database verification and migration gate

Use the screening service PostgreSQL metadata migration runner and independently tracked owner lineage. Before application installation:

1. Capture PostgreSQL definitions and query plans from a separate disposable reference database.
2. Install prerequisite auth/data-model/decision/case owners and screening lineage in the documented order.
3. Apply the new source-data steps to the disposable target; verify empty install, migration ordering, upgrades, rollback and restart after interruption.
4. Test constraints, scope-consistent FKs, unique source identity, active-generation changes and publication atomicity.
5. Verify exact runtime grants: collector can stage/publish only owned source data; reader cannot mutate; decision reader cannot change screening-owned data.
6. Verify deny paths using the actual restricted accounts, including tenant leakage and attempted unreviewed publication.
7. Keep experimental schemas, volumes, credentials and network isolated from the new-fraud-system application project.

No experimental migration, destructive setup, root-credential inspection or live source import against the application DB belongs in this audit stage.

## Security design gates

These are required future controls, not claims about an implemented collector.

- External retrieval uses TLS verification, request deadlines, configured byte caps and bounded concurrency.
- URLs come from reviewed source configuration, never customer requests. Validate every redirect and resolved address against approved source/CDN policy; reject private/loopback/link-local/metadata destinations and unsafe schemes.
- The verified OFAC and UN endpoints redirect to publisher-managed storage. Pin approved hosts and fetch a fresh redirect; never persist expiring signed URLs or credentials in result payloads.
- Keep secret references in configuration and secret values in the existing runtime secret mechanism. Redact query strings/headers in diagnostics.
- Use private immutable artifacts, generated object names and safe temporary files. Never derive a filesystem path from an external filename.
- Prefer uncompressed XML initially. If archives are added, independently enforce entry count, extracted byte limits and path traversal/symlink protections.
- XML parsing must reject DTD/external-entity directives, bound nesting/text/record sizes and never retrieve referenced schemas or entities from input. Use reviewed pinned schema/structural validation.
- Administrative source config, import and publication need explicit platform authority and durable actor receipts. An ordinary tenant API token cannot administer global feeds.
- Treat publisher text as untrusted content in the UI; escape it and validate links.
- Keep global raw artifacts private; tenant-visible evidence contains only approved provenance and identity details.
- New logs/metrics expose source IDs, run status, timings/counts and safe error codes, not submitted names, identity numbers or secrets.
- Apply retention, correction and audit requirements to raw files as well as normalized records.

## Ghana data protection and classification assessment

The [Ghana Data Protection Commission guidance](https://dpc.gov.gh/for-organisations/) identifies controller/processor registration, privacy and security responsibilities under Act 843. The [official Act publication](https://nca.org.gh/wp-content/uploads/2020/09/Data-Protection-Act-2012.pdf) and [2025 BoG/FIC guideline](https://www.bog.gov.gh/wp-content/uploads/2025/11/BOG-FIC-AML-GUIDELINE-2025.pdf) are relevant review inputs. This plan is an engineering assessment, not a determination that this deployment satisfies those requirements.

Before acquiring personal-data feeds, record purpose/lawful processing assessment, controller/processor roles, access/redistribution terms, minimum fields, retention, corrections and data-subject request handling. Assess applicable cross-border processing, sensitive data and allegation handling with qualified review.

Keep source claims distinct: official designation, PEP/public role, corporate ownership, warning, investigation, charge, conviction, acquittal and adverse media. A register entry or similar name is not proof of wrongdoing.

## Performance and operational acceptance

Use a disposable database and reproducible generated corpus at official-feed scale and larger measured growth cases. Fixture counts must be reported; no throughput claim is supported by the Stage 1 baseline.

Measure:

- End-to-end decision latency versus provider-only latency; p50/p95/p99, success/error rates and sustained successful TPS.
- Warm/cold index behavior, aliases per entity, Unicode/name distributions and low-selectivity queries.
- Tenant-private/global dataset combinations and hot beneficiaries.
- Index rows examined, query plans, join order, posting selectivity, shared buffers/disk use and candidate recall against a brute-force test oracle on a manageable corpus.
- Concurrent source staging, index building, publication and rescreen fanout while live searches run.
- Shared connection budget across all API/worker/collector/decision replicas, lock waits and pool queueing.
- Peak RSS, input bytes, rows per batch, staging/storage growth, recovery duration and bounded GC.
- Existing 500 TPS / approximately 50 ms aspirations with a declared hardware, concurrency, duration and percentile definition. Do not silently equate provider-only microbenchmarks with total decision performance.

Do not use in-memory copies of the entire corpus or name-by-name SQL scans without evidence that resource/recall targets hold. Permit additional search infrastructure only after these measurements justify it.

Operations should expose last check/publication, source lag, active generation, run history, queue/retry age, quarantine state and worker heartbeats. Provide manual retry, approved publication, rollback and stale-source recovery procedures before release.

## Evidence levels

| Label | Required evidence |
| --- | --- |
| Fixture-tested | Offline parser, ingestion, provider and deny-path tests pass |
| Database-tested | Real disposable migration, scope, concurrency and rollback tests pass |
| Live-source-tested | Explicit complete official retrieval + validation + import/search report |
| Deployment-verified | Configured real adapter and published datasets exercised through actual service/UI |
| Production-verified | Approved operating rights, measured load/recovery, monitoring and release acceptance |

Each source and provider capability has its own level. Never infer one level from another.
