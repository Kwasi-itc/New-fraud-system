# Data-model target metadata schema

Status: structural definitions implemented and seven real MySQL integration scenarios passed.
This is the metadata portion of SC-01/DM-01.1. It does not complete runtime DDL,
physical registry mappings, display/publication ownership, migration execution or
deployment permissions. No runtime constructor installs or selects this schema.

Definitions: [metadata package](../../../backend/data-model-service/internal/store/mysql/metadata/schema.go)
and its ordered `sql/` files. The package returns individual server statements;
routine bodies are never split on semicolons. The dictionary definitions precede
owner tables; guards follow tables. There is no `IF NOT EXISTS` that could silently
accept an incompatible object. A checksum/dirty-state migration runner and separate
executable now exist under DM-01.1k.1. The complete reviewed production manifest,
reference definitions and restricted-definer/grant installation remain required;
the definitions are not automatically adopted by the runner.

## Machine-checked source manifest

The [column manifest](../../../backend/data-model-service/testdata/metadata-column-manifest.json)
records all 101 source columns in 12 metadata tables through migration 000008.
Each entry includes its source declaration/file, target table/column/type/file and
conversion rule. Three UUID arrays map to ordered child relations; index-job text
arguments map to ordered JSON; ingestion versions use owner publication instead
of the source UUID default. Logical names and archived flags remain mapped.

The manifest also inventories all 24 named source index/function/trigger
definitions, including the three replacement function definitions in 000007.
Entries point to their structural replacement or staged owner implementation;
function/trigger replacements explicitly retain incomplete owner-integration
status. These references are coverage evidence, not proof of runtime equivalence.
Inline constraints remain in the source declarations and bound schema files;
the constraint/relationship rules below explain their target treatment.

Fifty-one LF-normalized SHA-256 source bindings cover all sixteen up/down metadata
migration files, all 26 metadata SQL definitions, four exact-key SQL definitions,
the two generated definition builders, and v002/v003/v004 upgrade builders. Verification
rejects changed or newly unbound SQL files, missing mappings, duplicate mappings,
source declaration drift, unsupported conversions and broken replacement references.
Review schema changes and update the relevant manifest entries intentionally;
blindly refreshing hashes is not schema acceptance. No migration history is rewritten.

`TestMetadataColumnManifestInstalled` compares every mapped type and nullability
with one bounded information-schema read from an isolated installation.
`TestMetadataIndexByteBudgets` rejects prefix keys, unknown key encodings and
excessive key width/parts, accounting for character encoding, nullable components,
variable-length overhead and a conservative secondary primary-key suffix.
The current base installation has 88 indexes / 205 components, maximum conservative
key bytes 221 and maximum key-plus-primary bytes 237, excluding record/page overhead.
These bounds do not establish query-plan quality, row capacity or production throughput.

The manifest is `needs_review`. It does not complete the dynamic-record schema,
production source-data profiling/import, deployment policies or owner/API acceptance.
DM-01.1 remains open until those requirements are satisfied.
Verification: [focused inventory and installed schema](../evidence/2026-10-28_column-manifest-focused.log),
[expanded metadata/index checks](../evidence/2026-10-28_column-manifest-expanded.log), and
[module vet/full selected race regression](../evidence/2026-10-28_column-manifest-regression.log).
The final regression passed without failures or skips.

## Column mapping

Additional P-01 persistence includes `data_model_physical_tables` (generated mapping,
publication version, epoch and write gate), `data_model_physical_fields` (tenant-consistent
field-component mappings), and `data_model_schema_change_intents` (immutable request/actor/expected
version/desired-shape identity and recovery state). The active-operation FK is a
separate ALTER. These staged definitions and verified preparation/barrier adapters
do not establish physical readiness or production installation. See the
[physical registry contract](01_data_model_physical_registry_contract.md).

The source is the complete set of eight [PostgreSQL metadata migrations](../../../backend/data-model-service/internal/migrations/metadata).
The following rows enumerate all source metadata columns. Target names are unchanged
except the four explicitly listed array transformations. Additional derived columns
and receipt tables are identified separately.

Common rules:

- UUID becomes `BINARY(16)` with network-order bytes, no swap; UUID strings remain
  the external representation. UUIDs remain caller supplied, except private dictionary IDs.
- TIMESTAMPTZ becomes UTC `DATETIME(6)`. Preserve each source column's nullability;
  date-only defaults or server-local time are not substituted. Preflight range and
  precision, and normalize owner clocks explicitly before using strict codecs.
- Unbounded TEXT becomes UTF-8 `LONGTEXT`, with binary-sensitive table collation
  `utf8mb4_0900_bin`; unique identity is derived from full UTF-8 bytes. No string is
  truncated. Packet/memory limits and source collation still need deployment profiling.
