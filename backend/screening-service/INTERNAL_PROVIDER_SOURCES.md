# Internal provider: external source inventory

Last research and endpoint probes: 2026-11-13 UTC. No connector is implemented.

## How to interpret this inventory

Documentation verified means an official publication or access guide was inspected. Prefix verified means an actual bounded XML response was retrieved, not that the file was fully downloaded, parsed, reconciled or indexed. Access conditions and current rights still require source-specific assessment.

For every source below: request-rate limits are **not established** unless stated; no frequency or SLA is inferred from a recent timestamp. Configured polling cadence is our operating policy, not the publisher's update frequency. Public display does not establish commercial redistribution or automated collection rights.

The existing SCREENING_SERVICE_SOURCE_INVENTORY.md maps monolith code ownership; it is not an external-feed inventory and is not replaced by this document.

## Inventory

All implementation status entries are Not started unless a separate access blocker is listed. Stable dataset IDs below are proposed, not registered runtime values.

| Source | Dataset | Category | Official URL | Retrieval method | Access requirements | Licensing | Update frequency | Incremental updates | Removal handling | Implementation status | Last verified |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| US Treasury OFAC | us_ofac_sdn | Sanctions designations | [SLS][ofac] | Official XML export / API; CSV and other formats published | XML prefix retrieved anonymously; User-Agent required by technical guidance | Federal-work guidance is relevant; retain source-specific rights assessment and third-party exceptions | Publication-driven; fixed interval not established | Official delta archive exists; parser/continuity not verified | First connector uses validated complete snapshot reconciliation; record removal separately from official delisting reason | Not started; prefix verified | 2026-11-13 |
| UN Security Council | un_sc_consolidated | Sanctions designations | [Consolidated list][un] | XML; HTML/PDF alternatives | Anonymous stable endpoint redirects to publisher blob storage | Dataset-specific commercial reuse not established; UN terms retrieval was denied | Updated when committees change designations | Update notices available; no verified machine delta contract | Compare validated complete snapshots; preserve listing/update references | Not started; prefix verified | 2026-11-13 |
| European Commission DG FISMA | eu_fsf | Financial sanctions | [Commission overview][eu] | FSF XML/CSV/PDF and robot/checksum guide | Guide describes registered user token; current deployment access not verified | Commission reuse framework exists; dataset-specific conditions and personal-data exceptions unresolved | As needed per official overview | No verified machine delta; proposed full snapshots | Snapshot comparison plus legal-act references | Blocked on current access verification; no connector | 2026-11-13 |
| UK FCDO | uk_sanctions_list | Sanctions; retain individual sanction types | [UKSL publication][uk] | XML/CSV plus other official formats | XML prefix retrieved anonymously | Publication page states OGL v3 except where otherwise stated; verify file-specific exceptions | Designation changes; no fixed SLA established | No verified machine delta | Snapshot reconciliation plus published changes | Not started; prefix verified | 2026-11-13 |
| Ghana FIC | gh_fic_notices | Source notices / UN references; national designations only if individually evidenced | [Sanctions publication][fic] | Linked UN data and PDF notices | Public reading; bulk API and automation permission not established | Commercial reuse and collection terms unresolved | Notice-driven; no fixed cadence established | Listing/delisting notices; no structured delta verified | Retain notice identity and corrections; use UN as the authoritative UN feed | Not started; source documentation verified | 2026-11-13 |
| Bank of Ghana | gh_bog_warnings | Regulatory warning / enforcement, not automatically sanctions | [BoG][bog] | Official notices and PDF publications | Public reading; bulk API not established | Automated retrieval/redistribution terms unresolved | Notice-driven | Not established | Human-reviewed superseding notices; page absence is not delisting | Not started; structured access unresolved | 2026-11-13 |
| Ghana SEC | gh_sec_warnings | Unlicensed activity / regulatory warnings | [Public notices][sec] | Dated HTML notices and PDFs | Public reading; no supported bulk API established | Automated retrieval/commercial redistribution not established | Notice-driven | No verified structured delta | Retain corrections, notice IDs, licence-status changes and effective dates | Not started; official example notice verified | 2026-11-13 |
| Ghana OSP | gh_osp_enforcement | Wanted notices / proceedings / outcomes | [Case files][osp] | Public pages; statutory reports | Public reading; no supported bulk API established | Automated retrieval and redistribution not established | Reports periodically published; notices event-driven | Active/archive presentation, not a verified feed | Preserve case status and archive/correction evidence; never infer guilt | Not started; controlled import candidate | 2026-11-13 |
| Ghana ORC | gh_orc_records | Corporate identity / ownership enrichment | [Entity information search][orc] | Search service; paid bulk search described | Fees apply; API, auth model and delivery format not established | Contract/usage scope needed; no public bulk licence established | Request-based / register changes; feed cadence unknown | Not established | Licensed refreshed extracts; corporate removal is not sanctions delisting | Blocked on acquisition/access terms | 2026-11-13 |
| Ghana Parliament | gh_public_office | Public office / PEP evidence requiring curation | [Members][parliament] | Paginated public profiles | Reading is public; automated bulk access not established | Reuse/automation rights unresolved | Officeholder changes; cadence not established | Not established | Keep term start/end dates and verified changes | Deferred; not a comprehensive PEP dataset | 2026-11-13 |
| Ghana SIGA | gh_state_entities | State ownership / corporate relationships | [Ownership reports][siga] | Reports and specified-entity publications | Public reports; automated structured bulk API not established | Reuse/automation rights unresolved | Described as annual; observed publication dates vary | Not established | Versioned reports and dated ownership statements | Deferred; controlled import candidate | 2026-11-13 |
| Publisher-specific media | No default dataset | Adverse media | No approved publisher selected | Only an approved feed or licensed/manual acquisition | Publisher-specific | Unresolved; public reading is insufficient | Publisher-specific | Unknown | Corrections, retractions, event status and retention policy mandatory | Deferred; no connector or coverage claim | 2026-11-13 |

