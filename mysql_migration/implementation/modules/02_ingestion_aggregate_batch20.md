# AVG, MIN, MAX and COUNT DISTINCT: 20 staged subtasks

Status: IN-01.3.025a-e, 026a-e, 027a-e and 028a-e verified, 2026-10-29.
Each parent retains its explicit f item for parity and runtime adoption. This is
20 completed granular implementation/verification items, not four production-ready
aggregate endpoints or closure of the ingestion migration.

## Source and shared implementation

The source is `backend/ingestion-service/internal/store/postgres/tenant_data_reader.go`,
AggregateRecords/buildAggregateExpression. It emits AVG(field), MIN(field),
MAX(field) and COUNT(DISTINCT field), delegating type compatibility and result
representation to PostgreSQL. Source behavior is not permission to assume every
MySQL type/collation is compatible.

New files below are relative to backend/ingestion-service/internal/store/mysql:

| File/function | Responsibility |
|---|---|
| record_aggregate.go: compileAggregate | Fixed operation allowlist, active selected single-column mapping, supported type checks, compiled filters and whole-statement budgets |
| record_average.go: AverageRecords / decodeRecordAverageDecimal | Nullable numeric AVG, decimal retention and guarded float result |
| record_extreme.go: MinRecords / MaxRecords / decodeRecordExtreme | Typed numeric/time extrema and explicit nullable aggregate contract |
| record_distinct.go: CountDistinctRecords | Non-null scalar distinct count with existing strict count decoder |
| record_float_accumulation.go: safeFloatAccumulation | Shared conservative SUM/AVG safety check; extracted from the already-tested SUM path |

No raw operation/field expression is accepted from callers. Names resolve through
RecordProjection; SQL contains validated physical identifiers. Unsupported,
archived, missing, wrong-case, SQL-looking and multi-column fields fail closed.
Compilation returns no partial query/arguments/schema on error. Parameters remain
owned and in depth-first order. Existing nil/empty group semantics are preserved.

The originating projection must be constructed with prior authorization in a
READ COMMITTED transaction; hold its shared publication barrier through execution.
These helpers do not independently authorize, verify transaction identity/isolation,
commit, cache or mutate. The conservative active/writable/open mapping gate remains.
The tests exercise real generated fixtures, not an adopted service/API owner path.

## Scope and result contracts

| Aggregate | Implemented inputs | Nonempty result | Empty/all-null result | Excluded pending review |
|---|---|---|---|---|
| AVG | int, float | RecordDecimal for int; finite float64 for float | nil | Boolean, timestamp, string, IP |
| MIN | int, float, timestamp | int64, finite float64, UTC time.Time respectively | nil | Boolean, string, IP |
| MAX | int, float, timestamp | int64, finite float64, UTC time.Time respectively | nil | Boolean, string, IP |
| COUNT DISTINCT | int, float, bool, timestamp | nonnegative int64 | int64(0) | String and IP |

### AVG: IN-01.3.025a-e

Integer AVG uses MySQL's decimal result. RecordDecimal retains the driver decimal
text without conversion through binary float. Parsing is limited to 64 bytes,
optional minus, decimal digits and an optional nonempty fractional part; scientific
notation, whitespace, NaN, fractional syntax errors and values outside INT32 input
range are rejected. Standard-library math/big verifies the bounded numeric range;
no new dependency was added. Byte input is copied and returned decimal values are
immutable strings. The helper returns nil plus error on decoding failure, not an
empty RecordDecimal hidden inside a non-nil interface.

The isolated fixture reports div_precision_increment=4. Tests pin 0.5000 and
0.3333, including negative values. The latter is rounded database output, not exact
one-third. Higher precision is preserved by the decoder if present; this does not
recover digits already rounded by the server. Changing session precision or public
JSON representation would require review. PostgreSQL numeric AVG precision and
public DTO compatibility remain unresolved; RecordDecimal is not a new API contract.

AVG selects a non-null count in the same statement; float AVG also selects maximum
absolute input. Only zero count permits a nil aggregate. A nonempty nil or invalid
result fails. Float accumulation reuses SUM's safety policy: at most 2^52 non-null
values, single finite input accepted, and for n>1 maximum magnitude at most the
next float below MaxFloat64/(2*n). Extreme cancellation may be rejected even when
the mathematical average is finite. This is a deliberately stricter staged policy,
not an approved replacement for PostgreSQL behavior. SUM's policy is unchanged;
its existing overflow regression passes after extraction of the shared helper.

### MIN: IN-01.3.026a-e; MAX: IN-01.3.027a-e

