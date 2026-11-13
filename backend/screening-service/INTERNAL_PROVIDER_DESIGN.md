# Internal provider: architecture and ingestion design

Status: Stage 1 design, 2026-11-13. All new packages, tables and configuration below are proposed. No DDL, source worker, provider registry or real-data provider has been installed.

## Boundaries and deployment

Keep the existing screening API and orchestration. Introduce a separately deployed source-worker executable in the same Go module, with its own credentials, pool, CPU/memory limits and schedule configuration. This meets the separate data-sourcer requirement without creating another application database or repeating screening workflows.

    Official source
      -> source connector (retrieval / source parser)
      -> ingestion coordinator (validation / normalization / reconciliation)
      -> staged immutable PostgreSQL generation
      -> atomic publication pointer + event
      -> existing screening provider contract
      -> existing persistence, decision and case-review flows

Proposed responsibilities:

| Component | Ownership | Boundary |
| --- | --- | --- |
| internal/sourcedata | Source, artifact, generation, record and category types | No downloads, SQL or policy outcomes |
| internal/sourceconnectors | Registry and independent OFAC/UN/etc connectors | Source format/retrieval knowledge only |
| internal/sourceingest | Retrieval orchestration, staging, validation, publication | No customer-facing search or review decisions |
| internal/sourcedatastore/postgres | Bounded source persistence and indexed read repository | Shared logical database, screening-owned tables |
| internal/matching | Normalization-aware candidate generation and scoring | No source-specific branching or network |
| internal/internalprovider | Five-method ScreeningProvider implementation | Compatible results, provenance and explicit capabilities |
| providerlookup | Exported screening-owned inline read facade | Reuses the same provider/matcher; no parallel search endpoint |
| cmd/source-worker | Scheduling and ingestion process | No public listener required for initial rollout |

A connector registry discovers source implementations by stable source key. A provider registry resolves configured provider keys independently of sources. Source keys such as us_ofac and dataset IDs such as us_ofac_sdn are not provider IDs.

Start with PostgreSQL indexed queries. Additional search infrastructure is a decision requiring real-size recall, latency and operational evidence, not a default dependency.

## Existing provider contract integration

Keep Search, Enrich, GetCatalog, GetFreshness and GetDatasetDelta. Document the internal adapter's JSON payloads rather than replacing the generic port.

| Method | Planned internal behavior |
| --- | --- |
| Search | Resolve trusted visibility and allowed datasets; pin published generations; retrieve bounded candidates; score and explain; return current ProviderMatch/ProviderResult shapes |
| Enrich | Read maintained source information locally. Preserve the original stored search evidence; identify which generation supplemental information came from |
| GetCatalog | List available/authorized dataset IDs, categories, coverage, current generation, counts, retrieval/publication times and status |
| GetFreshness | Report last successful source check, content publication time, active generation, stale/degraded state and last successful import |
| GetDatasetDelta | Read our immutable publication log after an opaque cursor; stable paging and gap/expiry error; expose changed datasets for existing rescreening jobs |

Use a versioned internal provider_config for allowed dataset IDs, categories, threshold and algorithm version. Unknown settings must be rejected. Additive fields must also pass manual input, authoring, private proxy and capability validation; opaque JSON does not bypass validation.

Registration must preserve the existing HTTP/OpenSanctions adapter for deployments that still use it, and keep the synthetic adapter explicitly available for development. Unknown keys fail; no fallback to a different dataset or silent synthetic substitution.

### Remove hardcoded assumptions in a coordinated change

- internal/app/app.go and cmd/worker construct the HTTP provider adapter; register the internal adapter in both compositions.
- manual_search.Request fixes provider to opensanctions.
- Decision authoring only decodes the opensanctions key and exact version-1 configuration.
- PostgreSQL decision-time matching is not wired; keep inline capability false until implemented and tested.
- Private intake has provider allowlists.
- Capabilities and frontend proxies validate an existing provider-specific feature set.

Generalize these points with server-owned registry configuration and versioned authoring compatibility. Do not silently reinterpret stored opensanctions requests as internal-source requests.

Old results and whitelists keep their provider identity. New real-source configurations use the new provider key. A switch must not erase analyst evidence or carry a synthetic whitelist into real sanctions data without an explicit reviewed migration.

### Decision-time integration without another search API

The services share a logical PostgreSQL database. The current decision engine dispatches screening after saving decisions; forced outcomes are not implemented on this branch. A future decision-time integration can use providerlookup: a small public Go facade that accepts the trusted tenant and encoded existing request and invokes the same internal adapter used by screening dispatch.

