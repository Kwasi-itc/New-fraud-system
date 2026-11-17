# Internal provider source inventory

## Phase 8 acceptance update — 2026-11-15

The [acceptance register](INTERNAL_PROVIDER_ACCEPTANCE.md) is the definitive current gate record. All 21 browser failures are repaired: **134 full-suite passes, zero failures, one dedicated skip**, plus a separate real API/PostgreSQL/worker **1/1 pass**. Final screening regression is **138/288**, auth **33/60**, decision **260/423**, case **90/157**, investigator **1/1** (top-level / including subtests). Name quality remains unaccepted; separate identifier engineering calibration is documented. The complete sustained workload measured **87.487 decisions/s, p95 290.738 ms**, below target. Migration 22, actual restricted logins, restored-clone binding, logical restore/worker replay and operational alert contracts pass disposable checks. Source permissions, representative quality/capacity, production security/recovery/alerting and explicit activation remain open. Existing frontend lint (20 errors/74 warnings) reproduces on HEAD and is recorded separately. **No production-ready claim or activation is made.**

See [matching](INTERNAL_PROVIDER_MATCHING_EVALUATION.md), [browser investigation](INTERNAL_PROVIDER_BROWSER_REVIEW.md), [source approvals](INTERNAL_PROVIDER_SOURCE_APPROVALS.md), [performance](INTERNAL_PROVIDER_PERFORMANCE.md), [security](INTERNAL_PROVIDER_SECURITY_ACCEPTANCE.md), [recovery](INTERNAL_PROVIDER_RECOVERY.md), [activation runbook](INTERNAL_PROVIDER_ACTIVATION.md) and [Phase 8 machine manifest](evidence/internal-provider/2026-11-14/phase8/manifest.json). Earlier dated measurements below remain historical where explicitly superseded by this update.

Updated 2026-11-15 UTC, PostgreSQL main base 18c6e5d plus the uncommitted implementation. This pack supersedes the earlier Phase 1/2 status prose. PostgreSQL is the shared application database; MySQL data is preserved separately. **Development engineering is delivered across the approved workstreams; production readiness is not accepted.** No application DB migrations, production source activation, tenant grants for actual official data, credential changes, commit or push occurred.

## Verified coverage and acquisition

| Source | Category and exact coverage | Technical verification | Usage/release gate |
| --- | --- | --- | --- |
| OFAC SDN | Sanctions; SDN only, not consolidated non-SDN or ownership-derived restrictions | Complete download/XSD evidence; 19,565 records: 7,541 people, 10,112 organizations, 1,570 vessels, 342 aircraft; all present types searched/enriched; unchanged full snapshot verified | Source-specific purpose, personal-data use/retention and redistribution review before tenant release |
| UN consolidated | Security Council consolidated individuals/entities; not a PEP database | Complete 1,007 records: 733 people and 274 organizations; stable IDs, aliases/date precision; search/enrich/idempotency | Commercial reuse and retention terms remain unresolved |
| UKSL | Current UK designations/specified ships; historical OFSI IDs retained; not the retired feed | Complete 6,440 records: 4,070 people, 1,676 organizations, 694 vessels; search/enrich/idempotency | Review applicable OGL/personal-data/third-party exceptions and purpose/retention |
| EU FSF | EU financial-sanctions records only | FSF 1.1 fictional fixtures, parser/provenance and hardened downloader groundwork | Approved production access/token/endpoint, exact live schema and rights missing; no live EU claim |
| Ghana BoG / SEC | Referenced regulatory/licensing warnings, with dated corrections | Controlled upload/review/withdrawal framework, fictional integration records only | Agency-specific acquisition/use/redistribution/retention approval; no scraping or actual publication |
| Ghana OSP | Wanted/proceeding notices; not guilt, PEP or complete investigation coverage | Same controlled reviewed framework, fixed proceeding_notice category | Source-specific acquisition/purpose/retention and correction policy |
| Ghana FIC | Reference channel to UN material | Reference-only registry entry; use the separate UN connector | No duplicate national feed or invented bulk API |
| Ghana ORC | Corporate information, not sanctions/PEP | Contract-required registry/specification only | Fees, API/export schema, entitlement, purpose and redistribution contract absent |
| Private tenant list | tenant_watchlist; coverage is the tenant-supplied snapshot | Signed/current administrator intake, strict CSV, different reviewer, full version/lifecycle/UI checks | Tenant-supplied data-use authorization; no global visibility |
| PEP/SOE and identity linking | No implemented production coverage | Deliberately deferred policy work | Taxonomy, authoritative provenance, corrections/expiry, purpose and any link policy required |

The [sanitized live-source report](evidence/internal-provider/2026-11-14/official-source-verification.json) contains artifact hashes, sizes, publisher dates, counts, type checks and idempotency results. It contains no raw official records. Each actual source was published only into a newly created disposable verification DB, dropped at exit. No tenant grant, commercial activation or production readiness was inferred from those tests.

## Authoritative endpoints and evidence

