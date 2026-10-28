# Typed physical changes and final verification

These are staged owner components. PostgreSQL/River remain configured. Planner
inputs must come from an authorized tenant/table metadata and registry snapshot;
the planner is not an authorization endpoint.

## Field and native index operations

### Initial table creation reference

`CreateTableReference` generates the initial record table and two exact-key
triggers from the complete, authorized two-field registry snapshot. It requires
the existing `object_id` (required unique string) and `updated_at` (required
timestamp) contracts; foreign, archived, duplicate, extra or altered mandatory
fields fail without returning statements. The physical names remain descriptive
UUID-backed registry names, while API names remain unchanged. The envelope uses
binary UUID `id`, microsecond `valid_from` and nullable `valid_until` (NULL is the
open-ended PostgreSQL infinity mapping). UTC connection/codec rules still apply.

The object ID remains unbounded LONGTEXT. A unique internal `record_object_key_id`
references the shared owner's exact-key dictionary. BEFORE INSERT and UPDATE
derive this key from the actual full UTF-8 bytes, the constant `record_object_id`
domain and the fixed-width tenant/table UUID pair. Caller-supplied keys are
overwritten, including key-only updates. The guards use a local OUT variable and
reject disabled integrity checks. Digest collisions still compare complete raw
bytes; no digest or prefix is treated as equality. Allocation rolls back with
the caller transaction. Dictionary retention obligations also apply to these
object IDs; production retention/data-volume acceptance remains open.

The returned locked trigger-definer policy grants SELECT/UPDATE/TRIGGER only on
the target and EXECUTE only on the shared resolver. The resolver's separate
definer/lineage must be verified independently. The fixture verifies this policy
and denies writer access to dictionary tables, standalone resolver execution and
DDL. A deployment must combine all authorized target grants into a reviewed
complete-account policy, and keep this identity separate from application logins.

`PhysicalTableCreation.Bind` now binds all three isolated reference observations
into a v2 physical plan. It rejects missing, reordered or substituted statements
and requires initially absent objects. The immutable creation identity pins both
field mappings, tenant/table UUIDs and the dedicated definer. Decoding regenerates
the exact SQL and validates it against the plan, then the executor independently
checks the tenant/table against its authorized scope. Version 1 remains table-only.

