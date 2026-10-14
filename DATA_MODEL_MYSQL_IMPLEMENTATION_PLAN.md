# Data-model service: PostgreSQL to MySQL implementation plan

Status: implementation reverted at the user's request; retained as a historical plan. No MySQL implementation remains from this work.

Date: 2026-10-14.

Priority: convert and validate the data-model service before continuing unrelated service migrations.

Revert update (2026-10-14): this priority and the phase checklists below are historical proposals. The user requested reversal of the implementation. The isolated MySQL prototype and its added test tooling were removed; ingestion preparation was restored to the pre-migration Git baseline. Historical audit findings remain useful, but references to prototype behavior describe removed code, not current implementation. No runtime database transition was performed.

Final planning review (2026-10-14): audit requirements are incorporated throughout this plan, with detailed findings in sections 14 and 15 and the consolidated outcome in section 16. They are implementation scope and exit gates, not completed work. The reviews did not run application migrations or change runtime configuration.

## 1. Objective and current status

Implement a complete MySQL persistence and schema-management backend for `backend/data-model-service`, preserving tenant authorization, model semantics, display settings, ingestion contracts, background work, and recovery behavior.

**The data-model service has not been converted. Its checked-in application wiring and deployment configuration still select PostgreSQL.** The final source review did not independently re-inspect running containers or the live database.

| Area | Current state |
|---|---|
| Metadata repositories and migrations | PostgreSQL |
| Tenant business tables and schema manager | PostgreSQL |
| Transaction contract | Exposes `pgx.Tx` through `MutationStore.RawTx()` |
| Display settings persistence | Direct PostgreSQL queries and raw transaction access |
| Index jobs and reconciliation | River/PostgreSQL integration |
| MySQL prototype | Removed at user request |
| Prototype field coverage | Historical tests only; prototype removed |
| Ingestion preparation | Reverted to the PostgreSQL/River baseline |
| Production MySQL deployment | Not implemented or activated |

The prototype is evidence for selected designs. It is not a production repository adapter, complete schema manager, queue replacement, or migration tool.

Implementation evidence check (2026-10-14):

| Deliverable | Evidence in the service checkout | Remaining work |
|---|---|---|
| MySQL metadata adapter | `internal/store/` contains the PostgreSQL adapter; no MySQL adapter found | Implement and validate the repository contracts on MySQL |
| MySQL physical schema manager | `internal/tenantdb/` contains the PostgreSQL implementation | Implement the production DDL lifecycle and all six model types |
| Portable mutation boundary | `internal/ports/transactions.go` still exposes `RawTx() pgx.Tx` | Complete Phase A extraction |
| MySQL migration runner | `cmd/migrate/main.go` imports the PostgreSQL migration driver and runs River migrations | Add independent MySQL migration lineage and engine-aware execution |
| API, worker, and reconciliation wiring | `internal/app/app.go`, `cmd/worker/main.go`, and `cmd/reconcile/main.go` construct PostgreSQL pools; the worker uses River | Wire and test all executables, including the repair/reconciliation path |
| Service-level MySQL integration suite | Existing service/router integration suites use `DATA_MODEL_TEST_DATABASE_URL` for PostgreSQL; the separate MySQL feasibility module was removed | Add mandatory real-MySQL service parity tests if conversion is resumed |

This is a source inventory, not a fresh inspection of the deployed database. No additional model type was found missing from the plan in this pass. The remaining blockers are implementation and validation, not evidence that the conversion is nearly complete.

Related work:

- [System migration plan](POSTGRES_TO_MYSQL_MIGRATION_PLAN.md)
- The former MySQL feasibility prototype, test instructions, and compatibility inventory were removed with the implementation revert.

## 2. Architecture constraints

- Preserve one shared logical application database. Do not create separate application databases for individual services or tenants as an incidental part of this conversion.
- Implement and validate data-model support first in isolated environments. Do not move its live tables to MySQL while dependent services still require those tables in PostgreSQL.
- Document the final MySQL architecture and update the standing PostgreSQL requirement when the runtime transition is approved. Development authorization does not imply permission to discard application data.
- Preserve table ownership and independently tracked service migrations. `core` currently contains objects owned by more than one service; do not migrate all `core` objects as if they belonged to data-model.
- Enforce tenant-consistent references and trusted identity at every mutation boundary. Physical table naming is not an authorization mechanism.
- Keep database locks narrow and transactions short. Do not hold locks during external delivery.
- Retain PostgreSQL support during implementation and comparison. Permanent support for both engines is not a default requirement.

## 3. Source inventory and conversion scope

| Component | Source | Required work |
|---|---|---|
| Transaction contract | `internal/ports/transactions.go` | Remove raw driver types; add transaction-owned operations for jobs and display changes |
| PostgreSQL transaction adapter | `internal/store/postgres/transaction_manager.go` | Preserve authorization checks and atomic metadata writes behind the revised contract |
| Metadata repositories | `internal/store/postgres/` | Implement equivalent MySQL repositories, projections, pagination, and error behavior |
| Tenant DDL contract | `internal/ports/schema_manager.go` | Separate transactional metadata mutation from recoverable DDL execution |
| Physical tables and indexes | `internal/tenantdb/postgres/` | Implement MySQL naming, types, indexes, catalog inspection, and recovery |
| Display persistence | `internal/display/store.go` | Move SQL into adapters and replace direct `RawTx()` usage |
| Portable model transfer | `internal/service/portable_data_model_service.go` | Preserve versioned import/export and define recovery for partially applied imports |
| Cache/revision behavior | `internal/httpapi/handlers/model_cache.go`, `internal/service/data_model_read_service.go` | Preserve conditional reads and distinguish model revisions, ETags, and ingestion contract versions |
| Access checks | `internal/access/` and transaction wiring | Port identity reads/locking without bypassing memberships or write grants |
| Index jobs | `internal/riverjobs/`, `internal/worker/`, index-job services | Replace PostgreSQL-specific queue integration and preserve retry/uniqueness contracts |
| Reconciliation | `internal/reconcile/` | Inspect MySQL catalogs and resume/reject inconsistent operations deterministically |
| Migrations | `internal/migrations/metadata/`, `cmd/migrate/` | Add MySQL migration lineage and coordinate dependencies |
| Runtime wiring | `internal/app/`, `internal/httpapi/`, `cmd/` | Select compatible adapters and validate configuration at startup |
| Consumers | Ingestion, decision-engine, auth, and other readers of data-model objects | Inventory SQL/API dependencies and coordinate compatible contracts before cutover |

Re-scan these paths at implementation time. Existing uncommitted display, authorization, and index work must be preserved and included in compatibility testing.

## 4. Metadata and migration coverage