## Download specifications and source-specific findings

### OFAC reference connector

Official distribution endpoint confirmed by [OFAC technical notice][ofac-tech]:

- https://sanctionslistservice.ofac.treas.gov/api/PublicationPreview/exports/SDN.XML
- Schema: https://sanctionslistservice.ofac.treas.gov/api/PublicationPreview/exports/XML.xsd

The [SLS site][sls] describes API-based data pulls; the [SLS overview][ofac] links its delta archive. Both classic and advanced XML have official [namespace/schema guidance][ofac-schema].

Proposed first connector is the classic SDN XML snapshot: explicit source UID, names, aliases, programmes and available identity details. Evaluate the advanced model separately where richer relationships are required; do not claim the classic feed provides all relationships. Preserve unsupported source types rather than coercing a vessel or aircraft into a company.

Bounded GET returned HTTP 206 and an sdnList root in the documented namespace. Schema prefix retrieval also succeeded with HTTP 206, although HEAD had returned 400. Full-file validation, record-count checks, legal-use recording and ingestion remain pending.

Initial updates reconcile complete snapshots. Delta support stays disabled until ordered publication IDs, schema coverage, gap recovery and snapshot parity are tested. A delta archive alone is not a safe incremental implementation.

### UN connector

Stable XML URLs linked from the [official publication][un]:

- https://scsanctions.un.org/resources/xml/en/consolidated.xml
- https://scsanctions.un.org/resources/xml/en/name/consolidated.xml

Both bounded GET probes returned HTTP 206 with CONSOLIDATED_LIST. The first endpoint returned 404 to HEAD, demonstrating that HEAD failure alone must not mark the source unavailable. Redirects went to unsolprodfiles.blob.core.windows.net; signed query parameters were not retained.

The official field guide describes permanent reference numbers, alternative names, dates and identity details. [Update notices][un-updates] distinguish listing, amendment and delisting. Use one XML ordering, not both as separate datasets.

UN commercial/data reuse conditions were not resolved: the terms page returned 403 through the research tool. Do not replace the missing rights assessment with an assumption.

### EU connector

The Commission [overview][eu] links its consolidated financial sanctions list. Its [official FSF guide][eu-guide] describes XML/XSD, CSV/PDF, checksums, registered-user robot URLs and RSS.

