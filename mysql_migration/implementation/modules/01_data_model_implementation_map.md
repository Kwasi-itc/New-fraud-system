# Data-model implementation coverage map

Status: IN_PROGRESS. This is the execution boundary for DM-01, not a completed SC-01 manifest. Finish this module before starting another module; shared dependencies are included only where needed to finish it. No target adapter is activated by this document.

Source paths below are relative to `backend/data-model-service/`. Column definitions remain authoritative in the [SQL catalog](../../12_schema_source_catalog.md). Target names and type rules come from [SC-01](../04_schema_and_data_access_design.md). Every row below needs implementation and verification before DM-01 can be checked.

## Repository ownership and target work

All metadata remains in one shared logical application database. Candidate `dm_` names denote ownership, not separate databases. Preserve public UUIDs and logical names; authorize tenant identity before resolving a physical name.

| Source under internal/store/postgres | Owned objects / behavior | Required target work |
|---|---|---|
| tenant_repository.go | core.tenants; create/get/list/status | data_model_tenants; preserve external-key and schema-name uniqueness; map legacy schema identity to registry, never CREATE DATABASE per tenant |
| table_repository.go | core.model_tables; names, aliases, semantic type, caption, archive | data_model_tables; tenant/name exact uniqueness; publication and display version mutation boundary |
| field_repository.go | core.model_fields; type, nullable, enum, unique, archive, alias | data_model_fields; tenant-consistent table FK; preserve archived metadata; physical field registry |
| field_enum_value_repository.go | core.field_enum_values; value/label uniqueness and sort order | data_model_field_enum_values; table-scoped publication snapshot; preserve sort_order then UUID contract order |
| link_repository.go | core.model_links; parent/child table/field references | data_model_links; composite tenant relationships; cleanup every dependent display and pivot before deletion |
| pivot_repository.go | core.model_pivots; ordered path_link_ids | data_model_pivots plus ordered path rows; retain nullable terminal field and exact traversal order |
| table_options_repository.go | core.table_options; displayed_fields, field_order | data_model_table_options plus ordered field rows; coordinate display JSON and version validation |
| navigation_option_repository.go | core.navigation_options | data_model_navigation_options; tenant-consistent source/target/ordering references and index requirements |
| index_job_repository.go | core.index_jobs; dedupe, attempts, schedule, state | data_model_index_jobs; nonempty exact dedupe key, fenced attempts, atomic history and durable delivery |
| schema_change_repository.go | core.schema_change_log | data_model_schema_change_log; JSON details and UTC times; intent/outcome distinction, no applied history before verified DDL |
| tenant_schema_migration_repository.go | core.tenant_schema_migrations | data_model_tenant_schema_migrations; migration identity/checksum/recovery ordering; retain assembled revision semantics |
| assembled_data_model_repository.go | Read aggregate across metadata repositories | Bounded snapshot reads and batched children; avoid carrying current per-table/per-enum N+1 pattern into target |
| transaction_manager.go, executor.go, db.go | pgx session and typed MutationStore ownership | Staged tenant-bound MySQL transaction adapter with mandatory authorization callback, atomic queue submission, rollback/ambiguous-commit propagation and explicit physical-operation rejection; concrete identity SQL, complete owner protocols and runtime adoption remain open |

`internal/display/store.go` is an additional SQL adapter, not covered by converting the repository directory. Its load/Get/Save/Read and recordQuery paths require authorized registry resolution, optimistic display-version updates, bounded projections, relation reads and atomic cleanup. The `internal/access` and HTTP authorization boundaries must remain in force for direct record reads as well as metadata mutations.

DM-01.1c implements the staged table, field and enum repository rows above,
including all methods of their existing ports. The [repository contract](01_data_model_repository_contract.md)
records transaction/tenant binding, limits, missing-row behavior and eight verified
real-engine scenarios. This does not complete the publication/display/physical
requirements in those rows or activate MySQL in the application.

DM-01.1d additionally implements all link and navigation port methods, with five
verified real-engine scenarios. Navigation resolves related names in one scoped
joined statement. DM-01.1e implements pivot persistence with ordered child rows,
bounded batch writes and single-statement reads; four real-engine scenarios passed.
Owner relationship validation, display cleanup and index scheduling remain open.

History and revision adapters are now implemented on the same transaction-bound
factory, including bounded compatibility lists and tenant/stream-scoped keyset
pages. DM-01.1f passed seven real-engine event scenarios, including final
byte-boundary verification. Runtime audit integration and HTTP cursor adoption
remain open.

## Migration coverage (up and down)

