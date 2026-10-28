# Staged MySQL display persistence and dependency cleanup

Status: implemented and fixture-verified adapter; runtime remains PostgreSQL.
This is DM-01.3d.1, not completion of DM-01.3d or the data-model module.

## Ownership and reads

`internal/store/mysql/display_store.go` supplies tenant-bound `DisplayStore.Get`
and `Save`. Construction requires an explicit database and row/byte budgets.
The caller must authorize the tenant; UUID scoping is not authorization.
All objects remain in the shared logical application database.

Get owns a read-only repeatable-read snapshot. Active tenant/table metadata,
options, eligible relationships, active fields and enum labels are read with
shared row/text budgets. Server projections suppress oversized JSON/text before
transport. A failure returns an empty result, not partial settings. Parent fields
and enums are loaded once per distinct parent table, in batches of at most 1000,
and shared by its links. Five SELECTs were measured with both one and 101 links
sharing a parent. Larger distinct-parent sets add bounded batches, not per-link
queries. This measurement is not a production latency or throughput result.

Eligible relationships require an active parent table, active same-tenant child
and parent key fields, a unique parent key and matching endpoint types. Related
target fields must be active on that exact parent. Physical record reads and
registry resolution are not implemented by this settings adapter.

## Validation and save transaction

`display.ValidateStrict` validates the exact target UUID, caption string field,
column identity/type, Unicode label limits, direct/relation shape and every
legacy list/map even when new display columns are present. It returns independent
slices, pointer values and normalized labels without mutating caller data.
Repeated direct columns with distinct column IDs remain legal; derived legacy
lists retain each field once in first-occurrence order.

This explicitly replaces checks supplied by PostgreSQL triggers. Its API validator
alone has a permissive related-caption fallback, but migration 000007 rejects an
invalid target UUID. MySQL cannot rely on that absent trigger. Mixed invalid
legacy/new representations and extraneous column-reference properties are rejected;
source data preflight and API parity acceptance must account for that stricter
validation, rather than silently importing invalid references.

Save owns a READ COMMITTED transaction and requires the expected display version.
It discovers requested parent tables and, for a caption change, indexed display
dependents. It locks exact table rows in UUID order, then rereads/revalidates state
and dependencies. A newly discovered edge outside the locked set aborts with a
conflict. No tenant-wide lock or automatic mutation retry is used.

Cleanup and caption clearing also discover and lock the parents of surviving
relationships before rebuilding their FKs. This parent set is rechecked under
the locks; a newly introduced unlocked parent causes a conflict. The final
metadata regression and module-wide vet passed after this lock-order refinement.

Both ordered lists, JSON settings, reverse dependencies, caption/version and the
display audit event commit atomically. Existing options identity is preserved.
An options upsert increments the version even if unchanged; a changed source
caption adds its own increment. Dependent display versions change with a parent
caption. Clearing a parent caption removes legacy linked-caption choices while
preserving explicit relation columns that select a still-valid target field.
Version exhaustion fails and rolls back all affected tables.

The required owner-effects callback runs on the same SQL transaction. Production
integration must supply authorization/revalidation and caption managed-index
request/delivery semantics; tests use a no-op callback only to isolate persistence.
Callbacks must not commit, perform physical DDL/network calls or invert lock order.
The adapter inserts its display audit event after owner effects, so an audit failure
rolls back those effects too. It does not establish full owner publication or
operation-receipt integration.

## Reverse dependencies and field/link removal

Three new relations record field, link and parent-table dependencies:
`data_model_display_field_references`, `data_model_display_link_references`, `data_model_display_table_references`.
Each has tenant-consistent FKs, immutable tuple guards, a source-table primary key
and an index beginning with tenant and referenced identity. Referenced resources
use RESTRICT; source-table retirement requires explicit dependency cleanup.
Parent captions are tracked by parent-table dependency because the chosen caption
field may change independently of the child's saved configuration.

