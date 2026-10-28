# MySQL metadata repository contract

Status: table/field/enum/link/navigation/pivot adapters implemented; seventeen
repository scenarios passed on MySQL with race detection and module-wide vet.
The full selected regression passed 67 MySQL scenarios. Runtime integration remains
pending. This document covers metadata persistence within DM-01, not module completion.

Audit-history and tenant-revision adapters passed seven additional real-engine
scenarios. Initial full selected regression passed 73 MySQL scenarios; the final
31-scenario metadata rerun passed after tightening JSON pre-allocation limits.
Both runs included race detection and module-wide vet.

Tenant persistence and the separate platform catalogue passed seven real-engine
scenarios. That full selected regression passed 81 MySQL scenarios, selected
unit tests, race detection and module-wide vet. No runtime activation is implied.

Ordered table-options persistence adds six real-engine scenarios. The subsequent
full selected regression passed with race detection and module-wide vet, including
the options tests. Runtime and the complete display protocol remain pending.

## Ownership and transaction boundary

### Registration scope allocation

`ports.TenantRegistrationManager` is a distinct platform-registration capability.
`TenantService.WithTenantRegistrationManager` selects it only for Create. The
owner validates input, allocates one nonzero tenant ID, then passes that exact
identity to RunRegistration before creating pending tenant metadata, history and
revision. Default PostgreSQL construction retains its existing transaction path.
Get, List and Provision keep their separate repository/catalogue/transaction paths.

The staged MySQL `RegistrationTransactionManager` copies immutable configuration
per invocation and uses the same bounded, authorized metadata transaction manager.
Its mandatory authorizer must verify trusted platform registration authority for
the newly allocated identity; ordinary permission on an existing tenant is not a
substitute. Existing tenant scopes are rejected before the owner callback. After
the callback, the transaction must contain that pending tenant or it rolls back.
Concurrent calls cannot overwrite a shared tenant scope. The registration manager
has no queue binding and retains explicit rejection of legacy physical operations.

This integrates scope allocation, not durable registration idempotency. No retry
occurs on CommitError; the tenant may already exist with committed audit/revision.
Do not repeat Create with a fresh UUID after an uncertain commit. An existing-ID
rejection is not successful replay or proof of original request identity. Stable
operation/actor/request receipts, original-result recovery, endpoint authorization
and production timestamp/credential wiring remain required before MySQL activation.