- BOOLEAN becomes BOOLEAN with an explicit 0/1 CHECK. INTEGER stays signed INT;
  BIGINT stays signed BIGINT. Defaults below are preserved, not inferred from Go zero values.
- JSONB becomes JSON; SQL NULL and JSON null remain different. JSON defaults are
  expression defaults. Object/array root checks are preserved where present.

| Source -> target | Columns and rules |
|---|---|
| `core.tenants` -> `data_model_tenants` | `id` UUID PK; `external_key` nullable TEXT; `name`, `schema_name` required TEXT; `status` required VARCHAR(20), checked pending/active; `created_at`, `updated_at` required UTC times. |
| `core.model_tables` -> `data_model_tables` | `id` UUID PK; `tenant_id` required UUID; `name` required TEXT; `description`, `alias`, `semantic_type`, `caption_field` required TEXT default empty; `archived` boolean default false; `created_at`, `updated_at` required UTC; `display_version` signed BIGINT default 0. |
| `core.model_fields` -> `data_model_fields` | `id` UUID PK; `tenant_id`, `table_id` required UUID; `name` required TEXT; `description`, `alias` required TEXT default empty; `data_type` required VARCHAR(20), checked against bool/int/float/string/timestamp/ip_address; `nullable`, `is_enum`, `is_unique`, `archived` boolean default false; `created_at`, `updated_at` required UTC. |
| `core.field_enum_values` -> `data_model_field_enum_values` | `id` UUID PK; `tenant_id`, `field_id` required UUID; `value`, `label` required TEXT; `sort_order` signed INT default 0; `created_at`, `updated_at` required UTC. Value remains the original string even for numeric enum fields. |
| `core.model_links` -> `data_model_links` | `id` UUID PK; `tenant_id`, `parent_table_id`, `parent_field_id`, `child_table_id`, `child_field_id` required UUID; `name` required TEXT; `created_at` required UTC. |
| `core.model_pivots` -> `data_model_pivots` | `id` UUID PK; `tenant_id`, `base_table_id` required UUID; `field_id` nullable UUID; `created_at` required UTC. `path_link_ids` becomes ordered `data_model_pivot_path_links` rows, not JSON. |
| `core.table_options` -> `data_model_table_options` | `id` UUID PK; `tenant_id`, `table_id` required UUID; `updated_at` required UTC; `field_display` required JSON object default `{}`; `display_columns` required JSON array default `[]`. `displayed_fields` and `field_order` become ordered child rows. |
| `core.navigation_options` -> `data_model_navigation_options` | `id` UUID PK; `tenant_id`, `source_table_id`, `source_field_id`, `target_table_id`, `filter_field_id`, `ordering_field_id` required UUID; `created_at` required UTC. Joined names remain query projections, not additional stored columns. |
| `core.index_jobs` -> `data_model_index_jobs` | `id` UUID PK; `tenant_id` required UUID; `table_id` nullable UUID; `table_name` required TEXT; `index_type` VARCHAR(20), checked search/navigation/repair; `columns` becomes required JSON array `columns_json`; `status` VARCHAR(20), checked pending/running/applied/failed/cancelled; `requested_by_operation`, `dedupe_key` required TEXT default empty; `error_message` nullable TEXT; `attempt_count` signed INT default 0 with nonnegative CHECK; `requested_at` required UTC; `started_at`, `completed_at`, `scheduled_at` nullable UTC. |
| `core.schema_change_log` -> `data_model_schema_change_log` | `id` UUID PK; `tenant_id`, `resource_id` required UUID; `operation`, `resource_type`, `status` required TEXT; `details` required JSON default `{}` without an added object-only restriction; `created_at` required UTC. Polymorphic resource ID intentionally has no resource-table FK. |
| `core.tenant_schema_migrations` -> `data_model_tenant_schema_migrations` | `id` UUID PK; `tenant_id` required UUID; `version` required TEXT; `applied_at` required UTC. This remains assembled-model revision history, not the new physical migration runner ledger. |
| `core.ingestion_contracts` -> `data_model_ingestion_contracts` | `table_id` UUID PK; `tenant_id` required UUID; `table_name` required TEXT; `version` required UUID; `payload` required JSON. Owner publication supplies the opaque version; no target server default substitutes for semantic token planning. |

The new finite status/type checks reflect values accepted by the domain services,
but are stronger than the old unconstrained TEXT columns. Unknown historical values
must be reported and resolved before loading. They must never be coerced or ignored.
The worker already rejects unknown job states and invalid JSON argument arrays.

## Ordered relations and additional objects

