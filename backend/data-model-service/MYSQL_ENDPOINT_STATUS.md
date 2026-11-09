# Local MySQL endpoint runtime status

2026-11-05. Checked means the route is wired to MySQL; it does not establish production readiness or complete failure testing. Swagger serves all operations regardless of this status.

Runtime wiring: 43/43 operations. All listed routes are wired to MySQL for the local development administrator runtime. Index-job and reconciliation requests enqueue durable work consumed by the local MySQL worker.

Worker acceptance: see [MYSQL_WORKER_STATUS.md](MYSQL_WORKER_STATUS.md). The local
deployment executes numeric/text search, caption, navigation and repair work.
Live checks include audit-failure retry and recovery after a process kill during DDL.

Current post-migration acceptance: [final report](evidence/mysql-worker-migration/2026-11-05/report.json).
All 43 routes passed again (10 suites, 428 requests). The complete isolated
regression passed 174 top-level tests / 954 test and subtest results with no
failures, skipped tests or race warnings; `go vet` passed. The earlier manifest
failure and skipped decision case documented below are historical and resolved.

- [x] GET /v1/tables/{tableId}/display-settings
- [x] PUT /v1/tables/{tableId}/display-settings
- [x] POST /v1/tenants/{tenantId}/record-displays
- [x] GET /healthz
- [x] GET /readyz
- [x] POST /v1/tenants
- [x] GET /v1/tenants
- [x] GET /v1/tenants/{tenantId}
- [x] POST /v1/tenants/{tenantId}/provision
- [x] GET /v1/tenants/{tenantId}/data-model
- [x] GET /v1/tenants/{tenantId}/data-model/export
- [x] POST /v1/tenants/{tenantId}/data-model/import
- [x] GET /v1/tenants/{tenantId}/tables
- [x] POST /v1/tenants/{tenantId}/tables
- [x] PATCH /v1/tables/{tableId} - alias, description, semantic type, caption set/clear and atomic index-job enqueue.
- [x] DELETE /v1/tables/{tableId}
- [x] GET /v1/tables/{tableId}/fields
- [x] POST /v1/tables/{tableId}/fields - native types, seeded string/int/float enums and full-value unique-text fields.
- [x] GET /v1/fields/{fieldId}/enum-values
- [x] POST /v1/fields/{fieldId}/enum-values
- [x] PATCH /v1/fields/{fieldId} - presentation, nullability, numeric/text uniqueness and enum flags, including existing-text key backfill and uniqueness removal.
- [x] DELETE /v1/fields/{fieldId}
- [x] PATCH /v1/enum-values/{enumValueId}
- [x] DELETE /v1/enum-values/{enumValueId}
- [x] GET /v1/tenants/{tenantId}/links
- [x] POST /v1/tenants/{tenantId}/links
- [x] DELETE /v1/links/{linkId}
- [x] GET /v1/tenants/{tenantId}/pivots
- [x] POST /v1/tenants/{tenantId}/pivots
- [x] DELETE /v1/pivots/{pivotId}
- [x] GET /v1/tables/{tableId}/options
- [x] PUT /v1/tables/{tableId}/options
- [x] GET /v1/tables/{tableId}/navigation-options
- [x] POST /v1/tables/{tableId}/navigation-options
- [x] DELETE /v1/navigation-options/{navigationOptionId}
- [x] GET /v1/tenants/{tenantId}/schema-change-log
- [x] GET /v1/tenants/{tenantId}/index-jobs
- [x] POST /v1/tenants/{tenantId}/index-jobs
- [x] GET /v1/tenants/{tenantId}/index-jobs/scoped
- [x] GET /v1/index-jobs/{jobId}
- [x] POST /v1/index-jobs/{jobId}/retry
- [x] GET /v1/tenants/{tenantId}/schema-migrations
- [x] GET /v1/admin/reconcile

Table creation uses the durable owner allocation, separate reference capture, restricted temporary DDL installer and verified publication workflow. Field deletion uses verified physical retirement and metadata cleanup. Table deletion closes writers, journals its intent, drops the physical table and retains hidden metadata for recovery receipts. Unique-text creation uses full-value exact keys with database insert/update guards; existing-text uniqueness and nullability changes use verified, recoverable physical plans. Enum membership uses the published ingestion catalog, as in PostgreSQL. Reconciliation reports drift and durably queues managed-index repairs consumed by the local MySQL worker.

## Implemented update behavior

