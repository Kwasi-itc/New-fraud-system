# Coverage and documentation validation

Date: 2026-10-22. Baseline commit `a2d241a53e082b1ff654634c41e83ecfb7328941` plus existing working-tree modifications and untracked source. This report distinguishes discovery, manually traced behavior and future runtime verification. It does not certify every SQL statement or live deployment.

## Discovery coverage

`rg --files --hidden` enumerated repository text with explicit exclusions for `.git`, dependency/build caches, `.local` secrets, virtual environments, Python cache and binary GeoIP data. The documented scan includes 1,668 files; root private `.env` contents were not read or copied. Named `.env.example`, manifests/lockfiles, Dockerfiles, Makefiles, SQL, Go, Python, shell/PowerShell, frontend code, HTTP definitions and historical docs were included. Generated run data was not interpreted as current performance validation.

35 discovery families searched drivers, connection settings, schema/DDL, SQL functions/operators/types, mutations/RETURNING/conflicts, queue/locks/leases, tenant/identity contracts, arrays/JSON/time, indexing/catalogs/errors and operational/test setup. 903 files have entries in the source evidence catalog. A separate callsite search found **1,310** query/execute/transaction calls across application and test/tool source, preventing reliance on the word `postgres` alone. False-positive patterns are explicitly distinguished in document 02.

| Component | Text files in discovery manifest | Database callsites (including tests) | Review coverage |
|---|---:|---:|---|
| Auth | 43 | 173 | Identity schema/upgrades, store/session/invitation locks, owner authorization, delivery, provider config, preflight CLI |
| Cases | 153 | 320 | Schema/tenant constraints, transaction/intake/membership/capacity, shared review, manual links, explorer, analytics, reports/evidence and maintenance/delivery |
| Data-model | 145 | 115 | Metadata/runtime DDL, contract/display triggers and stores, transaction permissions, index/catalog/worker/migrator paths |
| Decision | 344 | 256 | Persistence/job transaction, tenant read modes/aggregation, list/cursor/query filters, workflow/scenario/screening authoring and projection |
| Ingestion | 97 | 67 | Single/batch writer/conflict/idempotency, audit/outbox, model contracts/read pools, upload/deferred paths |
| Screening | 138 | 288 | Provider dispatch/claim/replacement, recovery, case delivery/projection, continuous/manual/exclusion/review paths |
| Investigator shared | 8 | 16 | Identity function invocation, operation replay, case/resource locks, atomic owner/case review |
| Stress tooling | 449 | 75 | Optional psycopg writes, psql observation/pilot/queue/plan paths; historical generated results not rerun |
| Integration tests | 39 | 0 in Go/Python call pattern | HTTP fixtures/contracts; not treated as no database dependency |
| Frontend | 186 | Not part of backend callsite scan | Manifest/lockfile and API/UI fixture discovery, indirect service contracts; no runtime DB driver found |
| Scripts/UI tooling/docker/root/plans | See manifest | Covered by SQL/operational search | Compose including scale/local/provider variants, Docker/Make/env examples, consolidation and historical plans |

All 57 application up-migrations and 57 matching down migrations, three embedded identity schema files, and two Docker SQL files are captured: **119 SQL source files**. The object index covers all **60 up/embedded schema files**. Current source migration maxima are data-model 8, ingestion 2, decision 23, screening 10, cases 14, identity 3. They are not reported as live applied versions.

## Cross-checks performed

- Inventory includes every top-level backend service/package/tool directory and frontend/docker/root deployment area.
- Driver/manifests/lockfile searches were supplemented by SQL feature searches and an independent DB-call search; every component with calls maps to findings/service analysis/tasks.
- Every PG-01–PG-35 family has an entry in 02, compatibility treatment in 05 and coverage in the M tasks. M implementation tasks remain NOT_STARTED.
- Table columns/defaults and migration alterations/FKs/checks/indexes/routines are retained with exact source locations. Dynamic DDL and dependency/provider-owned schema gaps are separate.
- Critical source paths were traced manually. Mechanical matches do not claim independent semantic review of every test line, generated report or CRUD statement.
- Searches for ORM, COPY/CopyFrom, LISTEN/NOTIFY, extensions, specialized indexes, RLS and CI/provisioning were checked against discovered code. No active application dependency was inferred from framework `GIN_MODE`, Go UUID construction, comments or historical plans.
- Local documentation/source link targets and line bounds are checked mechanically. One invalid line reference found during the first pass was corrected; pending cross-document links were resolved when the final documents were added.
- Source hashes in 13 support a final unchanged-source check. Verification totals are recorded below after the final pass.

## Final documentation verification result

The final mechanical pass checked **17 Markdown documents**, **11,353 local file/line links**, and **1,668 source SHA-256 hashes**. Result: zero missing link targets, zero out-of-bounds line references, and zero source hash changes from the captured manifest. PG-01 through PG-35 are all present once in the consolidated inventory; M-01 through M-16 are all present with NOT_STARTED status. The repository already contained substantial changes before this task; they were preserved. This work created/modified only documentation under `mysql_migration/`.

Link/line validation proves navigability, not that every interpretation is correct. Source tracing and the explicit uncertainty register remain necessary. Application tests and performance tests were not run.

## Remaining boundaries

No application test suite, benchmark, SQL migration or provider/container operation was executed. No live schema/data/permissions/pool settings, external CI or ignored secrets were inspected. River and Keycloak internal DDL is dependency-owned and not vendored; source discovery cannot establish installed versions or all runtime-created objects. MySQL suggestions are designs requiring M-01–M-16 validation. No unsupported claim of production readiness or comparative performance is made.

## Reproducing and maintaining the audit

Use `rg --files --hidden` with the exclusions above; scan driver/config patterns and SQL/type/lock/catalog/error patterns; separately search `.Query(`, `.QueryRow(`, `.Exec(`, `.BeginTx(`, `.SendBatch(` and Python database execute/connect calls. Inspect surrounding methods and caller/transaction paths, not only matches. Re-enumerate both tracked and untracked migrations. Compare SHA-256 with 13 to identify changed evidence, then update line links, finding interpretation and tests together.

The evidence appendices are snapshots and should be refreshed when code changes. Do not treat a later hash mismatch as permission to overwrite user code. Add new finding IDs instead of renumbering existing ones; record retired usages explicitly. Preserve historical planning documents outside this workspace.
