# Staged record result bounds

IN-01.3.021a-e, 2026-10-29. Component implementation; parent IN-01.3.021 remains
in progress until production record/aggregate/HTTP paths adopt approved limits.
Source: [record_bounds.go](../../../backend/ingestion-service/internal/store/mysql/record_bounds.go)
and [tests](../../../backend/ingestion-service/internal/store/mysql/record_bounds_test.go).

## Construction and limits

RecordProjection.Bounded compiles an immutable wrapper around the transaction-local
mapped projection. The caller's tenant authorization and publication transaction
remain mandatory. This component neither grants read access nor supplies filters,
ordering, row selection or an HTTP route. The caller selects explicit budgets;
these validation ceilings are implementation bounds, not production SLOs/defaults.

| Limit | Allowed range and meaning |
|---|---|
| FieldBytes | 1..1 MiB per projected string; UTF-8 validity is still checked by the typed decoder. |
| TransferBytes | 1..16 MiB worst-case selected-value bytes for one row; excludes protocol framing, driver/object overhead and database internal work. |
| RowBytes | 1..16 MiB conservative JSON-size accounting for one decoded record. |
| PageBytes | At least RowBytes, at most 64 MiB; conservative JSON-array accounting including brackets and separators, not an HTTP response envelope. |
| Rows | 1..1000 decoded records. A further row is an error, never silently truncated. |

Construction budgets FieldBytes+64 for each string and its length marker, and 64
bytes per fixed physical component. It rejects a projection whose worst-case
selected values exceed TransferBytes. With the existing 100-field projection cap,
this prevents a large number of individually legal strings from yielding an
unbounded driver row. Logical key lengths are also checked before multiplication.
Physical scalar size assumptions depend on the verified published schema; this
component does not replace installation/readiness drift checks.

## SQL string guards

For each mapped string column the projection emits:

    OCTET_LENGTH(column),
    CASE WHEN OCTET_LENGTH(column)<=? THEN column ELSE NULL END

The parameter is FieldBytes. Qualified table/columns come only from the existing
validated projection. SQL() returns a fresh argument slice. Each string adds one
parameter; the future combined query builder must charge these against its total
parameter budget (IN-01.3.020). Fixed-width scalar/IP components remain explicit
columns in their existing order. No SELECT * is generated.

Oversized storage returns its original length and NULL, so it can be rejected
without transferring the large content. Actual SQL NULL yields a NULL length and
NULL value and is checked against schema nullability. The decoder rejects negative
or malformed lengths, a non-null value with null marker, and byte-length mismatch.
Oversized markers are limit errors, not nullable successes. The server still reads
or evaluates stored values as required; suppressed transfer is not a claim of
reduced database I/O, improved query plans or throughput.

## Row and page accounting

Before copying any string into the typed output map, the entire row is checked.
Accounting reserves two bytes for braces, six bytes per UTF-8 key byte plus four
bytes for key/punctuation, up to six bytes per string byte plus quotes, and 64
bytes for each non-string logical value (including timestamps and IP prefixes).
Null strings cost four bytes. This deliberately overestimates encoding/json's
escaping needs and may reject output smaller than the configured cap. It is not
an exact serializer-size measurement, and no custom/raw JSON values are accepted.

Only after row validation does DecodeRecordValue produce copied strings and typed
values. A malformed later field returns no partial record. Collect owns sql.Rows,
checks the row-count and total page budgets, propagates Scan/Rows.Err/Close failures,
and closes rows on every exit. Any failure returns a nil page, discarding earlier
successful rows. Empty success returns an empty slice.

The collector may hold one additional bounded decoded row before discovering that
it exceeds the remaining page budget. Live memory therefore includes the bounded
page, one bounded driver row, one bounded decoded row, and normal maps/slices/driver
overhead. Limits are not exact process-RSS caps. The caller must also put a bounded
LIMIT in SQL (typically Rows+1 for overflow detection); early collector closure is
not a database execution-cost bound. A raw unguarded SQL() from RecordProjection is
still available for internal composition and must not be exposed by production
read paths without these or equivalent guards.

## Verification

[Evidence](../evidence/2026-10-29_ingestion-record-bounds.log): module-wide go vet
and five race-enabled tests, including four page-boundary subcases. Isolated MySQL
checks prove a 4096-byte stored string returns only its length/NULL when the field
cap is eight bytes; exact row/page boundaries, oversized rows/pages and excess rows
produce the expected complete success or no partial result. Unit checks cover
configuration limits, argument-slice ownership and NULL/length-marker distinctions.

A generated restricted account with SELECT on only the projected physical column
can run the guarded expression, while SELECT id is denied. Cancellation and scan
arity mismatch return no page and release the connection. Tests use existing
recordFixture publication/mapping setup in disposable schemas; production IAM,
full installation acceptance and database-scale performance are not established.

## Outstanding adoption (IN-01.3.021f)

All production record and aggregate readers must use approved budgets, bounded
queries and authorized publication-stable execution. HTTP envelopes, aggregate
representations, total query parameters, serialization compatibility, deadlines
and deployment policies remain open. Earlier per-field decoder and raw projection
contracts are now supplemented by this optional component, not globally replaced.
Runtime remains PostgreSQL/River. No dependencies, application migrations or
production read behavior changed in this batch.