DM-01.1g implements tenant-bound persistence and a separately constructed read-only
platform catalogue. Seven real-engine scenarios passed. PostgreSQL's global tenant
list must be routed through the authorized catalogue at runtime integration; the
scoped factory cannot enumerate other tenants. Registration scope allocation is
implemented under DM-01.2n; durable registration recovery, provisioning readiness
and runtime authorization remain open.

Source directory: [internal/migrations/metadata](../../../backend/data-model-service/internal/migrations/metadata). Build a new MySQL lineage; do not execute these PostgreSQL files against MySQL.

| Version | Existing objects / behavior | Target obligation and reversal constraint |
|---|---|---|
| 000001 | tenants, model_tables, model_fields, model_links, model_pivots, table_options, schema_change_log, tenant_schema_migrations, index_jobs | Map every column/default/key/FK; UUID arrays become ordered children, JSONB becomes JSON. Down removes foundational objects and is destructive, not a live rollback plan. |
| 000002 | field_enum_values and field/sort/created index | Preserve separate value and label uniqueness scoped to field, tenant consistency and deterministic contract ordering; down loses enum data. |
| 000003 | index job table reference, operation/error/attempt/schedule/dedupe columns; scheduling/history indexes and partial nonempty dedupe uniqueness | Translate conditional uniqueness without truncation or digest-only equality. Preserve pending/retry semantics; downgrade loses delivery evidence. |
| 000004 | navigation_options and tenant/source/time index | Preserve relationships and navigation index scheduling; down removes configuration. |
| 000005 | ingestion_contracts, publish/refresh functions, three publication triggers, two v1 views, backfill and function grants | Owner publication transaction, immutable identity, fresh UUID token only for changed payload, restricted consumer view, authorized registry and readiness gating. Down drops publication interfaces; consumers must already be stopped or compatible. |
| 000006 | field_display JSON, display_version, validation/touch/field/link cleanup functions and triggers | Preserve maximum 12 displayed fields, distinct active same-tenant references, caption/archive invalidation and cleanup. Down drops these guarantees and version state. |
| 000007 | display_columns JSON, replacement validation and cross-table field/link cleanup | Preserve direct/relation columns, parent uniqueness/type checks and cleanup of every affected same-tenant table. Down restores older direct-field functions; it is not merely dropping one JSON column. |
| 000008 | field alias | Preserve presentation alias independently of ingestion payload. Down loses alias data. |

The [metadata column manifest](01_data_model_metadata_schema.md) now maps every source metadata column, array replacement, structural relationship and key domain to target definitions. The private exact-key dictionary and structural schema passed their real MySQL integration scenarios. SC-01 still needs physical data-column/registry mappings, source preflight results, complete owner enforcement and production grant/migration integration. This map does not settle currently unbounded key domains by inventing VARCHAR limits.

## Physical DDL and inspection

Source: `internal/tenantdb/postgres/{schema_manager,indexing,index_introspection,identifier}.go`; interface: `internal/ports/schema_manager.go`.

| Operation | Current behavior | Required target behavior |
|---|---|---|
| ProvisionTenantSchema | CREATE SCHEMA | Allocate authorized registry namespace in shared DB, record readiness and grants |
| CreateTable / DropTable | Qualified logical tenant/table name; IF NOT EXISTS / deletion | Fixed `tenant_<tenant label>_<table label>_<full table UUID>` registry name; inspect full shape before accepting existing object; destructive retirement after consumer barrier |
| AddField / DropField | Type-specific column DDL | Registered physical columns; six type mappings, data preflight, idempotent intent and reconciliation |
| ArchiveField | Rename to old_name plus short SHA1 suffix | Keep API archive visibility and retained data; registry distinguishes retired/active identity without suffix collisions |
| CreateUniqueIndex / DropUniqueIndex | Generated name over table/columns | Exact uniqueness for the actual value domain; duplicate-data preflight, no prefix/digest-only substitute |
| CreateManagedIndex | Search/navigation indexes; IF NOT EXISTS | Recorded ordered parts/type/uniqueness and published readiness only after shape verification |
| GetManagedIndexState | to_regclass existence test | Compare full definition; same-name wrong index must fail/reconcile, never count as applied |

Physical base columns are id, object_id, updated_at, valid_from and valid_until. Preserve id and object_id semantics; UTC DATETIME(6) and explicit open-ended validity replace PostgreSQL infinity. Boolean/integer/float/string/timestamp/IP fields need source-compatible read/write codecs; IP needs family/address/prefix, not a lossy text-only approximation. Existing AddField does not itself enforce every metadata nullable/enum rule: record the intended enforcement site explicitly.

## Entry points and dependent contracts

