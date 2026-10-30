# Staged statement budgets and bounded selection

IN-01.3.020a-e, 2026-10-29. Whole-service adoption remains 020f.
Source paths below are relative to backend/ingestion-service.

## Budget contract

`internal/store/mysql/record_query.go` adds RecordQueryLimits and
ValidateRecordQuery. A statement has one parameter-count budget, one aggregate
parameter-value byte budget and one SQL-text byte budget. Limits must be positive
and cannot exceed staged ceilings of 60,000 parameters, 16 MiB parameter bytes and
1 MiB SQL text. These are implementation safety ceilings, not measured production
capacity targets or a statement of MySQL's server limits. Caller-supplied selection
limits may be lower. Empty SQL, excessive count/text/value bytes and unsupported
argument shapes fail with ErrRecordQueryLimit before execution.

| Argument shape | Accounted bytes per occurrence |
|---|---:|
| nil, bool | 1 |
| int, int64, finite float64 | 8 |
| string, []byte | actual length |
| mysqlstore.UUID | 16 |
| microsecond time.Time in MySQL year range | 32 |

Time validity is checked through the shared DateTime codec. Unknown argument types,
including arbitrary driver.Valuer implementations, are rejected without invoking
them. NaN/infinity and excessive timestamp precision fail. Parameter occurrences
are counted individually, even when values share memory or repeat the same key.
Remaining-budget subtraction avoids overflowing an accumulated byte total.

Accounting bounds logical parameter values, not wire escaping/framing, driver or
Go allocation overhead, process RSS, SQL execution cost or server-side row scans.
The validator is not an SQL parser or authorization check; it does not validate
placeholder arity. Generated callers own SQL construction. It performs no copying;
callers must retain argument ownership and avoid concurrent mutation through
execution. Existing bounded compilers create owned argument slices.

## Integrated query paths

`(*RecordProjection).SelectRecords` composes guarded projection columns, a bounded
CompileFilter tree, and a parameterized LIMIT from RecordResultLimits.Rows. It
counts all projection guard parameters, every nested scalar/list argument and the
LIMIT against the same budget. Empty filters deliberately omit WHERE, preserving
the documented source behavior. No caller SQL fragment is accepted. Failed
composition returns no SQL, arguments or collector.

After bounded compilation, the statement checker runs before QueryContext. This
is not a pre-JSON-allocation or pre-metadata-query limit: projection count, tree
shape and list sizes already bound their respective compilation work. Whole-query
limits can reject a composition whose individually valid parts exceed the total.
The guarded collector handles row/page limits and errors with no partial result.

SelectRecords is an unordered limited selection, not stable sorting, pagination or
a new public endpoint. It returns up to Rows records; it does not signal that more
matching rows exist. The caller must provide the originating projection's already
authorized READ COMMITTED transaction and hold its publication barrier. The helper
does not authorize or verify transaction identity/isolation itself. SQL errors,
cancellation and closed transactions propagate; zero matches return an empty slice.

`record_read.go: ReadExactRecord` also invokes the checker immediately before its
record SELECT. Its staged fixed bounds are 104 parameters (100 projection guards
plus four scoped identity arguments), 3 MiB parameter bytes (covering two copies of
the existing 1 MiB key cap, UUIDs and guards), and 1 MiB SQL text. Both key copies
are accounted. Metadata validation/barrier acquisition precedes this record query.
No generic upsert, key allocation or production configuration change is introduced.

## Remaining work

020f must apply the same accounting to future batch-key queries, aggregate
expressions, all production SQL/HTTP paths and reviewed runtime policies. The
validator can account repeated keys, but no batch-key read API is implemented here.
Standalone predicate/projection fragments remain callable; the whole service does
not yet enforce this budget universally. Request parsing/body limits, production
error mapping, stable pagination, source compatibility and restricted runtime
adoption remain open. Representative query plans and database load measurements
are still required; this batch makes no performance improvement claim.

## Verification

Five new race-enabled tests in record_query_test.go cover policy validation,
exact/over count/value/text boundaries, repeated keys, forbidden value shapes,
parameter order across projection/nested filters/LIMIT, detached composition,
real MySQL selection and empty filters, SQL LIMIT, empty results, output overflow,
canceled reads and closed transactions. An over-budget call with a closed
transaction proves budget rejection precedes database execution. An arbitrary
Valuer with a panicking method is rejected without invocation.

Five exact-read and five logical-filter regressions also passed against isolated
MySQL fixtures. All 15 tests passed without skips, and go vet ./... passed. Evidence: [query-budget tests](../evidence/2026-10-29_ingestion-query-budgets.log).
Runtime remains PostgreSQL/River. No dependencies, application data or deployment
schema changes are part of this batch.
