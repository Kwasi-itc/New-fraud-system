# Canonical owner publication requests

`PublicationRequestHash` defines `dm-publication-request/v1` for these existing
service input values only:

| Input | Operation kind | Included mutation data |
|---|---|---|
| CreateNavigationOptionInput | navigation.create | Tenant ID, source table/field, target table, filter and ordering fields; source table is the receipt scope |
| CreateTableInput | table.create | Tenant ID, name, description, alias, semantic type; stable allocated table ID in scope |
| CreateLinkInput | link.create | Tenant ID, name, both table IDs and both field IDs; child table is the receipt scope |
| CreatePivotInput | pivot.create | Tenant ID, base table, optional field, ordered link path |
| CreateIndexJobInput | index_job.create | Tenant/table IDs, type, ordered columns, requested-by operation and optional scheduled instant |
| UpdateTableInput | table.update | Table ID, description, alias, semantic type, caption field |
| CreateFieldInput | field.create | Table ID, name, description, alias, data type, nullable/enum/unique flags, ordered enum seeds with value/label/sort order |
| UpdateFieldInput | field.update | Field ID, optional description/alias/nullable/enum/unique |
| CreateFieldEnumValueInput | enum.create | Field ID, value, label, sort order |
| UpdateFieldEnumValueInput | enum.update | Enum ID, optional value/label/sort order |

The SHA-256 input is a pinned JSON structure containing a format identifier,
tenant/table scope, stable platform actor UUID, publication condition, operation
kind and explicit body fields. Wire structs are separate from service input
definitions: adding a service field must not silently redefine retained receipt
fingerprints. Golden-format and input-field coverage tests enforce that review.

Optional fields retain null versus explicit empty/false/zero. No trimming, case
folding, Unicode normalization or sorting changes request content. Empty and nil
create-field seed lists both encode as `[]`, since both request no inline values.
Nonempty seed order is preserved. Text must be valid UTF-8 before JSON encoding;
the helper fails instead of allowing silent replacement. Unknown inputs, generic
maps, raw JSON and input pointers are rejected; pass the supported input value.

New pivot paths and index column lists preserve nil versus empty as well as order.
Index scheduling uses UTC JSON timestamps, preserving nanosecond precision; two
time zones representing the same instant yield the same fingerprint. Nil versus
an explicit timestamp remains significant. The caller's timestamp is not modified.
This canonicalization does not promise MySQL timestamp storage precision or permit
the owner to change the original request when recovering a retained receipt.

The context must be assembled from verified platform identity and owner-resolved
scope, never a request-body actor or tenant header. Table IDs in table-update and
field-create inputs must match that context. Field/enum ownership still requires
the owner lookup and authorization; the hash itself does neither. Creation input
tenant IDs must match scope. Link receipts are scoped to the child table and bind
all parent/child endpoints; pivot receipts use the base table; index-job receipts
use the requested table. These scope checks do not authorize traversal or prove
that fields/links belong to those tables. Multi-table owner locking and dependency
checks remain mandatory. Table creation must allocate and retain scope.TableID
once across retries; regenerating an ID after uncertain commit is not recovery.
Session ID and
authorization-version tokens are not part of stable operation identity, permitting
reauthorized retries after session refresh. Current permission must be checked
again on every mutation, recovery and replay. Worker/service actors need a defined
identity contract before their requests can use this helper.

Navigation creation uses the source table as receipt scope and requires all six
input identities. It preserves endpoint roles: exchanging filter and ordering
fields changes the fingerprint. Self-table navigation remains representable.
The owner must still authorize both tables, validate active field ownership and
the supporting reverse link or self-table pivot, and atomically schedule the
target table's managed index. A canonical hash establishes none of those checks.

Use the resulting hash as `PublicationOperation.RequestHash`, with the same
condition, tenant/table and unmodified input in the eventual owner transaction.
The receipt UUID is a separate idempotency key. Do not hash an ingestion payload
instead: aliases and descriptions can change without altering that payload.
Do not log canonical input bytes; they may include sensitive metadata.

This helper performs no business validation, database work or authorization and
is not wired into runtime handlers. Creation fingerprints do not implement identity
allocation, original job/resource-result receipts or service-level replay. Those
owner transaction/result contracts remain open. Deletes/archives,
legacy options and imports still need explicit formats. Display SavePublished uses
its separate dm-display-save/v1 contract. Do not route unsupported commands through
a generic fallback. Preserve v1 for existing retained receipts; an encoding change
requires a versioned compatibility/recovery plan before deployment.

## Tenant registration requests

`RegistrationRequestHash` defines the separate `dm-tenant-registration/v1`
format. Registration has no table publication condition: it binds the stable
owner-allocated tenant UUID, verified platform actor UUID, `tenant.create` kind,
original name and optional external key. The helper accepts only the typed
`tenant.CreateInput`; explicit wire fields prevent future input additions from
silently changing retained fingerprints. Golden and every-field tests pin this
contract independently of dm-publication-request/v1.

Hash the original command before `tenant.New` trims names and normalizes external
keys. Nil, empty and whitespace-only keys remain different requests, as do case,
whitespace and Unicode normalization differences. The helper does not mutate
input. Invalid UTF-8 and zero actor/tenant identities fail explicitly. Business
validation remains with the owner; a fingerprint does not make an input valid.