| Entry | Path to cover | Acceptance evidence needed |
|---|---|---|
| Server | cmd/server -> internal/app -> HTTP handlers -> tenant/table/field/enum/link/pivot/options/navigation/index services | All endpoints use selected owner adapter; authorization, rollback, no success before publication |
| Display | HTTP display handlers -> internal/display/store.go | Version conflict, tenant rejection, archive/delete relation cleanup, bounded records and query plans |
| Import/export | internal/service/portable_data_model_service.go -> owner services | Preserve logical names/aliases/relationships; interrupted import exposes explicit recoverable outcome |
| Published aggregate | internal/service/data_model_read_service.go -> assembled repository and migration history | Keep revision token semantics separate from ingestion-contract UUID token |
| Worker | cmd/worker -> internal/worker -> index repository/schema manager/history | Fenced ownership, exact shape after uncertain DDL, atomic outcome and retry; stale worker cannot publish |
| Reconcile | cmd/reconcile -> internal/reconcile | Drift reporting, repair intent, durable scheduling and legacy pending-job recovery |
| Migrate | cmd/migrate -> service migrations | Independent checksum lineage, dependency ordering, dirty state and restart recovery |
| Ingestion consumers | core.ingestion_contracts_v1 and ingestion_tenants_v1 | Read-only grants, unchanged payload contract, fresh reads after version invalidation; barrier before physical retirement |
| Decision/explorer consumers | Published model and record/aggregate contracts identified in audit PG-20/21/32/35 | Registry compatibility and schema-version behavior; cannot enable target owner while readers still assume tenant PostgreSQL schemas |

## Execution gates

DM-01.1l provides staged stable physical table/field-component mappings and atomic
intent/barrier preparation with a shared-lock writer gate. Subsequent DM-01.2k/3c
work implements execution, grant/shape verification and atomic publication for
supported creation/native-field paths. This does not complete DM-MAP-03:
remaining operation coverage, installation orchestration, retirement and runtime
consumer adoption remain open. See the
[physical registry contract](01_data_model_physical_registry_contract.md).

DM-01.3d.1 now provides staged MySQL settings snapshots, strict validation,
optimistic save/audit, reverse dependencies, parent-caption invalidation and
field/link cleanup. All owner entry points, table lifecycle, physical record
readers, backfill/grants and API/runtime acceptance remain required. See the
[display contract](01_data_model_display_contract.md); DM-MAP-05 stays unchecked.

- [ ] DM-MAP-01: Complete SC-01 column/key manifest and exact-key decisions; review ownership and physical mappings.
- [ ] DM-MAP-02: Implement all metadata repositories and their shared-schema FK/order tests.
- [ ] DM-MAP-03: Implement durable DDL intent, reader barrier, fencing, shape verification, grant readiness and recovery.
- [ ] DM-MAP-04: Wire ingestion payload construction into atomic authoritative publication; version changes only on semantic change.
- [ ] DM-MAP-05: Replace all display validation/invalidation/cleanup triggers and test every mutation entry point.
- [ ] DM-MAP-06: Port worker, reconcile, migration command and legacy pending-delivery recovery.
- [ ] DM-MAP-07: Validate API/import/export/read contracts, consumer permissions and cross-tenant rejection on MySQL.
- [ ] DM-MAP-08: Run real-engine failure/concurrency tests and representative wide-model/many-table/read-write benchmarks with agreed acceptance thresholds.

All gates are required. Portable payload construction/version planning and staged MySQL publication/operation-receipt adapters have been tested, including lost-commit-acknowledgement recovery. Production schema, owner authorization/grants, DDL readiness and runtime wiring remain pending. These tests do not complete DM-MAP-04 or the module.

DM-01.1h implements staged ordered table-options persistence, including exact-table
locking, active-field validation, atomic normalized-list replacement and version
increments. Six MySQL scenarios passed with the selected regression, race detection
and module-wide vet. Display JSON is retained, not revalidated: full display owner
validation, archive/link cleanup and runtime activation remain open.

DM-01.1i implements all staged index-job repository methods on the authorized
tenant/caller transaction. Nine real-MySQL scenarios passed with the full selected
regression, race detection and module-wide vet. A nonunique generated hash index
supports tenant-bound full-byte lookup without private dictionary read grants;
exact uniqueness still comes from existing guards. Worker completion must continue
through the fenced execution store, not the unfenced legacy repository state ports.
Durable delivery, DDL readiness, owner retry integration and representative plans
remain open.

DM-01.1j implements assembled-model reads with a fixed nine-query snapshot, shared
row/text bounds and map-based reference assembly. A snapshot-aware owner service
reads model, tenant and revision inputs atomically while retaining revision-token
compatibility. Runtime selection, authorization and consumer acceptance remain open.