The decision account receives reviewed SELECT-only access to screening-owned read views/index projections. It never writes source tables. No source download, source-specific code or second matching implementation belongs in the decision engine.

This is a documented shared read contract, with version/compatibility tests and a package-dependency check to avoid cycles. A future independent network deployment can add a private transport adapter after an explicit contract decision; it is not required for the current shared-database profile.

Implement request-digest-bound immutable snapshots as part of the PostgreSQL decision-time work; do not assume the retired MySQL implementation exists here. Apply current counterparty exclusions before outcome escalation. Persist the exact resulting snapshot and replay it in the worker. A later publication must not change a saved decision's evidence.

## Connector contracts

Follow Go's small-interface convention. Proposed core contract:

    type SnapshotConnector interface {
        Metadata() SourceMetadata
        Fetch(context.Context, FetchRequest) (ArtifactHandle, error)
        Parse(context.Context, io.Reader, RecordSink) (ParseSummary, error)
    }

Optional interfaces, discovered by capability/type assertion:

    type UpdateChecker interface {
        Check(context.Context, SourceCheckpoint) (UpdateStatus, error)
    }
    type DeltaConnector interface {
        FetchDelta(context.Context, SourceCheckpoint) (DeltaArtifact, error)
    }

Names are proposed and will be checked against existing package names during implementation. FetchRequest contains trusted configuration, conditional headers, byte/deadline limits and a worker-owned artifact sink. The connector receives no arbitrary customer URL. ArtifactHandle is a bounded stream/reference and metadata, not an entire in-memory dataset. RecordSink writes bounded validated batches.

SourceMetadata declares source key, supported formats/entities, category mapping, full/delta capabilities, schema/parser version and documented access constraints. Registry discovery advertises implemented capabilities only. A source without deltas implements no DeltaConnector; missing capabilities are explicit, never no-op successes.

Source-specific specifications:

| Connector | Initial mechanism | Validation and mapping work | Activation gate |
| --- | --- | --- | --- |
| OFAC SDN | Full official classic XML snapshot | Root/namespace, source UID, advertised count, types, names/aliases/IDs/programs; retain unmodelled source data | Full parser/schema validation and isolated update/removal tests |
| UN | Full official XML snapshot | Root and generated metadata; permanent/source IDs, name parts, alias quality, identity details | Independent fixtures, terms review and full verification |
| UK | Current UKSL snapshot | UKSL IDs, entity type, sanction types, aliases and identity data; retain historical IDs | Current schema fixtures and scope/rights review |
| EU | Verified FSF download obtained through approved access | Versioned source-specific parser and checksum behavior | Establish current access/token/schema before active implementation |
| Ghana notice import | Approved operator-supplied structured records + official document reference | Notice identity, category, actor, effective dates, correction links | Acquired rights and two-person validation/publication |
| ORC / curated PEP / SOE | No automatic connector assumed | Contracted export or controlled curation | Access, provenance, intended use and corrections established |

OFAC delta implementation is optional follow-up after safe snapshot reconciliation. It must enforce publication continuity and recover gaps through a complete snapshot; otherwise delta is unavailable.

## Internal data model

Do not repurpose screening_matches as source entities. It is an investigation evidence table tied to a tenant and screening. Add a new source-data slice to the existing screening migration owner.

Proposed additive structures, not migration statements:

| Structure | Key/relationships | Stored information |
| --- | --- | --- |
| screening_data_sources | source ID; connector key | Official location, schema/parser IDs, allowed hosts, schedule, enabled/rights state, secret reference |
| screening_data_datasets | stable dataset ID plus explicit owner scope | Source, coverage/categories, entity support, active generation, validity and entitlement status |
| screening_data_runs | run ID; dataset; expected active generation | Trigger/actor, idempotency key, attempts, lease/fence, timestamps, counts, diagnostics, status |
| screening_data_artifacts | artifact ID; ingestion run | Immutable private blob reference, byte count, hash, format, publisher/check times, redacted retrieval metadata |
| screening_data_versions | generation ID; dataset; run | Parent generation, content/semantic hashes, parser/normalizer/algorithm versions, validation state, counts |
| screening_data_source_records | scope/dataset/generation/source-record ID | Original values/reference, primary name/type, source classification, designation/program/status, provenance |
| screening_data_names | source record/generation + name ID | Original name, normalized forms, alias type/quality/script, ordering |
| screening_data_identifiers | source record/generation + typed identifier | Original and normalized value, issuer/country, validity, source provenance |
| screening_data_attributes | source record/generation | Partial/ranged dates, nationality, residence, place of birth, address, registration and permitted extra attributes |
| screening_data_relationships | generation/source relationship ID | Typed source/target references, effective dates, provenance; unresolved endpoints preserved explicitly |
| screening_data_entities / links | entity ID; source-record identity | Conservative cross-source grouping, evidence, actor/rule/version and reversible link history |
| screening_data_name_terms | scope/dataset/generation/name term | Indexed exact/token/ngram postings for candidate retrieval |
| screening_data_changes | publication/record/change ID | Added/modified/removed records and field-level alias/identifier/program/relationship differences |
| screening_data_publications | ordered publication ID; dataset/generation | Previous/current generation, immutable event, rescreen delivery state |

