# Staged membership predicates

IN-01.3.016a-e, 2026-10-29. Full compatibility/runtime adoption remains 016f.
Source paths below are relative to backend/ingestion-service.

## Source and scope

PostgreSQL `internal/store/postgres/tenant_data_reader.go`,
buildAggregatePredicateSQL, accepts IN only with concrete nonempty []any and
preserves every parameter, including NULL and duplicates. Domain aggregate.go
sets MaxAggregateInListItems=100 but allows an empty list that the repository then
rejects. The staged compiler preserves repository empty-list rejection and enforces
the existing domain count limit directly at compilation.

`internal/store/mysql/record_membership.go` adds
`(*RecordProjection).MembershipPredicate` and a transaction-local SQL fragment.
Only an active, selected, single-column field from the validated projection is
eligible. Field names are whitespace-trimmed and case-sensitive; physical columns
come from publication-checked mappings. Unknown, archived, SQL-looking or
multi-column fields fail. Neither values nor logical field names enter SQL syntax.

Supported normalized element types match the staged scalar compiler:

| Field | Non-null element |
|---|---|
| int | int64 within signed INT32 range |
| float | finite float64 |
| bool | bool |
| timestamp | time.Time converted to UTC, years 1000..9999, exact microsecond precision |

Every supported type also accepts nil, including required fields. String and IP
membership are rejected until collation/multi-column semantics are resolved.
Raw JSON coercion is not implemented. This is not yet a drop-in replacement for
the source repository, which delegates raw-value type handling to PostgreSQL.

## Compilation and SQL semantics

Lists must be concrete []any with 1..100 items. Validate that bound before element
iteration/allocation. Emit one placeholder per item and retain order, duplicates
and NULL; do not sort, deduplicate, drop NULL or interpolate values. Non-null
normalization reuses encodeWriteValue. The fragment owns its argument slice and
SQL() returns a new copy; accepted elements do not retain caller-owned byte slices,
maps or nested lists. Any invalid element, including the last of 100, returns
ErrRecordMembership and no SQL/arguments. No partial fragment escapes.

IN retains SQL three-valued logic: a matching non-null item yields true; a
nonmatching value with a NULL list member yields unknown; a NULL stored value also
yields unknown. WHERE retains only true. Negating unknown still produces unknown.
Tests inspect `(predicate) IS NULL` and `NOT (predicate)` directly to distinguish
unknown from false. This does not add a not_in operator to the supported API.
Empty input is an error, not a constant-false rewrite or unrestricted query.

## Ownership, bounds and remaining work

Caller authorization and READ COMMITTED transaction ownership remain required.
Construct RecordProjection under its publication barrier and retain that transaction
through query execution and result collection. Use membership fragments only with
their originating projection/table. The SQL string interface does not enforce
lifetime, authorization or composition by itself. It performs no query or mutation.

Per-fragment count is bounded; whole-query parameter count, nesting and payload
budgets remain IN-01.3.020. Tests compose with bounded projection/collection and an
explicit SQL LIMIT. Production filter-tree/aggregate endpoints must enforce those
execution controls before activation. No index, throughput or optimizer claim is
made; representative query plans remain pending.

016f requires reviewed raw-input normalization/error mapping, live PostgreSQL/MySQL
parity, resolved string/IP coverage and authorized runtime composition. Parent 016
remains IN_PROGRESS. No dependency, schema, application database or runtime config
changes are part of this batch; PostgreSQL/River remains active.

## Verification

Five race-enabled isolated MySQL tests in record_membership_test.go cover:

- Parameterized physical mapping, list order/duplicates and input/output ownership.
- Typed integer/float/Boolean/timestamp membership, extreme numeric values and
  equivalent timestamp offsets using the staged bounded decoder/collector.
- Mixed/all-NULL lists, explicit UNKNOWN counts, negation and required-field NULL.
- One and 100 items accepted/executed; empty/nil/101-item/wrong-container rejection.
- Wrong element types, overflow/non-finite floats, invalid time precision, unknown
  and SQL-looking fields, unsupported types and late-item all-or-nothing failure.

Five scalar regression tests also exercise the shared query-result test helper.
Evidence: [membership and scalar test log](../evidence/2026-10-29_ingestion-membership.log).
go vet ./... and all ten race-enabled tests passed without skips in the
existing disposable MySQL verifier. This evidence does not replace production grants, source parity,
concurrent deployment or scale acceptance.