- Captions use ordered display-dependency locking, preserve unrelated settings,
  schedule search-index jobs transactionally, and retain the original PATCH result.
- Enum creation validates all inline seeds before physical preparation; allocation,
  seed persistence and publication are recovered using the original operation key.
- Enum-only changes publish the catalog and physical registry version atomically,
  without artificial DDL. Unsetting an enum with values returns 409.
- Nullable changes use physical ALTERs. Tightening populated columns checks for
  NULLs before preparation and execution; incompatible data returns 409 without
  substituting defaults. Adding a new required column to populated records still
  requires an explicit backfill and returns 409.
- Numeric uniqueness checks existing data before durable preparation. Removing
  uniqueness is rejected while a relationship depends on that parent field.
- Caption index jobs are durably queued on `index_jobs` and executed by the local
  MySQL worker. Read the job's applied status to confirm physical completion.

## Unique-text field creation

POST a field with `data_type: "string"` and `is_unique: true`.
Nullable unique text permits multiple NULLs; the empty string is a value and can
appear only once. Equality uses the complete UTF-8 bytes, including case and
trailing spaces, without truncation or prefix/hash-only uniqueness. Each field
has its own tenant-scoped dictionary domain and guards derive private keys on
both INSERT and UPDATE. Nullable fields can be added to existing records;
required new fields still require an empty table or an explicit backfill.

Schema changes journal the column and both guards independently. Recovery checks
the retained guards, current journal and exact table definition before continuing.
Later native changes preserve the text guards. The temporary installer loses
SET_ANY_DEFINER, is locked and has no active sessions before publication.
PATCH an existing string field with `{"is_unique":true}` to enable uniqueness,
or `{"is_unique":false}` to remove it. Enabling checks complete existing values
for duplicates before committing a schema intent, then fills exact keys in
256-record batches. Duplicates return 409 with the existing schema and write gate
unchanged. Removing uniqueness preserves text and rejects dependent parent-field
relationships. Nullable/required changes and valid combined enum/presentation
changes are supported. Existing NULLs prevent tightening to required.

Use a stable `Idempotency-Key` UUID when retrying a request. After an uncertain
DDL or publication response, retry the identical command with the same key.
The recorded guard/column steps and existing filled keys let work resume, and
published requests return their original Field response. The temporary installer
loses both its global definer capability and its column-specific UPDATE grant
before publication. Preparation uses a dedicated disposable database connection
to prevent temporary-table state surviving cancellation in the connection pool.

## Verification

2026-11-05 live audit: **43/43 operations, 10/10 suites, 428 HTTP requests passed**
against the running local MySQL 8.4.8 administrator runtime. This includes physical
SQL assertions, negative cases, concurrency, idempotency and recovery checks; it
does not mean every failure mode of every operation was tested. The
[per-operation report](evidence/mysql-endpoint-audit/2026-11-05/report.json)
lists the passing suites and observed statuses for each route. Adjacent `.log`
and `.jsonl` files retain assertions, fixture identities and actual HTTP responses;
`runtime.json` identifies the tested images. The full isolated regression result is
separate from this live endpoint result; the schema-lineage gap below was resolved
by the subsequent worker migration.

The completed isolated rerun passed `go vet ./...`, 12 packages and 150 top-level
tests (902 passing test/subtest results), with no race warnings or timeouts. It
exited 1 because `TestMetadataColumnManifest` failed. One nested cross-service
case, `TestPhysicalCreationAllocated/service_allocated_native_index_worker_decision`,
was skipped because its separate decision-engine MySQL fixture was not configured;
the runtime transaction tests both executed and passed. See
[isolated results](evidence/mysql-endpoint-audit/2026-11-05/isolated-report.json)
and [complete log](evidence/mysql-endpoint-audit/2026-11-05/isolated-go-tests-serial.log).
The subsequent [worker audit](MYSQL_WORKER_STATUS.md) supplied both reference
fixtures and executed that previously skipped decision-worker case successfully;
this does not change the historical full-regression result above.
The initial parallel run also had reference/account deadline failures and a
240-second package timeout; the serial rerun completed in full and those deadline
failures did not recur. Its metadata package took 260.674 seconds, exceeding the
old whole-package budget even though individual tests completed.

Run the complete live endpoint acceptance suite from this service directory:

```sh
node scripts/mysql-endpoint-audit.mjs
```

