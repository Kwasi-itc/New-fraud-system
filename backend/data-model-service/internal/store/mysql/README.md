# Staged data-model MySQL publication adapter

The [installation builder](installation/README.md) now supplies the current
structural lineage, explicit locked definers and exact definer-grant inspection.
The [physical step executor](physical_execution.md) adds durable CREATE/ALTER
progress and lost-acknowledgement recovery under a pinned table-specific lock.
Neither activates runtime or completes physical readiness/publication.

[Bounded physical recovery](physical_reconciliation.md) adds an explicit operator
command for existing intents, with per-operation deadlines, partial error reports
and state-specific continuation cursors. It keeps the registry closed and is not
scheduled or selected by the production application.

`DeliveryInspector` and the explicit `inspect-deliveries-mysql` command provide
bounded tenant-scoped delivery/job inspection with read-only credentials. They
show transport/domain divergence and reservations without exposing lease tokens
or loading histories. See the [delivery contract](../../../../../mysql_migration/implementation/modules/01_data_model_delivery_contract.md).
Operator authorization, audited redrive and runtime integration remain open.

[Typed physical planning and verification](physical_planning.md) now cover six
field types, native indexes, NOT NULL preflight and final schema/grant checks
under the execution lock. The installation builder includes a frozen 80-step
`fraud` reference artifact and a read-only schema/definer verification command.
Production policy review, unsupported schema operations and owner integration
remain explicit gates.

Guarded native changes use a v3 plan with regenerated typed SQL and retained
initial exact-key triggers. A separate temporary native installer has exact
table ALTER/INDEX/TRIGGER privileges and no SET_ANY_DEFINER capability. Final
publication verifies locked installer state, shared lineage, guards and the full
field/mapping postcondition. `FieldService.PublishPreparedCreation` now supplies
the native-field finalizer with canonical allocated identity, original attribute
validation and atomic audit/revision effects. Durable field allocation/result
recovery, complete index-owner workflows and runtime adoption are still incomplete.

Core SQL objects use descriptive ownership names such as `data_model_tenants`,
`data_model_tables`, `data_model_fields` and `data_model_schema_change_intents`.
Tenant record mappings use `tenant_<tenant label>_<table label>_<full table UUID>`;
field mappings use `field_<field label>_<full field UUID>_<component>`.
Labels are sanitized creation-time hints and remain fixed after logical renames.
Tenant IDs, foreign keys and authorization enforce isolation in the shared
application database. See the [physical naming contract](../../../../../mysql_migration/implementation/modules/01_data_model_physical_registry_contract.md).
These staged definitions do not rename deployed PostgreSQL objects or authorize
rewriting installed MySQL migration history.

Tenant-bound display settings snapshots, strict optimistic saves and indexed
field/link dependency cleanup are now staged in `DisplayStore`. See the
[display contract](../../../../../mysql_migration/implementation/modules/01_data_model_display_contract.md)
for required owner callbacks, lock order, validation differences, backfill and
uncompleted record-reader/runtime gates. The existing PostgreSQL display runtime
is unchanged; this adapter is not automatically selected by repository wiring.

`NewDisplayPublicationManager` now binds the concrete TableService display owner
to same-pool settings/publication/metadata, with authorization on every replay
and local authorization inside the transaction. Managed caption jobs/deliveries
and tenant revisions commit with settings, dependencies, audit and receipt.
`installation.DisplayOwnerPolicy` supplies the tested staged grant profile;
deployment review, complete lifecycle/record readers and API acceptance remain open.

Tenant, table, field, enum, link, navigation, pivot, history and revision persistence is also available through the transaction- and
tenant-bound `NewMetadataRepositories` factory. See the
[repository contract](../../../../../mysql_migration/implementation/modules/01_data_model_repository_contract.md)
for its limits, error behavior and verification. These repositories do not replace
the owner publication/display/DDL protocol and are not selected by runtime wiring.

The staged `NewTransactionManager` exposes those repositories through the service
mutation port with mandatory in-transaction authorization and same-pool durable
queue submission. `RunWithTx` supports explicit owner SQL/publication composition.
Its legacy schema-manager interface rejects every physical operation, including
inspection, with `ErrPhysicalProtocolRequired`. Runtime authorization, complete
owner protocols and application wiring remain pending; see the repository contract.