| Target | Shape / invariants |
|---|---|
| `data_model_pivot_path_links` | `(tenant_id,pivot_id,ordinal)` PK, nonnegative signed INT ordinal, required `link_id`; preserves order and repeated link IDs if the owner path contract allows them. Empty source array becomes zero rows. |
| `data_model_displayed_fields` | `(tenant_id,table_id,ordinal)` PK; unique `(tenant_id,table_id,field_id)`; nonnegative ordinal; same-tenant, same-table field FK. |
| `data_model_field_order` | Same structural rules as displayed fields, independently ordered. |
| `data_model_publication_operations` | `(tenant_id,table_id,operation_id)` PK; `request_hash` BINARY(32); `condition_mode` VARCHAR(8) checked any/absent/exact; `expected_version`, `version` BINARY(16); required JSON `payload`. This is new replay evidence, not a source array or migration-history replacement. |
| `data_model_exact_key_buckets`, `data_model_exact_keys` | Private full-value key dictionary; see its [contract](../../../backend/data-model-service/internal/store/mysql/exactkeys/README.md). Internal derived key IDs are not API fields. |

Child reordering is an atomic owner-controlled replacement, not updates that move
existing rows to another identity. Active-field validation, 12-column limits,
path connectivity, display JSON validation and display-version changes remain
DM-01.3d requirements; structural FKs alone do not implement those rules.

`columns_json` is the opaque ordered worker argument payload. Its root must be an
array; the worker adapter additionally rejects JSON null elements and non-strings.
This does not replace relational ownership of model fields.

## Exact uniqueness and tenant relationships

All nine text key domains use BEFORE INSERT/UPDATE guards deriving key IDs from
actual row values. Supplied derived IDs cannot override those assignments.

| Source uniqueness | Target domain / scope / NULL behavior |
|---|---|
| Tenant external key | `dm.tenant.external_key`, explicit global empty scope; SQL NULL maps to NULL key, allowing multiple absent keys. |
| Tenant schema name | `dm.tenant.schema_name`, global; required key. Logical legacy identity is retained, not used to CREATE DATABASE. |
| Table tenant/name | `dm.table.name`, tenant UUID; required key. |
| Field table/name | `dm.field.name`, tenant + table UUID; required key. |
| Enum field/value | `dm.enum.value`, tenant + field UUID; required key. |
| Enum field/label | `dm.enum.label`, tenant + field UUID; separate key and uniqueness from value. |
| Link tenant/child/name | `dm.link.name`, tenant + child-table UUID; required key. |
| Nonempty job dedupe key | `dm.index.dedupe`, global; exactly empty bytes map to NULL derived key. Other strings remain exact and unique. This preserves the existing dedupe string, not a redesign of its SHA-1 producer. |
| Publication tenant/table name | `dm.contract.name`, tenant UUID; required key. Owner writes the current snapshot's table name together with payload/version. |

Required derived-key columns are nullable during BEFORE-trigger assignment but have
IS NOT NULL CHECK constraints. Runtime cannot omit the value after the statement.
Global identity namespaces are separate from tenant-scoped namespaces.

Composite unique parent keys support standard MySQL FKs:

- Tables expose `(tenant_id,id)`; fields expose `(tenant_id,id)` and
  `(tenant_id,table_id,id)`; links and pivots expose `(tenant_id,id)`.
- Fields, options, jobs and publication reference their table with tenant scope.
  A nullable job table keeps a separate tenant FK, so NULL does not bypass tenancy.
- Enum values reference `(tenant_id,field_id)`.
- Link endpoint FKs include tenant, table and field, rejecting both cross-tenant
  references and same-tenant fields belonging to the wrong table.
- Navigation source/filter/ordering FKs include the corresponding table as well.
- Direct pivot field references include its base table. Ordered paths reference
  tenant-consistent pivots and links; path traversal validity is still owner logic.

Guards reject disabled session foreign-key or uniqueness checks, including nullable
branches, and reject changes to object IDs, tenant IDs and immutable parent identities.
History/revision/receipt contents cannot be rewritten by ordinary UPDATE. DELETE
permissions and complete owner-only mutation grants are still a deployment gate.

Source cascade behavior is retained for directly owned children, with explicit
restrictions for new retained evidence and normalized path references: deleting a
referenced path link requires the owner to remove dependent pivots/paths first;
publication receipts prevent table deletion until the retirement/retention protocol
authorizes cleanup. Tenant/table deletion ordering must be tested through that
protocol; this schema is not permission to run a blanket cascade as cutover rollback.

## Index mapping and capacity