`RemoveReferences` supports field archive/delete and link deletion. It discovers
affected tables by those indexes and relationship endpoint indexes, locks them in
UUID order, and rechecks the dependency set. Settings and ordered lists load in
bounded batches. Cleanup preserves JSON/list ordering, removes direct/target
references to an archived/deleted field, removes columns using deleted or invalid
key relationships, clears stale captions, strips legacy link choices while
preserving labels, and rebuilds dependencies atomically with owner effects.

The owner callback must actually archive/delete that field or delete that link.
A postcondition rejects unfinished work and rolls back cleanup. Callback failure
also rolls back versions and every affected display. The callback remains
responsible for the owner's schema-change audit, publication and revision work;
it is not an arbitrary external SQL execution interface.

All metadata writers must participate before activation. Existing low-level
repository updates and table-options upserts do not automatically maintain these
reverse references. Imports need validated dependency backfill. Table retirement,
table archive/restore and every owner field/type/unique/enum/alias mutation still
need integration and acceptance. RESTRICT FKs must not be disabled to bypass it.

## Failure and verification evidence

Uncertain COMMIT returns `mysqlstore.CommitError` and no successful settings result.
The lost-acknowledgement test consumes a real server COMMIT OK before returning EOF.
Healthy reads observe committed settings and audit; ordinary Save replay using the
stale expected version fails before owner effects. SavePublished, described below,
adds canonical operation-key recovery; production owner/API binding remains open.

Eighteen real-engine scenarios and five strict-validation scenarios passed with
race detection on MySQL 8.4.8/Go 1.25.8. Coverage includes validation, tenant/bounds,
atomic save, exact related target, concurrent optimistic writes, owner/audit failure,
caption invalidation/clearing, field/link cleanup, unfinished owner rejection,
cancelled competing lock waits, version exhaustion, lost COMMIT acknowledgement,
five-query reads and single-slot pool reuse. Module-wide vet and the full selected
contract/ports/worker/recursive-MySQL regression passed.

The first cleanup test exposed a MySQL JSON_TABLE error over a filtered derived
table. Reference rebuilds now use direct owner-table joins with tenant and bounded
table-ID predicates in every branch. The corrected queries passed real-engine
cleanup and regression tests; no SQL error was ignored.

Remaining: production lineage/grants/backfill; every owner mutation and index/
publication/receipt callback; table lifecycle; authorized physical record readers;
HTTP/import/export integration and PostgreSQL differential acceptance; dependency
insertion/removal race coverage across all entry points; representative query plans,
fan-out limits, load and operational recovery. No application DB was changed.

## Authorized display publication coordinator

`DisplayStore.SavePublished` is a staged internal coordinator. The required
authorization callback authenticates the trusted actor and authorizes the resolved
tenant/table on every call, including historical replay, before any database
transaction. The display and publication stores must share the same pool/schema.

The pinned `dm-display-save/v1` request hash binds tenant, table, actor, explicit
ingestion condition, expected display version and every display input. JSON object
keys sort deterministically; list order, exact strings, absent versus empty
collections and optional identities remain significant. Invalid UTF-8 is rejected
instead of silently replaced. Future DTO changes require explicit format review.

Receipt recovery precedes current topology discovery and optimistic validation.
If absent, the save locks affected model tables in UUID order and rechecks the
receipt before any effects. It requires an existing contract and an open physical
registry gate matching that contract; the shared gate remains held through commit.
Settings, reverse references, caption invalidation, owner SQL effects, display
audit and operation receipt commit atomically. An ingestion-token change rolls
back the transaction: semantic physical-model changes require the verified DDL
publication coordinator. Callbacks must not manipulate the registry, commit,
perform DDL/network calls or invert the documented lock order.

Historical replay returns the original ingestion publication receipt without
repeating display changes, audit or owner effects. It may succeed during a later
physical barrier and does not reopen it or certify current physical readiness.
The result is **not saved/current display settings**; current settings require a
separate authorized read. This is not a drop-in replacement for the existing HTTP
response contract. Ambiguous commit returns an error; retry the same operation and
exact command after fresh authorization. Do not allocate a replacement operation
ID merely because the acknowledgement was lost.