The [v2 execution protocol](physical_execution.md#version-2-initial-table-creation)
supports durable table/trigger execution and reconciliation. Execution leaves
the registry closed. Explicit `WithCreationReadiness` configuration now adds
shared installation/trigger/definer/installer readiness and atomic owner publication;
the native unconfigured v1 verifier continues rejecting v2. Production policies,
operational installation and runtime integration remain open. Stable owner
allocation and original-result recovery are described in [creation workflow](creation_workflow.md).
Executing only the table step must never be considered ready.
The direct reference fixture tests SQL enforcement; separate recovery tests check
the actual durable executor. Additional fields, arbitrary unique-text transitions
and existing-data backfill require separate plans.

`NewPhysicalPlanner` rejects foreign, duplicate or incomplete mappings,
validates UUID suffixes and copies inputs. Archived fields remain in the complete
validated snapshot so they do not block unrelated active-field changes. Every
typed field mutation rejects an archived target (including mixed active/archived
lists) without returning SQL. Archive does not rename or remove physical columns.
Logical names never become SQL. This boundary does not implement the owner's
archive transaction, display cleanup or publication/read authorization.
`AddFields` renders one bounded ALTER with at most 1,000 new physical columns:

| Type | Physical representation |
|---|---|
| bool | TINYINT with a 0/1-or-NULL CHECK |
| int | Signed INT, preserving the PostgreSQL INTEGER range |
| float | DOUBLE |
| string | LONGTEXT, utf8mb4_0900_bin; no value truncation |
| timestamp | DATETIME(6); the shared UTC codec remains mandatory |
| ip_address | VARBINARY(16), unsigned family/prefix; checked all-null or complete IPv4/IPv6 tuple |

IP checks preserve host bits, require four bytes for IPv4 or sixteen for IPv6,
and bound prefixes to 32/128. Partial NULL tuples fail. Adding any NOT NULL field
records an immutable `empty_table_before` precondition. The executor checks it
with a bounded existence query under the closed registry barrier. A populated
table becomes blocked before any DDL journal entry is created, preventing MySQL
from silently synthesizing zero/empty values. Nonempty tables require a separately
implemented expand/backfill/validate protocol.

`CreateIndex` and `DropIndex` use stable UUID-based `record_index_` names. Native
unique indexes use full values; IP expands to family/address/prefix. At most
sixteen fixed-size parts, each at most sixteen bytes, fit the key budget. This
does not establish query performance. Nullable unique keys retain multiple-NULL
behavior. Field-level native uniqueness uses the field UUID. Text indexes and
enum additions fail with `ErrPhysicalPlanningUnsupported`; no prefix or digest-
only uniqueness is used. Archive must retain mappings; no destructive field
removal is introduced by these helpers.

`CreateFieldUniqueIndex` and `DropFieldUniqueIndex` use the same immutable
`record_unique_<field UUID>` identity as AddFields. They resolve only fields in
the authorized planner snapshot and support complete fixed-width native values,
including the full family/address/prefix IP tuple. The current IsUnique metadata
flag is not authorization or a DDL success signal: the owner must validate
relationship/business dependencies, execute under the durable barrier, verify
the resulting shape and atomically publish the final metadata flag. Unsupported
long-text uniqueness cannot be removed through this native path because its
exact-key state requires a separate protocol. Dropping a field unique index
does not drop independently managed indexes or any physical field columns.

Creating native uniqueness over duplicate values fails rather than truncating,
deduplicating or choosing a winner. Five real-engine lifecycle scenarios verify
duplicate rejection, multiple NULLs, removal, failed recreation over duplicates,
recreation after explicit fixture repair and record preservation. Typed reference
binding, durable six-step execution/replay and the existing required-column data
preconditions also pass. Production collision resolution, concurrent ingestion,
owner integration and representative index-build lock/load acceptance remain open.

`Bind` requires the exact generated statements and complete ordered reference
definitions. Missing/substituted SQL, wrong objects, malformed definitions and
oversized plans fail. Reference observations come from a trusted isolated process,
not acceptance of application-target drift. Complete table-create/type-change/
backfill/retirement plans and dynamic exact-key triggers remain outstanding.

`BindRecordChanges` extends an existing v2/v3 guarded record table with a v3
native plan. It retains both exact-key trigger definitions and the original
creation identity, and stores typed inputs for every native statement. Decoding
regenerates the statements and empty-table preconditions; an ALTER prefix alone
is insufficient. The first before-definition must match the previous final
table definition. Mandatory object ID, timestamp and envelope columns cannot be
changed by this path. Execution checks retained guards before resuming and after
DDL; publication checks the complete desired field/mapping snapshot after owner
effects. See [execution](physical_execution.md). Concrete field/index owner
workflows and operational/load acceptance remain outstanding.

## Native nullability changes

`SetFieldNullable` renders one ALTER for a registered field, preserving the full
native type, text collation and timestamp precision. IP address/family/prefix
components change together; existing Boolean/IP checks and native unique indexes
remain in place. Enum fields and unique long-text fields fail closed because
their additional enforcement protocols are not implemented by this helper.

Allowing NULLs supports populated tables without changing existing values.
Requiring NOT NULL carries the same immutable `empty_table_before` precondition
as required-field addition. The durable executor rejects a populated table before
starting that step, even when its current values are all non-NULL. This deliberate
restriction avoids substituting an unimplemented populated-table validation or
backfill protocol. Owners must not bypass the precondition by executing the SQL
directly. A matching reference chain and pinned execution lock remain mandatory.

The planner does not update metadata, invalidate captions or publish readiness.
Those effects still require the authorized owner transaction after final shape
and grant verification. A nullable flag in a planner snapshot does not prove the
current physical state; the bound before/after definitions provide that check.

## Verification held through the owner callback

Owner code can use the driver-free `ports.PhysicalPublicationManager` capability.
The staged `NewPhysicalPublicationManager` binds one tenant/table, requires the
same pool/database for metadata and publication, and copies reviewed account
policies (including nested column lists). Its pre-lock authorizer must bind the
actual caller to the requested actor and scope on every call, including replay.
The metadata authorizer rechecks local permissions in the exact owner transaction
before new effects. It must not perform network calls while locks are held.

The owner callback receives transaction-bound `ports.MutationStore` repositories
and atomic queue submission, with no SQL transaction handle. Metadata, audit,
revision, job/delivery, contract/receipt and readiness commit together. Missing
queue configuration or legacy schema-manager DDL fails and rolls back all owner
effects. Historical replay reauthorizes and returns the retained result without
rerunning the owner. The store expires with the transaction. Concrete business
callbacks still must validate the operation and limit effects to the authorized
table; multi-table display cleanup/retirement requires a separate coordinator.

Six real-engine adapter scenarios verify commit/replay, denied replay, local
authorization, owner failure, missing queue, foreign tenant writes and legacy DDL
rejection with a one-connection pool. Constructor tests cover pool/database and
dependency mismatches and copying nested policies. These checks complete the
staged publication port binding, not owner-service or executable activation.

`WithVerifiedPhysical` reacquires the same table-specific pinned DDL lock. It
requires the active operation/epoch/version, a closed registry, a verifying
intent, every checksummed step applied and the exact final table shape. Native
v1 plans expect no physical triggers; unexpected triggers are checked separately
because SHOW CREATE TABLE does not include them.

Complete direct-account grant allowlists require distinct unlocked writer and
DDL accounts. Optional readers are read-only. On the physical table, writers
get SELECT/INSERT/UPDATE and DDL accounts get SELECT/CREATE/ALTER/INDEX. Consumer
policies cannot hide DDL permissions on another object. Global/database wildcard
grants, inherited or mandatory roles, grant options, missing permissions and
unrelated excess grants fail closed. The inspector needs privileged visibility
of accounts, grants and all physical triggers. Native v1 physical accounts cannot
have direct routine EXECUTE grants: a definer routine could bypass their DML/DDL
separation even when its grant was included in the account allowlist. Future
routine capabilities require a separate reviewed enforcement contract. Exclude
concurrent administrative privilege changes during verification/publication.

Role separation examines both table-wide and column grants on every object.
Readers cannot hide INSERT/UPDATE in a column list; consumers cannot hide a
REFERENCES capability on another object. The physical target requires the exact
table-wide role privileges above, so a DDL account cannot add column-level DML,
and column grants cannot substitute for the native target permission contract.
Reviewed metadata-column rights for writers remain supported. Real-engine tests
install unsafe but accurately allowlisted grants and verify that the owner
publication callback is never invoked; restoring the safe policy passes.

Only then does the synchronous owner callback run on the same connection while
the DDL lock remains held. It must reauthorize/recheck business conditions and
atomically publish metadata, contracts, audit, receipts, display and queue effects.
Callback and lock-cleanup errors propagate. The helper never opens the registry
or marks an operation published itself. Owner wiring, concrete production role
policies and full publication acceptance remain open.

Real-engine tests cover generated operations for all six types, Unicode/trailing
spaces, host bits, boolean/IP constraints, overflow, uniqueness, nullable keys,
replay and NOT NULL preflight. Verification tests reject missing/excess grants,
schema/journal drift, unexpected triggers, stale epochs and cancellation, retain
the lock through the callback and propagate owner failures. The lost-DDL-ack test
waits for actual server-side lock release before recovery; elapsed time alone is
never treated as fencing a previous executor.