`MetadataPublicationManager` supports an exact-version, existing-table presentation
command with current authorization, atomic owner effects/receipt and an open
physical registry gate held through commit. Semantic-token, caption and archive
changes are rejected and rolled back. `TableService.PublishPresentationUpdate`
supplies the description/alias/semantic-type owner with audit/revision effects.
Historical publication replay skips effects and preserves later edits/barriers;
original Table response receipts and production entry-point integration remain open.
`FieldService.PublishPresentationUpdate` uses the same manager for description/alias
edits with active tenant/table/field ownership checks and atomic audit/revision.
It rejects enforcement flags; original Field response receipts, restricted field
owner policies and runtime integration remain open.

Platform-wide tenant enumeration uses the separate read-only `NewTenantCatalog`
adapter after explicit caller authorization. The scoped tenant repository cannot
enumerate or mutate other tenants; runtime integration must preserve this split.

`TenantCatalogReader` supplies the service-level read-only listing capability:
each bounded snapshot requires explicit catalogue authorization. Select it with
`TenantService.WithTenantCatalog` to preserve global listing without widening
tenant lookup/mutation scope. Default PostgreSQL construction remains unchanged.
Production identity SQL, API paging and runtime selection are still pending.

`RegistrationTransactionManager` provides a separate creation capability selected
with `TenantService.WithTenantRegistrationManager`. The owner allocates a nonzero
tenant ID before the transaction is bound and authorized; existing scopes and
callbacks that do not leave a pending tenant are rejected. Configuration is copied
per call. This does not implement durable registration receipts or safe replay
after uncertain commit; those gates still block runtime adoption.