The operation key, generated timestamps, session tokens and authorization versions
are excluded. Current authorization must still run on every initial call and
replay. The future receipt protocol must retain the operation identity, allocated
tenant identity, original result and fingerprint atomically with registration;
the helper neither allocates nor recovers them. Do not generate a new tenant UUID
or hash normalized database metadata after an uncertain commit. Durable receipt
storage, append-only schema upgrades, retention/grants and owner/API recovery
remain outstanding, and this helper is not selected by runtime registration.

## Table creation before allocation

`TableCreationRequestHash` separately pins `dm-table-creation/v1` over trusted
actor/tenant and the original name, description, alias and semantic type. It
preserves normalization/case/whitespace distinctions, rejects invalid UTF-8 and
bounds input to 1 MiB. It excludes allocated IDs, timestamps and operation key.
The retained operation key locates the allocation; the existing publication hash
then binds its stable table UUID. Golden encoding and every-field distinctions
are tested. `PrepareCreation`/`RecoverCreation` use the
[durable receipt workflow](../store/mysql/creation_workflow.md); ordinary runtime
Create remains unchanged. This table protocol does not finish tenant registration
or receipts for other resource kinds.

## Table presentation updates

Table presentation updates now use the existing `table.update` canonical format
through `TableService.PublishPresentationUpdate`. The method accepts description,
alias and semantic-type edits; any caption input is rejected in favor of the
display dependency coordinator. The metadata publisher requires exact prior
ingestion version, authorizes every initial/replayed command and atomically
persists owner audit/revision with the receipt under the matching open gate.
Semantic, caption or archive changes fail and roll back. Later presentation edits
may retain the same ingestion token; historical replay does not reapply old edits.
The retained result is an ingestion publication, not the original Table API result.

## Field presentation updates

`FieldService.PublishPresentationUpdate` uses the existing `field.update` format
for description and alias edits. It validates active tenant/table/field ownership
inside the publication transaction, trims alias whitespace as the existing owner
does, and commits audit/revision with the publication receipt. Any supplied
nullable, enum or uniqueness flag is rejected, including unchanged values;
enforcement changes require the physical protocol. Current authorization, exact
prior publication and an open matching gate are provided by the metadata manager.
Concurrent duplicates and lost commits recover without repeating owner effects;
historical replay preserves later edits and closed gates. The method returns the
retained ingestion publication, not an original Field API response. Original-result
receipts, restricted field-owner grants and runtime entry points remain open.

## Prepared native nullability updates

`FieldService.PublishPreparedNullabilityUpdate` finalizes the existing native
nullability plan using the canonical `field.update` request, trusted API actor,
field identity and exact prior publication. The caller must prepare and durably
execute the typed v3 plan through the closed barrier first. The physical manager
verifies shape, retained guards, shared lineage, installer/grants and applied
journal under its execution lock through atomic owner publication.

The owner validates active tenant/table/field relationships and rejects reserved
mandatory fields, enum fields, enum/uniqueness input and missing nullability.
Description/alias edits may accompany the nullability command; alias trimming is
preserved. Field metadata, audit and tenant revision commit with the contract,
receipt and readiness. Current authorization runs on replay; changed commands
cannot reuse the receipt. Lost publication commits recover without repeating effects.

Tightening remains empty-table-only. A populated table is blocked before a DDL
journal entry and retains its rows and old metadata; no implicit data repair is
performed. Relaxation works on populated tables and retains existing values.
Enum/unique-text enforcement and populated tightening need their own protocols.
Original Field API-result retention, preparation/entry-point wiring and production
principal/operational acceptance remain open. The new method is staged; it does
not change the existing PostgreSQL API mutation path.

## Prepared native numeric uniqueness

`FieldService.PublishPreparedUniqueEnable` finalizes a completed typed v3 unique
index operation for integer or float fields. It binds the original `field.update`
request, field, trusted actor and exact prior publication. The verified physical
manager retains the execution lock through metadata, audit, tenant revision,
publication receipt and gate opening. The shared native-owner implementation
validates active scope and rejects reserved or enum fields. This method accepts
only is_unique=true, with optional description/alias; it rejects nullability/enum
flags, removal of uniqueness and nonnumeric fields. The owner does not execute DDL.

Existing duplicates make physical index installation fail with readiness closed;
records and the old metadata flag remain unchanged. Recovery may retry the same
durable operation only after an explicit, separately authorized data correction.
The acceptance fixture makes such a correction explicitly; application code never
chooses a duplicate winner or repairs records automatically. Published nullable
numeric fields allow multiple NULL values and reject duplicate INSERT and UPDATE.

Current authorization runs on replay, and lost commits recover without repeating
owner effects. This returns an ingestion publication, not an original Field API
response. Preparation/API integration, retained Field results, relationship-aware
uniqueness removal, long-text exact-key enforcement, production grants and
operational acceptance remain open. Runtime remains PostgreSQL/River.

## Allocated native field creation

`PreparedFieldCreationRequestHash` pins `dm-prepared-field-creation/v1` with the
retained field UUID and the complete existing canonical field-create command.
That command includes trusted actor/tenant/table and the exact prior publication
version. Changed allocation IDs, actor, original attributes or version cannot
reuse the publication receipt. This is an allocated-command fingerprint, not a
pre-allocation retry key. `FieldService.PublishPreparedCreation` validates it
before calling the authorized table-bound manager, and commits audit/revision
effects with the verified physical publication. It returns the original ingestion
publication on replay; stable field allocation and original Field-result receipts
remain outstanding. Existing API Create behavior is unchanged.