UUID parent/FK keys have fixed small byte budgets. Derived unique keys index a
16-byte dictionary identity; raw values are not prefix-indexed. The dictionary's
longest primary key uses 128 data bytes plus internal length overhead. Every
ordinary metadata composite UUID index is below the 3072-byte 16-KiB-page limit.

Enum indexes preserve field/sort/created access and add tenant/field/sort/id for
contract ordering. DM-01.1c also adds tenant/created/id table ordering,
tenant/table/created/id field ordering, and tenant/field/sort/created/id enum
repository ordering. Job indexes cover status/schedule/requested/id and tenant/
requested/id. Navigation retains tenant/source/created and a stable ID tie-breaker.
History and revision indexes add tenant/time/id for future bounded reads. These
are candidate access paths, not measured throughput guarantees. Representative
plans, distribution, lock-wait and pool-budget tests remain DM-01.4.

DM-01.1d/e add tenant/created/id ordering indexes for links, navigation and pivots.
Navigation source-scoped reads retain the tenant/source/created/id index. Pivot
path assembly uses the existing tenant/pivot/ordinal primary key. These definitions
passed structural and repository tests; they are not production migrations or
representative query-plan acceptance.

## Migration and activation gaps

The staged structural lineage now has a frozen 80-step `fraud` reference artifact,
append-only upgrade binding and a read-only schema/definer verification command.
See the [installation contract](../../../backend/data-model-service/internal/store/mysql/installation/README.md).
This completes reproducible artifact/verification tooling, not production review,
all business invariants, deployment roles or application activation.

Staged display definitions add `data_model_display_field_references`, `data_model_display_link_references`
and `data_model_display_table_references`. Their tenant/source/reference tuple keys and reverse
indexes support bounded affected-table discovery. Source table relationships
cascade; referenced fields, links and parent tables RESTRICT deletion until owner
cleanup. Tuple identities have the same immutable/integrity guards as other
metadata. Definitions are not a migration/backfill: existing display JSON and
ordered settings require validated dependency reconstruction before activation.
See the [display contract](01_data_model_display_contract.md).

Staged delivery definitions add `data_model_index_deliveries` and
`data_model_index_delivery_reservations`, plus the job `(tenant_id,id)` unique relationship.
These are new transport objects, not copies of River internal tables. They enforce
tenant/job ownership, fixed kind/payload version, lease/terminal-state shape and
retained delivery identity. Ready, expiry, retention and tenant-list access indexes
are explicit. Retained deliveries RESTRICT job deletion; owner retirement/drain
must satisfy those relationships. See the [delivery contract](01_data_model_delivery_contract.md)
for reservation lifetime, lock order, worker integration and outstanding gates.

DM-01.1i adds the internal generated BINARY(32) column `dedupe_lookup_hash` and
nonunique `(tenant_id,dedupe_lookup_hash)` job access index. Empty dedupe keys
generate NULL; nonempty keys hash their full bytes. Repository lookup must also
compare full binary dedupe bytes. This index does not enforce uniqueness or grant
access to the private dictionary; the existing exact-key guard remains authoritative.
The index supports a single-table current locking read without a stale dictionary
subquery. Production migration, storage cost and representative plans remain open.

Preflight status/type domains, UTC ranges, TEXT byte lengths and source collations;
find existing cross-tenant or wrong-table relationships, duplicate ordered fields,
NULL array elements, broken pivot paths and invalid display references. Resolve
them explicitly before importing with constraints enabled.

Load tenants -> tables -> fields -> enum/link/options -> pivot/path/navigation ->
jobs/history/revisions -> publication/receipts, preserving UUIDs and order. Derived
keys are regenerated through the same restricted triggers; do not copy arbitrary
key IDs. Validate each value and relationship against source snapshots. No fixture
result establishes production data quality or volume compatibility.

Implemented staged components now include checksum/dirty-state migration lineage,
metadata repositories, the physical object/column registry, durable intents and
per-step DDL recovery, final shape/grant verification, publication coordinators
and durable MySQL delivery persistence. See the module checklist for their
individual verification evidence; their existence does not complete SC-01/P-01.

Still required for DM-01.1: reviewed production installation/upgrade and principal
policies, remaining schema invariants, full physical base-table and exact text-key
enforcement, type/enum/backfill/retirement protocols, source-data preflight and
import validation, and API/consumer acceptance including archived fields.
DM-01.2 additionally requires concrete authorized owner integration of intents,
publication, replay, display cleanup and readiness, complete worker recovery and
delivery policy, and executable adoption. Import/export, privacy retention,
failover and representative performance acceptance remain module gates.

The integration fixture intentionally uses a privileged installer for structural
tests. The exact-key fixture separately proves a restricted locked definer and
runtime privilege denial. Neither is a completed production privilege matrix.