The guide includes test-environment examples: these are **not production endpoints or credentials**. No copied/shared token will be embedded in our repository. The current FSF deployment URL, schema, user access and applicable dataset licence must be verified before coding an active connector. A JavaScript-only data portal response did not establish anonymous bulk access.

[Commission reuse policy][eu-reuse] is a starting point, not confirmation that every personal-data field may be republished commercially.

### UK connector

The current XML endpoint is https://sanctionslist.fcdo.gov.uk/docs/UK-Sanctions-List.xml. The [publication][uk] supplies other formats and its format guide.

Bounded GET returned HTTP 206 with Designations; the HEAD response advertised approximately 22 MB. Do not use that observed size as a permanent cap.

The [official transition guidance][uk-transition] confirms that the OFSI consolidated list closed on 28 January 2026. The connector must use UKSL Unique IDs and retain sanction types; old OFSI IDs are historical identifiers. Publication content has the standard OGL notice, with exceptions to check.

### Ghana FIC

The [public sanctions page][fic] presents UN links and recent notices. This does not establish an independent, complete Ghana national-designation API. Reuse the UN connector for UN records and retain FIC notices as supplementary references when appropriate. A Ghana national-designation dataset requires an actual authoritative publication and legal scope, not a renamed UN copy.

### Bank of Ghana

Official [AML/CFT/CPF guidance][bog-aml] and [administrative sanctions guidance][bog-penalties] are policy references, not customer screening databases. The official financial stability review also describes public warnings about unlicensed activity.

Investigate individual official notices and their correction mechanisms. Their fields may include a business/app name, warning date, notice number and action; identity completeness is source-dependent. No API contract or authorized automated notice retrieval was established.

### Ghana SEC

[Official notices][sec] and an [example unlicensed-entities notice][sec-example] demonstrate HTML/PDF publication with names, dates and notice references. Classify these as regulatory warnings. They are not proof of an international sanctions designation, and the notice alone may not resolve the exact legal entity.

Until access and reuse are established, use controlled, reviewed imports with a source attachment, not a scraper advertised as reliable coverage.

### Ghana OSP

[Case files][osp] and [statutory reports][osp-reports] distinguish proceedings and outcomes. The [wanted/archive page][osp-wanted] expressly distinguishes a wanted notice from guilt.

Store allegation, wanted status, prosecution, conviction, acquittal and withdrawal separately, with original dates and source references. No bulk machine-readable API or reuse permission was established.

### ORC

[Entity search][orc] describes company, officer and beneficial-ownership information; the [fees page][orc-fees] describes paid searches including bulk tiers. This supports an acquisition discussion, not an assumption that we can scrape a free corporate/beneficial-owner API.

Obtain documented access, delivery formats, entitlements and reuse limits before implementing an automated connector. Do not treat a corporate-register entry as a risk designation.

### Ghana public office / SOEs / PEP

[Parliament profiles][parliament] and [SIGA reports][siga] are possible public-office/ownership evidence. Neither establishes complete Ghana PEP, family-member or close-associate coverage. Office history and identity evidence need curated review. A shared surname does not establish a family relationship.

A PEP classification is a separate risk category, not a criminal allegation or sanctions designation. Source records must state the public role, institution, dates and why the classification was assigned.

## Access, rights and historical-record assessment

