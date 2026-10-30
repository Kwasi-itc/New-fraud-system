# Staged numeric SUM

IN-01.3.024a-e, 2026-10-29. Compatibility and production adoption remain 024f.
Paths below are relative to backend/ingestion-service.

## Source and staged type contract

PostgreSQL tenant_data_reader.go emits COALESCE(SUM(field), 0), returning zero
for empty/all-null input and otherwise the database result. The staged
`internal/store/mysql/record_sum.go: SumRecords` accepts only active, selected,
single-column int/float fields from the validated projection. String, Boolean,
timestamp and multi-column IP inputs fail rather than relying on MySQL coercion.
Field trim/case behavior matches the existing source builder. No new aggregate
route or production adapter is activated.

Integer results are int64. MySQL SUM(INT) returns an exact scale-zero decimal;
the decoder uses bounded integer parsing directly, never float64. Values outside
signed int64 fail with ErrRecordSumResult, including MySQL decimal totals that
could otherwise exceed the source INT-to-int64 result range. Negative totals are
valid. Float results are finite float64 with normal approximate arithmetic and
order sensitivity; decimal exactness or bitwise cross-database equality is not
promised. Empty/all-null results use typed int64(0) or float64(0).

## Overflow discovery and safety policy

The first isolated test summed four stored math.MaxFloat64 values. The query
returned zero successfully. Diagnostic reads confirmed four non-null values,
MIN/MAX equal to math.MaxFloat64, and raw SUM=0 (float64), without SHOW WARNINGS
rows. This is observed behavior in this fixture, not a claim about every MySQL
version/mode. An initial NULL explanation was disproved by those diagnostics;
adding COUNT alone did not fix the problem.

Final integer SQL selects SUM(field), COUNT(field). Float SQL also selects
MAX(ABS(field)). All values come from the same statement snapshot and filter.
COUNT=0 is the only reason to synthesize zero; NULL/non-finite/unexpected results
with nonempty input fail. Float magnitude is additionally checked so a silently
incorrect zero cannot escape this tested overflow case.

For floats, counts above 2^52 are rejected. A single input may use the full finite
range. For n>1, maximum absolute input must not exceed the next float below
MaxFloat64/(2*n). This conservative factor-two headroom bounds absolute accumulation
and rounding; it rejects extreme cancelling inputs even when their mathematical
sum would be finite. This is a stricter staged safety policy, not established
PostgreSQL compatibility. Its false rejections and extra aggregate computation
must be reviewed under 024f before production adoption. No server setting or
production SQL behavior was changed to make the test pass.

## Query, ownership and errors

The optional filter reuses CompileFilter, including staged normalized leaf types,
empty-group behavior and parameter order. ValidateRecordQuery runs before SQL;
all compilation failures return no SQL, arguments or type marker. No LIMIT alters
the set of rows being summed. The query transfers two or three bounded scalar
values; result size and statement budgets do not bound scan cost or duration.
Representative EXPLAIN/cost evidence remains open, especially for MAX(ABS(field)).

Caller owns authorization, the originating projection's READ COMMITTED transaction,
publication barrier and a suitable context deadline. The helper does not verify
transaction identity, open/commit a transaction, cache or mutate data. Invalid
arguments, invalid result types, unsafe totals, cancellation and database errors
return nil plus an error, never a successful numeric zero. HTTP DTO/error mapping
is unchanged and awaits endpoint adoption.

## Verification

Five race-enabled tests in record_sum_test.go cover:

- Mapped numeric compilation, ordered detached arguments, unsupported fields and
  exact/over parameter-count, byte and SQL-text budgets.
- Negative integer totals and a real exact INT32-input total of 6,442,450,938.
- Fractional/negative floats using tolerance 1e-12, ignored NULL, empty matches,
  all-null input, empty OR and an empty disposable table.
- Decoder exactness beyond 2^53, signed int64 boundaries, overflow, invalid NULL,
  fractional integer encodings and NaN/Infinity rejection. Huge totals here are
  decoder cases, not claims of generating billions of records.
- Real overflow diagnostics, protected overflow rejection, a safe single maximum
  float, conservative rejection of extreme cancellation, canceled queries,
  closed/nil transactions and rejection before SQL execution.

Five COUNT and five statement-budget regressions also passed. All 15 final tests
passed without skips; go vet ./... passed. Evidence:
[final SUM tests](../evidence/2026-10-29_ingestion-sum.log),
[initial failing overflow test](../evidence/2026-10-29_ingestion-sum-overflow-discovery.log).
All fixture writes/deletion are within generated disposable databases and rolled
back; no application database is touched.

024f remains for live PostgreSQL parity, review of stricter input/magnitude/count
policies, public result/error mapping, restricted runtime grants, concurrent state
acceptance and representative plans. Runtime stays PostgreSQL/River. No dependency,
deployment schema or production configuration changes were made.

The 20-subtask AVG/MIN/MAX/DISTINCT batch extracted the unchanged accumulation
policy into record_float_accumulation.go: safeFloatAccumulation for reuse by AVG.
SUM overflow and singleton/cancellation regressions pass in the
[aggregate batch evidence](../evidence/2026-10-29_ingestion-aggregate-batch20.log).