The inspected PostgreSQL metadata migration sequence currently includes:

| Migration | Conversion coverage |
|---|---|
| `000001_init` | Tenants, tables, fields, links, pivots, options, schema changes, tenant migrations, and index jobs |
| `000002_field_enum_values` | Enum values, labels, ordering, and tenant-consistent ownership |
| `000003_index_jobs_hardening` | Job state, retries, constraints, and worker recovery behavior |
| `000004_navigation_options` | Navigation configuration and table/field relationships |
| `000005_ingestion_contracts` | Contract storage, generation, publication/invalidation behavior, and SQL consumers |
| `000006_display_settings` | Display configuration, version changes, validation, and cleanup after field/link changes |

The later migrations contain PostgreSQL-specific functions, triggers, arrays, and JSONB expressions. Reproduce their behavior explicitly. Creating equivalent-looking tables without the validation, cleanup, and version-update behavior is not a completed migration.

- [ ] Record ownership of every table, view, trigger, function, and index.
- [ ] Record constraints and all cross-service consumers.
- [ ] Write a behavior checklist for each trigger/function before selecting a MySQL implementation.
- [ ] Keep PostgreSQL migration files unchanged; add a separate MySQL lineage.
- [ ] Define ordering relative to identity objects and downstream ingestion/read contracts.

### 4.1 Migration ownership and interrupted deployment

Source audit: `cmd/migrate/main.go` runs metadata migrations followed by River migrations. Its `down` branch applies the same requested step count to both histories. The ingestion and decision-engine migration commands also invoke River migrations, and root Compose points these services at the same database. This establishes a shared migration-ownership concern; it does not establish that a deployed migration has already failed.

- [ ] Assign one explicit owner to shared queue-schema migrations during coexistence. A data-model rollback must not downgrade queue objects still required by ingestion or decision-engine.
- [ ] Track metadata and queue versions independently. Do not equate one metadata rollback step with one queue-schema rollback step; document dependency-aware recovery for each lineage.
- [ ] Give MySQL metadata migration tracking an explicit service-owned table name. Preserve the existing PostgreSQL history when changing runner configuration; do not silently start a fresh history or replay installed migrations.
- [ ] Validate rollback arguments before database mutation, including zero and negative step counts. Prefer a separately controlled queue migration operation over incidental queue downgrades in a service command.
- [ ] Test two concurrent migration invocations and bounded lock acquisition. A second runner must not execute the same migration concurrently or report success for a partial installation.
- [ ] Test interruption between metadata and queue migration stages, and after a committed statement inside a multi-statement MySQL migration. Inspect physical state and recorded version/dirty status before recovery; do not automatically clear a dirty marker or stamp a target version without verification.

Acceptance: install all participating services in one disposable database, repeat migration startup, interrupt a staged migration, and perform a supported data-model rollback. Other services' metadata history and required queue objects must remain intact. These are Phase B/D/E migration gates, not tests executed by this audit.

## 5. Target storage design

### 5.1 Shared database and physical names

MySQL does not offer PostgreSQL's separate schema namespace inside a database. Use one application database with service-owned table prefixes and a physical identifier registry.

Proposed mapping, to be finalized before migrations:

| PostgreSQL object | Proposed MySQL representation |
|---|---|
| `core.tenants` | `<application_db>.dm_tenants` |
| `core.model_tables` | `<application_db>.dm_model_tables` |
| `core.model_fields` | `<application_db>.dm_model_fields` |
| Other data-model metadata | `<application_db>.dm_<owned_table>` |
| `tenant_<id>.<logical_table>` | `<application_db>.<registered_physical_table>` |
| Logical field name | Registered physical column, resolved through published model metadata |

The prototype uses immutable tenant/table IDs to produce deterministic bounded names, and immutable field IDs for column names. Production adoption requires a versioned mapping contract, collision detection, catalog validation, and logical-to-physical resolution in every reader/writer.

- [ ] Preserve logical API names independently of physical identifiers.
- [ ] Define behavior for rename, archive, restore, and delete/recreate operations.
- [ ] Prevent cross-tenant registry lookup or mutation.
- [ ] Define how legacy `schema_name` values and ingestion model payloads transition.
- [ ] Validate identifier lengths and safely quote identifiers; bind all values.
- [ ] Do not truncate arbitrary user names or silently reuse a conflicting mapping.

### 5.2 Types and constraints

| Existing type/behavior | Required MySQL contract |
|---|---|
| UUID | Consistent representation across metadata, tenant rows, foreign keys, and jobs; `BINARY(16)` is the prototype candidate |
| Boolean | Enforce boolean values explicitly; a numeric alias alone is insufficient |
| Integer | Preserve the intended signed range and reject overflow |
| Float | Preserve validation and aggregation behavior; test boundary and non-finite inputs |
| Text | Define charset, collation, comparison, and uniqueness behavior |
| Timestamp | Define UTC normalization, precision, supported range, and round-trip behavior |
| PostgreSQL `INET` | Specify validation, normalization, IPv4/IPv6 and prefix handling, storage, and comparison behavior |
| JSONB | Preserve null/absence distinctions, validation, and API serialization while replacing PostgreSQL expressions |
| UUID/text arrays | Choose normalized child rows or JSON according to ordering, relationship, and query requirements |
| Timestamp infinity | Define explicit open-ended validity semantics; NULL is the prototype candidate and requires reader/writer changes |

Important indexing constraint: MySQL cannot simply reproduce every unique index on arbitrary PostgreSQL `TEXT`. Choose and document admissible lengths or another correctness-preserving representation. Do not silently shorten values, treat a prefix index as full-value uniqueness, or rely on an unchecked hash collision assumption.

- [ ] Define nullable/non-nullable field behavior, including adding required fields to populated tables.
- [ ] Preserve uniqueness and tenant-consistent composite foreign keys.
- [ ] Test case sensitivity and trailing-space behavior for names and object identifiers.
- [ ] Implement all supported model types or explicitly block unsupported model changes before accepting them.

Source-level type inventory: `internal/domain/datamodel/model.go` declares exactly six model types: `bool`, `int`, `float`, `string`, `timestamp`, and `ip_address`. All six are represented above; UUID, JSON, and arrays additionally support metadata/system columns. This inventory is not evidence that MySQL implementations exist.

Enum acceptance details (added by the third source audit):

