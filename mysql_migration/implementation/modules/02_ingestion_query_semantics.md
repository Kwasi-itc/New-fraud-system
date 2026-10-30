# Ingestion source read/query semantics

Status: audited and characterized, 2026-10-29; IN-01.3.010a-e.
Scope: the current PostgreSQL ingestion reader and its API/service boundaries.
This is not a MySQL reader implementation or approval to change business semantics.

## Source map and evidence

Paths below are relative to the repository root; links resolve to source files.

- [TenantDataReader](../../../backend/ingestion-service/internal/store/postgres/tenant_data_reader.go): GetRecord, ListRecords, QueryRecords, AggregateRecords, buildAggregateExpression, buildAggregateFilterSQL and buildAggregatePredicateSQL.
- [Identifier quoting](../../../backend/ingestion-service/internal/store/postgres/identifier.go): sanitizeIdentifier; [schema resolution](../../../backend/ingestion-service/internal/store/postgres/tenant_data_writer.go): schemaNameForModel/tenantSchemaName.
- [Domain query](../../../backend/ingestion-service/internal/domain/ingestion/aggregate.go): AggregateQuery/Filter, validation, shape measurement and normalization.
- [Model/type definitions](../../../backend/ingestion-service/internal/domain/ingestion/model.go) and [write normalization](../../../backend/ingestion-service/internal/domain/ingestion/validation.go): six logical field types. Write validation is not read decoding.
- [Service](../../../backend/ingestion-service/internal/service/ingest_service.go): readTableModel and four record read methods.
- [Handlers](../../../backend/ingestion-service/internal/httpapi/handlers/ingest.go): read route parameters, JSON binding, concurrency gates and aggregate timeout; [errors](../../../backend/ingestion-service/internal/httpapi/handlers/error_response.go): classifyServiceError.
- [Router](../../../backend/ingestion-service/internal/httpapi/router.go) and [auth middleware](../../../backend/ingestion-service/internal/httpapi/auth.go): production PostgreSQL/read-pool selection and service-token route boundary.
- [Contract store](../../../backend/ingestion-service/internal/store/postgres/ingestion_contract_store.go) and [client](../../../backend/ingestion-service/internal/clients/datamodel/contract_reader.go): tenant/table metadata and publication version; metadata retrieval and physical read are separate operations.

New [SQL characterization tests](../../../backend/ingestion-service/internal/store/postgres/source_read_contract_test.go) and [validation characterization](../../../backend/ingestion-service/internal/domain/ingestion/aggregate_characterization_test.go) pin surprising behavior without changing production logic. Reused tenant_data_reader_test.go, aggregate_test.go, ingest_handler_test.go, error_response_test.go and ingest_service_read_path_test.go.

Evidence: [source read test log](../evidence/2026-10-29_ingestion-source-read-contract.log).
Module-wide go vet and runnable race-enabled tests passed. PostgreSQL integration
cases skipped because INGESTION_TEST_DATABASE_URL is unset. Capture tests stop
before database execution; fake aggregate return values are not codec/SQL-result
proof. No application database or new test database was modified for this batch.

## A. Projections, identities and types (IN-01.3.010a)

| Operation | Actual selection and response |
|---|---|
| GetRecord | SELECT * from schema/table, lookup field = $1, LIMIT 1. No ORDER BY. pgx.CollectOneRow/RowToMap; no row propagates its error. Handler returns {record: result}. |
| ListRecords | SELECT * ordered only by updated_at DESC, parameterized LIMIT. Handler returns {records: [...]}. |
| QueryRecords | Same full-row projection with exactly one field = $1 predicate; value is a string from the URL. No general filter tree on this endpoint. |
| AggregateRecords | Exactly one scalar aggregate expression, optional aggregate filter; result scanned into any and returned as {value: value}. |

SchemaName from the model overrides the derived tenant_<UUID-without-hyphens>
schema. Table lookup uses model.Tables[objectType], then table.Name for SQL.
Identifiers are double-quoted with embedded quotes escaped; values use pgx
parameters. Search and aggregate fields must exist in table.Fields or equal
RecordLookupField. This is a name allowlist, not a typed predicate validator.
Archived flags and per-field visibility are not separately checked in this
repository; do not infer protection beyond the actual upstream contract.

Record reads expose all physical columns through SELECT *, including system
columns. They do not implement caller-selected projections or hide unmodeled
columns. Top-level values whose Go type is exactly uuid.UUID become strings;
other values, including nil, retain pgx RowToMap representations. List/search
synthesize a missing lookup-field entry from trimmed fmt.Sprint(object_id);
GetRecord does not. The service derives the public object_id from the lookup
field. No recursive UUID conversion or shared typed decoder is present here.

Logical model types are bool, int, float, timestamp, ip_address and string.
Write normalization accepts explicit booleans (and true/false strings), numeric
normalizers, RFC3339 timestamps normalized to UTC, valid IP strings, and strings;
nullable and enum checks belong to write validation. Those checks do not run on
read predicate values or database results. Actual driver representations and
serialization boundaries for all six types, signed limits, floating-point values,
IPv4/IPv6, nulls and timestamp precision remain IN-01.3.012 integration work.
No decimal/exact-numeric or collection read type is asserted by this audit.