The shared internal implementation uses a fixed MIN/MAX operation, with separate
public methods and separate five-test suites. No Boolean ordering or text collation
is inferred from MySQL merely accepting an expression. Numeric extrema preserve
INT32 storage bounds and finite doubles; timestamps retain UTC microsecond precision
and MySQL's supported year range. Tests cover MinInt32/MaxInt32, both finite double
extremes, year 1000 and 9999 with microseconds, and timezone normalization.

Each query returns the extremum and non-null count together. Empty/all-null input
returns nil even for a required source field. A nonzero count with NULL output,
zero count with a non-NULL output, invalid count, invalid type/range/time precision
or archived field fails. This avoids mistaking malformed data for an empty aggregate.
The extra COUNT may affect index-only extrema plans; no plan benefit is assumed.

### COUNT DISTINCT: IN-01.3.028a-e

Only full single-column numeric, Boolean and timestamp values are compared. No
prefix, hash, truncation or string cast approximates equality. Real tests verify
that duplicates collapse, NULL is omitted, false and true remain distinct, adjacent
representable doubles remain distinct, +0/-0 compare equal, and timestamps separated
by one microsecond remain distinct. Repeating timestamps reduces the count as
expected. COUNT's existing decoder rejects NULL, negatives, overflow and unexpected
representations; caller must check errors when the returned numeric value is zero.

String collation/trailing-space/full-LONGTEXT equality and multi-column IP semantics
remain excluded, not claimed complete. No DISTINCT sort/temp-table memory or scan
cost bound follows from the one-scalar result.

## Budgets, execution and failures

All four operations reuse CompileFilter and ValidateRecordQuery. Exact/over
parameter count, parameter-byte and SQL-byte boundaries are tested per aggregate.
Invalid statements fail before touching a closed transaction. Context cancellation,
closed/nil transactions, filter errors and decoder errors propagate. No input-row
LIMIT, GROUP BY or window syntax changes the aggregate's source semantics.

Outputs are one to three bounded scalar columns; statement and output bounds do
not bound rows scanned, CPU, sort/temp-table space, locks or elapsed time. Deadlines,
concurrency policies and representative EXPLAIN/load evidence remain required.
There is no performance optimization claim. Queries read one statement snapshot;
separate requests may differ as ingestion changes the data.

## Twenty-subtask evidence map

| Checklist group | a | b | c | d | e |
|---|---|---|---|---|---|
| 025 AVG | mapped numeric query | bounded decimal retention | guarded float AVG | nullable/error contract | integration/boundaries |
| 026 MIN | mapped MIN | typed extrema | empty/null | filtered budgeted execution | boundary/failure tests |
| 027 MAX | mapped MAX | typed extrema | empty/null | filtered budgeted execution | boundary/failure tests |
| 028 DISTINCT | mapped distinct query | numeric equality | Boolean/time/NULL | filters/count decoding | failure boundaries |

`record_average_test.go` provides five AVG tests. `record_extreme_test.go` provides
five MIN and five MAX tests over shared assertion helpers. `record_distinct_test.go`
provides five distinct-count tests. `record_aggregate_test.go` holds reusable
mapping/budget/error assertions, executed separately for all four public operations.
The tests use existing scalarFixture/queryLimits and disposable shared metadata
fixtures, with fixture updates/deletions rolled back. No application database is
used and no new test framework is introduced.

Verification command: isolated ingestion-owner-v2-verifier, go vet ./..., then
`go test -mod=readonly -race -count=1 -timeout=180s -v ./internal/store/mysql -run TestRecord`.
Result: 55 top-level tests and 10 subcases passed, zero failed/skipped. This includes
all 20 new aggregate tests and 35 existing record tests, including SUM safety,
COUNT, projection publication/tenant mapping, decoding, filters and result budgets.
Evidence: [aggregate batch log](../evidence/2026-10-29_ingestion-aggregate-batch20.log).

## Remaining acceptance

025f/026f/027f/028f remain open for reviewed type/precision policies, live
PostgreSQL/MySQL parity, excluded types, public result/error DTOs, restricted runtime
grants, authorized endpoint adoption, concurrent publication/state acceptance and
representative query plans. The conservative AVG float policy and decimal string
representation are especially material review items. These tests do not establish
full-stack production readiness or tenant authorization at an API endpoint.

Runtime remains PostgreSQL/River. No production configuration, deployment schema,
application data or dependencies were changed in this batch. Pre-existing unrelated
workspace changes were retained; nothing was committed or pushed.