- [ ] Preserve enum values as service-owned metadata with value, label, and sort order; do not substitute a native MySQL ENUM column without an explicit API/storage redesign.
- [ ] Preserve both unique constraints from `000002_field_enum_values`: `(field_id, value)` and `(field_id, label)`. Define case/trailing-space comparison for each, and preserve duplicate sort-order support.
- [ ] Reconcile numeric enum validation with the physical field range. Current integer enum validation uses `strconv.Atoi`, while physical PostgreSQL fields use `INTEGER`; do not assume host-sized validation establishes storage compatibility. Test range boundaries and reject non-finite floating-point values under the chosen contract.
- [ ] Decide whether numeric spellings such as `1` and `01` are distinct enum metadata values or canonical equivalents. Test ingestion and model export against the same decision; do not silently merge existing definitions during transfer.

Gate: enum round-trip and duplicate/range tests pass in Phase B/C, and any intentional behavior change is documented separately from engine parity.

## 6. Transaction and DDL design

### 6.1 Driver-neutral metadata transactions

- [ ] Remove `pgx.Tx` from data-model service contracts.
- [ ] Add transaction-owned index-job enqueue operations.
- [ ] Move display updates into transaction-scoped repository operations.
- [ ] Preserve access/membership validation at the appropriate transaction boundary.
- [ ] Keep metadata mutations, audit/schema-change records, and job/outbox insertion atomic.
- [ ] Return errors for unconfigured or unimplemented queue behavior; remove silent no-op success paths.
- [ ] Prove the revised PostgreSQL adapter still satisfies existing behavior before adding MySQL wiring.

Do not replace `RawTx()` with `any`, runtime type assertions in service code, or a transaction handle that still requires service code to know the driver.

### 6.2 Recoverable DDL workflow

The current mutation store exposes a schema manager using the same PostgreSQL transaction. MySQL DDL implicitly commits, so this contract cannot be ported literally.

Use an explicit operation lifecycle:

1. Authorize and validate the requested model change.
2. In a short metadata transaction, record immutable desired state, operation identity, audit context, and durable execution work.
3. Claim work and acquire the narrow coordination mechanism for the affected physical object.
4. Inspect the current schema. Execute only the required supported DDL.
5. Inspect the resulting columns/indexes and compare them with the intended definition.
6. In a metadata transaction, mark the operation applied and publish the usable model/ingestion contract.
7. On interruption, reconcile intent against the physical schema before resuming.

Suggested states: `pending`, `applying`, `applied`, `failed`, and `needs_repair`. Final names and API representation must align with existing model publication semantics.

- [ ] Distinguish desired model state from the last usable published model.
- [ ] Never advertise a column/index as ready before physical verification.
- [ ] Define compatibility behavior for reads/ingestion while changes are pending.
- [ ] Reject conflicting operation definitions and unexpected schema drift.
- [ ] Implement bounded retries, lease ownership/fencing where applicable, and startup/background reconciliation.
- [ ] Cover interruption before DDL, after DDL, and before metadata completion.
- [ ] Define recovery for multi-step operations where only some steps committed.
- [ ] Do not promise transactional rollback of already committed DDL.
- [ ] Review the prototype's pinned-session table lock against the selected production topology; it assumes cooperating writers on one writable server.
- [ ] Implement archive/drop operations with explicit dependencies, data-retention policy, and recovery limits. Do not automatically delete data to resolve drift.

## 7. Index jobs and background processing

River's current integration is PostgreSQL-specific. The queue design is part of the service conversion, not optional cleanup.

- [ ] Inventory enqueue, scheduling, uniqueness, attempt limits, cancellation, and reconciliation behavior for index jobs.
- [ ] Select a maintained MySQL-compatible queue or a transactional outbox with an external queue.
- [ ] Commit index-job metadata and durable execution work together.
- [ ] Apply that same transaction boundary to reconciliation-created repairs and retries, not only API-created jobs. In `internal/reconcile/service.go`, `scheduleRepairJobs` currently calls `Create` or `Retry` before a separate `Enqueue`; subsequent scans skip pending/running jobs. An enqueue failure can therefore leave pending metadata that this reconciler will not reschedule.
- [ ] Inject enqueue failure after repair-job creation and after retry scheduling; require rollback of the metadata transition or durable execution intent that reconciliation can recover. Seed a legacy pending job with no queue work and prove recovery without duplicate execution. Pending status alone must not be treated as evidence that work is durably scheduled.
- [ ] Implement bounded claims, retry backoff, expired-work recovery, and visible terminal failures.
- [ ] Keep retries from creating duplicate indexes or incorrectly declaring success.
- [ ] Introspect index column order, uniqueness, prefixes, expressions, and validity as supported by the selected MySQL release.
- [ ] Validate DDL algorithm and lock requirements on populated tables. Do not assume an online operation is nonblocking.
- [ ] Measure unrelated-table progress and shared database CPU/I/O impact.

The prototype's polling/lease code is not a full River replacement. Do not activate MySQL index jobs until their complete execution contract passes tests.

## 8. Display, navigation, and ingestion contracts

### Display and navigation

- [ ] Preserve caption-field validation and table display-version changes.
- [ ] Preserve displayed fields, field ordering, enum labels, formatting, and relationship display settings.
- [ ] Preserve cleanup when fields are archived/deleted or links are removed.
- [ ] Preserve tenant-scoped relationship validation and navigation target rules.
- [ ] Define transaction ownership for concurrent display edits and index-triggering changes.
- [ ] Verify existing frontend/API consumers against unchanged response semantics.

### Ingestion publication and cross-service reads

- [ ] Inventory consumers of `core.ingestion_contracts`, model tables, fields, tenants, and related views/functions.
- [ ] Define a versioned read contract with logical and physical naming information where required.
- [ ] Preserve publication consistency, invalidation, and compatibility/error behavior.
- [ ] Ensure ingestion never writes against partially applied physical models.
- [ ] Verify SQL-reader and HTTP-reader paths independently.
- [ ] Coordinate identity/auth objects and authorization reads in the shared database.
- [ ] Plan coordinated deployment for consumers that read these objects directly.

Building this service's MySQL backend first does not make a standalone live database switch safe. Cross-service foreign keys, SQL reads, and shared identity dependencies must be resolved before cutover. Do not introduce untracked metadata replication between PostgreSQL and MySQL as a shortcut.

#### Consumer evidence and required compatibility tests

The downstream source audit found concrete physical-name assumptions. This table refines the inventory requirement above; it is not a claim that all consumers have been exhaustively enumerated.