The canonical registration request prerequisite is available as
`service.RegistrationRequestHash` (`dm-tenant-registration/v1`), binding the
original typed input, stable allocated tenant and trusted actor. It preserves
original request distinctions before tenant normalization. This does not close
any of the durable receipt or recovery requirements above; see the
[canonical request contract](../../../backend/data-model-service/internal/service/publication_request.md#tenant-registration-requests).

### Authorized platform catalogue service path

`ports.TenantCatalog` exposes only List. `TenantService.WithTenantCatalog` selects
that capability explicitly without changing Get, Create or Provision. The default
constructor retains PostgreSQL's existing repository listing. A selected catalogue
error propagates without falling back to the tenant-scoped repository; nil catalogue
configuration fails explicitly.

The staged MySQL `TenantCatalogReader` owns a bounded read-only repeatable-read
snapshot per List call and requires a `CatalogAuthorizer` on that transaction.
The authorizer must verify trusted platform identity and current global-list
permission using local read-only SQL, without network calls or locking reads.
Authorization runs on every request, including repeated requests after revocation.
Snapshot semantics do not promise revocation of a read already in flight. Concrete
production identity SQL and API wiring remain required before runtime adoption.

The reader wraps the explicitly global `TenantCatalog`; it exposes no tenant lookup
or mutation methods. Row/byte overflow, authorization errors, cancellation and
transaction failure return no partial catalogue. Existing keyset paging remains
available on the transaction-level catalogue; this service List path fails on
configured bounds rather than silently returning a partial global list. API paging
and cross-request export consistency remain separate requirements.

### Staged service transaction manager

`mysql.NewTransactionManager` implements the existing `ports.TransactionManager`
with a fixed, owner-resolved tenant, explicit row/byte budgets and a mandatory
`MutationAuthorizer`. Every call starts one READ COMMITTED transaction and runs
that authorizer before exposing the mutation store. The authorizer must validate
trusted identity, current write permission and resource ownership using local SQL
on the supplied transaction; a tenant UUID alone is not authorization. Production
identity SQL and endpoint binding remain unimplemented, so no runtime constructor
selects this manager and test authorizers do not establish authorization readiness.

All eleven repository ports use that transaction and tenant. An optional delivery
store must use the exact same database pool and database; enqueue then commits or
rolls back with metadata, history and revisions. Missing delivery configuration
returns `ErrJobQueueUnavailable` when enqueue is requested. There are no automatic
retries. Cancellation, callback errors and panics roll back; `mysqlstore.CommitError`
requires operation-specific receipt recovery because the commit may have succeeded.

`RunWithTx` exposes the same transaction only for owner adapters composing SQL and
publication. Its result is provisional until commit. Callbacks must not retain
the store/transaction, commit or roll back independently, change session state,
perform DDL or make network calls. The legacy `SchemaManager` port intentionally
returns `ErrPhysicalProtocolRequired` for every method, including inspection;
it cannot imply success or readiness while physical integration is unfinished.
Use the separate durable DDL protocol and final verified publication path instead.

The manager does not automatically publish contracts, maintain display dependencies
or authorize platform tenant enumeration. Registration must allocate its tenant
scope before constructing the manager and preserve that identity through the
owner call and uncertain-commit recovery. `TenantService.Create` can use this
preallocated scope for pending metadata/audit/revision creation in the isolated
fixture; provision/read/list/API integration and durable registration receipts
remain separate work. Global catalogue reads must continue through the explicit
read-only platform catalogue after their own authorization.

The [repository factory](../../../backend/data-model-service/internal/store/mysql/metadata_repositories.go)
requires an existing `*sql.Tx`, a validated database identifier, a nonzero tenant
UUID and explicit row/byte budgets. The adapters implement the existing
`ports.TableRepository`, `ports.FieldRepository` and `ports.FieldEnumValueRepository`
interfaces, plus `ports.LinkRepository` and `ports.NavigationOptionRepository`.
The factory also supplies `ports.PivotRepository`.
It now also supplies `ports.SchemaChangeRepository` and
`ports.TenantSchemaMigrationRepository`.
Every lookup, list, update and delete is restricted to the factory's
tenant. Create/update reject mismatched record tenants. Child inserts also rely
on the schema's composite tenant/parent foreign keys; updates lock the exact
tenant/id/parent tuple before changing mutable values.

The owner must authorize the request before constructing these repositories.
Tenant binding is defense in depth, not an authorization decision. The adapter
does not begin or commit transactions, retry mutations, perform physical DDL,
enqueue work, publish ingestion contracts, clean displays, or write history.
Callers must roll back on any error and coordinate these operations in their
owner protocol. MySQL statement errors do not automatically roll back all prior
statements in the transaction. No application constructor uses this factory yet.

## Behavior preserved and explicit differences

| Adapter | Behavior |
|---|---|
| [Tables](../../../backend/data-model-service/internal/store/mysql/table_repository.go) | All source domain columns; archived rows retained; newest-created list order. Updates preserve name, identity and creation time, matching the existing PostgreSQL repository's mutable column set. |
| [Fields](../../../backend/data-model-service/internal/store/mysql/field_repository.go) | All six data types; flags, aliases, timestamps; archived rows retained; oldest-created list order. Updates preserve name/type/parent/creation time. |
| [Enums](../../../backend/data-model-service/internal/store/mysql/field_enum_value_repository.go) | Value and label exact uniqueness enforced by dictionary guards; signed INT sort order; sort_order then created_at list order. This repository order is distinct from ingestion publication's sort_order then UUID order. |
| [Links](../../../backend/data-model-service/internal/store/mysql/link_repository.go) | All endpoints and timestamps; exact name uniqueness scoped to child table; newest-created order with UUID tie breaker; tenant-bound deletion. Pivot path RESTRICT constraints must be satisfied by the owner before deleting a referenced link. |
| [Navigation](../../../backend/data-model-service/internal/store/mysql/navigation_option_repository.go) | All source/target/filter/order references; resolved names fetched in one statement through tenant/table-consistent joins; tenant and source-table lists ordered by created_at then UUID. Supplied name fields are read projections, not persisted authority, as in the PostgreSQL adapter. |
| [Pivots](../../../backend/data-model-service/internal/store/mysql/pivot_repository.go) | Nullable direct field or ordered path; repeated links preserved. One joined read assembles all parents/children coherently under READ COMMITTED; noncontiguous ordinals and invalid field/path shapes fail explicitly. Creation batches at most 1,000 path rows per statement on the caller transaction. |
| [History and revisions](../../../backend/data-model-service/internal/store/mysql/event_repository.go) | Caller-transaction event inserts and bounded tenant lists. Separate `ListPage` methods use descending time/UUID keyset cursors on existing indexes. Audit JSON stays JSON rather than a byte-exact archive; source operation/resource/status and revision version strings are retained. |
| [Tenants and catalogue](../../../backend/data-model-service/internal/store/mysql/tenant_repository.go) | Tenant-bound create/get/list/status methods preserve nullable external keys and exact global key uniqueness. A separate read-only platform catalogue exposes bounded list/keyset pages. All methods use the caller transaction; there is no tenant database provisioning in this adapter. |

UUIDs use the shared network-byte-order BINARY(16) codec; timestamps use the UTC
microsecond codec. Text is parameterized and rejects invalid UTF-8 and NUL.
No dynamic identifier comes from model names. Database identifiers are validated;
table/column fragments are private fixed source constants.

Lists add UUID tie breakers to otherwise tied creation times. Missing updates and
deletes return `sql.ErrNoRows`, including records hidden by tenant scope. This is
an intentional improvement over unchecked PostgreSQL row counts, and needs owner
error translation at runtime integration. Cross-tenant parent lists return empty
lists; explicitly requesting another tenant's table list is an error.

Updates take an exact-row `FOR UPDATE` lock before executing the UPDATE so a
no-change result is distinguished from a missing row without relying on the
driver's affected-row mode. No shared tenant/table parent lock is introduced by
this adapter. Normal foreign key and exact-key dictionary locking still applies.
Multiple-resource owner transactions must establish their lock order and retry
policy; this layer does not imply optimistic revision protection.

## Bounded reads and failure contract

Each list reads at most configured maxRows + 1 and rejects overflow without a
partial result. The SQL projection computes original text byte size and suppresses
oversized text in the same statement. Get/list reject oversized records rather
than returning truncated text. Lists also enforce a cumulative text byte budget
while scanning. Fixed-size UUID/boolean/time columns and Go object overhead are
additional to the text budget. The server may send up to the row limit times the
per-row text budget before rows are closed/drained; this is not a claim of a
single aggregate network-byte cap. Configure both budgets conservatively and
measure representative wide models before activation.

Create/update text arguments have a cumulative byte budget. UPDATE budgets cover
the mutable text arguments, not immutable text already stored. SQL errors retain
their original driver type/code; missing rows use `sql.ErrNoRows`; limit failures
use `ErrSnapshotLimit`; a closed caller transaction returns `sql.ErrTxDone`.
Owner-level API mapping and uncertain commit recovery remain integration gates.

Added access indexes match tenant/table ordering predicates:
`data_model_tables_created(tenant_id,created_at,id)`,
`data_model_fields_created(tenant_id,table_id,created_at,id)`, and
`data_model_enum_owner_created(tenant_id,field_id,sort_order,created_at,id)`.
Link/navigation definitions also include tenant/created/id ordering indexes;
navigation source-table lists use the existing tenant/source/created/id index.
These are target schema definitions, not executed production migrations. Their
representative query plans and capacity costs remain DM-01.4 acceptance work.

Pivot limits count joined rows, not only parent objects. A direct-field pivot
uses one row; a path pivot uses one row per ordered link. The read rejects
overflow before returning any model; its maxRows + 1 sentinel must never be
treated as a truncated array. There is no variable text in this projection, so
row limits bound UUID path storage separately from maxBytes. Parent/path writes
share the caller transaction, and the caller must roll back the entire transaction
if a later batch fails. The adapter validates field-versus-path exclusivity and
nonzero endpoint identities, but path traversal connectivity/semantic suitability
still belongs to the owner service. The target includes a tenant/created/id pivot
index; real workload query plans remain unverified.

## Audit and revision reads

`History.Create` requires nonzero event/resource/tenant identities and valid UTF-8
JSON, with a cumulative input byte budget. `Revisions.Create` retains version text
and the applied timestamp. Both writes participate in the caller's owner transaction;
duplicate IDs and all other errors propagate. Neither method establishes physical
DDL readiness or records a successful outcome on behalf of an unverified operation.
The schema guards reject changes to existing event contents; production retention,
DELETE privileges and locked definer installation remain separate requirements.

The existing `ListByTenant` ports return a complete bounded result or a limit error.
The new concrete adapters additionally expose `ListPage(ctx, limit, after)`:

- Cursor scope includes tenant and stream (`history` or `revisions`), timestamp and
  UUID. The factory's tenant is authoritative; a cursor is not authorization.
- Order is descending timestamp then binary UUID. The next cursor is the last
  returned record, and a one-row sentinel determines whether another page exists.
- A page exceeding its byte budget returns neither partial records nor a cursor.
  No OFFSET or total-history COUNT is issued; existing tenant/time/UUID indexes
  support the predicates. Representative query-plan and load evidence is pending.
- Pagination across independent transactions is not a frozen snapshot. Exports
  requiring one consistent history must run within an appropriate snapshot
  transaction; HTTP cursor encoding and endpoint adoption are not implemented.
- JSON may be normalized by MySQL; code must not compare serialized audit bytes
  to establish semantic equality. Numeric range and source-data preflight still
  apply to migration. The shared codec rejects malformed JSON/UTF-8, not every
  possible cross-engine JSON semantic difference.

## Tenant scope versus platform administration

`NewMetadataRepositories` now includes `Tenants`. This adapter can create the
already authorized, newly allocated tenant UUID before its database row exists;
it cannot create or access a different UUID. `List` returns only the bound tenant
(or an empty result if absent). This is intentionally narrower than the original
PostgreSQL repository's global `List` and is **not** a drop-in replacement for the
platform-wide `TenantService.List` route. Runtime integration must route authorized
catalogue reads through the explicit read-only `NewTenantCatalog` adapter and
tenant operations through their bound factory. Do not simulate global access with
a zero-tenant sentinel. Neither constructor verifies a platform role itself.

Catalogue reads return global rows only after the caller authorizes that access.
The type exposes no mutation methods. Pages order by created_at and binary UUID,
with a sentinel row and byte budget; a separate catalogue cursor avoids reusing
tenant event cursors. A new created_at/id index supports this ordering, but query
plans at production scale remain unverified. Export consistency still requires
the caller to hold an appropriate snapshot transaction.

NULL external_key remains distinct from empty text; no normalization happens in
the repository. The tenant domain constructor retains its existing normalization
responsibility. The existing exact-key guards enforce separate global uniqueness
of non-NULL external keys and required schema names. SchemaName remains metadata
describing legacy identity; it is never interpolated as SQL or used to create a
per-tenant MySQL database.

Status mutation locks the exact tenant row, rejects absent/foreign tenants and
invalid status values, and updates `updated_at` with UTC microsecond statement
time. It permits unchanged status updates independent of affected-row reporting.
This preserves the source server-assigned update timestamp intent, but PostgreSQL
NOW() transaction-start timing is not claimed to be identical to statement time.
Physical readiness/provisioning must be established by the owner before selecting
active status; this adapter does not infer or implement that protocol. Tenant
registration, history and revisions can share one transaction. Caller failures
must roll back the whole transaction; retries/ambiguous commits remain owner work.

## Validation and remaining work

[Real-engine tests](../../../backend/data-model-service/internal/store/mysql/metadata/repositories_test.go)
exercise domain round trips, all field types, mutable columns, unchanged updates,
tenant/parent isolation, ordered archived-inclusive lists, deletes/missing rows,
transaction rollback after exact-key conflicts, closed transaction rejection,
multibyte/row/cumulative limits, malformed text, exact-row lock cancellation,
independent-tenant updates, constructor rejection and read-only transactions.

Relationship tests additionally cover parameterized names, archived name reads,
cross-tenant lookups/deletes/lists, wrong-table endpoints, joined-name byte limits,
missing deletes, transaction rollback, pivot deletion barriers and concurrent
duplicate link creation. Endpoint existence and ownership are database constraints;
parent field uniqueness/type suitability, display cleanup and managed-index job
creation remain owner-service responsibilities, not implemented by these adapters.

Pivot tests cover a 1,001-entry ordered path across two insert batches, repeated
links, exact and cumulative joined-row limits, tenant/field ownership, invalid
shapes, cascading child deletion, rollback of both parent and earlier owner writes
after a child insert fails, and rejection of a path with a missing ordinal.

Event tests verify normalized JSON with an exact integer above 2^53, immutable
event/revision updates, keyset paging across equal timestamps, tenant/stream
cursor rejection, exact input/stored JSON byte limits, cumulative page limits,
and owner/audit/revision rollback when either event write fails.

Tenant tests verify nil/empty/multibyte external-key round trips, scoped reads and
status changes, global catalogue versus scoped-list separation, equal-time pages,
exact global external/schema identity, registration/audit/revision rollback, byte
and row budgets, invalid catalogue construction/cursors and concurrent duplicate
external-key registration.

## Ordered table-options persistence (DM-01.1h)

The [adapter](../../../backend/data-model-service/internal/store/mysql/table_options_repository.go)
implements `TableOptionsRepository` on the caller's tenant-bound transaction.
Source behavior is defined by PostgreSQL `table_options_repository.go`,
`OptionsService.Upsert`, and metadata migrations 000006/000007. The source
upsert retains the existing options ID and both display JSON documents; its
trigger increments `display_version` even when values are unchanged.

The MySQL adapter locks the exact model table before validating referenced fields
in sorted, bounded batches with shared locks. Every field must be active and belong
to that tenant/table; each list must be distinct and displayed fields are capped
at twelve. It inserts or updates the scoped parent explicitly, retaining its ID,
replaces both ordered child lists in batches of at most 1,000, and increments the
version within the same transaction. Version exhaustion fails before mutation.
Any error requires rollback of the entire caller transaction, including prior
owner/audit writes. There is no internal commit or automatic retry.

Reads use one scoped UNION ALL/LEFT JOIN statement, avoiding a cross product and
observing parent and lists in one statement snapshot. Absent options return nil;
empty lists return empty slices. The combined child count is bounded by maxRows
(an empty parent occupies one result row); overflow or ordinal gaps fail without
returning partial options. Shared field locks preserve validation until commit,
but do not implement subsequent archive cleanup. All eventual owner mutations
must follow compatible table-before-field lock ordering.

[Six tests](../../../backend/data-model-service/internal/store/mysql/metadata/options_repository_test.go)
cover identity/JSON retention, unchanged version increments, empty replacement,
scope/active/distinct validation, audit-failure rollback, limits/corruption/version
overflow, 1,001 ordered fields across batch boundaries, and concurrent first
upserts without mixed lists or lost version increments. These are correctness
tests, not workload benchmarks or proof of production query-plan adequacy.

The retained display JSON is deliberately not modified or revalidated here.
DM-01.3d must implement complete direct/relation JSON validation, caption and
archive/link cleanup, optimistic display writes and reordering after deletions
before activation. FK cascades alone can leave ordinal gaps and do not satisfy
that owner protocol. This item does not complete the display migration.

## Index-job persistence (DM-01.1i)

The [job adapter](../../../backend/data-model-service/internal/store/mysql/index_job_repository.go)
implements every IndexJobRepository method on the tenant-bound caller transaction.
It retains nullable table/error/timestamps, ordered repeated column names, schedules
and monotonic attempt counts. Empty columns are an empty JSON array; nil arrays,
null elements, malformed text, exhausted attempts and configured limits fail.
Missing mutations return sql.ErrNoRows; missing or empty dedupe lookups return nil.
Retry clears outcome timestamps/error and preserves the attempt count.

Nonempty dedupe uniqueness remains global through the exact-key dictionary guard.
Lookup is tenant-bound and uses a separate nonunique generated SHA-256 access index
plus full binary equality. Hash equality alone is never identity. This single-table
lookup avoids private dictionary privileges and stale subquery snapshots during
FOR UPDATE reads under REPEATABLE READ. Unlocked reads retain the caller's snapshot
semantics. Empty keys are excluded; absent-key lookup is not a reservation. A racing
create must handle the unique-key failure by rolling back the owner transaction.
READ COMMITTED remains the intended owner mutation isolation level; its missing-key
reads do not require the next-key gap locking used by REPEATABLE READ.

Lists use the tenant/requested_at/id index and reject row or cumulative byte overflow
without partial results. SQL suppresses oversized text/JSON before transfer. Column
count is also limited by maxRows. Input JSON encoding and MySQL's normalized JSON
representation may have different byte lengths; input and stored-read limits are
both enforced. The generated lookup index is an additional storage/write cost;
representative query-plan/load qualification remains outstanding.

State methods preserve the source repository's low-level update semantics. They do
not authorize transitions, claim delivery, publish readiness or append audit events.
In particular, StartAttempt is an unconditional repository transition, unlike the
execution store's terminal-redelivery handling. MarkApplied/MarkFailed/MarkPendingRetry
have no expected-attempt parameter and MUST NOT replace the worker's fenced
IndexJobExecutionStore.CompleteAttempt. Owner retry decisions must hold the job lock
through status inspection, retry, history and durable enqueue. An uncertain commit
must be handled by the owner protocol; the repository neither commits nor retries.

Production grant installation, durable delivery, DDL fencing and runtime adoption
remain open. This repository is staged only.

[Nine real-engine scenarios](../../../backend/data-model-service/internal/store/mysql/metadata/job_repository_test.go)
passed on isolated MySQL 8.4.8: round trips/state transitions; tenant/parent scope;
exact/empty keys and rollback; bounds/order/corrupt JSON/attempt exhaustion;
concurrent duplicate creation; cancelled exact-row locking with independent writes;
current locked reads after an older repeatable-read snapshot; exact/cumulative byte
limits and audit rollback; forced lookup-digest collisions. The full selected
contract/ports/worker/recursive-MySQL regression passed with race detection and
module-wide vet. No production database or PostgreSQL runtime was changed.

## Assembled and published snapshot reads (DM-01.1j)

The [assembled reader](../../../backend/data-model-service/internal/store/mysql/assembled_data_model_repository.go)
binds one tenant and owns a read-only REPEATABLE READ transaction. Nine SELECTs
load tables, fields, enums, links, joined pivot paths, navigation, options parents
and the two ordered option relations, regardless of table/enum count. All reads
share one cumulative source-row/text budget with overflow sentinels; pivot paths
count joined rows and option children count individually. Maps resolve references
without per-table/per-field SQL or repeated linear link searches.

Archived metadata and empty collection shapes are retained. Enum ordering remains
sort_order/created_at/UUID, distinct from ingestion-contract ordering. Missing or
inconsistent aggregate references, corrupt ordinals, cancellation, row/byte limits
and transaction failures return no partial model. A nonexistent tenant returns an
empty raw aggregate, matching the existing repository; the published snapshot path
requires the tenant record and returns sql.ErrNoRows if it is missing.

GetDataModelSnapshot adds tenant and revision inputs to the same snapshot and the
same cumulative budget (eleven SELECTs). NewSnapshotDataModelReadService consumes
that driver-free port and preserves the existing public revision hash algorithm.
It rejects tenant/revision mismatches and propagates snapshot errors without a
partial envelope. This avoids pairing a model with a separately read newer revision.
The PostgreSQL constructor and application wiring remain unchanged. Revision-history
capacity and HTTP error handling remain runtime/acceptance concerns; no token
format or silently truncated history is introduced.

Real-MySQL tests cover every relation, ordering/empty shapes, tenant rejection,
constructor validation, exact shared row/byte limits, archived visibility,
concurrent model and revision snapshots, cancellation/pool reuse, corrupt path
rejection and owner-service integration. Session counters measured exactly nine
SELECTs at both two and 102 tables. This verifies query count, not production
latency, query-plan suitability or throughput. Standalone owner-service tests
verify revision compatibility, scope rejection and failure propagation.

## Remaining module work

Still required in this module: production repository/owner integration;
tenant catalogue/registration runtime integration;
runtime audit/revision integration; owner publication/display
closure; physical registry and DDL recovery; durable MySQL delivery runtime; migration
runner and restricted production grants; all API/import/export/worker/reconcile
and consumer wiring; full correctness and representative performance acceptance.
The service and worker runtime remains PostgreSQL/River.

## Caller-owned publication transactions (DM-01.3b.2d)

`PublicationStore.MutateAndPublishOperationTx` joins an existing READ COMMITTED
owner transaction. It uses the same exact-table lock, condition check, bounded
snapshot, semantic token decision and receipt persistence as the transaction-owning
entry point. It does not open another connection or commit. Every returned value
is provisional until the caller's commit is acknowledged; every error requires
rollback of the entire owner transaction. An uncertain outer commit must be
reconciled with RecoverPublication on a healthy connection using the original
operation ID, hash and condition, after current authorization succeeds.

This enables existing DisplayStore.Save/RemoveReferences callbacks and metadata
repository operations to publish in their transaction, together with history,
tenant revision and durable queue submission. Acquire all affected table locks in
UUID order before combining multiple tables; the publication method also locks
its own table. Never invoke the transaction-owning publication method from an
owner transaction or display callback. No DDL or external network calls belong
inside either callback. Physical readiness and restricted grants remain mandatory
owner preconditions, not facts inferred from metadata or these tests.

Receipt replay skips only the mutation callback supplied to the publication
method. It cannot undo or deduplicate effects executed by its caller beforehand.
An owner must authorize and recover a retained receipt before repeating outer
work (such as a display save), or place all replayable work inside the guarded
callback. Display optimistic versions remain separate from ingestion versions.
This adapter is not a completed display request-replay protocol or owner API.

Six real-engine composition scenarios use the full metadata Definitions, exact-key
triggers, immutable receipt guards and tenant foreign keys. A single-connection
pool proves composition does not acquire another connection. Coverage includes:
all six field types and archived/enum payloads; metadata/history/revision/queue
commit; outer rollback after a provisional result; display rollback on receipt
failure and successful combined commit; archived-field display cleanup with the
same published snapshot; actual lost-COMMIT acknowledgement and replay without
repeating history/queue; and foreign-tenant rejection before callbacks. Receipt
mutation and table deletion with retained evidence are rejected by database guards.

These are full-schema adapter integration tests, not production installation,
authorized business-service tests, physical readiness acceptance or runtime
activation. DM-01.3b/c/d/e remain open until their complete owner paths and consumers
are implemented and verified. DM-01.3a remains the completed payload builder.

### Original Table and Field presentation responses (2026-10-28)

`TableService.PublishPresentationResult` and `FieldService.PublishPresentationResult`
reuse their existing canonical validation and owner effects, while
`MetadataResultPublicationManager` commits the original persisted resource with
its publication. Results use pinned `datamodel.table/v1` and `datamodel.field/v1`
formats in the existing v004 result table; no frozen installation step changes.
The response is read after the owner writes and returned only after commit.
Commit ambiguity returns an error and requires the same command/operation replay.

Every replay reauthorizes before the transaction. A matching publication receipt
reads the immutable original result without repeating owner effects, consulting
current resource state or reopening a newer physical barrier. Missing legacy
results fail closed. Result kind, tenant, table and field identities must match;
byte limits apply to encoding, persisted JSON and recovery reads.

Presentation transactions expose narrow table/field UPDATE implementations:
description/alias/updated_at, plus table semantic_type. They validate the unchanged
identity/enforcement/lifecycle attributes under the existing ordered locks.
`PresentationOwnerPolicy` grants only these UPDATE columns, required reads,
caption-job producer capabilities and INSERT/SELECT receipts/history/revisions.
It excludes DDL, dictionary access, gate/contract mutation, caption/archive/
enforcement updates and receipt UPDATE/DELETE. Deployment account composition
still requires review and trusted tenant/actor authorization.

This closes presentation-response and grant-profile gaps within DM-01.3b/c; it
does not complete either parent. Other owner operations, original creation and
physical-update Field results, lifecycle/retention/retirement, API/runtime
integration and production acceptance remain outstanding.
