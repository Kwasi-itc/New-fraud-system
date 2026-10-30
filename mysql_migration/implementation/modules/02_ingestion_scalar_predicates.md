# Staged scalar predicates

IN-01.3.015a-e, 2026-10-29. Production compatibility/adoption remains 015f.
Paths below are relative to backend/ingestion-service.

## Implementation and ownership

`internal/store/mysql/record_predicate.go` adds
`(*RecordProjection).ScalarPredicate` and immutable `ScalarPredicate` fragments.
`record_projection.go` retains the validated physical first-component column for
each requested logical field. The compiler accepts only fields in this projection;
no independent schema lookup, logical-name SQL interpolation or implicit field
reference is performed. Unknown, archived, unmapped or multi-column fields fail.

Construct the projection with NewRecordProjection in an already authorized
READ COMMITTED transaction. Its tenant/table/publication mapping and shared
publication barrier apply. Use the fragment only with this projection's table
while retaining that transaction. A fragment is neither an authorization token nor
a cross-transaction cache item; the SQL string API cannot enforce caller lifetime.
No new route or production repository is wired by this change.

Field lookup trims surrounding whitespace and preserves case; operators trim and
lowercase, matching the source aggregate predicate builder. This does not change
exact-read object identity or invent field-to-field comparison semantics.

## Accepted semantics

| Field type | Comparison inputs | Operators |
|---|---|---|
| int | int64 within signed INT32 bounds, or nil | eq, neq, gt, gte, lt, lte |
| float | finite float64, or nil | eq, neq, gt, gte, lt, lte |
| bool | bool, or nil | eq, neq, gt, gte, lt, lte; false precedes true |
| timestamp | time.Time convertible to UTC year 1000..9999 with exact microsecond precision, or nil | eq, neq, gt, gte, lt, lte |
| All above plus string | Value ignored | is_null, is_not_null |

Comparison values use placeholders, with exactly one argument; null checks use
none. The compiler reuses encodeWriteValue for non-null normalized scalar values,
including DateTime conversion and validation. It does not coerce JSON float64 to
integer, parse strings into numbers/dates, accept numeric booleans, or round time.
Returned argument slices are detached; accepted values have no caller-owned
mutable slices/maps. A failure returns ErrRecordPredicate and an empty fragment.

Comparing to nil binds SQL NULL, including required fields; it does not invoke
write-time nullable rejection or rewrite eq/neq into null tests. Comparisons yield
UNKNOWN and WHERE excludes those rows. neq likewise excludes null stored values.
Explicit null tests preserve the source behavior of ignoring a supplied Value.

String comparisons/patterns and all multi-column IP predicates are rejected
pending their contracts. Lists, nested groups and arbitrary operator strings also
fail; they are not silently interpreted as scalar values or identifiers.

## Compatibility limits

PostgreSQL buildAggregatePredicateSQL currently binds raw values and lets the
database resolve/coerce types. This staged normalized-input interface is stricter;
passing its tests does not establish PostgreSQL parity or authorize changing API
acceptance behavior. IN-01.3.015f must resolve raw-value normalization and HTTP error
mapping, then compare live PostgreSQL/MySQL outputs for supported input types.
String collation/IP ordering remain separate decisions. No temporal visibility
predicate is introduced by timestamp comparisons.

A scalar fragment has at most one parameter and resolves at most 100 selected
projection fields. Combined-query parameter/payload/nesting budgets remain 020;
this is not a complete filter-tree compiler or an aggregate execution path.
Callers still need bounded SQL row limits and RecordProjection.Bounded/Collect for
result transfer and decoding. No index or query cost improvement is claimed;
representative plans and concurrent schema/ingestion acceptance remain open.

## Verification

`record_predicate_test.go` contains five race-enabled isolated MySQL tests:

- Mapped columns, normalized operator names, detached arguments, unknown/case-
  mismatched/SQL-looking fields, multi-column rejection and a nil projection.
- All six numeric comparisons over negative/zero/positive/null rows, integer
  bounds and a finite float boundary.
- All six Boolean/timestamp comparisons, exact microsecond equality and equivalent
  timezone-offset conversion.
- NULL comparison truth tables, explicit null checks with ignored values, nullable
  and required columns, and null exclusion for neq.
- Malformed types, numeric overflow, NaN/infinity, excessive timestamp precision,
  invalid timestamp years and unsupported operators/types fail with no fragment.

Queries combine compiled predicates with the staged bounded projection
and collector. Five new tests, 20 existing projection/decoder/bounds/exact-read
tests plus ten regression subcases passed without skips; go vet ./... passed.
Evidence: [scalar tests](../evidence/2026-10-29_ingestion-scalar-predicates.log),
[regressions](../evidence/2026-10-29_ingestion-scalar-regression.log).
Only generated disposable databases were used. Runtime stays PostgreSQL/River.
