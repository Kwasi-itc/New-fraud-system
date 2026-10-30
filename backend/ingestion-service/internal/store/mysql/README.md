# Staged ingestion MySQL adapters

Runtime remains PostgreSQL/River. This package implements staged tenant-bound
receipt transactions, physical keyed writes and a composed atomic record owner
with audit/outbox appends. It is not the complete ingestion MutationStore,
production API/worker composition, upload/deferred repositories or delivery queue.

## Atomic keyed record owner

`KeyedRecordOwner.Execute` composes the v1 canonical receipt, current external and
transaction-local authorization, published-model validation, physical writer,
audit/outbox and original result into one transaction. It requires a trusted
tenant/table/object-type binding, bounded limits, both authorizers, a local
transaction-bound model loader and a clock. No default authorizer or network
model fallback is provided. These integration hooks are not production adapters.

Raw commands are canonicalized and copied before callbacks, preserving exact
JSON numbers with UseNumber. Existing receipts replay without invoking mutable
model validation; current authorization still runs. New commands lock the
publication and sorted lookup guards, reject duplicate normalized batch keys and
validate using the existing domain validator. Batch effects and results retain
input order. Data, success audit, per-record outbox, batch-completed event and
receipt commit together. Alternate unique violations roll back rather than
redirecting an update to a different object. Physical readback currently verifies
the trigger-derived exact key identity; it is not a general record-read API.

Validation failures roll back the data transaction, then append failure audits
in a separate reauthorized, bounded transaction. A failure-audit error is joined
with `RecordValidationError`, never discarded. No success receipt is retained for
validation failures. API adapters must map combined storage/validation failures
explicitly; merely finding `RecordValidationError` is not sufficient to call it a
clean validation rejection. The immutable response retains Replayed=false;
`ReceiptResult.Replayed` separately signals replay for future DTO adaptation.

The keyed owner requires a nonempty caller key. Unkeyed/worker execution identity,
legacy SHA-1 translation, current production authorization/model binding, API
error/DTO adaptation and upload/deferred fencing remain unimplemented. It is not
safe to enable MySQL runtime selection from this component alone.

Write coordination is staged in `LockWriteSnapshot` and `LockLookupKeys`.
Neither is a physical record writer or a substitute for owner authorization.

`ReceiptManager.Execute` authorizes every call before locks and rechecks local
authority inside the transaction, including historical replay. Trusted owner
code must provide both authorizers. `NewReceiptRequest` builds the staged
`ingestion-request/v1` hash using the driver-free domain canonicalizer. Production
owner entry points and authorization binding are not yet implemented. The hash
cannot substitute for authorization.

The pinned v1 JSON envelope orders version, tenant, table, mode, kind and records.
Map keys sort; record/array order, omitted versus null fields, exact Unicode and
numeric spellings remain significant. Single commands contain exactly one object;
batches contain 1-500 objects. Byte, node and depth limits reject oversized or
cyclic input before unbounded traversal; final encoded bytes include the envelope
and JSON escaping in the byte limit. Invalid UTF-8, nonfinite numbers and custom
Go values/marshalers are rejected. No caller-owned input is modified.

Pass raw decoded input, not model-normalized records. Accepted value types are
nil, bool, string, json.Number, finite float64, map[string]any and []any. Future
HTTP/worker integration must use json.Number to retain exact input numbers;
float64 cannot recover precision lost during an earlier decode. This helper is
not a wire JSON parser and cannot detect duplicate keys already discarded by a
decoder. JSON encoding follows Go encoding/json, including HTML escaping, pinned
by the v1 golden test. Version the format if these rules change. Never relabel
legacy SHA-1 receipts as v1. Historical replay computes identity without current
model defaults, coercions or publication versions; new effects still require
current model validation and physical readiness.

The coordinator locks a tenant/digest bucket before owner effects. A bounded
primary-key range reads full-byte key comparisons, not digest-only identity.
Keys remain tenant-wide; table, mode, response kind and hash substitutions
conflict. Different buckets can proceed independently. Collision capacity fails
closed before effects; a full bucket still permits an existing receipt replay.
Key, response and collision bounds are mandatory. Only a matching bounded
response is projected. Original valid UTF-8 JSON bytes are retained unchanged.

Owner callbacks must use the supplied transaction for every local effect.
They must not perform network calls, DDL, session changes, commit or rollback.
Failures return no provisional result. There are no automatic mutation retries.
After `mysqlstore.CommitError`, a healthy repeat of the identical authorized
command reads the committed receipt or runs the transaction if it rolled back.
This protocol does not deduplicate effects outside that transaction.

