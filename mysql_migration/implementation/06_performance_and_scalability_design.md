# Performance and scalability design

Status: measurement plan; no MySQL benchmark run in this phase. ADR-12 remains proposed.

## Existing evidence and its limits

The repository [database-scale analysis](../../backend/stress-tests/DATABASE_SCALE_RESULTS_ANALYSIS.md#L157) reports decision p95 482 ms in empty/1M/5M seed phases, p99 629/570/629 ms, and API decision rates 30.15/30.33/30.16 per second. Ingestion p95 is 558/560/559 ms. These are historical report values, not a current PostgreSQL baseline or approved SLO. Each phase reached one million successful ingestions and one million API-successful decisions; API success did not verify every fraud outcome.

The same [failure analysis](../../backend/stress-tests/DATABASE_SCALE_RESULTS_ANALYSIS.md#L201) reports 11,671/11,977/11,940 ultimately failed ingestion records (HTTP 502), replaced by later source records, plus more than 228,000 extra retries on successful records per phase. Do not call this a zero-loss workload or inherit that failure rate as acceptable. Equal histogram p95 buckets do not prove identical underlying latency. Ingestion and decision percentile sums are not an end-to-end percentile.

The report lacks a pinned source revision, exact final row counts and server/wait metrics. Normal stress orchestration stops ingestion/decision/screening workers for direct synchronous execution, so it does not establish background throughput, queue recovery or full-system contention. Current [stress protocol](../../backend/stress-tests/STRESS_TEST_PROTOCOL.md) is a starting point; example thresholds are not product-approved targets.

## Reproducible comparison

Record source/build hashes, exact server/driver versions, migration checksums, CPU/RAM/storage limits, buffer/cache settings, TLS, pool/worker sizes, dataset hash, tenant/key skew, scenario/rule versions, provider stub latency and random seed. Restore the same logical dataset to both engines. Run warmup, steady load and cooldown; repeat runs and report variance. Use fixed arrival-rate tests as well as concurrency sweeps to reveal queuing and saturation. Count every source record, final outcome, retry, ambiguous commit and emitted side effect.

Measure p50/p95/p99/max end-to-end and stage timings, achieved arrival/success rate, correctness mismatches, pool wait duration, connections, locks/deadlocks, query fingerprints/rows examined, temporary spills, CPU, memory, IOPS, disk latency, redo/binlog growth and queue oldest age. Use EXPLAIN and safe representative EXPLAIN ANALYZE on disposable data; executing analyze on a mutation is not a read-only production experiment.

| Workload ID | Actual path and variations | Measurement and acceptance evidence |
|---|---|---|
| PERF-01 Decisions | EvaluateScenario -> AST aggregate/direct or ingestion_http reads -> persistence/jobs | Identical rule evidence/outcomes at approved numeric tolerance (no tolerance across decision boundaries); p95/p99, throughput and DB cost |
| PERF-02 Ingestion | Single, batch, patch/no-op, alternate unique keys, duplicate request hashes | Successful unique records/sec and every attempted record accounted for; atomic audit/outbox/receipt; lock and pool waits |
| PERF-03 Model changes | Create/add/archive/type/index/display while reads/writes run | Publication correctness, per-table unavailable time, metadata lock duration, DDL space and recovery latency |
| PERF-04 Screening | Provider success/slow/error, continuous reruns, exclusions, candidate replacement | Provider-vs-DB time, stale response rejection, candidate limits, queue age and projection lag |
| PERF-05 Cases | Inbox queue, intake, assignment last-slot contention, transfer, review, explorer | Page latency/rows examined, bounded projections, no N+1, fair unrelated-case progress |
| PERF-06 Analytics/workflows | Large allowed permission scope, max workflow parents, wide payloads | Snapshot-consistent output, cap enforcement, memory/spill/transaction duration |
| PERF-07 Identity | Login/session lookups, revoke-vs-write, last-admin and invitation delivery | Auth latency, revocation correctness, lease generation safety and provider/local pool use |
| PERF-08 Work engine | All 15 kinds, mixed priorities, backlog/restart, hot resource, poison jobs | No lost jobs/effects; duplicate deliveries reconciled; age/fairness/drain rate/CPU and retention cost |
| PERF-09 Recovery | Primary failure, client cancellation, restore/PITR, pool reconnect | Measured RPO/RTO and ambiguous-operation reconciliation, no split-brain writes |

Use current 0/1M/5M scales as reproducible starting datasets, then add many tenants, hot keys, realistic retention, large case histories, many dynamic tables and mixed workloads. Test skew rather than only uniformly random UUIDs. Include cases/auth/worker load omitted from the old harness. Keep provider variability controlled for database comparisons, then run a separate end-to-end integration soak.

## Capacity and tuning rules

Start with explicit owner pool budgets and reserve connections for recovery. Tune worker concurrency against the shared server, not service-by-service in isolation. Index additions need before/after plans, write amplification/storage and representative selectivity evidence. A bounded LIMIT query can still scan many rows; inspect actual rows examined. Case and workflow list projections must not load whole histories.

Caches are allowed only for immutable/versioned contracts or a documented stale-read contract; never cache away revocation correctness. Read replicas are optional for explicitly stale-tolerant reporting, with lag limits and fallback; current permission checks and dependent writes stay on primary. Partitioning is deferred pending retention/query evidence and FK compatibility. Connection count, replicas and partitioning are not default performance fixes.

## Acceptance contract

G0 must assign numeric targets for successful decision/ingestion rate, latency percentiles, allowed error budget, queue/projection age, recovery time and retained-data scale. The owner signs these before PF-01 starts. Require zero unexplained data/tenant/replay/invariant mismatches; do not trade those for throughput. Performance passes only if agreed absolute SLOs and approved regression budgets hold under paired representative load and soak. Until targets and runs exist, status is NOT_STARTED, not passed.