| Consumer/source (relative to `backend/`) | Observed dependency | Required acceptance evidence |
|---|---|---|
| `decision-engine-service/internal/store/postgres/tenant_data_reader.go` | `resolveTable` derives `tenant_<UUID without hyphens>` through `tenantSchemaName`, even after obtaining model metadata | Resolve registered physical table/column names from the published contract; lookup and aggregation return the same logical fields with opaque physical names |
| `ingestion-service/internal/store/postgres/tenant_data_writer.go` | Uses published schema name or derives the tenant schema; update statements use record field names as SQL columns | Resolve logical input fields to registered columns for both inserts and updates; return logical names and preserve reserved fields |
| `ingestion-service/internal/store/postgres/ingestion_contract_store.go` | Reads and validates `core.ingestion_contracts_v1` and `core.ingestion_tenants_v1` | Validate replacement read contracts, including physical mappings and tenant activation, before accepting work |
| `case-manager-service/internal/store/postgres/explorer.go` | Reads model metadata directly; builds SQL from logical table/field names; filters row JSON keys against model field names | Explicitly project registered columns back to logical names; preserve archived/internal-field exclusion, tenant checks, relationship traversal, cursor pagination, and response-size limits |
| `auth-service/internal/platform/schema.sql`, `schema_v3.sql`, and `store.go` | Membership/invitation foreign keys and authorization/session reads depend on `core.tenants` | Coordinate tenant-table migration before identity dependents; tenant deactivation must invalidate access under the replacement contract |
| `case-manager-service/internal/migrations/metadata/000003_phase1_access.up.sql` | Inbox/tag tenant foreign keys require `core.tenants` | Test shared migration ordering and tenant integrity with the replacement tenant table |

- [ ] Use a fixture whose logical table/field names deliberately differ from every registered physical identifier. Exercise ingestion, decision lookup/aggregation, and case exploration against the same published model.
- [ ] Verify returned JSON uses logical field names and does not expose internal physical identifiers or archived columns.
- [ ] Reject missing or mismatched mappings explicitly on the MySQL path; do not fall back to PostgreSQL tenant-schema derivation.
- [ ] Preserve service ownership of mutations while coordinating these read/FK dependencies. Do not relocate consumer-owned tables into data-model migrations merely to satisfy startup.

Gate: Phase D consumer tests and Phase F coordinated migration tests must cover these paths. Converting only metadata APIs cannot satisfy this gate. This audit inspected source only and did not run consumer compatibility tests.

## 9. Implementation sequence and exit gates

### Phase A — Contract extraction and baseline

- [ ] Guard destructive test setup and isolate disposable test databases before running baseline integration suites (section 10).
- [ ] Complete the source inventory and behavior matrix.
- [ ] Extract transaction-owned enqueue and display operations.
- [ ] Separate metadata transactions from the physical DDL lifecycle.
- [ ] Keep the PostgreSQL adapter working through the new interfaces.
- [ ] Capture baseline API, authorization, concurrency, and failure behavior.
- [ ] Address discarded audit/migration-write errors and map database errors into driver-neutral service errors.
- [ ] Resolve the section 14 API lifecycle, identity, import/export, and cache contracts before implementing MySQL adapters.

Exit gate: no driver-specific transaction types in service contracts; PostgreSQL regression tests pass with recorded limitations.

### Phase B — MySQL metadata backend

- [ ] Finalize version, table mapping, type mapping, collation, and pool budget.
- [ ] Add independently tracked MySQL migrations.
- [ ] Implement every data-model metadata repository and display store operation.
- [ ] Implement access/identity read contracts and tenant-consistent relationships.
- [ ] Replace PostgreSQL trigger/function behavior with reviewed MySQL or service-owned equivalents.
- [ ] Cover partial-index deduplication semantics, immutable ownership, and composite relationship preflight checks.

Exit gate: metadata/API parity and authorization tests pass against disposable MySQL, including rollback and concurrent mutations.

### Phase C — Complete physical model management

- [ ] Promote tested naming and DDL recovery into production adapters.
- [ ] Implement all field types, constraints, lifecycle operations, and index operations.
- [ ] Implement persisted operation state, retries, and reconciliation.
- [ ] Preserve publication boundaries and logical-to-physical resolution.
- [ ] Specify reserved-field mappings, dry-run conflict reports, and mutation-time dependency revalidation.

Exit gate: all `SchemaManager` behaviors are implemented and tested; no operation reports success for unsupported work.

### Phase D — Jobs, publication, and consumers

Dependency clarification: choose the durable execution contract during Phase A and implement the scheduling/recovery subset needed by Phase C alongside its DDL lifecycle. Phase D completes index-worker and consumer integration; it is not permission to mark Phase C complete with an in-memory or no-op scheduler.

- [ ] Implement the selected queue backend and durable enqueue contract.
- [ ] Port index workers, startup migration behavior, and reconciliation commands.
- [ ] Implement ingestion-contract generation/publication.
- [ ] Update dependent read contracts and verify shared-schema migration ordering.

Exit gate: complete tenant/model/index/publication journeys pass under retries, worker interruption, and concurrent requests.

### Phase E — Data transfer and operational validation

- [ ] Build bounded, restartable metadata and tenant-data transfer tooling.
- [ ] Preserve IDs, logical names, field definitions, relationships, display settings, timestamps, and audit history.
- [ ] Reconcile pending schema/index jobs and physical mappings.
- [ ] Validate per-tenant counts, normalized content checksums, constraints, and publication versions.
- [ ] Benchmark representative tenant counts, table sizes, model changes, and concurrent API/worker load.
- [ ] Record query plans, latency distributions, lock waits, pool saturation, and DDL duration.
- [ ] Verify backup restoration and failure recovery in a rehearsal environment.
- [ ] Verify role privileges, startup/readiness compatibility checks, bounded history/list reads, and pending-DDL observability.

Exit gate: repeatable transfer/reconciliation and agreed operational targets pass. Do not infer performance from small fixture tests.

### Phase F — Coordinated runtime transition

- [ ] Confirm all dependent service contracts are ready for the shared MySQL target.
- [ ] Agree downtime, recovery objectives, stop conditions, and operator responsibilities.
- [ ] Pause writes and workers; reconcile in-flight schema and index operations.
- [ ] Complete final transfer and verify physical/metadata consistency.
- [ ] Deploy compatible services together and verify before reopening writes.
- [ ] Monitor failures, ingestion contract mismatches, queue backlog, locks, and latency.
- [ ] Retain the PostgreSQL fallback until the agreed recovery window closes.

Exit gate: the configured runtime actually uses MySQL, end-to-end behavior is verified, and the recovery runbook is tested.

## 10. Required test matrix