`ReceiptSchema` is staged DDL, not an accepted deployment migration. Install it
only in disposable fixtures until migration checksums/recovery, cross-owner
tenant/table lineage, legacy receipt translation, retention and production grants
are complete. Objects belong to the one shared application database. Generated
databases in tests are disposable isolation only.

The fixture verifies a principal with SELECT/INSERT on receipts and INSERT plus
SELECT(key_digest)/UPDATE(key_digest) on buckets. Receipt UPDATE/DELETE, bucket
DELETE and DDL are denied. This is not a production grant installer or protection
against an administrator. Retention must not delete receipts and allow an old
key to rerun silently; no retention implementation is supplied here.

From the repository root, run the isolated suite:

```powershell
docker compose -p ingestion-mysql-test -f backend/ingestion-service/compose.mysql-test.yaml run --rm test
docker compose -p ingestion-mysql-test -f backend/ingestion-service/compose.mysql-test.yaml down --volumes
```

The fixture has no host ports or application volumes. Tests refuse other MySQL
addresses and fail when the required fixture is missing. They cover duplicate
concurrency, collisions, authorization/replay, cancellation, owner/result rollback,
restricted grants and a real lost-COMMIT-OK acknowledgement. Physical writes,
audit/outbox and worker effects still require integrated acceptance under IN-01.

The application image now builds with the backend directory as its context:

```powershell
docker build -f backend/ingestion-service/Dockerfile backend
```

## Transaction-bound audit and outbox

`EffectStore` requires the existing owner transaction, tenant, stable table ID,
exact logical object name and row byte budget. The owner obtains that scope from
authorized metadata; this constructor does not prove table ownership or authorize
access. No nested pool acquisition, transaction commit or network work occurs.
`RecordIngestion` validates both rows before writing. Tenant/name, object identity,
timestamp and successful record-event association must match. JSON shape, UTF-8,
status/mode, UUID and microsecond timestamp constraints fail explicitly.

`RecordBatch` accepts 1-500 ordered audit/event pairs and uses two parameterized
multi-row INSERTs. Every pair is validated first. The separate batch byte budget
includes all bound values; SQL/parameter counts are bounded by the record limit.
Per-row limits still apply. The owner appends the batch-completed event in that
same transaction and must propagate every SQL error to roll back all effects.
No generic upsert or silent duplicate handling is used. Record normalization,
duplicate lookup rejection and physical writes remain owner integration work.

`AppendAudit` supports the separate failed-validation transaction and returns its
errors. API mapping of combined validation/audit failure remains unimplemented.
`AppendOutbox` supports the additional batch event. Stored events are pending;
there is no delivery consumer or acknowledgement lifecycle implemented here.

`EffectSchema` is staged DDL, not reviewed installation lineage. It preserves
source audit/outbox fields, adds stable table identity and defines indexed
tenant/table history and pending-event ordering. It uses JSON for semantic
payload retention (not original JSON whitespace), DATETIME(6) UTC for instants,
binary UUIDs and full-length text/binary keys. There is no text-prefix uniqueness.
Cross-owner tenant/table foreign-key lineage, legacy table attribution, retention,
delivery state and production grants remain open. No query-plan/load acceptance
is claimed from the index definitions.

Real MySQL tests cover atomic receipt/effect replay, owner/receipt/outbox failure,
cancellation after writes, insert-only grants and denied outbox insertion, exact
large integers/NUL keys/timezone round trips, pair rejection before writes, a
separate failure audit, and 500-row batches with late failures. Lost-COMMIT replay
verifies one retained audit and outbox event. Physical record writes in these
tests are still fixture effects, not a completed ingestion data writer.

## Physical write coordination

The authorized owner acquires locks in this order: receipt reservation (when
present), exact tenant/table publication row, sorted lookup guard buckets, then
physical rows. Worker fencing must precede these effects according to its owner
lock protocol. Use READ COMMITTED and retain the transaction through commit.

`LockWriteSnapshot` takes FOR SHARE on the data-model registry row and requires
the exact expected version, open state and no active operation. It rejects
archived tables, stale/absent publications, invalid physical identifiers and
missing/extra field components. One bounded joined query loads up to 1,000 fields
with at most three components per field. It includes archived fields so callers
can reconcile the complete published contract. Returned mappings are descriptions
valid in that transaction, not transferable authorization tokens. Model validation
and field-level write eligibility remain owner responsibilities.