Use an explicit owner-scope key for global data versus tenant-private data; do not depend on nullable uniqueness semantics. Composite keys/FKs must carry scope, dataset and generation through every child/index table. Global public feeds are fetched once; private datasets always require a verified tenant. Generic tenant headers or provider_config cannot select another tenant's records.

Stable candidate IDs derive from source/dataset/source-record identity, not row position, primary name or generation. Identical imported source IDs conflict rather than silently overwrite different records. Linking across datasets is a separate audited operation; each source record remains independently addressable.

Provenance includes original source URL/record ID, dataset/version, retrieval and publication/effective times, last modification, original artifact reference, run ID, source status and transformation version. Where raw retention is not permitted, retain an allowed immutable reference and the minimum approved evidence.

Birth dates may be years, ranges or alternatives; do not invent a full date. Preserve address components and source country text alongside a separately validated country code. Gender and other personal fields require an approved purpose and retention policy.

Relationships are evidence, not inferred from names. Preserve unmodelled source types and explicit exclusion counts; never convert every OFAC object into Person/Organization or claim complete coverage after silently dropping records.

## Normalization and identity resolution

Store original and normalized values separately. Version normalization so results are reproducible.

- Apply Unicode normalization and case folding, normalize whitespace/punctuation, and retain original script.
- Preserve accents and apostrophes in the original; any alternative folding is a separate search key.
- Token ordering and alternate full-name forms support varied Ghanaian naming patterns without assuming a Western first/last-name split.
- Preserve identifier leading zeros, issuer/type and source-defined punctuation significance. Never fuzzy-match an identifier by default.
- Map country names/codes through a versioned reference; unknown values remain visible.
- Represent organization suffix handling as an explicit configured search feature, not destructive canonicalization.
- Source-record canonical hashes include aliases, identifiers, programmes and relationships in stable order; byte changes alone need not imply semantic changes.

Initial entity resolution keeps source records separate. Later links require sufficient evidence, such as corroborated typed identifiers and compatible identity attributes, or reviewed source references. Record who/what linked them and how to undo it. No fuzzy-name-only merges, family links or beneficial ownership inference.

## Initial ingestion and updates

1. Acquire a per-dataset lease with an increasing fence. Persist an idempotent run and expected current generation.
2. Fetch to a private bounded artifact stream outside SQL transactions. Capture stable URL, conditional headers, hash and safe publisher metadata.
3. Validate response status/type, complete file, format/root/schema and source completeness. Reject empty/truncated data unless an explicit approved source case establishes valid emptiness.
4. Parse incrementally, applying per-record/field/depth/count limits. Reject duplicate/conflicting source IDs and unresolved schema changes.
5. Normalize into a staging generation with bounded batches. Record original information and all transformation versions.
6. Build indexes and a deterministic source-record/field change set against the parent generation. An alias disappearing is a field removal, not necessarily an entity delisting.
7. Run consistency/count/coverage checks. Unexpected large drops quarantine the generation for operator review; a small valid delisting must remain processable.
8. In a short transaction, verify lease fence and parent generation, mark generation validated/published, compare-and-swap the active pointer and enqueue the publication event. Commit all publication changes together.
9. Complete the run and expose freshness/metrics. Failed or abandoned staging never changes active search data.
10. Deliver the publication event to existing rescreen machinery with bounded, checkpointed tenant fanout and idempotent receipts.

No download or large parse runs under a publication/database lock. An expired worker cannot publish after another worker. Keep the prior generation available for rollback; cleanup is separate, bounded and subject to retention/evidence/reference rules.

