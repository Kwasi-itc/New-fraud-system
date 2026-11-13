# Internal provider: progress and outstanding decisions

Updated: 2026-11-13 UTC.

## Current boundary

Stage 1 — repository audit and implementation plan — is complete.

The internal provider, source worker, source-data schema and source connectors are **not implemented**. This plan now targets PostgreSQL main. Screening UI/API compatibility changes are tracked separately in POSTGRESQL_PORT_STATUS.md; they do not add an authoritative data source. The original MySQL audit and source probes remain historical evidence.

## Progress log

| Date | Work | Evidence | Result |
| --- | --- | --- | --- |
| 2026-11-13 | Read attached brief; inspect actual provider/service/worker/store/runtime paths before changes | [Audit](INTERNAL_PROVIDER_AUDIT.md), HEAD 224731f | Native MySQL composition and synthetic inline injection verified |
| 2026-11-13 | Inspect provider DTOs, category/identifier gaps, authoring restrictions and evidence replay | Existing source paths in audit | A provider-URL-only switch is insufficient; five-method port is reusable |
| 2026-11-13 | Inspect documentation conventions and migration owner | Service-root planning documents; mysqlscreening + mysqlschema | Added this initiative to the existing flat documentation area |
| 2026-11-13 | Run focused existing Go tests offline/read-only | [Baseline report](evidence/internal-provider/2026-11-13/report.json) | 29 top-level / 46 test-subtest passes; no failures; no DB tests |
| 2026-11-13 | Research international and Ghana official sources | [Inventory](INTERNAL_PROVIDER_SOURCES.md) | Access, rights, categories and unknowns recorded individually |
| 2026-11-13 | Probe five official XML/schema endpoints using bounded GET | [Probe metadata](evidence/internal-provider/2026-11-13/source-probes.json) | HTTP 206 + expected roots; 4 KiB each; not full parsing |
| 2026-11-13 | Validate architecture against actual entry points | [Design](INTERNAL_PROVIDER_DESIGN.md) | Shared provider/matcher and screening-owned inline read facade; no new customer search endpoint |
| 2026-11-13 | Produce staged tasks and acceptance matrix | [Plan](INTERNAL_PROVIDER_PLAN.md), [verification](INTERNAL_PROVIDER_VERIFICATION.md) | Foundation precedes OFAC; no source marked implemented |

The log above records the original migration-branch audit. For the current PostgreSQL port, see [verification status](POSTGRESQL_PORT_STATUS.md). The user selected the existing PostgreSQL database, with MySQL data preserved separately. No real-source ingestion was activated.

## Architectural decisions and evidence

PostgreSQL transition completed on 2026-11-13: preserved the migration branch,
dirty work and MySQL data; retained the screening editor and required PostgreSQL
API/worker dependencies; started the gateway on port 8088. Verification passed
756 Go test/subtest results, 61 browser contracts and live Swagger execution for
the four services with documentation routes. See
[PostgreSQL port status](POSTGRESQL_PORT_STATUS.md). No source provider was activated.

- Shared PostgreSQL on main is the active target, per the user's latest direction. Preserve the retired MySQL containers/volumes and do not migrate NovaPay demo data.
- Collection is a separate process under the current screening module, not another application database or screening orchestrator.
- Provider identity must remain explicit. Do not relabel historical synthetic evidence as real-source screening.
- Preserve the existing provider contract. Add source-data structures because investigation result tables are not a source catalog.
- Initial matching uses PostgreSQL indexes and a common Go engine. An additional search service is not justified yet.
- A future shared DB read facade can serve decision-time evaluation; that path is not currently wired on PostgreSQL. It without creating a parallel screening endpoint or holding network-dependent transactions.
- Source identities remain separate. Reversible cross-source links are a later feature, not fuzzy-name merges.
- Snapshot reconciliation is the first OFAC update strategy; deltas need independent continuity/recovery evidence.
- Keep LastSuccessfulCheck distinct from content publication date. HEAD may be unsupported even when GET succeeds.

## Outstanding questions and gates

| ID | Question / limitation | Evidence needed | Effect |
| --- | --- | --- | --- |
| DEC-01 | Exact internal source schema and names | Disposable migration implementation and ownership review | Design tables are proposed, not installed |
| DEC-02 | Scoring/threshold semantics and candidate retrieval recall | Labelled matching corpus, algorithm contract and benchmarks | Do not claim a name similarity is identity probability or complete recall |
| DEC-03 | Private source visibility in every provider path | Trusted context contract and cross-tenant negative tests | SearchRequest currently has no tenant field; provider_config cannot be authority |
| DEC-04 | Personal identifier/category inputs | Compatible versioned DTO/proxy/authoring tests | Existing request shape cannot already accept a passport collection |
| DEC-05 | OFAC schema coverage | Full XSD and payload analysis, source-type mapping, fixture/full-file validation | Prefix/root probe is insufficient for source acceptance |
| DEC-06 | UN reuse conditions and full artifact behavior | Source-specific rights assessment and complete validation | Do not assume public XML permits all redistribution |
| DEC-07 | EU current FSF endpoint/token/terms | Approved deployment access and current schema | EU connector access work is blocked; no copied token |
| DEC-08 | Ghana agency automation/redistribution | Source-specific permission/access documentation and correction process | No fake API/scraper; use controlled imports where appropriate |
| DEC-09 | ORC delivery/auth/entitlements | Paid-access agreement and supported format | No free public bulk corporate/beneficial-owner API assumed |
| DEC-10 | Ghana PEP completeness, history and relationships | Approved source coverage policy and curator workflow | Parliament/SIGA leads are not a complete PEP service |
| DEC-11 | Required-source staleness and release policy | Institution requirements and explicit settings | Never treat a required unavailable source as a clean no-hit |
| DEC-12 | Source and search resource budgets | Real-size shared-DB benchmark, query plans and process-recovery tests | No 500 TPS / 50 ms or production-readiness claim |
| DEC-13 | Evidence retention/enrichment of old generations | Approved retention rules and generation-scoped read behavior | Later source updates cannot rewrite old candidate evidence |
| DEC-14 | Old provider identity / exclusion transition | Audited migration and rollback tests | Do not transfer synthetic exclusions into real-source results silently |

These gates do not require blanket permission to proceed with controlled foundation work. They prevent unsupported source activation or production claims.

## Next work

Follow FND-01 and DB-01 from the [task checklist](INTERNAL_PROVIDER_PLAN.md), then MAT-01/PRV-01/API-01/INL-01 and the controlled end-to-end gate. Complete the OFAC snapshot connector only after that foundation works.

No application migration is authorized by a successful unit test or source probe. The brief requires migration verification against disposable databases first; that order is retained.
