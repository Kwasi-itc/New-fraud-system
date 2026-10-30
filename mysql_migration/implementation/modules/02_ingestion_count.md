# Staged COUNT(field)

IN-01.3.023a-e, 2026-10-29. Production parity/adoption remains 023f.
Source paths below are relative to backend/ingestion-service.

## Source semantics and supported scope

PostgreSQL `internal/store/postgres/tenant_data_reader.go`, AggregateRecords and
buildAggregateExpression, require a field and emit COUNT(field). They do not
special-case a missing field as COUNT(*). The staged implementation preserves
non-null counting, including zero, false and empty non-null values, and returns
zero for empty input or all-null values. It does not count only truthy values.

`internal/store/mysql/record_count.go` adds `(*RecordProjection).CountRecords`.
The target field must already be selected in the validated projection, active,
mapped and single-column. Supported fields are int, float, bool, timestamp and
string. Whitespace trimming follows source field lookup; case is preserved.
Unknown, empty, SQL-looking and multi-column IP fields fail with ErrRecordCount.
No physical component is arbitrarily chosen as a substitute for an IP aggregate.
COUNT DISTINCT, SUM, AVG, MIN and MAX are not implemented by this helper.

## Compilation and execution

COUNT uses the validated physical identifier and the projection's qualified table.
The optional filter delegates to CompileFilter; only its staged supported leaf and
group capabilities apply. Nil/empty filters omit WHERE, including empty OR, in
accordance with the source contract. Parameters remain ordered and detached.
Invalid field/filter/budget input returns no SQL or arguments.

ValidateRecordQuery checks the entire generated statement before QueryRowContext.
COUNT has no projection guard or LIMIT parameters; it transfers one scalar rather
than selected record values. It deliberately does not apply record-page limits to
the aggregate input. A scalar result and statement budget do not bound the number
of rows scanned, query CPU/I/O, or execution duration. Callers must provide suitable
context deadlines and concurrency policies, and representative query plans remain
IN-01.3.031. No performance improvement is claimed.

The caller must use the originating projection's authorized READ COMMITTED
transaction and retain the publication barrier through execution. Transaction
identity/isolation and authorization remain caller contracts; this helper does not
open or commit a transaction. There is no cache, external call or mutation in the
implementation. Projection construction still requires the conservative published,
active/writable/open mapping gate inherited from the staged reader.

## Result and error contract

The driver result is decoded through the existing bounded integer decoder and
must fit nonnegative signed int64. Accepted representations are int64, uint64
within signed range, and integer byte strings of at most 20 bytes. NULL, negative,
overflowed, fractional, floating-point, Boolean and string-typed results fail with
ErrRecordCountResult rather than truncating, wrapping or returning success.

Real MySQL tests confirm decoded non-null and zero counts. Max-int64 and overflow
checks are decoder unit cases, not claims of having generated billions of rows.
Query/cancellation/closed-transaction errors propagate. The numeric zero returned
alongside an error is not a successful zero count; callers must check the error.
The HTTP result/error DTO is not wired or changed in this batch.

## Verification and gaps

Five race-enabled tests in record_count_test.go verify:

- Independently enumerated counts across five supported mapped field types,
  including nullable numeric/Boolean values, zero and false.
- All-null filtered input, empty matches, nested scalar/membership groups, empty
  OR behavior and a genuinely empty disposable table (deleted within a rolled-back
  fixture transaction).
- COUNT(field) SQL rather than COUNT(*), no LIMIT/record guards, exact statement
  budget boundaries, invalid fields/filters and detached arguments.
- Nonnegative int64 result boundaries and explicit rejection of invalid results.
- Cancellation, invalid/nil/closed transactions, and budget failure before SQL.

Five statement-budget and five logical-filter regressions also passed. All 15
tests passed without skips; go vet ./... passed. Evidence:
[COUNT test log](../evidence/2026-10-29_ingestion-count.log).

023f remains for live PostgreSQL/MySQL result/error parity, reviewed IP COUNT
coverage, authorized production endpoint adoption, restricted-reader grants,
concurrent schema/data behavior and representative scan-cost evidence. The
normalized filter interface remains stricter than the source's raw-value binding.
No production configuration, schema, dependency or application database changes
are made; PostgreSQL/River remains active.