`LockLookupKeys` accepts 1-500 normalized full-byte object IDs under key/batch byte
limits. Duplicate exact IDs are rejected before writes. SHA-256 guard buckets are
sorted and reserved in one parameterized INSERT, including initially absent keys.
Digest collisions only serialize callers; they never establish record equality.
The input order is unchanged. Guard rows persist after commit and a composite FK
binds them to the same tenant/table registry. The narrowly scoped guard principal
needs INSERT and SELECT/UPDATE(key_digest); registry reads need SELECT only.

The fixture installs the actual data-model registry DDL on minimal referenced
metadata. Its privileged publication setup is not production publication or full
metadata installation acceptance. Tests verify shared writers, blocked schema
preparation, independent tables/keys, cancellation, invalid mappings, opposite
batch lock orders, forced guard collisions and restricted privileges.

The staged physical writer now consumes data-model's v005
`data_model_find_record_object_key_v1` routine through `FindRecordKey`. The routine
performs full-byte lookup without granting the application private dictionary
SELECT or resolver execution. It supplies an exact-key ID for the indexed physical
lookup, with full-byte equality checked again on the object column. Missing or
ungranted routines are errors, not absent records. Existing unique-key triggers
remain the integrity authority. Tests install the actual lookup SQL with fixture
metadata/triggers; complete v005 installer/readiness and restricted production
policy acceptance remain open. No application record write path is enabled yet.


## Staged owner and work integration

`NewAuthorizedRecordOwner` binds service-token authentication and current tenant/table
metadata to local bounded publication reads. `NewWorkOwner` adds immutable upload/deferred
resources and shared `mysqlstore/work` delivery mechanics. The worker fences before
receipt/publication locks; resource completion, job completion, record writes and
append-only audit/outbox effects share a transaction. Parsing occurs outside locks.
Internal execution receipts use a separate namespace from public caller keys.

`InstallationStatements` is the staged source manifest. `InstallationPlan` requires
exact source SQL and reviewed reference definitions; the shared runner uses an
independent ingestion ledger in the same database. `RuntimeGrants` emits explicit
object/column grants for existing dedicated principals; physical table grants stay
with data-model publication coordination. No installer or runtime switch is automatic.

`NewLegacyImporter` additionally requires an operator authorization policy. Imports
require trusted reconciled original requests, historical models, successful audits and
stored responses. Imported result JSON is canonically serialized, not byte-for-byte
original whitespace. Provenance hashes are not signatures. Missing evidence fails
closed. Ordinary API/worker roles have no legacy-provenance INSERT grant.

Remaining production gates include reviewed installation artifacts, complete API/read
contracts and worker startup binding, operator queue redrive/retention, full deployment
acceptance and real legacy reconciliation. These adapters do not complete cutover.


## Upload list projection

`WorkOwner.ListUploads` returns 1..100 metadata summaries using an opaque-to-the-
transport cursor tuple scoped to tenant and stable table. Ordering is descending
creation time and UUID; `limit+1` determines whether another page exists. The
`upload_history(tenant_id,table_id,kind,created_at DESC,id DESC)` index isolates
upload history from deferred work. The query requires this index: the unhinted
plan chose the older kind-agnostic history index on the mixed-history fixture.
The installation/readiness contract must therefore include this index. One exact-ID join supplies job status; an
orphan resource fails instead of silently disappearing from the listing.

The projection reads no raw payload, request key or result JSON. Filename and
content type are bounded in SQL before transfer and checked against the combined
creation limit. It requires both create and patch owner authorizations before and
inside the read transaction, matching the existing conservative write-scope read
policy. A dedicated read-only business policy and HTTP DTO wiring remain separate.
State/error code remain internal queue values; this is not a new public status enum.

Pages are live reads, not a retained database snapshot. Concurrent inserts above
the cursor appear on a new first-page request; metadata on already listed work may
change. Cursor values carry no authority. Do not alter immutable creation time/ID
in runtime code. Historical import and operator history access are separate paths.


## Staged operator cancellation

NewWorkOperator requires a separate trusted operator policy in addition to current
owner authorization. Cancel locks the job before business effects, fences running
workers, releases its reservation and appends an immutable action outcome in the
same transaction. Terminal work remains intact. Stable action UUID replay checks
actor, scope and reason; authorization is rechecked even on replay. Any transaction
error returns no provisional result. CancellationGrants grants only the required
owner columns and append-only action access to a separate existing principal.
WorkOperatorSchema is the appended ingestion_v002_work_actions installation step.