Tenant isolation in these SQL statements relies on the trusted model's physical
schema, not a tenant_id row predicate. The service obtains table metadata first;
configured readDataReader avoids the write transaction manager, otherwise it uses
that manager's TenantReader. Source router authentication is a configured shared
service token (or explicitly disabled mode), not proof of per-tenant operator IAM.
The MySQL port must use its authorized owner contract and stable mapped identities;
copying PostgreSQL schema naming is not sufficient authorization.

## B. Scalar, string, collection and null predicates (IN-01.3.010b)

These operators exist in the aggregate filter builder only. Op is trimmed and
lowercased; field name is trimmed but retains case for map lookup and SQL quoting.

| Operator | SQL shape | Parameter and edge behavior |
|---|---|---|
| eq / neq | column = $n / column <> $n | One raw Value; nil is still a bound comparison, not rewritten to IS NULL. |
| gt / gte / lt / lte | > / >= / < / <= | One raw Value; no field-type comparison/coercion policy in builder. |
| in | column IN ($n, ...) | Requires concrete []any and at least one element in repository; []string is rejected. Null members are retained. Domain validator permits empty []any but repository rejects it. |
| is_null / is_not_null | column IS NULL / IS NOT NULL | No parameter; supplied Value is ignored. These test SQL null, not absent payload fields. |
| starts_with | column LIKE ($n || '%') | Raw Value is bound unchanged. %, _ and backslash are not escaped; this is SQL pattern-prefix behavior, not guaranteed literal prefix matching. |

Parameter order follows depth-first input order. Values never become identifiers
or field references. Unsupported operators include not_in, contains, contains_any,
ends_with, regex, exists and between. NOT around supported predicates can express
logical negation, but it retains SQL null semantics. Boolean and date comparisons
use generic operators against appropriate database columns; no specialized boolean,
calendar, timezone, relative-window or field-to-field evaluator is implemented.

SQL null comparisons can produce unknown; WHERE retains only true conditions.
Thus neq is not an inclusion rule for null rows, and NOT does not turn unknown
into true. This follows generated SQL, not an in-memory rule evaluator. Case,
collation, pattern escaping, database coercion and unsupported type/operator
combinations require PostgreSQL/MySQL integration evidence before port acceptance.
Do not claim case-insensitive matching: there is no ILIKE or explicit folding.

Compatibility decision: IN-01.3.017 asks for wildcard escaping, but the source
currently does not escape. Literal-prefix correction would change business
behavior and must have an explicit reviewed disposition; no correction is made
in this audit. SQL-generation characterization preserves the observed argument.

## C. Logical groups and validation limits (IN-01.3.010c)

Kind defaults to group when empty. Group operator defaults to and; and/or/not
are supported recursively, with trimmed lowercased names. NOT requires exactly
one child. Children become parenthesized clauses and parameter order is preserved.
The builder does not specify database short-circuit order or cost-based evaluation.

Empty AND and empty OR emit no SQL. Empty children are omitted. NOT of a child
that emits nothing also emits nothing. Consequently an empty top-level OR does
not yield false; it produces an unfiltered aggregate. Empty groups mixed with
other children disappear rather than contribute Boolean identities. This is
characterized behavior that needs a compatibility disposition before changes.

Published constants: depth 5, nodes 50, children/group 20, IN items 100. Domain
validation enforces operators, group arity/child limits and list type/max count;
repository builders independently check fields and reject empty IN. Direct
service/repository calls do not invoke ValidateAggregateQuery, so handler-level
limits are not universal repository guarantees.

Two concrete gaps were found in the source validator:

1. validateAggregateFilter recursively calls measureAggregateFilter at every
   visited node, recounting descendants. A flat group with 20 leaves has 21
   actual nodes and weighted count 41 (accepted). Wrapping it in one group has
   22 actual nodes, depth 3, but weighted count 63 (rejected by the limit of 50).
   MeasureAggregateQueryShape reports the actual 22. A new test pins this mismatch.
2. Shape measurement/normalization in the HTTP handler occurs before validation.
   Recursive measurement also descends before child depth validation. The stated
   maximum depth therefore does not bound all prevalidation traversal/allocation.
   Predicate children are not semantically built, although measurement traverses
   them. No malformed deep-input safety guarantee is inferred.

NormalizedAggregateQueryShapeKey sorts child shape strings and omits values for
metrics grouping; it is not an execution plan/cache key or proof of equivalence.
No optimizer, predicate reuse or result cache is implemented by TenantDataReader.

## D. Temporal reads, ordering and pagination (IN-01.3.010d)

Record SQL has no valid_from/valid_until predicate, despite these names appearing
in ManagedSystemFields. There is no as-of input, temporal range selector, implicit
current-version filter or event-time watermark. Aggregate timestamp predicates
can express caller-supplied >= / < windows, but the caller must construct them;
there is no built-in velocity/history operation in this repository.