For snapshot sources, compare complete generations by source identity. Records absent from an authoritative complete snapshot become removed-from-current-source; record an official delisting only when supported by source evidence. Historical rows and past matches are retained. Changed aliases/IDs/programmes/relationships are replaced within the new immutable generation, not accumulated forever.

For future deltas, verify base version, publication ordering, repeat handling and the complete delta's integrity. Duplicate delivery is idempotent. A gap or unsupported operation fails closed and requests a snapshot; never advance a cursor past failed changes.

## Scheduling, retries and staleness

Source scheduling is global source administration, not an arbitrary tenant dataset-update job. Start with a private operator CLI for source config, run, retry and history; reuse platform administrator/session policy if a UI is added. No public source-upload/search endpoint is needed.

Configuration supplies an interval/calendar, UTC interpretation, maximum backoff, retry count, jitter, concurrency, byte limit and stale threshold per source. Polling defaults do not stand in for publisher guarantees. Disabled/unreviewed sources never fetch automatically.

Track last attempted check, last successful check, publisher version time, last successful publication, next due time, error class and retry state separately. A successful unchanged/304 check refreshes verification time without fabricating a new dataset publication.

Retry transient failures within configured bounds; validate Retry-After. Authentication, parser/schema and rights failures need visible action rather than endless retries. Sanitized diagnostic codes and structured logs distinguish unavailable, invalid, quarantined, stale, failed and published states.

Continue using the last successfully published generation within the configured freshness policy. If a required source is missing or too stale, return an explicit unavailable/incomplete result according to the declared policy. Never make source failure look like no match.

## Matching engine and explainability

Candidate generation and scoring are separate stages.

First use scoped, indexed exact names and typed identifier keys. Evaluate indexed token/ngram postings for fuzzy candidates and alias lookup; collect all query candidates in bounded batched reads, not an N+1 per candidate or a scan of an arbitrary first subset. Evaluate PostgreSQL B-tree exact/identifier indexes and pg_trgm where justified; PostgreSQL full-text alone must be evaluated for short names, non-Latin scripts, stopwords and recall before use.

Pin the selected generation set consistently. Fetch bounded evidence in a short read snapshot; release the DB transaction before CPU scoring. Concurrent publication cannot mix names from one generation with IDs from another.

Use a documented versioned scoring algorithm with separate evidence for exact/normalized/fuzzy name, matched alias, identifier, birth-date precision and country/nationality compatibility. Missing values are not mismatches. A conflicting identifier cannot be hidden behind a high name score. Weights and threshold semantics require labelled accuracy evaluation; they are not established in Stage 1.

Return bounded candidates sorted deterministically, with matched/different/missing attributes, actual alias, dataset/category, source record/version, and score components. Fit existing candidate cards using current properties/datasets plus additive structured explanation metadata. No fabricated confidence or identity probability.

Respect requested threshold and limit. If retrieval is incomplete, say partial; do not report a complete no-hit result. Any future decision-time validation must reject partial zero-candidate results as an explicit evaluation error. Threshold 0/broad queries require a truthful completeness policy, not unbounded table scans or silent truncation.

Provider output is evidence. Existing scenario policies and authorized reviewers decide outcomes. Neither PEP status nor a fuzzy name hit automatically establishes guilt or authorizes customer rejection.

## Migration and release strategy

Use additive, independently tracked screening metadata migrations in the shared PostgreSQL database. Qualify every source table with the screening schema and use pgx in the owner repository. Test fresh installation, upgrades and rollback against disposable PostgreSQL instances before application installation. Review least-privilege grants independently for source ingestion writer, search reader, inline decision reader and operator.

Keep existing tables, APIs, saved requests and results readable. Add optional request fields compatibly where a real deficiency exists: category selection and typed personal identifiers require coordinated DTO/proxy/OpenAPI/version tests. Existing clients must retain existing behavior; unsupported capabilities remain disabled.

Stage controlled fixtures before real data. Enable the internal provider only after API/worker/inline/UI compatibility tests pass and the requested datasets are actually published. An empty/missing internal dataset must fail, not fall back to synthetic data.

Roll back the configured provider for new requests and/or republish an allowed prior generation through an audited pointer transition. Do not rewrite prior outcomes or result evidence during rollback. Reconcile in-flight jobs and publication/rescreen receipts before retiring the old path.

Detailed acceptance gates: [verification](INTERNAL_PROVIDER_VERIFICATION.md). Work status and unresolved decisions: [progress](INTERNAL_PROVIDER_PROGRESS.md).