This requires the local MySQL API at `http://localhost:8080` and Docker access to
`fraud-mysql-dev-mysql-1` for physical SQL assertions. The ten suites allocate
new fixtures, retain their test tenants and exercise deletion/drift only on their
own resources. They do not reset the development database. The runner records
per-suite logs, actual HTTP responses and a per-operation matrix under
`evidence/mysql-endpoint-audit/<timestamp>/report.json`. It fails if any suite
fails or any documented operation lacks a successful response in a passing suite.
Expected rejection responses alone do not count as successful route coverage.

Run isolated MySQL transaction, physical recovery, permissions and race tests:

```sh
docker compose -p data-model-mysql-tests -f compose.mysql-test.yaml up --abort-on-container-exit --exit-code-from test
```

The MySQL Compose command requires its disposable fixture and now includes
`internal/mysqlruntime` and both delivery inspection/redrive CLI packages.
Runtime fixtures accept the standard MySQL test environment, fail when a required
fixture is missing, and remove only the database they successfully created on the
guarded disposable server. Packages run sequentially against the shared DDL server;
the metadata package has a 900-second budget for its complete race/recovery suite.
`make test-integration` is the PostgreSQL target; it is not MySQL acceptance.

Resolved schema-lineage gap found during the initial 2026-11-05 audit:
`TestMetadataColumnManifest` rejects the unbound `000009_index_job_table_scope`
up/down migrations. That PostgreSQL migration adds
`(tenant_id, table_id, requested_at DESC, id DESC)`. The MySQL job table has tenant
chronology and relationship indexes initially lacked that equivalent composite index.
The subsequent worker migration binds those source hashes and adds the equivalent
MySQL composite index through append-only v007, alongside the indexed predecessor
intent lookup. Earlier retained migration definitions remain unchanged. The
manifest check remains enabled. See the worker status for current regression
evidence; representative sustained load remains separate acceptance.

The dedicated `scripts/mysql-partial-endpoints-smoke.mjs --physical` test exercises
caption set/clear, invalid-caption rollback, seeded enums, combined constraint
updates, retry receipts, duplicate data, multiple NULLs, populated-column
nullability, writer-gate rollback and caption-delivery deduplication. It creates
retained test data only. Full shared-service integration, broader operational
failure testing and representative load acceptance remain separate work.

The dedicated `scripts/mysql-unique-text-smoke.mjs` test exercises long strings
with identical prefixes, duplicate inserts and updates, concurrent duplicates,
NULL/empty/case/trailing-space semantics, private-key derivation, rollback,
populated-table additions, request replay/conflict, subsequent native changes,
scope isolation and installer deactivation. It retains only newly created test
tenants and tables. This is local runtime acceptance, not a load-test claim.

The dedicated `scripts/mysql-text-constraints-smoke.mjs` covers 600+ existing
records, 40KB common-prefix strings, INSERT/UPDATE/concurrent duplicate rejection,
nullable/required transitions, dependency rejection, removal/re-enabling,
duplicate-preparation rollback, original receipts, later native changes, enum
combinations and grant cleanup. EXPLAIN confirms digest-index candidate lookup
and primary-key record lookup; this is not production throughput evidence.

Isolated `TestPhysicalTextConstraintRecovery` covers lost responses for column
ALTER, both guard creations, a committed partial backfill batch, the final CHECK,
both guard retirements, key removal and enable/disable publication COMMIT, plus
cancellation and foreign-tenant execution rejection. Preparation tests cover
cross-batch duplicates, canceled transaction rollback and connection disposal.

## Field deletion and enum value mutations

- `DELETE /v1/fields/{fieldId}?dry_run=true` returns a preflight report without
  changing metadata, physical columns, contract version or writer state.
- Deletion protects mandatory fields and rejects dependent links, pivots,
  captions, display references, navigation and pending/running/applied index jobs.
  Conflicts return 409 with `performed` and `conflicts` at the top level.
- Successful deletion journals physical component removal, including bool/IP
  CHECKs, native unique indexes and unique-text guards/keys. Final publication
  removes physical mappings and field metadata, cascades enum values/field order,
  records audit/revision and reopens writers with the new contract atomically.
  Remaining rows and fields are preserved. Exact-key dictionary entries are not
  garbage-collected by this endpoint.
- Enum PATCH validates string/int/float values, labels and signed INT sort order;
  duplicate values or labels return 409. PATCH and DELETE atomically republish
  the ingestion catalog and registry version. Existing record values are retained.