List/search order only by updated_at DESC. Equal timestamps have no deterministic
secondary key. GetRecord LIMIT 1 has no deterministic order if the physical lookup
is not unique. There is no cursor, offset, total-count query, caller-selectable
sort or pageable continuation in the port/interface. Do not advertise current
pagination as stable or promise history version selection.

HTTP list/search default to 100. Noninteger limit returns 400; any parsed integer
passes to the service. Repository limit <=0 resets to 100; positive values have
no maximum clamp. Search requires a nonempty field parameter; missing value is
an empty string. Neither handler nor repository bounds individual result bytes,
column bytes or total response bytes. These remain IN-01.3.019/020/021 work.

IN-01.3.014 is conditional: there is no source validity-time predicate to port.
Its next implementation must explicitly document current-row behavior or obtain
an approved temporal contract, rather than inventing an as-of capability. Stable
tie-breaking and bounded pagination are target improvements requiring a documented
public behavior decision, not existing source functionality.

## E. Aggregates, errors and execution controls (IN-01.3.010e)

Aggregate names are trimmed/lowercased by the repository. A valid field is required
for every function; count without a field is accepted by domain shape validation
but rejected by the repository. No COUNT(*) special case is implemented.

| Aggregate | Generated expression | SQL-level implication; live parity tests still required |
|---|---|---|
| count | COUNT(field) | Counts non-null field values, not every filtered row. |
| count_distinct | COUNT(DISTINCT field) | Counts distinct non-null values; equality depends on database type/collation. |
| sum | COALESCE(SUM(field), 0) | All-null/empty aggregate produces zero; type/overflow must be verified. |
| avg | AVG(field) | Null inputs omitted; empty/all-null result is null. |
| min / max | MIN(field) / MAX(field) | Null inputs omitted; empty/all-null result is null; comparison follows SQL type/collation. |

Repository does not validate function/type compatibility. Returned values are
scanned into any without normalized scalar output type. Existing aggregate tests
use fake returned int64/float64 values, so they do not prove database result types,
exact precision or overflow behavior. There is no GROUP BY, HAVING, aggregate
window function, multi-aggregate query or historical baseline helper. IN-01.3.029
must record unsupported grouping rather than implement an assumed feature.
IN-01.3.023's generic row-count wording must preserve COUNT(field), not replace it
with COUNT(*) accidentally.

HTTP aggregate uses ShouldBindJSON, then shape measurement and validation. No
explicit UseNumber setting or body byte cap was found on this handler path;
write-path UseNumber does not establish aggregate numeric precision. A positive
configured aggregate timeout wraps metadata lookup and SQL execution, but starts
after JSON parsing/shape validation. List/get/search use request context without
that aggregate timeout. Read and aggregate concurrency gates are separate local
channels: nonpositive configuration disables each; saturation rejects immediately
with HTTP 429 instead of queueing. These do not implement a database-wide budget.

| Failure | Observed HTTP classification |
|---|---|
| Invalid tenant UUID, malformed limit/JSON, missing search field, domain shape rejection | 400 bad_parameter / invalid_request. |
| Read or aggregate gate saturation | 429 read_path_overloaded or aggregate_path_overloaded. |
| context.DeadlineExceeded / context.Canceled | 504 request_timeout / 408 request_canceled. |
| Recognized dependency error text | 502 dependency_failure. |
| Unknown object/field/operator, missing aggregate field, pgx no rows, ordinary SQL/codec errors | Default 500 internal_error unless they match another classifier branch. No dedicated 404 or typed read-error mapping exists here. |

Errors propagate from repository to service; SQL failures do not trigger an HTTP
fallback. Query metadata is obtained before physical SQL with no publication lock
or version recheck held through execution. Archive/rename/recreation races and
read authorization/barrier integration remain IN-01.3.002/003/008/009. The SQL
contract store has versioned metadata reads, but this is not an execution barrier.

## Follow-on acceptance cases

- IN-01.3.011/012: explicit mapped projections and six-type database round trips,
  including null, large integers, floating-point edge values, IP families and time
  precision. Record exposure of physical/system fields needs an explicit contract.
- IN-01.3.013/014/019: exact lookup/no-row semantics, no invented validity-time
  filtering, tied updated_at order, page limits and schema transitions.
- IN-01.3.015-018: every supported operator, nil comparisons, null list members,
  empty lists/groups, literal versus wildcard prefixes, nested parameter order and
  approved node-budget semantics. Test at the repository boundary, not only HTTP.
- IN-01.3.020/021: pre-decode payload bound, bounded traversal, one total parameter
  budget and bounded result bytes before large allocations; distinguish policy
  changes from source compatibility.
- IN-01.3.023-029: independently calculate null/empty aggregates, COUNT(field),
  precision/overflow and collation-sensitive DISTINCT/MIN/MAX on both databases.
- IN-01.3.022/030/031: authorized routes, intentional error DTOs, publication barriers
  and representative query plans before claiming reader readiness or performance.

Five audit subtasks are complete. The MySQL query builders, typed decoder,
endpoints and the broader IN-01.3 parent remain unfinished. This batch changes
characterization tests and documentation only; runtime stays PostgreSQL/River.
