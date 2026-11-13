# Internal screening provider: implementation plan

Updated: 2026-11-13 UTC for PostgreSQL main. Stage 1 deliverable. The internal provider is not implemented.

## Entry point and scope

Build a provider we operate, fed from authoritative sources, without a hosted OpenSanctions dependency or purchased aggregated datasets. Extend the existing screening service; retain its orchestration, persisted results, investigations and authorization.

The immediate requested deliverable is the repository audit and implementation plan. This pack completes that stage. None of the planned source connectors, database structures or matching capabilities below are claimed as implemented.

The existing documentation is a flat planning/runbook set at this service root. Keep this initiative in the same area under the INTERNAL_PROVIDER prefix, rather than introducing a second documentation hierarchy:

- [Repository audit](INTERNAL_PROVIDER_AUDIT.md): actual contracts, composition, persistence, workers, tests and gaps.
- [Source inventory](INTERNAL_PROVIDER_SOURCES.md): access, rights, formats, verification and Ghana strategy.
- [Architecture and data model](INTERNAL_PROVIDER_DESIGN.md): boundaries, connector specifications, normalization, publication, matching and migration.
- [Verification and operations](INTERNAL_PROVIDER_VERIFICATION.md): baseline evidence, future acceptance tests, security and performance.
- [Progress and decisions](INTERNAL_PROVIDER_PROGRESS.md): stage log, unresolved questions and release gates.
- [Baseline evidence](evidence/internal-provider/2026-11-13/report.json).

## Decisions grounded in the audit

1. Target the shared PostgreSQL database on main and the screening-owned metadata migrations. The user retired the PostgreSQL development runtime; its data and historical evidence remain separate.
2. Preserve the five-method ScreeningProvider contract. Add provider-specific ingestion components behind it.
3. Run collection as a separately deployed process in the existing screening module, with its own database credentials and resource budget. Do not add a separate application database.
4. Use a provider registry and a new explicit provider key, proposed as internal-watchlists. Keep synthetic data under an explicit development provider. Never relabel old synthetic evidence as real-source evidence.
5. Reuse existing manual, scenario, continuous and investigation flows. Do not add a parallel customer search endpoint.
6. Preserve current asynchronous screening and counterparty exclusions. Decision-time forced outcomes require a separate PostgreSQL implementation and tests; current capabilities disable them.
7. Start with controlled fixtures, then complete OFAC SDN snapshot ingestion. UN follows; EU/UK and Ghana sources follow independently.
8. Use PostgreSQL indexed retrieval as the first matching candidate. Additional search infrastructure requires measured justification.
9. Unknown source rights and undocumented APIs remain unknown. They are not permission to scrape.
10. A download probe is not a connector, a connector is not a published dataset, and a published dataset is not production acceptance.

## Actionable checklist

Status values: Not started, In progress, Implemented, Tested, Blocked, Deferred.
For planning documents, Tested means links/contracts or recorded checks were verified; it never means a runtime capability exists.
Paths marked proposed are design locations, not existing modules.