| Category | Required evidence |
|---|---|
| Tenant isolation | Wrong-tenant IDs, physical mappings, links, pivots, fields, jobs, and display changes are rejected |
| Authorization | Membership/session/grant changes are respected; direct SQL paths do not bypass trusted identity |
| Atomic metadata | Metadata, audit, and enqueue writes roll back together |
| Type compatibility | Every supported type round-trips with validation, nullability, boundary values, and comparison behavior |
| Physical DDL | Tables, fields, archive/drop, uniqueness, and managed indexes behave as specified |
| DDL recovery | Replay after committed DDL, session termination, worker restart, timeout, and partial operation completion |
| Concurrency | Conflicting same-object operations serialize correctly; unrelated tables continue progressing |
| Jobs | Duplicate delivery, stale claims, retries, cancellation, exhaustion, and failed enqueue are visible and recoverable |
| Display/navigation | Validation, version increments, cleanup, ordering, and relationship navigation remain correct |
| Publication | Consumers observe only compatible published models and handle pending/failed changes predictably |
| Portable models | Supported export/import round-trip, version validation, partial-import recovery, and replay behavior |
| Cache correctness | ETag/304 behavior, tenant-state and enum invalidation, contract-token recreation, and concurrent publication |
| Destructive mutations | Dry-run remains read-only; dependency conflicts are revalidated before mutation |
| Error contracts | Not-found, uniqueness, authorization, unavailable dependencies, deadlocks, and audit failures have explicit outcomes |
| Migration | Fresh install, supported upgrade paths, shared dependency order, repeatability, and failed-migration recovery |
| Transfer | Per-tenant reconciliation includes metadata, business rows, physical mappings, and pending work |
| Performance | Representative data, query plans, bounded reads, DDL lock duration, and shared-resource budget |
| Operations | Restore, interrupted cutover, observability, and post-cutover recovery |

Use disposable test databases only. Never execute reset/drop-based test setup against application infrastructure. Record skipped tests and remaining gaps explicitly.

Required integration-job behavior:

- [ ] Keep optional local test skipping distinct from the mandatory conversion gate. The gate must fail when its disposable database URL, server, migrations, or required test dependencies are unavailable.
- [ ] Record executed and skipped integration cases separately; a successful `go test` exit with database tests skipped does not establish parity.
- [ ] Exercise API, worker, migration, and reconciliation executables with the selected MySQL backend. A working HTTP server alone does not establish complete runtime wiring.
- [ ] Verify disposable database identity before destructive fixture setup, and isolate parallel test packages so one suite cannot reset another suite's database.

Confirmed baseline-test prerequisite: the URL helpers in `internal/store/postgres/repository_integration_test.go`, `internal/service/integration_test.go`, and `internal/httpapi/router_integration_test.go` currently accept any nonempty `DATA_MODEL_TEST_DATABASE_URL`. Their reset helpers then drop `core`, matching tenant schemas, and the migration tracking table. The test-variable name alone does not establish that the target is disposable.

- [ ] Before running these baseline suites, add a shared fail-closed test-target guard and provision dedicated disposable databases with restricted credentials. Check the parsed target and connected database identity before reset SQL; reject application targets and do not print credentials in rejection messages.
- [ ] Add guard tests for application database names, malformed URLs, missing disposable identity, and mismatched connection identity. Establish package isolation in the runner; all three suites currently consume the same environment variable and reset overlapping objects.

This audit did not run the reset helpers or connect to their configured target. Guarding existing PostgreSQL tests is a Phase A prerequisite, not work deferred until MySQL testing.

## 11. Rollback and recovery

Before MySQL accepts application writes, the original PostgreSQL deployment remains the fallback, subject to reconciliation of any external effects.

After MySQL accepts writes, a connection-string reversal is not a lossless rollback. It requires a tested reverse-transfer/replay process or an explicitly chosen forward-recovery strategy. Include new model definitions, physical table changes, tenant data, display state, jobs, and published contracts in reconciliation.

Committed destructive DDL may not be reversible without a backup. Define retention and restoration before exposing those operations on MySQL.

## 12. Definition of complete

Do not report the data-model conversion as complete until all of the following are true:

- [ ] Every required metadata repository and API behavior runs correctly on MySQL.
- [ ] Every supported model type and physical schema operation is implemented.
- [ ] Tenant authorization and relationship integrity pass integration tests.
- [ ] Index jobs, reconciliation, and DDL recovery work without PostgreSQL/River dependencies in the MySQL execution path.
- [ ] Display/navigation semantics and ingestion publication are preserved.
- [ ] Import/export, conditional caching, reserved-field behavior, and destructive-operation previews pass compatibility tests.
- [ ] Dependent services can use the shared MySQL model contracts correctly.
- [ ] Existing data and pending operations can be transferred and reconciled.
- [ ] Operational and performance targets are supported by evidence.
- [ ] Runtime configuration has been deliberately switched and verified, separately from code implementation.
- [ ] Recovery and rollback limitations are documented and tested.

Completion reports must distinguish implemented code, configured/deployed behavior, passed tests, skipped tests, and outstanding work.

## 13. Immediate next work

Start with Phase A in `backend/data-model-service`:

1. Guard and isolate the existing destructive integration fixtures; prove rejection of application targets before running database baselines.
2. Replace `MutationStore.RawTx()` consumers and the driver-specific authorization boundary with transaction-owned operations and portable contracts.
3. Fix required audit/enqueue failure propagation, reconciliation scheduling atomicity, and stale mutation handling; verify the PostgreSQL adapter with failure/concurrency tests.
4. Define desired-versus-published state, request/operation identity, and durable DDL execution before writing MySQL service adapters.

No completion date is committed until the inventory and contract extraction establish scope. The queue backend, final type/index mapping, production topology, data volume, and downtime/recovery requirements remain explicit decisions.

## 14. Audit additions: missing or underspecified requirements

These findings come from repository source inspection. Some are omissions in this plan; others are existing behavior or correctness gaps that the conversion must resolve explicitly. This is not a claim that every existing PostgreSQL behavior is already correct or should be copied unchanged.

### 14.1 Portable model import/export

Evidence: [portable_data_model_service.go](backend/data-model-service/internal/service/portable_data_model_service.go) implements document version `v1`, exports logical model definitions, and imports through multiple service mutations. The HTTP router exposes both `/data-model/export` and `/data-model/import`. Import can return an error after earlier operations have already succeeded.

- [ ] Add portable document versioning, ordering, validation, and logical-name resolution to the parity suite.
- [ ] Test supported tables, fields, enum values, links, ordered pivot paths, table options, and navigation options through export/import.
- [ ] Verify the exact supported portable representation; do not assume it includes every display-formatting setting, identity, audit event, pending job, or business row.
- [ ] Define partial-import reporting and recovery, including duplicate-name handling on replay. Do not claim an entire import rolls back atomically.
- [ ] Define whether new imports wait for physical readiness or return a durable operation identity; a dependent link/index step must not run against an unready table.
- [ ] Keep portable model import separate from lossless PostgreSQL-to-MySQL data migration. Portable import creates new resource identities and is not a replacement for migration tooling that preserves IDs and history.

Gate: round-trip tests for the supported document format and fault-injection tests at intermediate import steps, with visible partial state and a documented resume policy.