| Source group | API and bulk status | Authentication and rate limit | Historical availability | Rights / automated retrieval decision |
| --- | --- | --- | --- | --- |
| OFAC | Official API and export mechanism documented; SDN XML prefix verified | No credential used for probe; explicit User-Agent; numeric quota unknown | Delta archive documented; completeness/retention not validated | Automation explicitly contemplated by [technical notice][ofac-tech]. [US Copyright Office][us-copyright] gives federal-work context; record dataset-specific review and exceptions |
| UN | XML bulk prefix verified; no search API contract assumed | No credential supplied; publisher-generated redirect; quota unknown | Change notices available; historical full snapshots not verified | Technical download works; commercial reuse/retention decision unresolved |
| EU | Bulk robot mechanism documented; current deployment unverified | Registered-user mechanism in official guide; no token provisioned; quota unknown | Historical snapshot/archive policy not verified | Verify specific licence, token terms and personal-data scope |
| UK | Current XML bulk prefix verified; public search UI is not an assumed API | No credential supplied; quota unknown | Publication change history; full historical snapshot availability not established | OGL v3 notice subject to exceptions; record attribution and specific dataset checks |
| FIC / BoG / SEC | Public notices/PDFs; bulk API not established | Reading public; machine auth/quota unknown | Notice/report archives are not verified complete datasets | Automation and redistribution unresolved; curated import only after assessment |
| OSP | Public active/archive pages and reports; no verified bulk API | Public reading; quota unknown | Reports and archive views; correction completeness unverified | Preserve proceeding status; acquisition/reuse unresolved |
| ORC | Paid search and bulk-search tiers; API/format unverified | Contract/account requirements to establish; quota unknown | Historical extracts and entitlement rules unknown | Access agreement and beneficial-owner restrictions unresolved |
| Parliament / SIGA | Profiles and reports; bulk API unverified | Public reading; quota unknown | Some reports available; officeholder history incomplete | Curation and reuse review required; no comprehensive PEP claim |
| Media | No selected feed | Publisher-specific | Publisher-specific | Deferred pending approved acquisition, retention and correction process |

## Verification limits and next evidence

[Probe evidence](evidence/internal-provider/2026-11-13/source-probes.json) records stable URLs, status, content type, byte limit and root tag. Only 4 KiB per endpoint was read. No complete source file, real-person fixture or signed redirect URL was committed.

Before marking a connector supported, record: complete artifact hash and schema, retrieval permissions, expected coverage/types, reference counts, field mapping, parser version, full initial/update/removal tests and its own live verification report. A healthy website is insufficient.

## Official references

[ofac]: https://ofac.treasury.gov/sanctions-list-service
[sls]: https://sanctionslist.ofac.treas.gov/Home/index.html
[ofac-tech]: https://ofac.treasury.gov/sdn-list-data-formats-data-schemas/ofac-technical-actions-in-reverse-chronological-order/20240516_44
[ofac-schema]: https://ofac.treasury.gov/recent-actions/20240507_44
[un]: https://main.un.org/securitycouncil/en/content/un-sc-consolidated-list
[un-updates]: https://main.un.org/securitycouncil/en/content/list-updates-unsc-consolidated-list
[eu]: https://finance.ec.europa.eu/eu-and-world/sanctions-restrictive-measures/overview-sanctions-and-related-resources_en
[eu-guide]: https://circabc.europa.eu/d/a/workspace/SpacesStore/9cf02a9e-7604-4178-a21d-d0b6bdcb4114/MA_FSF_EN.pdf
[eu-reuse]: https://eur-lex.europa.eu/eli/dec/2011/833/oj/eng
[uk]: https://www.gov.uk/government/publications/the-uk-sanctions-list
[uk-transition]: https://www.gov.uk/guidance/moving-to-a-single-list-for-uk-sanctions-designations-28-january-2026
[fic]: https://www.fic.gov.gh/publications-sanctions.html
[bog]: https://www.bog.gov.gh/
[bog-aml]: https://www.bog.gov.gh/wp-content/uploads/2025/11/BOG-FIC-AML-GUIDELINE-2025.pdf
[bog-penalties]: https://www.bog.gov.gh/wp-content/uploads/2023/03/BOG-FIC-AML-CFT-P-SANCTIONS-GUIDELINE-14-03-23.pdf
[sec]: https://sec.gov.gh/public-notices-press-releases/
[sec-example]: https://sec.gov.gh/public-notice-list-of-entities-operating-without-a-license/
[osp]: https://osp.gov.gh/case_files/
[osp-reports]: https://www.osp.gov.gh/resource_and_publications/statutory_publications
[osp-wanted]: https://osp.gov.gh/case_files/archive
[orc]: https://orc.gov.gh/service-entity-information-search.php
[orc-fees]: https://orc.gov.gh/fees.php
[parliament]: https://www.parliament.gh/members?page=1
[siga]: https://siga.gov.gh/resource-category/state-ownership-reports/
[us-copyright]: https://copyright.gov/history/copyright-exhibit/lifecycle/
