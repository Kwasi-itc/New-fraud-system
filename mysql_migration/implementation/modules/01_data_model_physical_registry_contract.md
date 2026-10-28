# Physical registry and durable change-intent persistence

Status: staged persistence verified under DM-01.1l. Runtime remains PostgreSQL/
River. This is not a completed P-01 executor, publisher or authorization layer.

DM-01.2k.1 adds [durable step execution](../../../backend/data-model-service/internal/store/mysql/physical_execution.md)
with pinned execution locks, checksummed progress and full before/after table
inspection. Nine real-engine recovery scenarios passed, including actual lost
CREATE/ALTER acknowledgements. This executor stops at verifying with the registry
closed. Operation-specific planners, physical grants and publication remain open.

DM-01.2k.2/3 now add [typed field/native-index plans and final verification](../../../backend/data-model-service/internal/store/mysql/physical_planning.md).
The supported planner binds exact reference definitions and records required-column
empty-table preconditions. Final verification checks progress, shape, unexpected
triggers and restricted account policies under the lock through an owner callback.
Full table lifecycle/text/enum plans, concrete deployment policies and actual
authorized publication remain outstanding.

## Mappings

`data_model_physical_tables` binds tenant/table identity to the generated name
`tenant_<tenant label>_<table label>_<32 lowercase table UUID digits>` in the
shared application database. Labels are sanitized creation-time hints; raw logical
names and aliases never become SQL identifiers. Registration creates a
closed, unpublished entry; existence does not establish physical readiness.

`data_model_physical_fields` generates
`field_<field label>_<32 lowercase field UUID digits>_<component>`.
Boolean, integer, float, string and timestamp use `value`; IP uses `address`,
`family` and `prefix`. Physical SQL types, base columns, constraints and index
semantics still require the DDL planner and inspector. Composite FKs enforce
tenant/table ownership; immutable guards reject rebinding. RESTRICT relationships
retain mappings through archive and prevent unsafe metadata deletion.

Tenant labels have a 10-byte ASCII budget; table/field labels have 12. Labels use
lowercase ASCII letters/digits separated by underscores, with safe tenant/table/
field fallbacks when the source has no ASCII label. Physical tables are at most
63 bytes and columns at most 59, retaining the full UUID. Registration reads only
a bounded source-name prefix. Labels are immutable, validated by database guards,
and preserved on repeated registration after logical renames. Identical friendly
labels across tenants still yield distinct physical tables through UUID identity.

Core objects use descriptive `data_model_` names, including `data_model_tenants`,
`data_model_tables`, `data_model_fields`, `data_model_schema_change_intents` and
`data_model_physical_tables`. The previous `dm_`/`dm_r_`/`f_` names were staged
definitions, not deployed application objects. This revision does not rewrite an
installed migration history or rename a live table. Existing applied artifacts
would need an explicit reviewed upgrade; checksum checks remain enforced.

PhysicalRegistry is tenant-bound and transaction-bound. RegisterFields locks the
exact model table, registry, then fields ordered by UUID. It validates at most
1,000 distinct fields per call with one query, reads existing mappings with one
query, and inserts components in batches of at most 1,000. Existing mappings must
be complete and compatible. Reads use one bounded joined statement and reject
incorrect/incomplete components without partial results. Callers must roll back
the whole transaction after any error.

## Writer barrier and intent

LockPublished requires a nonzero expected contract version and takes FOR SHARE
on the exact tenant/table registry. It rejects closed/retired gates, absent or
stale versions and active operations. Consumers must hold this shared lock through
their physical data transaction. Preparing a schema change takes FOR UPDATE on
the same registry row, waits for previous writers, persists the intent, closes
the gate, binds the operation and increments the epoch atomically. Other tables
are independent. No DDL or network callback executes under these row locks.

Preparation currently closes the gate for all changes. Any policy permitting old
read versions during additive work requires separate compatibility verification.
These gates protect only consumers that adopt them; production integration and
restricted grants must prevent bypass of the owner protocol.

`data_model_schema_change_intents` records immutable request hash, actor kind/identity, expected
epoch/version, desired JSON object and operation/tenant/table identity. The active
operation FK is installed by a separate ALTER, which the production manifest must
recover explicitly. Matching replay returns retained state without reopening the
gate or incrementing the epoch; changed request/actor/expectations/shape conflict.
Owners must canonicalize requests and reauthorize every replay. Persisting an
actor does not authenticate it. Shape is internal plan data, not API-supplied SQL.

DDLIntentStore owns the short preparation transaction. Input and normalized
stored JSON are bounded; reads suppress oversized plans on the server and return
no partial result. Pending discovery uses tenant/state/operation-ID keyset pages
of at most 1,000 IDs and the reconciliation index. This supports durable discovery
when queue notification is lost, but does not execute recovery. Commit ambiguity
returns an empty result; a healthy matching replay inspects persisted state.
No application success response is wired to preparation.

## Verification and remaining gates

Descriptive naming verification (2026-10-26): the full selected race suite and
module-wide vet passed after renaming staged SQL objects. Two additional registry
scenarios verify readable names across tenants, stability after logical renames
and repeated registration, maximum identifier length, Unicode fallback and
database rejection of malformed labels (including trailing newlines). The final
metadata race regression, vet and migration-command build passed. Evidence:
[full regression](../evidence/2026-10-26_mysql-descriptive-naming-verification.log)
and [final metadata regression](../evidence/2026-10-26_mysql-descriptive-naming-final-verification.log).

Eleven MySQL 8.4.8 scenarios passed: stable mappings for all six field types,
tenant/parent rejection, 1,000 IP fields/3,000 component rows, batch rollback,
corrupt components, stale/retired gates, bounded reads, immutable replay identity,
previous-writer blocking, independent tables, competing intents, failure after
intent insertion, normalized JSON limits, keyset pages and actual lost-COMMIT
acknowledgement recovery. Module-wide vet and full selected race regression passed.
Readiness gates in tests are opened only through privileged fixture setup; that
is explicitly not a production publisher or proof of physical readiness.

Remaining: deployment-specific production artifacts and complete role grants;
base-column/type/index mappings; operation-specific desired plans and integration
of the staged pinned DDL execution/per-step recovery;
shape/constraint/collation/grant verification; atomic metadata/display/contract/
outbox publication and barrier release; retirement; trusted authorization and
transaction-manager integration; consumer adoption; full crash/DDL/failover and
representative query-plan/load acceptance. Overall P-01 remains unchecked.