| Done | ID | Work | Dependencies | Status | Files / ownership | Coverage / outstanding issues |
| --- | --- | --- | --- | --- | --- | --- |
| [x] | AUD-01 | Inspect real provider contracts and both runtime compositions | — | Tested | ports.go; provider/http_client.go; internal/app/app.go; cmd/server; cmd/worker | Source audit; focused baseline |
| [x] | AUD-02 | Audit schemas, identity, persistence, review, queues and frontend boundaries | AUD-01 | Tested | internal/store/postgres; internal/migrations/metadata; internal/httpapi; frontend screening proxies | Existing evidence reviewed; full DB regression not rerun |
| [x] | SRC-01 | Investigate international/Ghana sources and explicit unknowns | — | Tested | INTERNAL_PROVIDER_SOURCES.md | Official documentation + five bounded XML GET probes |
| [x] | ARC-01 | Validate provider integration and deployment design | AUD-01, AUD-02 | Tested | INTERNAL_PROVIDER_DESIGN.md | Five methods mapped; hardcoded provider and decision-time integration remains unimplemented |
| [x] | TST-01 | Run focused existing baseline without application DB access | AUD-01 | Tested | evidence/internal-provider/2026-11-13 | Historical migration-branch baseline: 29 top-level / 46 results; not PostgreSQL acceptance |
| [ ] | FND-01 | Implement connector registry and typed ingestion models | ARC-01 | Not started | proposed internal/sourcedata | Optional delta interface; reject unknown capabilities |
| [ ] | DB-01 | Add append-only source/version/record/index/run schema and grants | ARC-01 | Not started | proposed internal/migrations/metadata source-data migration; PostgreSQL role grants | Disposable target/reference migration tests required |
| [ ] | ING-01 | Implement streaming staging, validation, atomic publication and diff | FND-01, DB-01 | Not started | proposed internal/sourceingest | Idempotency, removal, rollback and fencing tests |
| [ ] | MAT-01 | Implement explainable indexed matching with controlled records | DB-01 | Not started | proposed internal/internalprovider; internal/matching | Alias/identifier/attribute tests; candidate completeness handling |
| [ ] | PRV-01 | Register internal adapter with existing five-method contract | MAT-01 | Not started | internal/clients/provider; internal/app/app.go | Search, enrich, catalog, freshness and delta contract tests |
| [ ] | API-01 | Generalize fixed provider selection and capability gating | PRV-01 | Not started | manual_search.go; decision screening authoring/store; frontend proxies | Preserve historic provider keys, read-only roles and DTO compatibility |
| [ ] | INL-01 | Route decision-time matching to same screening-owned implementation | PRV-01, API-01 | Not started | proposed providerlookup; decision internal/service; screening internal/service | Read-only DB facade, trusted tenant context, unchanged evidence replay |
| [ ] | FND-02 | Verify controlled provider through API, worker, history, case UI and inline decision | ING-01, PRV-01, API-01, INL-01 | Not started | existing orchestration and regression suites | Required foundation gate before real-source activation |
| [ ] | OFAC-01 | Complete schema/rights/artifact review and official download specification | SRC-01 | In progress | INTERNAL_PROVIDER_SOURCES.md; proposed sourceconnectors/ofac | Prefix/schema-root probes passed; complete XSD/payload validation pending |
| [ ] | OFAC-02 | Build official SDN snapshot connector and realistic fixtures | FND-01, OFAC-01 | Not started | proposed internal/sourceconnectors/ofac | Names, aliases, IDs, programs; account for non-person entity types |
| [ ] | OFAC-03 | Verify import/update/removal end to end on disposable PostgreSQL | OFAC-02, FND-02 | Not started | ingestion and provider integration tests | Original IDs, field changes, no partial publication |
| [ ] | OFAC-04 | Run explicit live retrieval/full-parse verification | OFAC-03 | Not started | proposed cmd/source-verify; evidence | Ordinary CI stays offline; no app migration |
| [ ] | OPS-01 | Separate source-worker, configured schedules and administrator controls | ING-01 | Not started | proposed cmd/source-worker; Docker runtime/config/grants | Leases, jittered retries, stale state, manual run, secrets |
| [ ] | OPS-02 | Publish update events and enqueue tenant rescreen jobs | OPS-01, PRV-01 | Not started | existing dataset jobs; proposed publication outbox | Global fetch once; bounded tenant fanout; receipt replay |
| [ ] | UN-01 | Verify source terms/schema; implement independent UN snapshot connector | OFAC-03 | Not started | proposed internal/sourceconnectors/un | Official XML prefix fetched; full parsing and permission assessment pending |
| [ ] | UK-01 | Implement current UKSL connector, identifiers and sanction types | UN-01 | Not started | proposed internal/sourceconnectors/uk | Current XML prefix fetched; do not use retired OFSI list |
| [ ] | EU-01 | Establish current FSF access and permitted reuse before connector | SRC-01 | Blocked | INTERNAL_PROVIDER_SOURCES.md | Official robot guide found; deployment token/current bulk access not verified |
| [ ] | EU-02 | Implement EU connector against verified schema | EU-01, UN-01 | Not started | proposed internal/sourceconnectors/eu | No third-party mirror or copied token |
| [ ] | GH-01 | Resolve FIC/BoG/SEC/OSP access, category and correction processes | SRC-01 | Not started | source inventory and controlled import design | No bulk API/automation permission established |
| [ ] | GH-02 | Implement auditable manual notice import with source attachments | GH-01, ING-01 | Not started | proposed controlled source intake CLI / private admin surface | Two-person publication approval; allegations distinct from convictions |
| [ ] | GH-03 | Acquire ORC access and usage agreement if needed | SRC-01 | Blocked | source inventory | Paid search is documented; bulk API/auth/redistribution terms not established |
| [ ] | GH-04 | Curate public-office/PEP and SOE records with validity periods | GH-01 | Deferred | proposed curation/import tools | Parliament/SIGA are leads, not complete PEP datasets |
| [ ] | RSK-01 | Add category filtering and optional typed personal identifiers compatibly | API-01, MAT-01 | Not started | request DTOs, authoring, proxy validation and matching | Current SearchQuery has no passport/national-ID input |
| [ ] | RES-01 | Implement reversible cross-source identity links | ING-01 | Deferred | proposed identity-link tables/events | Start source-separated; never merge by fuzzy name |
| [ ] | HARD-01 | Execute malicious-file, authorization, process-death and concurrency gates | OFAC-03, OPS-02, INL-01 | Not started | verification matrix | Rights, XML limits, stale workers, denied tenants |
| [ ] | PERF-01 | Benchmark real-size datasets and ingest/search interference | HARD-01 | Not started | disposable benchmark fixtures and EXPLAIN evidence | 500 TPS / 50 ms are targets, not current guarantees |
| [ ] | REL-01 | Controlled release, rollback and operational acceptance | HARD-01, PERF-01 | Not started | release runbook and runtime profile | No automatic activation by adding a connector |

## Stage exit criteria

- Stage 1: this linked audit/design/source inventory/checklist with verified baseline and explicit unknowns.
- Stage 2: controlled records work through the existing API and inline decision path, with scoped authorization, compatible results and rollback-tested migrations.
- Stage 3: OFAC full retrieval, parsing, repeatable import, modifications, removals, versioned search evidence and negative-path tests pass on isolated PostgreSQL.
- Stages 4–6: each additional source independently passes access, parsing, integrity and classification gates. Do not label all Ghana data as sanctions.
- Stage 7: measured throughput/latency, recovery, operational procedures and rights review support a limited release. Production acceptance is separate from fixture or live-download verification.

Next implementation stage: FND-01 and DB-01, followed by controlled provider integration. No application migration or provider switch is part of Stage 1.