- PATCH retries with a stable operation key return the original enum response
  while the resource exists. After successful field or enum deletion, a repeated
  HTTP DELETE returns 404 because the resource is absent; it does not repeat
  audit or schema effects. Interrupted physical deletion resumes with the same
  key while its metadata remains. The underlying publisher also supports receipt
  reconciliation after a lost COMMIT response.

`scripts/mysql-field-delete-enum-smoke.mjs` verifies live routes, invalid patches,
concurrent enum retries, duplicates, contract versions, dry runs, every field
storage type, guard/mapping cleanup, linked-field and pivot conflicts, and row
preservation. `TestCatalogRetirementRecovery` uses a disposable MySQL database
and socket faults to verify owner rollback, lost DDL acknowledgement, lost COMMIT,
cancellation, tenant/table scope rejection and replay without duplicate effects.
These checks do not establish production authorization or load acceptance; this
compose remains the explicit local development administrator setup.

## Display settings and record displays

- GET returns scoped fields, outgoing links, captions, display columns and version.
- PUT requires the current version; stale updates return 409. Reuse the same
  Idempotency-Key and body to recover the original successful response, even after
  later updates. Versions are monotonic and may advance by more than one.
- Caption/index-job changes, settings, audit and publication receipts commit
  atomically. Caption jobs are queued and dispatched by the local MySQL worker.
- Record displays accept 1-100 objects, preserve request order and duplicates,
  and support direct typed values, enum labels, related captions and raw-value
  fallback. Unknown objects return found=false. Only current records are read.
- Reads use bounded projections and ordered shared metadata/registry locks;
  closed physical gates fail explicitly. Exact record keys are materialized before
  unique-index joins, avoiding function-driven record scans.
- Verified with targeted race tests and vet, isolated MySQL owner commit,
  concurrency, lost-commit, permission and rollback tests, and
  `node scripts/mysql-display-smoke.mjs` against the running local compose.
  The live test checks 100 requested records against 1,001 physical records;
  EXPLAIN reports eq_ref on the record unique index. This is not a throughput test.
- This is the explicit local-admin runtime. It does not establish production
  authentication/grants readiness. The display test creates its own scoped link
  fixture directly; POST links is now covered by the remaining-endpoints suite.

## Portable model import/export

- GET export returns the existing portable v1 document from a consistent,
  tenant-authorized published snapshot. It contains schema definitions, not
  ingested records. Managed fields are recreated automatically on import.
- POST import creates new tables/fields with verified physical publication, enum
  seeds and unique-text support, then captions, links, pivots, options and
  navigation. Existing table names return 409; import does not merge or replace.
- Complete graph validation precedes creation. Local limits are 1 MiB JSON and
  200 tables, fields, enum values and relationships combined. Snapshot budgets
  still apply. Large requests may reach the 60-second owner deadline.
- Send a UUID Idempotency-Key and retain the exact request body. Retrying after
  cancellation/503 resumes durable creation; successful retries return the
  original summary/revision. A changed body with the same key returns 409.
- MySQL DDL is not one atomic import transaction. Successfully published tables,
  fields and captions can remain visible after interruption. The final links,
  pivots, options, navigation/index queue and completion receipt commit together.
- Immutable portable_import started/completed schema-change log entries are
  recovery records. Retain them with the corresponding creation/publication
  receipts; pruning them independently breaks import recovery.
- Verification: race tests/vet, live export/import/export round trip across two
  tenants, aliases/enums/relations/navigation, invalid input before writes,
  historical/concurrent replay, and cancellation after allocation followed by
  successful resume. Disposable MySQL tests prove final-phase authority checks,
  cancellation, tenant-bound receipts and rollback after injected audit failure.
- Portable v1 retains its existing PostgreSQL scope: displayed_fields and
  field_order are portable; extended display-column/label JSON is not part of
  that format. Index jobs are queued and consumed by the separate MySQL worker.

## Table deletion and standalone enum creation

- DELETE `/v1/tables/{tableId}?dry_run=true` checks dependencies without changing
  data. Actual deletion drops the physical table, all its records and attached
  triggers/indexes. Links, pivots, navigation, external display references and
  pending/running index jobs block deletion with 409.
- Deletion uses the table DDL lock, a durable intent and a closed writer gate.
  If interrupted, retry with the same UUID `Idempotency-Key`. Successful retries
  return the recorded result; a new key against the deleted table returns 404.
  Hidden archived metadata and immutable receipts remain for lineage/recovery;
  published model, export and table lists exclude the retired table. Recreating
  its original name requires a new explicit creation key.