### OFAC SDN

The [official Sanctions List Service](https://ofac.treasury.gov/sanctions-list-service) publishes the SDN exports. The connector pins the classic export at https://sanctionslistservice.ofac.treas.gov/api/PublicationPreview/exports/SDN.XML and its official schema mapping. [OFAC technical format guidance](https://ofac.treasury.gov/sdn-list-data-formats-data-schemas/ofac-technical-actions-in-reverse-chronological-order/20240516_44) distinguishes available formats. A full retained download was separately validated against the official XSD with zero schema errors and the expected 19,565 records; the runtime Go parser uses its strict structural contract, not a generic XSD engine.

UIDs, all four entity types, primary/native/alias names and alias quality, identifiers/issuing jurisdiction, programs and original XML/provenance are retained. Unknown country/date text is preserved rather than coerced into a strong match. The US publication record is useful rights evidence, but it does not replace a source-specific purpose/personal-data/retention review for this deployment. NSDN-01 remains separate future coverage.

### UN

Use the [Security Council consolidated-list page](https://main.un.org/securitycouncil/en/sanctions/un-sc-consolidated-list) and https://scsanctions.un.org/resources/xml/en/consolidated.xml. The downloader permits only the documented official redirect destination. DATAID, list references, strong/weak aliases, partial dates and source documents are preserved. Unknown/approximate dates are evidence text, not invented full birth dates. [UN terms](https://www.un.org/en/about-us/terms-of-use) require a separate usage review; the terms probe was not sufficient to settle commercial reuse. The source remains unreviewed for application use.

### UK

Use the [UK Sanctions List publication](https://www.gov.uk/government/publications/the-uk-sanctions-list) and https://sanctionslist.fcdo.gov.uk/docs/UK-Sanctions-List.xml. The [single-list transition guidance](https://www.gov.uk/guidance/moving-to-a-single-list-for-uk-sanctions-designations-28-january-2026) identifies the current UKSL path, not an obsolete OFSI feed. Preserve UKSL designation IDs, historical IDs, entity types, structured/native names, ships and restrictions. General OGL guidance must not be generalized to exempt personal/third-party material without review.

### EU

The [official EU dataset catalogue](https://data.europa.eu/data/datasets/consolidated-list-of-persons-groups-and-entities-subject-to-eu-financial-sanctions) links the [FSF distribution portal](https://webgate.ec.europa.eu/fsd/fsf#!/files). The [official FSF guide](https://circabc.europa.eu/d/a/workspace/SpacesStore/9cf02a9e-7604-4178-a21d-d0b6bdcb4114/MA_FSF_EN.pdf) informed fixture mapping. Approved live access is absent; example or third-party tokens were not copied. The URL is explicitly supplied through an approved secret/secret-file reference and is still subject to the official host/redirect boundary. A fixture parser is not full live schema or reuse acceptance.

### Ghana

The authoritative inventories checked were [FIC sanctions publications](https://fic.gov.gh/publications-sanctions.html), [BoG notices](https://www.bog.gov.gh/notice/), [SEC public notices](https://sec.gov.gh/public-notices-press-releases/), [OSP archive](https://osp.gov.gh/case_files/archive), and [ORC](https://orc.gov.gh/). These public pages do not establish a bulk API or redistribution permission. The reviewed intake therefore takes an authorized structured upload and retained PDF/captured text; it never fetches arbitrary customer URLs or scrapes sites.

Registry source keys are reviewed-gh-bog, reviewed-gh-sec and reviewed-gh-osp. References must use that source's official HTTPS host. Category and coverage text are server controlled. Submission records notice/date/reference/rights-review reference, the original attachment and CSV hashes. Two distinct authorized global operators review content; rights_state remains unreviewed, collection remains disabled and no tenant grants are added. The separate approve-use operation is a future authorized rights configuration action, not a consequence of content approval. Withdrawals/corrections are new reviewed snapshots and retain history.

## Common source controls

- [x] Full-snapshot reconciliation includes additions, modifications and removals; unchanged content with the same normalizer creates no new publication event. A normalization upgrade requires a new reviewed generation.
- [x] Downloads, redirects, decoded size, record size/depth/count, encoding, source IDs/types, strict closure and duplicate IDs are bounded and validated.
- [x] Original bytes/hashes and source/parser versions are retained before publication. Quarantine does not claim successful freshness.
- [x] Native scripts are preserved; cross-script candidates need explicit source aliases. No invented transliteration or confidence upgrade is claimed.
- [x] 304 checks are bound to the last verified active generation and current normalizer. Source failure retries are leased, persisted, jittered and capped; large-drop/schema anomalies require operator review.
- [ ] Rights/retention/purpose approvals and representative per-source matching acceptance precede any tenant release.
- [ ] EU access, ORC agreement and PEP/SOE/linking policy require their own approvals.

All source collection and private/inline production activation remain separate authorized operations. The separate no-login role template and optional Docker profile do not create credentials or enable a source.
