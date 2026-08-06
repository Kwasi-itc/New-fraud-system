# Optimization Partitioning Performance Investigation

Date: 2026-08-06

Branches compared:

- `optimization-partitioning` at `c9c168d`
- `main` at `6936395`

This report records the read-only remote-artifact investigation and the controlled
local confirmation runs. No product source was changed as part of the
investigation.

## Executive conclusion

The optimization branch is not inherently slower at small scale. In a controlled
20,000-event replay it was faster than the actual `main` branch in both
`direct_cached` and `direct` aggregate modes.

The multi-hour remote divergence is a scale-dependent aggregate bottleneck,
amplified by several differences in what the two runs actually completed:

1. The remote main run rejected 123,701 ingestion attempts, mostly immediately
   with HTTP 429, while the optimization run ingested every scheduled event at
   its captured checkpoint.
2. The optimization run moved the bottleneck into synchronous decision
   aggregates. Its decision p95 reached 25.1 seconds and client requests began
   reaching their 30-second limit.
3. Daily logical buckets memoize filter-specific aggregate components; they do
   not physically partition the transaction table.
4. Multi-day lookback windows can cache complete prior days even when the input
   sample only contains a partial first day. With high-cardinality filters this
   creates large numbers of cached empty results with little reuse.
5. The active partial day is not cacheable and is still queried directly.
6. Two merchant IDs account for 95.3% of the local sample. Broad merchant
   aggregates therefore touch a substantial part of the growing active day,
   even with a `(merchant_id, date)` index.

## Remote artifacts

Main artifacts:

- `/Users/kwilson/Downloads/main/summary.json`
- `/Users/kwilson/Downloads/main/checkpoint.json`
- `/Users/kwilson/Downloads/main/errors.ndjson`
- `/Users/kwilson/Downloads/main/experiment-settings.json`

Optimization artifacts:

- `/Users/kwilson/Downloads/optimization-partition/checkpoint.json`
- `/Users/kwilson/Downloads/optimization-partition/errors.ndjson`

The manifest snapshots and profile summaries match in row counts, time range,
stream distributions, categories, and reference coverage. Their recorded source
fingerprint values differ, so the artifacts do not prove that the generated
sample files were byte-for-byte identical.

### Remote main result

- Wall time: approximately 8 hours 14 minutes
- Harness-completed events: 500,000
- Successful ingestions: 376,299
- Ingestion failures: 123,701
- HTTP 429 `write_path_overloaded`: 123,694
- Successful decisions: 376,299
- Decision failures: 0
- Decision average: 737.56 ms
- Decision p95: 1.59 seconds
- Decision p99: 2.37 seconds

The harness counts an event as completed even when ingestion failed. Therefore
24.74% of the main workload did not reach decision evaluation.

### Remote optimization checkpoint

- Wall time at capture: approximately 25 hours 27 minutes
- Completed events: 396,083
- Successful ingestions: 396,083
- Ingestion failures: 0
- Successful decisions: 383,381
- Decision failures: 12,702
- Ingestion average: 929.96 ms
- Decision average: 8.547 seconds
- Decision p50: 4.13 seconds
- Decision p95: 25.1 seconds
- Decision p99: 28.6 seconds

Decision-error breakdown:

- 10,377 client errors had an empty response body and are consistent with the
  configured 30-second client timeout.
- 2,325 returned HTTP 400.
- 2,251 explicitly included `context deadline exceeded`.
- The largest named groups were `High Weekly Merchant Volume` and
  `Abnormal Merchant Average Ticket`.

At its checkpoint, optimization had already produced 7,082 more successful
decisions than the completed main run. Nevertheless, successful-decision
throughput remained materially lower, so main's rejected work explains only
part of the difference.

## Controlled local confirmation

The local test used:

- The exact same 20,000-event manifest and source fingerprint for every mode
- Source time from `2026-06-01T00:00:04Z` to `2026-06-01T02:43:34Z`
- Multiplier `100`
- Maximum in flight `50`
- Synchronous decision callbacks
- Fresh tenants
- Fully applied managed indexes before replay
- Isolated Docker projects and volumes

The actual main branch was exported to `/private/tmp` with `git archive`, built
there, and run with `TENANT_DATA_READ_MODE=direct_db` and explicit synchronous
ingestion-trigger evaluation. Neither repository branch was checked out or
modified for that control.

| Mode | Elapsed | Throughput | Decision avg | Decision p95 | Decision p99 | Decision failures |
|---|---:|---:|---:|---:|---:|---:|
| Optimization `direct_cached` | 9m 36s | 34.7/s | 988.6 ms | 1.78 s | 3.20 s | 9 |
| Optimization `direct` | 10m 55s | 30.5/s | 874.36 ms | 1.92 s | 2.97 s | 7 |
| Main `direct_db`, sync | 12m 19s | 27.1/s | 1.206 s | 3.19 s | 9.03 s | 0 |

The optimization direct tenant ran after the cached tenant in the same
optimization core database. Tenant transaction tables were isolated, but shared
core decision and metadata tables were larger for the second run. The exact
cached/direct wall-time difference should therefore be treated as directional.