DM-01.2j stages delivery persistence and lease-bound worker metadata outcomes.
Job/history/delivery atomicity, retention/dedupe, expiry and commit-ambiguity tests
are separate from executable polling, physical DDL fencing, operational redrive,
production migration/grants and River-drain acceptance. See the
[delivery contract](01_data_model_delivery_contract.md).

DM-01.2j.4 adds the staged driver-free maintenance scheduler: independent expiry
and retention cursors, alternating bounded pages, paced cycles and explicit
partial/ambiguous failure propagation. The scheduler and its adapter are tested
together against locked prefixes and restart. Executable integration and source
retry/rescue policy compatibility remain unchecked under DM-01.2j.

## Plan audit (2026-10-28)

This source review compared the module checklist and owner audit with
`internal/httpapi/router.go`, `internal/access/platform.go`, the owner services,
the repository/display/delivery contracts and the validation/operations plans.
It is not a test run or an exhaustive proof of implementation correctness. No
completion checkbox changes follow from it. The overall plan covers the major
areas; the following details require explicit acceptance within existing tasks.

| Finding | Existing task | Required closure evidence |
|---|---|---|
| The owner audit did not explicitly enumerate registration/provisioning, options replacement, enum/pivot/navigation deletion, job retry and administrative reconcile. These routes exist today. | DM-01.2, DM-01.3b/d/e | Route-to-owner-to-storage evidence for every route, including authorization, dependency cleanup, audit/revision/queue effects and operation-specific replay. The main owner matrix now includes these rows. |
| Canonical commands and stored responses do not define HTTP retry behavior by themselves; current handlers do not bind the staged publication protocol. | DM-01.3b.2b.2, DM-01.3e | Define request-key transport/lifetime, stable identity allocation, trusted actor and version preconditions, original response serialization and conflict/lock-busy/ambiguous-outcome status mapping. Verify client retry after a lost HTTP response. Keep existing synchronous success semantics; do not silently introduce pending success. |
| Runtime authorization is a cross-owner prerequisite, not satisfied by callback fixtures. `access.Check` uses pgx, PostgreSQL ownership SQL and auth-owned `identity.authorize_data_model_v1`. | DM-01.3e; AU-01.2/3 | Agree and implement the auth-owned MySQL contract; prove revoked sessions/permissions, resource-to-tenant resolution and current authorization on replay. Keep this dependency explicit despite the data-model-only implementation scope. |
| Physical record display reads and external published-contract consumers are distinct from metadata/settings reads. | DM-01.3d/e | Verify current-record/relation queries, logical-to-physical mapping, all six codecs, archive behavior and version/barrier semantics with restricted reader grants. Validate ingestion and decision/explorer consumers before activation. |
| A disposable SQL fixture is not an API/frontend development stack. App/router still select PostgreSQL and River. | DM-01.3e; OP-01.2 | Provide isolated application startup using the shared logical MySQL database, ordered migrations/reviewed grants, real authorization and worker dependencies; prove API/frontend requests actually use MySQL. Keep production PostgreSQL/River unchanged until the system activation gates pass. |
| The default MySQL Compose test command uses a 120-second package timeout, while the recorded metadata regression took 170.218 seconds with a larger timeout. | DM-01.4 | Align the documented/default test runner with the verified suite duration and required package coverage, then execute it without skips. Do not interpret this configuration mismatch as a newly observed test failure. |
| Historical preparation entries contain limits that later work superseded; the coverage map also retained stale registration/physical-execution wording. | DM-01.1/2/3 | Use the current owner matrix plus evidence for current scope. Preserve historical evidence; reconcile current summaries without treating later partial implementations as completed parents. Registration and physical-execution summaries above have been corrected. |
| Load, operational supervision and data migration are planned globally but still need concrete data-model acceptance evidence. | DM-01.1/4; Q-01, PF-01, DT-01 | Establish agreed workloads/thresholds, query plans and database-wide pool budgets; verify worker shutdown/lease loss, metrics, source-data preflight/backfill, River drain/translation and timed recovery/rollback. Fixed-query-count tests alone do not close these gates. |

Before completing DM-01.3e, reconcile all routes in `NewRouter` with this map,
including read/list/history routes and their pagination/limit behavior. Also
cover non-HTTP owners: import/export, worker, reconcile and migration commands.
Keep metadata revision, ingestion-contract token and display version distinct in
API and consumer tests. A working single-table API journey is an intermediate
milestone, not completion of this route and consumer inventory.
