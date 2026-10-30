# Ingestion reader consumers and portable reader contract

Original scope: IN-01.3.001/004/005/006/007, 2026-10-29. The later complete-schema
section below supersedes the original MySQL-store gaps. These are client/construction
capabilities, not completion of the MySQL contract-store adapter, read-query port,
startup readiness or production runtime adoption. PostgreSQL/River remains active.
Paths in the consumer table are relative to backend/ingestion-service/.

The [2026-10-30 live parity/adoption review](#live-parity-and-isolated-adoption-2026-10-30) supersedes earlier supported-type and runtime-composition gaps. Deployment approval remains separate.

## Consumer inventory (IN-01.3.001)

| Caller / construction | Actual call and required semantics |
|---|---|
| internal/service/ingest_service.go: Ingest, BatchIngest | readTableModel uses TableDataModelReader.GetPublishedTable when implemented; otherwise calls the explicitly selected full-model reader. Requires current tenant status, table identity, fields, types, managed fields and revision before validation/write. This interface selection is not fallback after a table-reader error. |
| Same service: GetRecord, ListRecords, QueryRecords | Same readTableModel path. Requires scoped table metadata and mappings for record projection/validation; unknown table remains a domain validation result for an existing tenant. Physical read readiness and mapping through query execution remain IN-01.3.008 and later. |
| Same service: AggregateRecords | Same table lookup for query.ObjectType before aggregate validation/dispatch. Requires current field types and published revision; actual MySQL aggregate SQL is not implemented by the client. |
| internal/service/upload_log_service.go: RunLog | Calls BatchIngest and therefore the same selected table/full-model path. |
| internal/service/deferred_ingest_service.go: RunDeferredIngest | Calls Ingest and therefore the same selected table/full-model path. |
| internal/service/model_contract_service.go: Get | Explicit full-model GetPublishedDataModel. Router constructs this service then discards it (`_ = modelContractService`); no currently wired public full-model endpoint was found in this router. Do not mistake construction for a live consumer route. |
| internal/httpapi/router.go: NewRouter | Builds HTTPClient with private DATA_MODEL_SERVICE_TOKEN; sql mode wraps it in ContractReader using the PostgreSQL store. Its HTTP provider serves explicit full-model calls only. |
| cmd/worker/main.go: main | Same HTTP/SQL composition for upload/deferred workers. PostgreSQL connection and River selection remain unchanged. |
| internal/app/app.go: New and cmd/worker/main.go | SQL startup calls PostgreSQL IngestionContractStore.Validate for versioned read views before traffic/jobs. Portable reader construction does not replace these checks. |
| internal/store/mysql/authorized_owner.go: NewAuthorizedRecordOwner | Separate staged transaction-local bounded contract loader by stable tenant/table ID. Reuses DecodePublishedTable; current tenant/table authorization also runs on receipt replay, but mutable model load runs only for new effects. This does not implement name-to-ID resolution or the general IngestionContractStore port. |

Ports: internal/ports/data_model_reader.go defines DataModelReader (full model),
TableDataModelReader (one object type), and IngestionContractStore.ReadContract
(tenant, name, known stable table/version). The existing PostgreSQL adapter reads
core.ingestion_contracts_v1 and core.ingestion_tenants_v1. Status/version/payload
are one statement for a known table; payload is omitted only on matching stable
identity and version. Missing tables consult tenant state without caching absence.
No database-specific types leak through these ports.

## Explicit reader construction (IN-01.3.004)

NewReader in internal/clients/datamodel/reader.go is an additive staged factory:

- Mode must be exactly http or sql; there is no auto mode.
- HTTP mode uses the existing full-model HTTP client, with a required http(s)
  service URL, separate private integration credential and positive timeout
  capped at 30 minutes. Credentials embedded in URLs, query/fragment configuration
  and newline credentials are rejected. Redirects are not followed.
- SQL mode requires an authoritative contract store. Optional HTTP configuration
  enables explicit full-model calls only. Without it, full-model calls error.
- HTTP reads retain /v1/tenants/{id}/data-model, private bearer credentials,
  request-ID propagation, ETag revalidation, no-store handling and the existing
  32 MiB response bound. Non-200/304 responses, malformed data and timeouts error.
- The factory does not create database pools, install schemas or choose MySQL.
  Existing runtime constructors remain in place; adopting the factory belongs to
  IN-01.2.038/039. Test servers are local httptest servers, not live integrations.

## Fail-closed table reads (IN-01.3.005)

ContractReader.GetPublishedTable always calls the authoritative store, including
cache hits. Database, authorization and publication-gate errors propagate without
calling a configured HTTP provider or returning cached data. Missing store errors
rather than panics. Tenant mismatch, invalid table/version identity, malformed
payload and an unproven nil-payload cache hit fail closed. A disabled tenant is
returned non-writable after fresh status revalidation, not bypassed with HTTP.

The store remains responsible for actual authorization/publication-gate enforcement
and bounded payload retrieval. Tests inject those failures at the port to prove
reader behavior. Complete MySQL gate enforcement/readiness is not established by
these tests; it remains the store/owner integration gate.

## Cache identity (IN-01.3.006)

The lookup slot is tenant + object name. The immutable representation additionally
carries stable table UUID and publication UUID; each SQL read sends both known
values for authoritative revalidation. Nil payload is accepted only when both
match the returned identity. Same-name table recreation with a different UUID
cannot reuse the old representation even if its publication UUID is unchanged.
Missing tables return no cached table. Every model returned to a caller owns deep
copies of its maps, managed-field slice and enum-value slices.

ContractReader now sets PublishedDataModel.PublicationVersion on cold and warm
results, independently of public RevisionID (contract-v1:table:version:status).
HTTP full-model revisions are not reinterpreted as physical publication UUIDs.

## Cache resource and lifetime policy (IN-01.3.007)

The staged factory accepts paired zero capacities (disabled), or positive limits
up to 10,000 entries and 64 MiB encoded bytes. These are validation ceilings, not
recommended deployment settings. Existing direct HTTP constructor defaults remain
128 entries/16 MiB. Oversized/nonpositive byte-accounting entries are not retained;
replacement and LRU eviction maintain aggregate accounting under a mutex.

A representation's absolute reuse age is five minutes from insertion. A hit does
not extend that age. Expiry is checked lazily on access; expired idle objects may
remain allocated until access, eviction or process exit, but cannot be reused
after expiry. There is no background cleanup goroutine. Every permitted hit still
requires successful origin revalidation, so five minutes is not a stale-serving
window. HTTP 304 and SQL nil-payload responses require matching retained identity.

Byte accounting measures encoded payload size, not Go heap/RSS; decoded models,
map keys and per-request copies add overhead. Entry/encoded-byte caps and bounded
HTTP responses are implemented, but deployment memory sizing requires measurement.
SQL transfer/decoding bounds belong to the store implementation (IN-01.3.003),
not to this cache budget. No performance target or memory benchmark is claimed.

## Verification and remaining gates

reader_test.go covers explicit mode/configuration, private credentials, warm-cache
401/403/503 failures, deadline propagation, configured-but-unused HTTP fallback,
missing SQL store/full-model provider, same-name recreation, physical publication
identity, 160 concurrent SQL reads with per-request revalidation, disabled cache,
invalid accounting and the exact expiry boundary. Existing auth/cache/HTTP/contract
reader tests retain redirect, ETag, no-store, deep-copy, LRU and error coverage.
Race-enabled client/domain tests and module vet are recorded in
[reader batch evidence](../evidence/2026-10-29_ingestion-reader-batch.log).

Still open: data-model-owned MySQL read contract and indexed name resolution,
concrete SQL adapter, publication coordination through record/aggregate execution,
production construction/startup readiness, and full-stack differential acceptance.
These five scoped tasks do not complete IN-01.3 or authorize database cutover.


## Complete shared-schema and operator batch (20 existing tasks, 2026-10-29)

This batch completes existing leaves, without introducing replacement checklist
items. Runtime remains PostgreSQL/River. No application database was installed,
loaded, or changed. The migration tooling and MySQL adapters are staged code.
Independent deployment review, actual API/worker composition, PostgreSQL/MySQL
parity and Q-01 operational acceptance remain their own unchecked gates.

| Checklist IDs | Implementation and acceptance evidence |
|---|---|
| IN-01.2.017, .020; IN-01.4.001 | `data-model-service/cmd/export-mysql-fixture` emits actual owner v001-v006 SQL and locked-definer grants. `shared_fixture_test.go:fullSharedFixture` creates a fresh generated database on pinned MySQL 8.4.8, captures intermediate definitions, recreates that database, and applies/verifies both migration ledgers twice. There are 91 data-model and 21 ingestion steps, including the appended deferred-metrics index. Reference plans, source manifest, commands and source fingerprints are retained with the run. |
| IN-01.2.022; IN-01.4.002 | Distinct runtime, worker, reader, inspection, legacy-import and read-only preview accounts exercise their permitted operations. Direct metadata writes, private dictionary reads, DDL and unrelated evidence mutation are denied. Tests include authorized writes/replay, worker submission/completion, reads, metrics and offline import; service operations use restricted accounts, while schema creation, seeding and reconciliation use the fixture administrator. |
| IN-01.2.026; IN-01.3.002, .003, .009 | Owner v006 `data_model_find_ingestion_contract_key_v1` resolves only `dm.contract.name`, scoped to binary tenant ID and full bytes. `IngestionContractStore` projects bounded metadata by unique key and stable tenant/table ID; `ResolveRecordOwner` authenticates before lookup and its resulting owner rechecks the binding inside every operation. Tests cover absence, foreign scope, archive, rename, replacement table identity, closed gate, version mismatch, status refresh and over-budget cached payloads. |
| IN-01.3.008 | `WithRecordRead` authenticates, opens READ COMMITTED, reads bounded metadata, acquires the existing shared physical-publication barrier, and rechecks identity/version/status after acquiring it. The transaction remains open through the local query callback. A competing publication close is blocked until the read ends; closed or stale mappings never reach the query callback. No HTTP fallback is involved. |
| IN-01.3.014, .029 | Existing PostgreSQL `TenantDataReader` has no implicit validity-time filter, as-of endpoint, GROUP BY or SQL analytic-window API. The MySQL fixture verifies that stored `valid_until` alone does not remove rows, and exercises all six timestamp comparisons plus inclusive/exclusive windows at microsecond boundaries. Only explicit supported timestamp filters are carried forward. Raw HTTP date normalization and full engine parity remain separate. |
| IN-01.3.031, .032 | `TestFullSharedReadPlans` retains JSON EXPLAIN for exact lookup, metadata lookup, bounded selection and COUNT/SUM/AVG/MIN/MAX/COUNT DISTINCT on 1,000 isolated records with selective indexed and unindexed filters. The exact lookup defect and remaining scan limitations are described below. |
| IN-01.2.037 | `WorkInspector.DeferredMetrics` returns one REPEATABLE READ snapshot after authorizing every tenant/queue/state scope. Five covered index ranges each read at most the configured cap plus one; exceeding the cap fails the entire result. Pending includes scheduled available work. Running, completed, discarded and cancelled counts are separate; uploads sharing the queue are excluded. The v004 index is `(tenant_id,queue,kind,state)`. No payload, lease token or error text is read. |
| IN-01.2.066, .067 | `cmd/import-mysql-receipts` defaults to SELECT-only preview and explicitly selects import mode. It validates original evidence through `LegacyImporter`, reports would-import/already-imported/conflict/rejection counts, and retains scoped immutable provenance. Import uses a dedicated restricted principal and writes only receipt/provenance state. Its input-hash/target/limits-bound checkpoint advances after a committed result; a failed checkpoint write is an error, and retry reconciles the existing result. Tests execute the real CLI with preview/import principals, checkpoint-write interruption, replay, changed input and conflicting source evidence. |
| IN-01.4.004 | `internal/testfixtures` now owns the existing PostgreSQL characterization model, seeded record, expected values and upload CSV. PostgreSQL tests call it without changing the source cases. The full MySQL fixture reuses its required amount field and status enum, then verifies record and upload outcomes. This is fixture reuse, not a claim of live PostgreSQL/MySQL parity. |
| IN-01.4.005 | `fullDurableCounts` reconciles physical rows, public/internal receipts, audit, outbox, resources and jobs under the generated fixture's stable tenant/table. Work rows are joined to scoped resources. Tests assert successful durable effects, replay stability and all-zero rollback when any required effect grant is absent. |
| IN-01.4.006 | `cmd/verify-mysql-fixture` requires the isolated address, explicit required-suite setting and owner exporter. It discovers the full-schema tests, records Go JSON events and reference artifacts, and fails on missing infrastructure, missing execution, unexpected tests, failures or any skipped test/subtest. Unit tests cover missing infrastructure and invalid event accounting. |

### Indexed read contract and measured query-plan correction

Objects are schema-qualified `data_model_tenants`, `data_model_tables`,
`data_model_ingestion_contracts`, `data_model_physical_tables`,
`data_model_fields` and `data_model_physical_fields`, plus the fixed-domain
v006 lookup. Ingestion receives SELECT on the documented metadata contract and
EXECUTE on the lookup; it receives no SELECT on `data_model_exact_keys`.
The definer is independently locked and has only dictionary SELECT and the
required routine EXECUTE rights. This appends v006 after v005; it does not expand
the original dictionary resolver's frozen grants or edit prior migration steps.

The contract key is resolved first, then bound to the unique
`table_name_key_id` predicate. A caller-supplied name is never used as an SQL
identifier. Payload and tenant schema-name transfer are guarded by
OCTET_LENGTH/CASE; configuration permits 1 byte through 16 MiB. Cache hits still
validate payload size, tenant state, archive state, stable IDs and the open gate.
Unknown tables retain tenant state without caching an absence; unknown tenants
error. Archived, closed or mismatched publication versions fail closed. Pending
tenants are non-writable metadata; authorized record queries reject them.

On MySQL 8.4.8 with 1,000 seeded rows, placing the NOT DETERMINISTIC lookup
function directly in the record predicate produced `access_type=ALL` and an
estimated 1,000 examined rows. `ReadExactRecord` now resolves the immutable,
full-byte key first and binds it to `record_object_key_id`; EXPLAIN reports
`access_type=const` and one estimated row. The original plan is retained beside
the changed plan, and the test requires the unique-key plan. Both statements keep
the publication lock and tenant/table scope. They use READ COMMITTED snapshots;
concurrent record changes can be observed between statements. An immutable
scoped dictionary identity cannot redirect the lookup to another object. Missing
keys/rows return `sql.ErrNoRows`; permission or database failures remain errors.

The same function-in-predicate issue was avoided in contract metadata lookup.
No function is falsely labelled DETERMINISTIC to coerce the optimizer. No generic
any-unique upsert was added. The exact-read regression suite checks byte identity,
tenant/version scope, bounded values and failure-versus-absence behavior.

Remaining cost limitations are explicit: unindexed filters and broad aggregates
can scan the table; a bounded result does not bound input scan work. Indexed
selective numeric filters use the fixture's unique index. COUNT/MIN/MAX may scan
a covering index because the actual query also needs count/null information.
No new per-customer or timestamp index is inferred without an owner-approved
query workload. Unordered selection remains unordered; stable public pagination,
raw string/IP semantics and endpoint deadlines are separate adoption work.
EXPLAIN estimates are not measured latency or throughput improvements, and this
1,000-row dataset does not establish million-row or production capacity.

### Reproduction and operational limits

From the isolated verifier, build owner and operator binaries from the same source:

```sh
cd /backend/data-model-service
go build -mod=readonly -o /tmp/ingestion-dm-fixture ./cmd/export-mysql-fixture
cd /backend/ingestion-service
go build -mod=readonly -o /tmp/ingestion-import-receipts ./cmd/import-mysql-receipts
INGESTION_DATA_MODEL_FIXTURE_EXPORTER=/tmp/ingestion-dm-fixture INGESTION_IMPORT_TEST_BINARY=/tmp/ingestion-import-receipts go run ./cmd/verify-mysql-fixture /new/result/directory
```

The verifier also requires `INGESTION_MYSQL_TEST_ADDRESS=mysql:3306` and
`INGESTION_REQUIRE_MYSQL_TEST=1`. It does not start Docker, accept an application
database, or silently skip missing prerequisites. The owner exporter emits only
for `ingestion_full_test_<32 hex>` and generated account suffixes; the fixture
uses CREATE without IF NOT EXISTS and cleans up only identities it created.
Source/provenance fingerprints matter because the working tree contains
uncommitted and untracked implementation changes; a Git HEAD alone is insufficient.

The offline operator accepts trusted JSONL `LegacyImport` records, a reviewed
source snapshot identifier, explicit tenant/table/object scope, and a new or
matching checkpoint path. Export input is bounded to 1 GiB and copied to a private
temporary file while hashing, preventing source-file edits from changing a run
midway. Each line is capped at 16 MiB; evidence also passes the aggregate semantic
byte/structure bound. An invocation processes at most 10,000 records (default
1,000). `HasMore` explicitly reports an invocation stopped at its record cap;
import resumes from `LastCommittedLine`. Dry-run totals describe only that bounded
invocation, not a completed source inventory. Checkpoints pin input SHA-256, source snapshot, address/database, stable
scope and receipt limits. Changed files or targets cannot resume the old checkpoint.
A lost commit acknowledgement or checkpoint error must be retried with unchanged
input; the importer reconciles retained canonical results and original provenance.

Configuration uses `INGESTION_IMPORT_MYSQL_ADDRESS`, `..._USER`, `..._PASSWORD`
and `INGESTION_IMPORT_OPERATOR_TOKEN`. Passwords are not command-line arguments.
TLS verification is required normally; `-isolated-fixture` permits plaintext only
for the generated Compose database namespace. The default mode is `-mode dry-run`;
`-mode import` additionally requires `-checkpoint`. Scope flags are `-database`,
`-tenant`, `-table`, `-object-type`, `-snapshot` and `-input`. Receipt policy flags
`-command-bytes`, `-result-bytes`, `-key-bytes`, `-collision-rows` must match the
reviewed target policy; defaults are 1 MiB, 1 MiB, 1,024 bytes and 16 rows.
LegacyOperatorGrants defines distinct preview/import privileges. The command
neither installs grants nor reconstructs missing evidence or raw historical input.
Source inventory/export, exception disposition and real migration reconciliation
remain unchecked; synthetic CLI acceptance does not approve importing production.

The fixture installs the real shared metadata/ingestion migrations. It seeds
metadata under those constraints and reuses the existing physical test-table
builder; it does not exercise data-model production publication or its dynamic
physical-definer deployment. API and worker process startup/adoption are not
provided by these tests. The callback query layer is internal and synchronous;
callers must not retain its transaction/projection, make network calls under its
lock, or use output after an error. Independent schema/grant review remains open.
Full-stack IN-01.4 B/C checks stay open rather than being inferred from these
complete-schema adapter and operator tests.


Final retained evidence: [12-test summary](../evidence/2026-10-29_ingestion-existing20/final/summary.json),
[raw events](../evidence/2026-10-29_ingestion-existing20/final/raw.jsonl),
[source fingerprint and generation commands](../evidence/2026-10-29_ingestion-existing20/provenance.json),
[36 reference artifacts](../evidence/2026-10-29_ingestion-existing20/reference_manifest.json)
and [24 query plans](../evidence/2026-10-29_ingestion-existing20/query_plans.json).
All 12 top-level tests and 11 subtests passed with no skips/failures on the final
source. Five exact-read regressions, the physical writer regression and 63
installation recovery subcases also passed. See the execution log for unit/vet
checks and earlier repetitions. No live PostgreSQL/MySQL parity is inferred.

Grant composition is explicit: RuntimeGrants supplies mutation/queue rights and
v005 lookup execution; ContractReadGrants adds the v006 metadata lookup contract.
Physical-table privileges are separate owner-coordinated grants. The standalone
contract-reader profile does not grant v005 record lookup or physical-table
access by itself. LegacyOperatorGrants supplies separate SELECT-only preview or
receipt/provenance import rights; neither profile grants physical writes. These
verified fixture profiles are not independent approval of a deployment role plan.


## Live parity and isolated adoption (2026-10-30)

This batch addresses 16 existing leaves, not the requested 20: IN-01.3.015f,
016f, 018f, 020f, 021f, 023f, 024f, 025f, 026f, 027f, 028f and IN-01.4.007,
008, 009, 010, 018. Completion is based on the retained verification linked below.
No new leaves were substituted for external gates. The remaining work requires
source evidence, retention/exception dispositions or independent acceptance.

### Implemented contracts and compatibility review

The actual PostgreSQL TenantDataReader/TenantDataWriter and staged MySQL
RecordProjection/RecordWriter execute against identical generated fixtures.
Tests use PostgreSQL 16.15 and MySQL 8.4.8, not SQL string mocks. Physical UUIDs
are generated independently and excluded from logical field comparisons.
Source text uses explicit C collation; this does not certify an unknown production
locale. Existing source characterization remains authoritative for pre-write
validation, including duplicate batch rejection before the row writer is called.

| Leaf | Implementation and acceptance |
|---|---|
| 015f | Explicit raw scalar normalization; six comparison operators, NULL checks, IP family/network/mask/host ordering. Live typed/raw input cases, malformed input rejection and IP pair comparisons exercise the real SQL reader. |
| 016f | String IN is byte-exact, case-sensitive and preserves trailing spaces; IP IN binds the full address/family/prefix tuple. Mixed NULL lists and NOT retain three-valued logic. Mutable byte arguments are detached at fragment and filter boundaries. |
| 018f | AND/OR/NOT and nested/empty groups match the source clause semantics. Predicate children are deliberately rejected; weighted-node/depth limits retain the staged safety policy. Empty OR and NOT(empty) produce no WHERE clause, matching source behavior. |
| 020f | Exact, search, list and six aggregate routes retain the same whole-statement parameter/byte/SQL limits. A valid 20-by-40 IP membership tree expands to 2,400 parameters and is rejected with 413 against the 2,048 cap. No batch-key read route exists; future routes must use this same composition. |
| 021f | Guarded projection/collection bounds precede transfer/decoding; list envelopes are checked before response. Text extrema guard value bytes inside SQL. Oversized stored values fail with 413, never truncated success. JSON preflight bounds nesting before handler metric traversal. |
| 023f | COUNT covers all six field types, including all-null/non-null IP values. It remains COUNT(field), not COUNT(*). Restricted API grants and live result/error cases are verified. |
| 024f | SUM retains integer exactness, finite float results and empty/all-null zero. Conservative float accumulation policy remains explicit below. Live results are compared with source and an independent four-row oracle. |
| 025f | Integer AVG uses exact signed-64-bit SUM/COUNT and PostgreSQL division scale/rounding, eliminating MySQL's four-place default. Repeating 1/3, zero, sign/scale boundaries and 64 source numeric-division cases are checked. Session precision settings 0, 4 and 12 cannot change the result. |
| 026f / 027f | MIN/MAX add bounded binary text and IP ordering to numeric/timestamp support. Nine representative host/prefix/family values are compared pairwise against PostgreSQL (81 pairs); every pair also checks all six scalar comparisons. |
| 028f | COUNT DISTINCT covers all six types, using full binary strings and complete IP tuples. Tests distinguish case, trailing spaces, Unicode spellings and strings sharing more than 2 KiB of prefix; NULL values are excluded. |
| IN-01.4.007 | Live single create/patch results, omitted fields, explicit NULL, patch-missing insertion and targeted alternate-unique failure match. |
| IN-01.4.008 | Live ordered batches/partial updates and late unique-error rollback match. Duplicate batches remain rejected by the source service and actual target API, as retained source characterization and TestProcessOrderedPatches verify. |
| IN-01.4.009 | Logical projections, nullable values, exact reads and limit ordering match on equivalent fixtures; existing current/time-window fixture semantics and isolated read/publication concurrency evidence remain applicable. |
| IN-01.4.010 | Supported aggregate outputs and empty sets match both the real source and an independent oracle. Numeric precision, long text and IP cases extend the earlier aggregate tests. |
| IN-01.4.018 | Actual API write/replay, exact/list/search/aggregate, upload creation/list/detail and deferred detail paths reject foreign scopes/resources. Trusted operator cancellation cannot mutate foreign work. Administrative HTTP routes are not exposed by this isolated router; operator composition is tested directly. |

Production wiring remains PostgreSQL/River. This is service/API contract adoption
in the isolated composition, not a deployment switch or authorization to migrate.
Tests use owner-authored shared fixture migrations, restricted API credentials,
publication locks and current owner authorization. No generic any-unique upsert
was introduced, and source PostgreSQL business logic was not changed.

### Deliberate limits and unresolved deployment decisions

- Inputs use explicit finite numeric, Boolean, RFC3339/microsecond timestamp and
  IP address/prefix normalization. Malformed/unsupported inputs are rejected as
  bounded client errors; raw database diagnostic text is not the API contract.
  The live raw-input matrix covers JSON numbers, numeric strings, bools,
  timestamps, NULL and malformed/overflow values. It does not promise every
  PostgreSQL session-dependent date/coercion syntax.
- Text matching/order is binary/C-compatible. The prior literal starts_with
  escaping policy remains: `%` and `_` in input are literals, unlike the source
  wildcard interpretation. Production collation and this safety difference must
  remain explicit in rollout review; no locale-independent equivalence is claimed.
- Predicate children are rejected rather than silently ignored. JSON is one
  document with at most 64 nesting levels, checked regardless of Content-Type
  before handler traversal. The CSV path retains its bounded multipart parser.
  AST depth 5, weighted nodes 50, group children 20 and membership items 100 remain.
- Whole read SQL limits are 2,048 parameters, 2 MiB parameter bytes and 1 MiB SQL.
  Read defaults/maximum are 100/1,000 rows. String transfer cap is 16 KiB per
  field, 2 MiB per transfer row, 1 MiB decoded row and 4 MiB page/envelope checks.
  These are conservative safety bounds, not production performance objectives.
- Float SUM/AVG preserve the conservative factor-two absolute-sum headroom and
  count <= 2^52 policy. A mathematically finite cancellation outside that bound
  can still be rejected; no silent zero or approximate success is returned.
  Existing threshold/overflow regressions plus live ordinary-value parity verify
  this policy. It is not unrestricted PostgreSQL float parity.
- Integer AVG rejects an unrepresentable signed-64-bit sum/count or out-of-range
  mapped result. The scale rule is checked against the live source and documented
  in [PostgreSQL 16 numeric.c](https://github.com/postgres/postgres/blob/REL_16_STABLE/src/backend/utils/adt/numeric.c)
  (`select_div_scale`). Output remains a JSON number without a float64 round trip.
- MIN/MAX(bool) and SUM/AVG(non-numeric) remain unsupported. IP comparison includes
  prefix and address family; IPv4-mapped IPv6 is not collapsed into IPv4. The
  write validator still accepts addresses, while read comparisons can represent
  retained prefixes. No network containment operator was invented.
- Result bounds do not bound database work. On the 1,000-row fixture, IP extrema
  use a full scan/filesort; unfiltered binary text extrema/distinct scan the table.
  The exact bound-key lookup retains const access/one estimated row, and indexed
  numeric range COUNT estimates ten rows. These are EXPLAIN observations, not
  latency, capacity, optimization or deployment-wide pool claims.

### Retained verification and reproduction

[Verification](../evidence/2026-10-30_ingestion-read-parity/verification.json),
[raw parity/record events](../evidence/2026-10-30_ingestion-read-parity/parity/raw.jsonl),
[raw process events](../evidence/2026-10-30_ingestion-read-parity/process-final/raw.jsonl),
[45 query plans](../evidence/2026-10-30_ingestion-read-parity/query_plans.json),
[reconciliation](../evidence/2026-10-30_ingestion-read-parity/reconciliation.json),
[source and binary provenance](../evidence/2026-10-30_ingestion-read-parity/provenance.json).
The accepted suites contain 77 parity/record tests, 9 process/query-plan tests and
26 handler/runtime tests, with zero failed or skipped tests. Scoped vet passed.
The DTO package has no standalone test files; this package event is recorded
separately. Rejected expectations and superseded runs are not accepted evidence.

Use only the existing isolated verifier and disposable PostgreSQL fixture:

```powershell
docker compose -p ingestion-parity -f backend/ingestion-service/compose.migration-test.yaml up -d postgres
docker network connect ingestion-parity_default ingestion-owner-v2-verifier
```

Do not repeat network connect if the verifier is already attached. The scripts
run from `/backend/ingestion-service` inside the verifier, with its existing
`INGESTION_REQUIRE_MYSQL_TEST=1` and `INGESTION_MYSQL_TEST_ADDRESS=mysql:3306`.
They require fresh output directories and retain raw output:

```sh
INGESTION_REQUIRE_PARITY=1 sh testdata/run-read-parity.sh testdata/new-read-parity
INGESTION_DATA_MODEL_FIXTURE_EXPORTER=/tmp/ingestion-dm-fixture sh testdata/run-read-process.sh testdata/new-read-process
```

Parity source connections use only the fixed disposable hostname/database and
create/drop generated `parity_*` schemas. No application schema is reset. The
fixture SQL setup is not a production export or legacy reconciliation. No new
dependency, production cleanup, service database split, commit or push occurred.

### Inputs still required to reach twenty completed tasks

The remaining ten leaves are IN-01.2.018/.054/.057/.062/.063/.064/.068/.069/.070
and IN-01.4.029. Independent schema/grants and shared Q-01 acceptance cannot be
replaced by self-review or ingestion component tests. Legacy inventory/export,
classification, reconciliation and transfer need an approved retained source
snapshot with original inputs, historical model attribution, audits/results and
pending River work. Missing evidence must remain an exception, never be invented.
Retention additionally needs reviewed replay/redrive horizons, holds, delivery
obligations and permitted archival/deletion scope; the existing
[retention contract](02_ingestion_operation_contracts.md) explicitly preserves
receipts, immutable actions and active work. There is no default safe production
deletion set. These inputs were requested; no response has been assumed.