The operation contracts in mysql_migration/implementation/modules/
02_ingestion_operation_contracts.md define unkeyed synchronous identity, operator
capabilities, future redrive lineage and retention dependencies. Production policy
binding, operator endpoints, redrive and retention tooling remain separate tasks.


## Staged queue inspection

NewWorkInspector requires an explicit tenant-wide queue inspection authorizer.
List accepts one exact queue/state, a scoped creation-time/UUID cursor and 1..100
rows. Each read-only transaction reauthorizes and fetches only bounded job identity,
attempt and timestamp metadata. No payload, keys, tokens or free-text errors are
read. InspectionGrants provides column-only SELECT for a separate existing account;
trusted policy metadata reads and production access-audit binding stay separate.

Installation step ingestion_v003_operator_history adds the index required by the
query. The unhinted mixed-history plan used ready and filesort, so the builder uses
FORCE INDEX (operator_history). First and continuation plans now use ref/range
access without filesort. Pages are live views, not a snapshot across state changes.
See the operation contract for evidence, limits and remaining deployment gates.


## Staged worker telemetry

The shared work.RunnerConfig accepts optional Metrics; Queue.Telemetry snapshots
fixed counters/histograms and pool statistics without a database query. Isolated
tests exercise completion, retries/exhaustion, expiry/rollback, stale heartbeat
fences, database-clock queue wait and pool contention. Existing worker grants need
no change. Production exporter/admin-policy binding remains part of worker adoption;
no runtime switch or public metrics endpoint is activated. See the operation
contract for counter scope, ambiguous-commit limits and remaining acceptance work.


## Staged typed record decoder

DecodeRecordValue translates authorized projection values for bool, int, float,
string, timestamp and ip_address into strict typed values. It reuses shared UTC
DATETIME(6) and inet codecs, enforces null/component consistency and bounds UTF-8
string copying. Callers must still bound SQL transfer and total row/page size.
It does not resolve mappings, authorize a read or define HTTP serialization.
See mysql_migration/implementation/modules/02_ingestion_record_decoder.md for
round-trip evidence and remaining integration gates.


## Staged mapped projections

NewRecordProjection reuses the existing writer mapping/publication barrier to
compile 1..100 distinct active logical fields into explicit physical columns.
It keeps logical aliases out of SQL, preserves IP component order and decodes
through DecodeRecordValue without returning partial records. Callers must already
be authorized and hold the acquiring transaction through query execution.

The conservative writable/open gate is retained pending a separate read policy.
Before-Scan/row/page byte limits, query predicates, restricted reader grants and
HTTP compatibility remain integration work. See the projection contract under
mysql_migration/implementation/modules/02_ingestion_record_projection.md.


## Staged result-size enforcement

RecordProjection.Bounded adds SQL string-length guards and explicit per-field,
worst-case row-transfer, decoded-row, page and row-count budgets. Collect owns the
query rows and returns no partial page on limits, decode/scan errors or cancellation.
The component does not supply a predicate or LIMIT; callers must compose bounded
queries and retain authorization/publication locks. HTTP and aggregate adoption
remain open under IN-01.3.021f. See the result-bounds contract for conservative
JSON accounting, driver-memory overhead and test evidence.


### Isolated read and worker operations adoption (2026-10-30)

The isolated API now wires the existing get/list/search and aggregate handlers
through authorized bounded MySQL reads. The default deployment remains
PostgreSQL/River. See
`mysql_migration/implementation/modules/02_ingestion_operations_batch20.md`
for the exact field/operator/result limits, process fault tests, event-consumer
audit and remaining parity/deployment gates. The application API does not expose
operator cancellation/redrive; trusted operator composition is tested separately.


### Live parity extension (2026-10-30)

The isolated read composition now includes binary string and complete IP
membership, IP scalar ordering, all-type COUNT/DISTINCT, bounded string/IP
MIN/MAX and source-compatible integer AVG precision. Full-byte comparisons,
query/result bounds, raw-input policies and deployment limitations are reviewed
in the [current reader contract](../../../../../mysql_migration/implementation/modules/02_ingestion_reader_contract.md#live-parity-and-isolated-adoption-2026-10-30).
Earlier component limitations are historical. This does not activate a MySQL
production runtime or close independent deployment/legacy migration gates.