The separate `dm-tenant-registration/v1` canonical fingerprint helper now binds
the original typed creation input, allocated tenant ID and trusted actor ID.
See [tenant registration requests](../../service/publication_request.md#tenant-registration-requests).
It is not a receipt store and does not change runtime registration behavior.

`PublicationStore.MutateAndPublish` implements the metadata transaction portion
of ingestion publication for one **existing** model table. It acquires the exact
tenant/table row lock before invoking the internal owner mutation, loads the
complete table/field/enum snapshot, and writes the payload/version in that same
READ COMMITTED transaction. Results are returned only after acknowledged commit.
All SQL uses a validated, quoted database name and fixed owner table names; values
are bound parameters. `mysqlstore.CommitError` propagates without automatic retry.

The table lock also covers first publication when no contract row exists. Every
metadata writer must participate in that lock protocol. The callback is an
internal adapter boundary, not an authorization mechanism or an endpoint for
arbitrary SQL. Restricted grants and complete service wiring are still required
to prevent bypass. The table guard coordinates publication; it must not become a
general lock for ingestion, case work or unrelated records.

Fields and enum values are loaded in two queries, including archived fields.
Configured count limits use a one-row sentinel and abort the transaction when
exceeded. They never publish a truncated model. Limits are not new public model
size rules: runtime settings require profiling and compatibility review. Enum sorting is performed
by the portable contract builder. Snapshot queries depend on tenant/table/field
indexes and tenant-composite foreign keys.

The constructor also requires an explicit `maxBytes` (1..64 MiB; no default).
This is an adapter safety ceiling, not an approved product or deployment limit.
It independently bounds cumulative selected metadata text and each publication
JSON representation. `CASE`/`OCTET_LENGTH` projections return SQL NULL instead of
an oversized table name, field text pair, enum value/label pair, prior payload or
receipt payload. NULL is rejected with `ErrSnapshotLimit`, never published as
empty data. Streaming row checks debit a shared byte budget across table, fields
and enums before retaining each row. Counts bound structural overhead separately.
These controls bound retained input, not total Go heap or MySQL execution memory;
JSON construction/escaping and driver buffers still consume additional memory.

New JSON must fit both Go's encoded bytes and MySQL's normalized textual bytes.
Changed publications use one additional scalar query,
`SELECT OCTET_LENGTH(CAST(? AS JSON))`, before persistence. This avoids committing
a result that cannot subsequently be read under the same limit. MySQL inserts
whitespace when displaying normalized JSON; see its
[JSON normalization documentation](https://dev.mysql.com/doc/refman/8.4/en/json.html#json-normalization).
The integration fixture verifies the actual MySQL 8.4.8 boundary. This measures
text returned to the driver, not binary on-disk JSON storage size.

A limit failure rolls back owner mutations/publication/receipt together. An
oversized prior or replay receipt fails before invoking the owner callback.
Reducing limits below retained receipts blocks recovery: restore a compatible
limit after capacity review and reuse the same operation identity. Never treat
this error as absence of a receipt or permission to run a new operation.
Before runtime adoption, inventory existing payloads and benchmark representative
wide models, enum text, escaping, concurrent publications and receipt reads. Align
limits with packet size, timeouts, pool concurrency and available memory; retain
headroom below `max_allowed_packet`. The 1 MiB test setting is fixture-only.
The projection guards do not eliminate database reads/sorts; query plans and
latency/memory measurements remain outstanding. No performance gain is claimed.

The [MySQL locking-read reference](https://dev.mysql.com/doc/refman/8.4/en/innodb-locking-reads.html)
documents transaction-held FOR UPDATE locks. The concurrency tests exercise this
protocol on MySQL 8.4; the lock is not a substitute for owner authorization.

## Fixture and remaining adoption gates

### Durable metadata-operation replay

`MutateAndPublishOperation` adds an operation receipt to the metadata/publication
transaction, including unchanged-publication operations. Its key is
`(tenant_id, table_id, operation_id)`; its request fingerprint is a SHA-256 value
supplied by the owner. The owner must define a versioned canonical request that
includes the operation kind, expected version, mutation inputs and trusted actor.
Hashing only ingestion JSON would miss presentation-only mutations. Fingerprint
construction is available for ten owner input types in the
[canonical request contract](../../service/publication_request.md): table create/update,
field create/update, enum create/update, link/pivot/navigation creation and index-job creation.
Creation-result receipts, other operation formats and runtime
adoption remain pending. Endpoint authorization is not implemented by this adapter.

Each operation must also choose an explicit `PublicationCondition`: `absent`
requires no existing ingestion publication, `exact` requires a nonzero matching
contract UUID, and `any` explicitly permits whichever contract exists. The zero
value and ambiguous combinations fail validation. Owner APIs must choose the
appropriate policy; do not silently turn a missing client precondition into
`any`. The unrestricted mode is for deliberately authorized operations whose
invariants are validated against current metadata under the table lock.

For a new operation the adapter reads the current publication after acquiring
the table guard and checks the condition **before** invoking the mutation. A
mismatch returns `ErrPublicationVersionConflict`, with no mutation or receipt.
The receipt records condition mode and expected version separately from the
request hash, so reusing an operation with a weakened condition conflicts even
if the caller accidentally keeps the same fingerprint. Replay checks the saved
request/condition first and returns the original result without checking it
against a later publication. This preserves recovery after intervening edits.

This is an **ingestion-contract** condition, not a complete metadata revision or
a DDL-operation fence. Presentation changes can leave the ingestion token
unchanged. Display concurrency, field-specific invariants, physical readiness
and the P-01 operation/version checks still require their own owner enforcement.
Do not claim this condition prevents every stale metadata change.

A retry acquires the same table guard, then returns the saved version/payload for
a matching operation without invoking the mutation. A different fingerprint
returns `ErrPublicationOperationConflict`. An old operation returns its original
result even when a later edit has changed the current publication. This is a
replay result, not a claim that the saved version is still current.

After `mysqlstore.CommitError`, use `RecoverPublication` on a healthy primary
connection with the same identity, following fresh authorization. It waits for
the original table lock and reads the receipt without invoking a mutation.
`sql.ErrNoRows` is not permission to invent a new operation ID: explicit retry
must use the original ID/fingerprint through `MutateAndPublishOperation`. Other
errors and cancellations do not establish rollback. Neither method retries
automatically. The original `MutateAndPublish` remains a low-level, untracked
transaction primitive and cannot provide receipt-based recovery by itself.

Receipts are metadata-transaction results, **not** durable DDL intents. They do
not certify physical readiness or make DDL/network delivery transactional. Full
adoption still needs immutable/restricted receipt access, canonical requests,
retention/capacity policy and coordinated retirement. Do not delete receipts or
their model-table guard during the supported recovery window. The fixture uses
a restrictive FK with no cascade; production retirement/history design remains
open. Each saved result duplicates the contract JSON, so storage growth must be
measured before choosing a retry window or cleanup policy.

Run from the repository root:

```powershell
docker compose -p data-model-mysql-test -f backend/data-model-service/compose.mysql-test.yaml run --rm test
docker compose -p data-model-mysql-test -f backend/data-model-service/compose.mysql-test.yaml down --volumes
```

The publication fixture uses a dedicated `data_model_publication_test` database, tmpfs, no published
ports or application networks, and explicitly permits plaintext only there. It
creates fresh tables; repeating the suite requires a fresh fixture. The schema
contains the publication columns/relationships and deliberate failure constraints
needed by those tests. Separate exact-key and metadata fixtures create and remove
generated test-only databases in the same isolated server. They test the dictionary
with restricted locked definers, and the target structural schema with source-column
coverage, tenant/table FKs and publication/worker integration. The publication adapter
persists table_name alongside version/payload, including when the model name changes.
See the [metadata manifest](../../../../../mysql_migration/implementation/modules/01_data_model_metadata_schema.md).
These definitions are **not** an activated production migration lineage. Full owner
enforcement, physical readiness, display and consumer views remain required.

The separate [migration runner](migrations/README.md) and `cmd/migrate-mysql` now
provide ordered checksums, dirty-state recovery and exact object inspection.
Complete reviewed production manifests and restricted grants/definers remain
required; runtime does not invoke this installer or adopt MySQL automatically.

`PhysicalRegistry` and `DDLIntentStore` stage generated mappings, shared
published-version gates and atomic durable intent/barrier preparation. They do
not execute physical DDL, release barriers or publish model changes. Owner and
consumer wiring remains required; see the
[physical registry contract](../../../../../mysql_migration/implementation/modules/01_data_model_physical_registry_contract.md).

Initial v2 creation now has an explicitly configured `WithCreationReadiness`
publication path. It verifies complete source-bound shared installation lineage,
all three physical objects, exact locked definers and an inactive temporary
installer while retaining the execution lock through atomic owner publication.
`TableService.PublishPreparedCreation` validates the original command and writes
audit/revision effects for an already prepared/executed operation. Restricted DDL,
grant-denial and lost-trigger/commit recovery tests pass in the disposable fixture.
The [durable owner workflow](creation_workflow.md) additionally supplies retained
allocation, original-result receipts and recovery across lost commits. Operational
reference/installer orchestration, production privilege deployment and runtime
activation remain open; see the
[creation protocol](physical_execution.md#version-2-initial-table-creation).

No application constructor imports this adapter. Before adoption implement and
verify production migrations, owner-restricted mutation APIs/grants, durable DDL
intent/readiness and operation fencing, new-table creation and retirement,
display cleanup, cache/outbox coupling, service-level commit recovery and all
entry points. A model-table row existing in this fixture does not establish
physical tenant-table readiness. Preserve PostgreSQL runtime until these gates
and shared consumers are ready. Do not point this fixture at an application DB.

The staged TableOptionsRepository preserves ordered fields, existing identity and
display JSON, with tenant/active-field checks and atomic version increments. Its
six real-engine scenarios passed with the selected regression and race detection.
The full display owner protocol (JSON validation, caption/archive/link cleanup and
optimistic writes) remains required; this adapter is not wired into runtime.

The factory also supplies a tenant-bound IndexJobRepository. Nine MySQL scenarios
and the full selected race-enabled regression passed, including old-snapshot
locked lookup, forced lookup-digest collisions, rollback and concurrent duplicates.
A nonunique generated hash index narrows lookups before full binary comparison;
it neither replaces dictionary uniqueness nor requires private dictionary access.
These are metadata methods, not durable delivery. Worker outcomes must use the
attempt-fenced execution store with atomic history. Production migrations/grants,
owner retry locking, DDL readiness and runtime adoption remain outstanding.

`NewDataModelReadRepository` supplies tenant-bound assembled reads in one read-only
snapshot with nine bounded SELECTs and a shared row/text budget. Its published
snapshot method includes tenant/revision inputs in the same snapshot; the new
snapshot-aware service constructor preserves revision hashing. Application wiring
has not switched. See the repository contract for limits and test evidence.

`IndexDeliveryStore` provides staged atomic enqueue, cross-queue reservations,
claim/heartbeat, fenced local completion, retry/cancellation and cursor-based expiry/
retention maintenance. `LeasedIndexJobExecutionStore` binds worker attempts/outcomes
to a committed lease and commits job/history/delivery changes together. See the
[delivery contract](../../../../../mysql_migration/implementation/modules/01_data_model_delivery_contract.md).
Neither adapter replaces P-01 physical DDL fencing or constitutes an enabled worker:
operational tooling, grants, migration lineage, River drain and runtime
wiring remain required. No application constructor selects these adapters.

Staged worker polling/supervision and expiry/retention scheduling now exist in
`internal/worker`. Maintenance follows independent cursors through locked pages,
bounds each cycle and propagates partial/ambiguous failures. These primitives do
not complete source-compatible retry/rescue semantics or executable integration.

Audited discarded-delivery redrive is staged in `delivery_redrive.go` and the
explicit `cmd/redrive-delivery-mysql` operator command. It preserves domain attempts,
creates delivery/reservation/history atomically, and replays a source-scoped request
after uncertain commit or source retention cleanup. Pending jobs only; running jobs
need physical reconciliation and terminal jobs need owner retry. The operator must
authorize the tenant on every call. See the delivery contract for required inputs,
limited column grants for MySQL locking, replay semantics and outstanding deployment
policy support. This does not activate the MySQL API or worker runtime.

`PublicationStore.MutateAndPublishOperationTx` composes publication and receipts
with an existing owner/display READ COMMITTED transaction. It returns a provisional
result and never commits; callers must roll back on every error and reconcile an
uncertain outer commit by the original operation identity. Full metadata-schema
integration verifies atomic audit/revision/queue/display effects on one connection.
Replay skips only its supplied mutation callback, not earlier outer effects.
Authorization, physical readiness, all owner entry points and runtime consumer
compatibility remain required; see the repository contract's transaction section.

`PhysicalExecutor.PublishVerifiedOperation` now provides the staged single-table
commit boundary from verified physical work to publication. It requires fresh
authorization, binds the intent's actor/hash/version, holds the pinned DDL lock,
and atomically commits owner effects, contract/receipt, published intent and open
registry version. Historical replay never reopens a newer barrier. Archive and
orphan-receipt states fail closed. See [physical execution](physical_execution.md)
for the owner semantic checks, multi-table coordination, grants and runtime work
still required before activation.

`DisplayStore.SavePublished` adds canonical display-command receipts, mandatory
authorization before locks on every replay, and atomic settings/owner effects/
audit/publication. New saves require an open physical gate matching the contract
and must leave its ingestion token unchanged. Lost commit acknowledgement can be
recovered without repeating effects or failing the old display-version condition.
It returns the original ingestion receipt, not saved settings; production API
response integration and concrete owner callbacks remain open. See the
[display contract](../../../../../mysql_migration/implementation/modules/01_data_model_display_contract.md#authorized-display-publication-coordinator).

Original settings response recovery is available through the staged
`TableService.PublishDisplaySettingsResult` path, using append-only v004 owner
result receipts and the complete 89-step dependency verifier. It requires the
separate locked result definer and SELECT/INSERT result-owner policy. Historical
publication-only operations fail closed instead of returning current settings.
See the [display result contract](../../../../../mysql_migration/implementation/modules/01_data_model_display_contract.md#original-display-results-v004-2026-10-28).
The reference artifact remains needs_review; API/runtime deployment, remaining
owner results/lifecycle and production acceptance are still incomplete.
