# Internal provider: PostgreSQL repository audit

Updated 2026-11-13 UTC for main (base a2d241a) with the retained screening UI/API changes. The earlier migration-branch baseline at 224731f is preserved under evidence/internal-provider/2026-11-13; it is not PostgreSQL acceptance evidence.

## Runtime and ownership

The active application target is the shared PostgreSQL database fraud. Service-owned schemas and independently tracked migrations remain in place. The optional root docker-compose.gateway.yml exposes one public origin while the original docker-compose.yml supplies PostgreSQL APIs, workers and migration ordering. MySQL containers and volumes are retained separately, with no NovaPay data import.

The screening service owns screening results, candidates, review, exclusions and rescreen orchestration. Decision owns scenarios/configuration and dispatch. Case manager owns investigation grouping and case history. Auth owns current user/session/membership authority. Sharing a database does not grant cross-tenant access or permission to bypass another owner's mutations.

## Findings

| Area | Current PostgreSQL implementation | Remaining gap |
| --- | --- | --- |
| Provider port | Search, Enrich, GetCatalog, GetFreshness, GetDatasetDelta in [ports.go](internal/ports/ports.go) | A source-data provider must implement all five meaningfully |
| Composition | [app.go](internal/app/app.go) and [worker](cmd/worker/main.go) construct the HTTP provider client | No internal authoritative-source adapter is registered |
| HTTP configuration | Default SCREENING_PROVIDER_URL, provider URL map, and dedicated OpenSanctions configuration | A configured URL is not evidence of a working or licensed data source |
| Manual search | Capability-gated requests, saved history/report and case intake | Current request construction is specific to opensanctions; registry/category inputs require coordinated changes |
| Scenario authoring | Authenticated authoring boundary, mapped fields, preprocessing and capability gating | Authoring currently preserves the opensanctions provider/config contract |
| Decision execution | Saves decisions and queues screening dispatch; workers deliver and persist screening evidence | Decision-time forced outcomes are not wired on PostgreSQL; inline_supported remains false |
| Investigation | Persisted candidates, current actor/session/inbox checks, review and scoped exclusion history | Real-source identifiers/generations must be added compatibly |
| Continuous screening | Monitored objects, dataset checks, durable jobs and rescreen execution | Source collection, normalization and atomic catalog publication do not exist |
| Persistence | [PostgreSQL stores](internal/store/postgres) and [metadata migrations](internal/migrations/metadata) | Investigation tables are not an authoritative source catalog |
| New source ingestion | No OFAC/UN/EU/UK/Ghana importer in this port | Connector registry, staging/versioning, publication, rights and live verification remain planned |
| Verification | Disposable PostgreSQL API/worker regression and frontend/gateway checks tracked separately | No real-source acceptance or production throughput claim |

## Contracts and evidence

[model.go](internal/domain/screening/model.go) defines names, entity type, birth date, nationality, country, registration number and address inputs. There is no typed passport/national-ID collection or first-class sanctions/PEP/adverse-media category filter. SearchRequest carries provider, object/decision/scenario/config references, queries, limits and idempotency/counterparty metadata. Trusted request context supplies tenant and actor authority; opaque provider_config must not become authority.

ProviderResult contains raw provider evidence, partial/completeness state and candidates. Scores are similarity measures, not probabilities of identity. Existing result storage supports investigations; it must not be reused as a mutable source-record catalog.

The HTTP client's catalog/freshness/delta wrappers are extension points, but their JSON payload contracts need documentation for an internal provider. The dedicated OpenSanctions delta logic compares catalog versions; that is not an importer of source additions, modifications and removals.

Manual input validation, decision authoring, private frontend proxies and capabilities all participate in request construction. Changing only a provider URL will not add category filters, personal identifiers or decision-time execution. Unsupported fields must remain disabled or return explicit errors.

## PostgreSQL changes retained for the UI

The backport retains scenario screening creation/editing and rules-list entries, manual screening/history/report pages, candidate review/exclusions and decision/case evidence. Server-side proxies preserve sessions, current tenant, actor permissions and separate service credentials. Private API tokens are not compiled into browser bundles.

New owner migrations retained with those flows are core 000023_screening_authoring_audit; screening 000008_manual_history, 000009_object_exclusions and 000010_exclusion_history_indexes; and case_manager 000014_manual_screening. They extend existing owner tables; none creates a source-data provider. Root startup preserves the required order: data model, ingestion, decision, screening, case; auth depends on data-model metadata. Test fixtures also install the real identity owner definitions for case reads.

## Integration approach

Keep collection as a separately deployed executable inside this module, with its own credentials, bounded pool and schedules. Add PostgreSQL source/version/record/index/publication structures under the screening schema. Implement an explicit internal-watchlists provider key behind the existing port. Start with controlled records before official OFAC snapshots, then approve each additional source independently.

Use indexed exact/identifier retrieval first and measure pg_trgm or token projections for fuzzy recall before adding another search system. Tenant-private catalogs require trusted tenant context in search, enrichment, catalog, explanation and freshness paths.

Future decision-time screening must call the same screening-owned matcher through a documented read contract, apply current exclusions, capture immutable request-bound evidence and handle timeouts/partial results explicitly. It is a separate implementation gate, not a capability enabled by this branch switch.

No real list is activated by these changes. See [source inventory](INTERNAL_PROVIDER_SOURCES.md), [design](INTERNAL_PROVIDER_DESIGN.md), [plan](INTERNAL_PROVIDER_PLAN.md) and [verification](INTERNAL_PROVIDER_VERIFICATION.md).