### 14.2 Cache validators and revision identities

Evidence: [model_cache.go](backend/data-model-service/internal/httpapi/handlers/model_cache.go) hashes the full HTTP representation for ETags and uses `private, no-cache`. [data_model_read_service.go](backend/data-model-service/internal/service/data_model_read_service.go) separately derives `RevisionID` from tenant state and migration records. [migration 000005](backend/data-model-service/internal/migrations/metadata/000005_ingestion_contracts.up.sql) uses independent opaque UUID contract versions.

- [ ] Treat HTTP ETags, assembled-model revisions, display versions, and ingestion contract tokens as separate contracts.
- [ ] Preserve conditional GET behavior, including `If-None-Match`, weak validators, and 304 responses without a response body.
- [ ] Test tenant activation/status changes, enum edits, field edits, display edits, and archive/delete/recreation against each affected cache.
- [ ] Prevent an unchanged migration-derived revision from incorrectly validating a changed HTTP representation.
- [ ] Preserve no-op contract publication behavior where payloads are unchanged, and ensure recreated contracts cannot reuse stale identities.
- [ ] Define deterministic ordering and serialization, plus timestamp-precision handling during migration. If revisions intentionally change at cutover, document and test cache invalidation rather than promising byte-identical hashes.
- [ ] Test two concurrent edits to one model; publication must not lose one writer's fields or publish a mixed/incomplete snapshot.

Gate: explicit HTTP and SQL-consumer cache tests; no stale contract accepted after a meaningful schema or tenant-state change.

### 14.3 Identity authorization is a concrete database dependency

Evidence: [access/platform.go](backend/data-model-service/internal/access/platform.go) exposes a `pgx.Row`-based query interface, performs resource-owner SQL lookups, and calls `identity.authorize_data_model_v1(...)`. Removing `RawTx()` alone will not make this path MySQL-compatible.

- [ ] Introduce a driver-neutral authorization boundary while preserving trusted principal, session, tenant, authorization-version, and read/write semantics.
- [ ] Coordinate the implementation and migration order of `identity.authorize_data_model_v1` or its reviewed replacement with the auth/identity owner.
- [ ] Preserve the behavior that a denial differs from an unavailable identity/resource lookup; an identity failure must not grant access.
- [ ] Recheck authorization at the owner mutation boundary. Define which principal authorizes deferred DDL and what happens after membership/session revocation or tenant state changes.
- [ ] Define and test resource ownership lookup plus mutation locking so a pre-transaction check cannot authorize a changed resource accidentally.

Gate: authorization revocation/concurrency and dependency-failure tests against both supported persistence adapters. A missing MySQL identity contract blocks live cutover.

### 14.4 Audit writes, migration records, and database error mapping

Evidence: [tenant_service.go](backend/data-model-service/internal/service/tenant_service.go) and other mutation services discard some `SchemaChanges().Create` errors. [tenant_schema_migration_helpers.go](backend/data-model-service/internal/service/tenant_schema_migration_helpers.go) discards migration-record errors and returns no error. Some HTTP handlers import `pgx` directly to classify errors.

- [ ] Make required audit/schema-change and migration-record failures propagate to the transaction boundary.
- [ ] Decide which records are mandatory; do not silently skip a required repository when it is absent.
- [ ] Test rollback when the business mutation succeeds but audit, migration tracking, or durable enqueue fails.
- [ ] For already committed DDL, preserve pending/reconciliation state when completion recording fails; do not claim the physical change rolled back.
- [ ] Define driver-neutral not-found, duplicate/conflict, invalid input, transient lock/deadlock, unavailable dependency, and authorization errors.
- [ ] Preserve API status/body contracts where intended; explicitly test planned behavior changes. Do not expose raw database errors or credentials to callers.

Gate: injected failures cannot produce a successful model-change response without its required durable records.

### 14.5 Partial indexes and relationship integrity

Evidence: [migration 000003](backend/data-model-service/internal/migrations/metadata/000003_index_jobs_hardening.up.sql) makes `dedupe_key` unique only when it is nonempty. Initial metadata relationships include separate tenant and resource foreign keys, which are not by themselves proof of tenant-consistent linkage. Publication triggers also prohibit reparenting published tables, fields, and enum values.

- [ ] Specify an equivalent MySQL dedupe constraint: nonempty duplicate keys must conflict, while multiple empty/unset keys remain allowed. Select and test the representation rather than creating a plain unique index with different behavior.
- [ ] Define tenant scope and identity inputs in dedupe keys; schema/table name changes must not accidentally duplicate or suppress unrelated jobs.
- [ ] Preflight cross-tenant or orphan metadata relationships before adding composite constraints. Report violations without deleting or repairing application data automatically.
- [ ] Preserve table/field/enum ownership immutability and table-to-field consistency, not only existence of referenced IDs.
- [ ] Review cascade ordering and cleanup behavior for links, pivots, display settings, navigation, and published contracts.