Concrete business authorization, revision/index/queue callbacks, API response and
replay integration, display cleanup/table lifecycle commands and runtime wiring
remain required. The ordinary Save and RemoveReferences entry points do not gain
automatic idempotency from this adapter.

## Concrete display owner publication (2026-10-28)

`display.PublicationManager` exposes the internal owner contract without SQL
transactions in the service signature. `mysql.NewDisplayPublicationManager`
requires matching tenant, database and connection pool for metadata, settings and
publication stores. It copies configuration, authorizes identity before locks on
every call/replay, and rechecks local authorization inside the exact display
transaction before exposing transaction-bound repositories and queue submission.

`TableService.PublishDisplaySettings` validates the canonical original request,
active tenant/table and saved caption. It uses the existing managed search-index
job semantics for a nonempty caption and writes a tenant revision. Settings,
dependencies, caption invalidation, job/delivery, revision, audit and publication
receipt commit together. Matching retries do not repeat effects. A new save with
the same caption reuses its pending managed job; clearing a caption does not
enqueue another job. This does not implement MySQL search-index execution or
change the existing HTTP response: the result is the original ingestion
publication, not saved/current display settings.

`installation.DisplayOwnerPolicy` renders an explicit staged object/column grant
profile for this workflow. It excludes physical DDL, dictionary access, contract
mutation, gate mutation and receipt update/delete. MySQL requires an update
capability for the reservation locking read; only immutable `tenant_id` is granted
for UPDATE, with installed guards rejecting identity changes. Producers receive
no delivery lifecycle UPDATE or reservation DELETE. Grants do not provide tenant
authorization or permission to bypass owner rules. Deployment must review the
complete account and installation lineage; this helper provisions nothing.

Real-engine owner tests cover concurrent duplicates, missing queue, revision and
local-authorization rollback, revoked/changed replay, lost commit, job reuse and
caption clearing. Additional journeys use the complete v003 installation and
actual restricted physical creation/verified TableService publication before
saving display settings, including a restricted display login. A manually opened
fixture gate is not used for those journeys. Production deployment, dependency
backfill, table lifecycle, all removal/type/enum/alias owners, physical record
readers and API/import/export acceptance remain open. Parent tasks remain unchecked.

## Original display results (v004, 2026-10-28)

`TableService.PublishDisplaySettingsResult` and `display.SettingsPublicationManager`
return the settings committed by the original command. The same owner transaction
writes settings, dependencies, caption effects, audit/revision, publication and a
versioned `display.settings/v1` result. Both pre-lock recovery and the locked
concurrent-duplicate check return that immutable snapshot. Authorization still
runs on every replay. Later edits, changed relation topology or a newer closed
barrier do not replace the original response or repeat effects. Historical
publication-only receipts return `ErrOwnerResultUnavailable`; current state must
never be presented as the original result.

Append-only v004 adds `data_model_owner_results`, keyed by tenant/table/operation
and constrained to the corresponding publication receipt. Its dedicated locked
definer guards all immutable columns, rejects deletes and disabled integrity
checks. JSON size is bounded before and after MySQL serialization and on reads.
The stored envelope preserves internal tenant/schema scope omitted from API JSON.
`DisplayResultOwnerPolicy` adds only SELECT/INSERT on the new table. Missing INSERT
privilege rolls back the complete owner mutation, including the publication.

`NewExactKeyReadinessWithOwnerResults` source-binds all 89 installation steps and
checks the result definer's exact grants, locked/session state and exact trigger
set on the execution connection. Partial installation, missing/extra triggers or
grant drift cannot establish creation readiness. This is an installation
inspection, not a reusable authorization/readiness certificate.

The v004 artifact remains `needs_review`. Runtime request-key transport, API
binding, deployment/backfill, all other original owner results, retention and
retirement, complete physical readers and operational/load acceptance remain open.
The older publication-returning method remains available for its explicit internal
contract; neither method activates the MySQL backend.