- POST `/v1/fields/{fieldId}/enum-values` supports existing string/int/float enum
  fields. Invalid types, non-finite floats and out-of-range sort orders return
  400; duplicates return 409. Creation, audit/revision and ingestion catalog
  publication commit together. Same-key retries return the original response,
  even after a later label edit; changed request bodies return 409.

Verified against live local MySQL with
`scripts/mysql-table-delete-enum-create-smoke.mjs`: validation, duplicates,
publication version, original-response replay, dry run, populated table/trigger
removal, retired-model visibility, name reuse and pivot rejection. Isolated
`TestTableRetirementRecovery` covers authority/tenant rejection, lost preparation
COMMIT response, failure after DROP, closed-gate recovery and replay.
`TestEnumCreationPublicationRecovery` covers owner rollback and lost publication
COMMIT response without duplicate effects. Existing assembled-model and catalog
retirement regressions, focused race tests and vet passed.

This remains local administrator runtime acceptance. Pending index jobs require
resolution before table deletion; the local compose now dispatches those jobs.
Table-retirement recovery currently uses the endpoint with the original key;
production reconciler dispatch/grants, process-restart and final retirement COMMIT
fault acceptance remain separate work. PostgreSQL production configuration is unchanged.

## Relationships, options, index jobs and reconciliation

The remaining 11 routes are wired in the local MySQL compose:
link and pivot create/delete, options PUT, navigation create/delete, index-job
create/scoped-list/retry, and the admin reconciliation report.

Relationship commands validate tenant/table/field ownership inside a transaction,
lock affected tables in UUID order, and check open physical registries. Pivot
paths resolve explicit IDs in bounded batches (maximum 100 links); relationship
validation does not search a truncated tenant graph. Links and pivots with dependent
navigation, display references or pivot paths cannot be deleted. Link/pivot DELETE
supports dry_run; navigation DELETE does not. Table-options PUT preserves display
JSON and returns the existing unwrapped TableOptions response contract.

Use a UUID Idempotency-Key on relationship mutations. Owner changes, audit/revision,
queued navigation index work and the original response receipt commit atomically.
Same-key replay survives resource deletion; changed bodies return 409. Navigation
creation/deletion advances the tenant model revision. These logical relationships
do not introduce physical record foreign keys, matching the original API contract.

Index creation atomically persists the job and a delivery reservation, deduplicated
by tenant/table/type/columns. Scoped reads require repeated table_id parameters and
an explicit limit. Retry accepts failed jobs; pending/running jobs return 409, and
terminal delivery reservations are replaced in the same transaction as requeueing.
Neither a 201 response nor a pending status means a physical index has been built.

GET /v1/admin/reconcile compares expected physical column names and managed-index
coverage with MySQL information_schema. Missing tables, unexpected columns, unknown
physical tenant tables and closed writer gates prevent a healthy result. Missing
managed indexes enqueue deduplicated repair jobs; completed repairs can be requeued
if the gap recurs. The optional tenant_id query limits inspection and scheduling to
one tenant. Global reports are bounded to 10,000 inspected rows, 4 MiB of inspected
text/output and 100 candidate repair gaps; overflow fails explicitly. A global
report can require tenant-scoped calls on larger installations.

Reconciliation is an observation during potentially concurrent DDL, not exact type,
trigger, definer or full-schema attestation. It does not resume arbitrary physical
DDL intents or perform repairs synchronously. The local MySQL worker executes
navigation and retained managed-index repair asynchronously. Local process-restart
acceptance passed; production permission and sustained load acceptance remain
separate. PostgreSQL production configuration is unchanged.

Verification: scripts/mysql-remaining-endpoints-smoke.mjs exercises all 11 routes,
concurrent creation/replay, invalid-input rollback, dependency blocking, options
response compatibility, navigation queueing, scoped jobs, failed delivery retry,
physical column drift, repair deduplication/requeue and tenant isolation using only
new fixtures. TestRemainingRelationshipTransactions uses an isolated MySQL database
and injected receipt-write failure to prove owner/audit/revision rollback, concurrent
replay, tenant rejection and cancellation while waiting for locks. Existing
TestStagedIndexJobEndpoints and TestScopedIndexJobs also pass. Focused race tests
and vet pass; these checks do not establish production throughput.