MySQL 8.4-specific acceptance details (third audit): cascaded foreign-key actions do not activate triggers, and re-enabling `foreign_key_checks` does not validate rows inserted while checks were disabled. See the [MySQL 8.4 foreign-key reference](https://dev.mysql.com/doc/refman/8.4/en/create-table-foreign-keys.html).

- [ ] Map direct deletion and parent-triggered cascading deletion separately. `000006_display_settings` currently uses field/link delete triggers for display cleanup; do not assume a literal MySQL trigger port handles cascading removal.
- [ ] Test deletion of a referenced parent table that cascades through a link while its child table survives. The surviving child's display settings and published contract must contain no dangling link reference, with the required version invalidation.
- [ ] Assign cleanup ownership explicitly to service transactions or another verified database mechanism; cover authorized SQL mutation paths as well as HTTP handlers.
- [ ] Prefer constraint-preserving transfer order. If a transfer tool disables foreign-key checks, scope and restore that session setting on every exit and run explicit orphan, tenant-consistency, and table/field-ownership checks before publication. Re-enabling the setting is not a validation gate.

Gate additions: direct/cascaded deletion parity in Phase B/D; intentionally corrupt a disposable transfer fixture and prove Phase E validation detects it and blocks activation.

Gate: constraint tests cover empty-key duplicates, nonempty duplicates, cross-tenant references, ownership changes, and cascade effects.

### 14.6 Reserved fields and existing physical/metadata differences

Evidence: [table_service.go](backend/data-model-service/internal/service/table_service.go) creates metadata for managed `object_id` and `updated_at` fields. The physical schema manager also creates `id`, `valid_from`, and `valid_until`. The prototype uses ID-derived names for custom fields. Existing PostgreSQL field creation does not by itself demonstrate enforcement of every metadata nullability rule in physical DDL.

- [ ] Define reserved/system field mappings separately from custom field mappings; avoid creating a second physical `object_id` or `updated_at` column under an ID-derived name.
- [ ] Preserve object-lookup uniqueness, required timestamps, managed-field protections, and ingestion lookup semantics.
- [ ] Define how portable import recognizes managed fields without duplicating them.
- [ ] Inventory metadata promises versus actual database enforcement. Classify differences as existing gaps, intended validation-layer behavior, or conversion defects.
- [ ] Preflight existing nulls, duplicate lookup values, and invalid values before adding stricter constraints; report remediation requirements rather than silently changing records.

Gate: fresh table creation, migrated table lookup, custom-field creation, and portable import use the same unambiguous field mapping.

### 14.7 Tenant provisioning, pending responses, and delete dry-runs

Evidence: [tenant/model.go](backend/data-model-service/internal/domain/tenant/model.go) defines `pending` and `active` states and derives `schema_name`. `TenantService.Provision` activates a tenant, and table creation requires an active tenant. Field/table deletion paths expose dependency reports and dry-run behavior.

- [ ] Specify what provisioning means in one MySQL database; it must establish the registry/readiness contract without creating a database per tenant.
- [ ] Activate only after required provisioning succeeds; interruption must not leave an active-but-unusable tenant.
- [ ] Document synchronous completion versus accepted/pending operation responses for each affected endpoint, including status codes, operation IDs, polling, and frontend behavior.
- [ ] Update the served specification (`internal/httpapi/openapi.yaml`, embedded by `internal/httpapi/docs.go`), the reference specification (`docs/openapi.yaml`), handler DTOs, and `frontend/src/lib/data-model-api.ts` together when choosing pending-operation responses. The two specification files currently differ in descriptions/examples; byte differences alone do not demonstrate an API incompatibility. Validate response schemas and status semantics against handlers and clients.
- [ ] Test the client journey from accepted operation through ready or failed state, including reload and expired authorization. Existing create-table/create-field handlers return a resource with HTTP 201; an asynchronous implementation must not let clients treat mere acceptance as physical readiness. This refines the pending-response requirement rather than adding another model-conversion feature.
- [ ] Keep dry-run deletion read-only and preserve dependency/conflict report shapes and managed-field protection.
- [ ] Revalidate deletion dependencies during the mutation workflow; a prior dry-run cannot authorize deletion after concurrent relationship changes.
- [ ] Test archive/delete interactions with pending DDL/index jobs, publication, and tenant writability.

Gate: API and concurrency tests cover provisioning interruption, pending model changes, dry-run safety, and stale deletion previews.

### 14.8 Bounded metadata reads and capacity limits

Evidence: repository interfaces include unpaginated tenant/job/history lists, and `DataModelReadService.Get` loads migration history to compute a revision. These are existing contracts to review, not evidence that queries are already bounded or performant.

- [ ] Inventory full-model assembly separately from user-facing lists/history reads; define explicit model-size and response-size budgets.
- [ ] Use bounded/indexed queries for operation queues and histories. If pagination changes an API contract, version or coordinate that change.
- [ ] Evaluate a transactionally maintained revision token to avoid reading the entire migration history on every model read; preserve revision semantics if adopting it.
- [ ] Set and test supported tenant/table/column/index/model-import sizes against the selected MySQL version and hosting limits.
- [ ] Include metadata catalog growth, concurrent DDL, pool occupancy, and long-lived reader effects in scale tests.

Gate: representative query plans and capacity measurements; no claim of optimization from an interface or index definition alone.

### 14.9 Startup compatibility, privileges, and observability

Evidence: the current readiness handler is `pgxpool`-specific and checks database reachability. Configuration and executable wiring are PostgreSQL-oriented. The ingestion SQL reader separately validates the shapes of `ingestion_contracts_v1` and `ingestion_tenants_v1`.

- [ ] Validate selected engine, schema version, required read contracts, authorization contract, queue schema, and permissions at startup.
- [ ] Fail clearly on unsupported engine/DSN combinations; do not silently fall back to PostgreSQL or report ready with missing required objects.
- [ ] Separate cheap liveness checks from readiness and startup compatibility checks; avoid expensive catalog scans on each probe.
- [ ] Define least-privilege roles for migrations, metadata APIs, DDL/index workers, and cross-service read views. Test denial of unauthorized mutations.
- [ ] Define TLS/timeouts and strict configuration parsing; malformed pool/worker settings must not silently become unsafe defaults.
- [ ] Expose operation identity, tenant/resource identity, pending age, attempt count, last error, lock wait, DDL duration, and publication lag through appropriate logs/metrics/admin views.
- [ ] Provide operator commands for inspection, retry, and manual-repair marking with authorization and audit trails.

Confirmed packaging gap: `backend/data-model-service/Dockerfile` builds and copies `server`, `worker`, and `migrate`, but not `cmd/reconcile`. Source availability is therefore not evidence that the deployed image can execute the recovery command.

- [ ] Package the reconciliation executable in the production image or a separately versioned operations image, with a documented invocation and required credentials/permissions.
- [ ] Smoke-test the built artifact, including access to engine-specific migration assets and the configured working directory. Exercise reconciliation against disposable pending/interrupted operations without requiring a source checkout or Go toolchain.

Gate: deployment fails safely on incompatibility, and a stuck/failed schema operation can be located and recovered without ad hoc SQL edits.

### 14.10 Phase A completion clarification

Section 9's driver-neutral contract gate includes `access.Queryer`, display persistence, job enqueueing, and error classification—not just `MutationStore.RawTx()`. All additions above feed the corresponding phase and definition of complete.

This audit reviewed source contracts and representative implementation paths. It did not execute a new MySQL parity suite, exhaustively inspect every SQL statement, establish production data quality, or measure performance. Those remain implementation evidence requirements.

## 15. Second audit: explicit concurrency and recovery acceptance cases

These cases refine the existing gates rather than establish another conversion phase. Source findings below describe risks visible in the current implementation; they have not been reproduced with new tests in this audit. Proposed MySQL behavior remains unimplemented.

### 15.1 Prevent stale field updates before publication

Evidence: `internal/service/field_service.go` reads and modifies a field before entering `txManager.Run`. `internal/store/postgres/field_repository.go` then writes all mutable properties using only `WHERE id = $1`, without checking a version or affected-row count. A later write can therefore restore stale properties even if the two requests intended to edit different properties. Publication locking alone does not protect this earlier mutation.

- [ ] Read and validate the current resource within the mutation transaction under a narrow resource lock, or use an explicit expected-version contract with conflict handling. Apply only the requested patch to that current state.
- [ ] Revalidate enum, uniqueness, archive, and relationship dependencies at the mutation boundary, with a consistent lock order for dependent resources.
- [ ] Distinguish missing resources, stale versions, and legitimate no-op updates in repository outcomes. Specify affected-row semantics for the selected adapter rather than assuming every zero result means not-found.

Acceptance: concurrent description and uniqueness edits preserve both accepted changes or return an explicit conflict; update-versus-delete never reports a successful mutation of a missing resource. Cover both adapters during Phase A/B.

### 15.2 Queue acknowledgement must follow durable domain state

Evidence: `internal/worker/index_job_runner.go` ignores the error from `MarkFailed`; several lookup/introspection failure paths then return nil. `MarkApplied` and its audit write are separate, and audit failure is only logged. `internal/store/postgres/index_job_repository.go` starts attempts and completes jobs by ID without an expected attempt/status condition.

- [ ] Propagate failure to persist required job state or audit records; do not acknowledge execution as complete before durable terminal state exists.
- [ ] Separate retryable dependency failures from permanent invalid jobs. Define the relationship between queue attempts, domain attempts, scheduled retries, and operator retries.
- [ ] Guard claims and completion using expected state and attempt ownership. An expired worker must not overwrite a newer attempt's result, and duplicate delivery must not restart an already completed job.
- [ ] Commit terminal domain state and its required audit record together. Reconcile queue/domain disagreement after interruption without blindly repeating physical DDL.
- [ ] Test an existing index with the expected name but the wrong definition. Current PostgreSQL introspection in `internal/tenantdb/postgres/index_introspection.go` checks only name existence; that is not sufficient evidence of MySQL completion.

Acceptance: inject terminal-status and audit persistence failures, duplicate delivery, and a late expired worker. No false success, overwritten terminal state, or untracked completion is allowed. Include these cases in Phase A baseline fixes and Phase D replacement-queue tests.

### 15.3 Request retries and schema-operation ordering

The existing plan requires immutable operation IDs and worker fencing but does not fully specify recovery when a client loses the acceptance response, or when two generations of a model operation execute out of order. Queue deduplication alone does not define this API contract.

- [ ] Define a tenant-scoped request idempotency contract for DDL-backed mutations: key, canonical request fingerprint, durable operation receipt, retention period, and response on conflicting reuse. Reauthorize receipt access on retries.
- [ ] Resolve an uncertain metadata commit by looking up the existing receipt; do not blindly create a new resource or DDL intent after a timeout.
- [ ] Record expected model/resource generation and operation dependencies. Check eligibility before DDL and again before publication; obsolete work must not recreate a dropped object or publish an older model over a newer one.
- [ ] Specify reconciliation if ownership is lost while DDL is in progress. Metadata fencing cannot by itself undo physical work that already completed.

Acceptance: lose the response after committing intent, retry the request, and obtain the same operation. Reuse the key with different input and receive a conflict. Delay an older operation until after a newer generation is accepted; prove that physical execution and publication follow the defined order. Required in Phase C/D.

### 15.4 Read a model and its revision consistently

Evidence: `internal/service/data_model_read_service.go` separately reads the tenant, assembled model, and migration history. Its contract does not require those reads to share a snapshot. Section 14.2's cache tests must exercise this read path as well as the publication writer.

- [ ] Return the model, tenant state, and revision from one consistent metadata snapshot, or from an immutable published-version record. Define this at the repository/service boundary.
- [ ] Ensure enum, navigation, and display data included in the response belong to the same supported publication/version semantics.
- [ ] Keep snapshot transactions bounded; do not hold them across DDL or network calls.

Acceptance: pause a model read between component queries while another transaction publishes a change. The reader receives a coherent old or new result and a matching revision/ETag, never an old payload tagged with the new revision. Required in Phase B/D.

### 15.5 Retire old consumers before destructive physical changes

Desired-versus-published state protects newly added objects, but does not by itself protect an in-flight reader or writer still using an older contract when a column is removed or changed incompatibly.

- [ ] Define the compatibility window for cached contracts, in-flight ingestion, queued work, and rolling application versions.
- [ ] Separate logical retirement from physical deletion. Delay incompatible DDL until old consumers are drained or explicitly reject obsolete work through a tested version contract.
- [ ] Include old/new binary compatibility with metadata migrations in deployment validation; document the point after which reverting only application binaries is unsafe.

Acceptance: keep an old-version ingestion request and queued job alive across model retirement. They must complete under the documented compatibility policy or fail explicitly before an incompatible write; no silent data loss or indefinite retry loop. Required in Phase D/F.

Second-pass outcome: the plan now names these additional acceptance cases, but this remains a source-level review. Completion requires implementation and real database tests; neither the audit nor the existing prototype establishes full conversion or production readiness.

## 16. Final planning review and implementation priorities

Disposition: sufficient scope to begin Phase A. No additional model category was identified in the final source pass. This is a planning conclusion, not proof of exhaustive coverage or production readiness. Further findings should come from implementation, targeted tests, and migration rehearsal rather than repeatedly expanding speculative checklists.

| Priority | Work package | Completion evidence |
|---|---|---|
| First | Test-target guards and isolated baseline runner | Application targets rejected; PostgreSQL database cases actually execute |
| Next | Portable transactions, authorization, display, and durable enqueue | Atomic rollback, failure propagation, tenant isolation, stale-edit protection, and repair scheduling tests |
| Before MySQL adapter completion | Storage mapping, metadata migrations, DDL lifecycle, queue contract, and publication semantics | Real-MySQL type/constraint parity and interruption/replay tests |
| Before coordinated cutover | Consumer mappings, migration ownership, packaged recovery, data transfer, and operational limits | Shared-database integration, image smoke tests, reconciliation, load measurements, and restore rehearsal |

Decisions still open, with the point at which they must be resolved:

- Before Phase B schema design: target MySQL release/topology, UUID and text/index representations, collation, timestamp/IP contracts, and supported model sizes.
- During Phase A lifecycle design: queue backend and ownership, operation receipts/retry semantics, and synchronous versus pending API responses.
- Before Phase E measurements: representative data/load, shared connection budget, latency/capacity targets, and backup/restore objectives.
- Before Phase F: coordinated consumer readiness, maintenance window, operator responsibilities, stop conditions, and recovery after MySQL has accepted writes.

These decisions do not block test isolation or contract extraction. Keep the shared logical database architecture and service mutation ownership intact.

Evidence boundary: source and configuration inspection, comparison against the six metadata migration pairs and declared model types, and documentation consistency checks. Earlier prototype test results remain limited to their recorded scope. No application database was queried or reset, no new runtime tests were executed, and no conversion code was implemented during this final review. Phase A and every later exit gate remain open.