Local result artifacts:

- `/private/tmp/fraud-replay-ab/cached-results/replay-20260806T152525-709637Z/summary.json`
- `/private/tmp/fraud-replay-ab/direct-results/replay-20260806T154509-724757Z/summary.json`
- `/private/tmp/fraud-replay-main-clean-results/replay-20260806T163041-722122Z/summary.json`

## Logical-bucket cache behavior

The bucket planner divides a time-bounded aggregate into logical calendar days.
A part is cacheable only when it covers a complete day and that day is older
than the configured seal delay. A partial day is queried directly.

Consequently, a replay containing only the first few hours of June 1 can still
perform caching:

- A seven-day or multi-day lookback contains complete days before June 1.
- Those prior days are old enough to be considered sealed.
- They may contain no records, but an empty aggregate is still cached.
- The cache key includes the aggregate and canonical non-time filter.
- A different account, phone, terminal, product, or filter combination creates
  a different key even when every prior-day result is zero.
- The June 1 partial-day component remains uncached and still executes against
  PostgreSQL.

The local cached replay ended with:

- 685,405 Redis keys after 20,000 events
- 260.18 MB Redis memory use
- 714,175 keyspace hits
- 685,405 keyspace misses
- No evictions yet

This is approximately 34.3 keys per input event. The Compose Redis limit is
512 MB with `allkeys-lfu`; approximately linear growth would reach that cap near
40,000 events, after which cache churn and eviction are expected.

The Redis cache has no per-key TTL by design. Cache keys include bucket
generation so stale generations become unreachable, but unreachable keys are
left for the Redis eviction policy to remove.

## PostgreSQL and index evidence

All requested indexes were applied before replay. Transient deadlocks occurred
while some indexes were being created concurrently, but the worker retries
succeeded before replay started. Missing indexes did not cause the measured
runtime behavior.

Optimization cached tenant statistics:

- Transaction index scans: 1,068,505
- Transaction sequential scans: 26
- Sequential tuples read: 493,909
- `(merchant_id, date)` index tuples read: approximately 261.5 million
- Date-only index tuples read: approximately 56.4 million

Optimization direct tenant statistics:

- Transaction index scans: 300,408
- Transaction sequential scans: 53
- Sequential tuples read: 939,896
- `(merchant_id, date)` index tuples read: approximately 274.0 million
- Date-only index tuples read: approximately 4.2 million

Main clean tenant statistics:

- Transaction index scans: 281,998
- Transaction sequential scans: 38,036
- Sequential tuples read: approximately 212.3 million
- `(merchant_id, date)` index tuples read: approximately 266.4 million

The optimization direct executor greatly reduced sequential scans, showing that
the new query and index paths are being used. The dominant merchant aggregate
still reads very large index ranges because the merchant predicate is poorly
selective.

### Merchant skew

In the local sample:

- Merchant `1099`: 11,190 rows, or 55.95%
- Merchant `1327`: 7,869 rows, or 39.35%
- Only 32 distinct merchant IDs were present
- There were 19,559 distinct account references

A read-only `EXPLAIN (ANALYZE, BUFFERS)` for a broad merchant aggregate on
merchant `1099` selected a sequential scan despite the existing
`(merchant_id, date)` index:

- Rows matched: 11,190
- Rows rejected: 8,810
- Cold execution: 98.544 ms
- Warm execution: 7.099 ms

PostgreSQL made a reasonable choice: when more than half the table matches, the
index is not selective enough to avoid reading a large fraction of the table.
At high concurrent decision volume, repeated broad scans or index-range reads
accumulate into database queueing.

## Interpretation

The current logical bucket is a memoization layer over daily aggregate query
components. It is not a pre-aggregated merchant/day summary and it is not a
physical table partition.

It works best when:

- Query signatures repeat frequently.
- Completed buckets contain reusable data.
- Filter cardinality is low.
- The active partial component is a small part of the total query window.

The production replay has the opposite shape during its first day:

- Most data is in one growing partial day.
- Many account and identifier filters are effectively unique.
- Complete prior days are often empty.
- Broad merchant filters match most of the active table.
- Fifty concurrent events evaluate multiple aggregate rules.

This explains why the local cache can appear helpful at 20,000 events while its
key cardinality and database work indicate poor scaling toward 500,000 events.

## Recommended next steps

Immediate operational comparison:

1. Run the optimization service with `AGGREGATE_EXECUTION_MODE=direct` for a
   high-volume replay and disable the bucket cache for that experiment.
2. Measure successful ingestions and successful decisions per second, rather
   than harness completion count alone.
3. Run branches sequentially on fresh volumes or separate hosts.
4. Capture Redis `DBSIZE`, memory, hit/miss, eviction, and PostgreSQL query/index
   statistics throughout the test.
5. Do not use a larger aggregate timeout as the primary fix; it risks turning
   explicit three-second query failures into larger request backlogs.

Implementation direction:

1. Add cache admission so one-off high-cardinality filter signatures are not
   cached.
2. Avoid storing separate empty prior-day entries for every account-like value.
3. Materialize reusable daily summaries such as `(day, merchant_id)` with count,
   sum, and average components.
4. Maintain incremental current-day aggregates for the high-volume merchant
   rules instead of rescanning the growing transaction table.
5. Keep the composite indexes, but do not expect `(merchant_id, date)` alone to
   solve a distribution where one merchant owns more than half the rows.

## Investigation state

At completion:

- The repository worktree was clean.
- No product files had been edited.
- Isolated replay containers were stopped.
- The clean test volumes and `/private/tmp` artifacts were preserved.

## Design decision and direction

The main design lesson is not only that the cache-size estimate was too low. The
larger issue is that the expected reuse of cached aggregate values was too high.

A logical aggregate cache key effectively varies by:

```text
bucket x aggregate x field x filtered value x remaining filters x generation
```

For high-cardinality fields, most signatures are requested once. In the local
sample, 19,559 of 20,000 account references were distinct. The miss path can
therefore perform generation reads, a Redis lookup, a PostgreSQL query,
serialization, and a Redis write for a value that is never requested again. In
that case the cache miss costs more than direct execution.

At the same time, the populated partial-day component remains uncached. The
system is consequently capable of caching the cheap empty historical components
while repeatedly querying the expensive growing component.

### Preserve the useful optimization work

The optimization branch should not be discarded. The controlled tests show that
the direct aggregate executor and managed indexes are valuable:

- Optimization `direct` had lower average decision latency than actual main.
- It nearly eliminated sequential scans in the tenant transaction table.
- It produced no Redis key growth.
- It provides a more predictable starting point for scale testing.

The immediate operational posture should therefore be
`AGGREGATE_EXECUTION_MODE=direct`. The `direct_cached` mode should remain
experimental until admission control and bounded cardinality exist.

If the long-running remote cached replay is still active, its checkpoint and
metrics should be preserved and the run can be stopped. It has already
demonstrated the cache-cardinality and decision-latency failure mode.

### Stage the next direct-mode replay

The direct mode should be validated on fresh volumes, sequentially rather than
alongside another branch, at increasing sizes:

1. 20,000 events
2. 50,000 events
3. 100,000 events
4. 500,000 events only if the earlier stages remain stable

Each stage should record:

- Successful ingestions per second
- Successful decisions per second
- Decision p50, p95, and p99
- Schedule-lag growth over time
- PostgreSQL index and sequential tuples read
- Database connection-pool wait time
- Aggregate query timeouts
- Ingestion rejection and retry counts

The key question is whether successful-decision throughput remains stable as the
active day grows. A small-run average by itself is insufficient.

### Replace arbitrary-query caching with explicit rollups

Merchant aggregates should be represented by incremental summary rows instead
of arbitrary query-result keys. A representative rollup could contain:

```text
bucket_start
merchant_id
transaction_count
amount_sum
amount_count
```

This allows:

- Weekly merchant volume to read and combine a small number of daily rows.
- Average ticket to use `SUM(amount_sum) / SUM(amount_count)`.
- The current day to be updated incrementally during ingestion.
- Sealed days to become immutable.
- Redis, if retained, to cache a small bounded summary rather than arbitrary
  filter combinations.

The cardinality then approaches:

```text
days x distinct merchants x supported measures
```

rather than growing with events, rules, filtered values, and lookback days. With
only 32 merchant IDs in the local sample, merchant rollups would have very high
reuse.

### Select optimization strategy by field cardinality

Different rule shapes should not all use the same cache:

- Merchant, category, and global aggregates: incremental daily/hourly rollups.
- Account, phone, terminal, and device aggregates: direct indexed execution for
  short windows unless measured reuse justifies another structure.
- High-frequency sliding-window rules: purpose-built TTL counters or sorted
  sets when their correctness model is defined.
- One-off signatures: bypass the cache.

An account with one transaction receives no benefit from a daily account rollup
or separately cached zero results for preceding days.

### Required controls before general caching is re-enabled

If arbitrary aggregate caching remains available, it should have explicit
admission and resource controls:

1. Admit a signature only after it has been requested more than once.
2. Bypass known high-cardinality filter dimensions by default.
3. Represent a globally empty table bucket with one bucket-level marker rather
   than storing a filtered zero for every value.
4. Enforce per-tenant and per-bucket key and byte budgets.
5. Measure keys and bytes created per ingested event.
6. Expire or actively clean unreachable generation keys.
7. Disable admission when the miss or eviction rate exceeds a configured
   threshold.
8. Coalesce concurrent identical misses so one database query populates all
   waiters.

### Revised role of logical buckets

Logical buckets remain useful as:

- Time boundaries for incremental rollups
- A lifecycle for identifying immutable periods
- A way to combine partial and completed aggregate components

They should not, by themselves, authorize caching every aggregate and filter
combination. The workload needs a bounded set of scenario-aware incremental
aggregates rather than a general unbounded query-result memoization layer.
