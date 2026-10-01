# Database-volume performance analysis: empty, 1M seed, and 5M seed

Prepared: 2026-09-25  
Scope: three user-supplied phase reports; ingestion concurrency 10 and evaluation concurrency 10.

## 1. Executive assessment

The three results show stable API performance as the starting transaction history increases from zero to one million and five million records. Decision throughput remains between 30.15 and 30.33 requests/second. Decision p95 remains approximately 482 ms in every phase. Ingestion p95 remains between 558 and 560 ms.

Both seeded phases satisfy all four of the suite's performance acceptance conditions against the empty baseline. No progressive throughput decline is apparent within the measured phases as another million transactions accumulate.

However, ingestion reliability is a separate unresolved issue. Each phase contains roughly 12,000 source records that ultimately failed ingestion, plus approximately 230,000 recorded additional attempts on eventually successful records. Failed source records were replaced with later records. Reaching one million successful ingestions therefore does not mean that all selected source records succeeded.

The overall pipeline slows modestly as seed size increases, while API-stage throughput stays flat. The harness includes initial source selection in pipeline timing but excludes it from API-stage timing. Scanning past the historical seed records is a plausible contributor to that difference; the reports do not separately time the scan.

These results support stable performance for this workload at concurrency 10 through approximately six million transaction records. They do not establish maximum throughput, fraud-detection accuracy, asynchronous delivery reliability, or production readiness.

The full-month phase was not supplied. No conclusion about the complete four-phase suite is made here.

## 2. Evidence and interpretation rules

### 2.1 Inputs

This report uses the three JSON results supplied in the conversation and the current repository implementation:

- [Suite implementation](production_replay/database_scale_suite.py): phase selection, pipeline timing, counters, database checks, and acceptance.
- [API client](production_replay/api_client.py): retries, HTTP success handling, and request modes.
- [Latency implementation](production_replay/replay.py): histogram-based approximate percentiles.
- [Scenario definitions](production_replay/scenarios.py): internal benchmark rules.
- [Setup implementation](production_replay/setup_environment.py): tenant, model, and publication preparation.
- [Suite documentation](production_replay/DATABASE_SCALE_SUITE.md).
- [Compose override](../../docker-compose.database-scale.yml).

The source revision used for the reported executions was not supplied. Implementation-based explanations assume those runs used behavior matching the current code.

No infrastructure metrics, request traces, server logs, exact transaction-table counts, or table-level storage breakdowns were supplied. The analysis separates direct observations from explanations that require those additional measurements.

### 2.2 Units and rounding

- GB means 1,000,000,000 bytes.
- Milliseconds describe client-observed request durations.
- Percent changes are relative to the empty baseline unless stated otherwise.
- Reported request rates are rounded to two decimal places.
- Latency percentiles are approximate histogram values.
- Derived timing gaps are estimates, not separately recorded durations.
- Database row counts are PostgreSQL estimates across user tables.

### 2.3 Run identities

| Phase | Tenant ID |
|---|---|
| Empty | b0725e46-1cc5-49d5-a0fc-e544b6fd606d |
| Seed 1M | 5342c840-9882-4ad2-8ccb-e5060503964e |
| Seed 5M | d8e6a28f-f53d-473e-9f55-61b0254b9751 |

These identifiers can be used to locate related records or correlate logs. They do not establish execution dates.

## 3. What was actually tested

Each phase recreated the database, set up a fresh tenant and the internal scenario set, loaded the relevant seed history, and then ran one million decision requests after successful single-record ingestions.

| Property | Empty | Seed 1M | Seed 5M |
|---|---:|---:|---:|
| Seed transaction records | 0 | 1,000,000 | 5,000,000 |
| Measured successful ingestions | 1,000,000 | 1,000,000 | 1,000,000 |
| Decision requests | 1,000,000 | 1,000,000 | 1,000,000 |
| Expected ending transaction volume* | 1M | 2M | 6M |
| Ingestion concurrency | 10 | 10 | 10 |
| Decision concurrency | 10 | 10 | 10 |
| Maximum observed ingestion concurrency | 10 | 10 | 10 |
| Maximum observed decision concurrency | 10 | 10 | 10 |

*Expected volume assumes distinct source objects and normal successful processing. The reports do not contain exact transaction-table counts.

The seeded phases use July 2026 source records. The 1M phase skips the first million July records for evaluation; the 5M phase skips the first five million. The empty baseline selects the earliest source records across the combined source timeline; its selected month is not stated in the supplied result.

The phases are independent experiments, not cumulative additions to one database. The 5M phase does not inherit the 1M phase's transactions, decisions, or caches in application processes recreated by the harness. Recreating a database does not guarantee an entirely cold host or storage environment.

A phase measures growth over an interval: empty to about 1M, 1M to about 2M, or 5M to about 6M. It does not hold database volume fixed throughout measurement.

### 3.1 Workload coverage

The internal set contains four scenarios and six rules covering high amounts, odd-hour activity, account transaction bursts, merchant diversity, and a 30-day account-average comparison. Trigger conditions determine which scenarios apply to an individual record.

One million decision requests therefore does not prove six million rule executions. Nor does it reproduce every production scenario, reference-data lookup, or integration.

The harness calls the decision endpoint directly with synchronous mode requested. Normal managed execution leaves ingestion, decision-engine, and screening workers stopped while API services and the data-model worker run. Audit and outbox writes are included, but asynchronous outbox delivery is not the path being benchmarked.

## 4. Runtime and throughput

### 4.1 Complete stage timing

| Measurement | Empty | Seed 1M | Seed 5M |
|---|---:|---:|---:|
| Seed duration, seconds | 0 | 2,287.125 | 9,251.445 |
| Seed duration | 0 | 38m 7s | 2h 34m 11s |
| Measured pipeline duration, seconds | 33,172.475 | 33,561.520 | 33,951.607 |
| Measured pipeline duration | 9h 12m 52s | 9h 19m 22s | 9h 25m 52s |
| Seed plus measured duration | 9h 12m 52s | 9h 57m 29s | 12h 0m 3s |
| Pipeline runtime change | baseline | +1.17% | +2.35% |

The last duration excludes source preprocessing, database recreation, migrations, scenario publication, and other operations outside the seed and pipeline timers.

### 4.2 API-stage rates versus pipeline rates

| Rate | Empty | Seed 1M | Seed 5M |
|---|---:|---:|---:|
| Ingestion successes/sec | 30.15 | 30.33 | 30.16 |
| Decision attempts/sec | 30.15 | 30.33 | 30.16 |
| Overall pipeline decisions/sec | 30.15 | 29.80 | 29.45 |
| API-stage rate change | baseline | +0.60% | +0.03% |
| Pipeline rate change* | baseline | -1.16% | -2.32% |

*Calculated from the displayed rounded pipeline rates.

API-stage throughput is essentially unchanged. The 5M phase differs from the baseline by only 0.01 requests/sec, far too small to justify a capacity claim from single runs.

The pipeline rate includes work before the first API request. The decision rate uses the interval from the first decision request start to the last completion. These metrics answer different questions and should both be retained.

### 4.3 Estimated time outside the decision measurement window

Using the displayed decision rate:

    Estimated decision window = 1,000,000 / decision requests per second
    Estimated outside-window time = pipeline elapsed - estimated decision window

| Phase | Estimated decision window | Estimated outside-window time |
|---|---:|---:|
| Empty | 33,167.5 s | about 5.0 s |
| Seed 1M | 32,970.7 s | about 590.9 s |
| Seed 5M | 33,156.5 s | about 795.1 s |

Because rates are rounded, the empty phase's small gap is particularly imprecise. The larger seeded-phase gaps remain material despite that rounding.

In the implementation, the evaluation iterator rescans the sorted source and skips seed records before returning the first evaluation event. This work occurs after the pipeline timer starts. It is therefore a credible explanation for much of the larger seeded-phase timing gap.

The gap is not itself a measured scan duration: it can also include initial ingestion and final orchestration outside the decision window. Source I/O and cache conditions are unknown. The scan-time explanation should be confirmed with dedicated timing instrumentation.

### 4.4 Concurrency interpretation

A rough fully occupied worker-pool relationship is:

    Throughput ≈ concurrency / average request duration

| Phase | 10 / average decision seconds | Observed decision requests/sec |
|---|---:|---:|
| Empty | 30.16 | 30.15 |
| Seed 1M | 30.34 | 30.33 |
| Seed 5M | 30.18 | 30.16 |

The close match is consistent with the decision workers being busy for most of their timing window. This is client-workload occupancy, not evidence that server CPU, database I/O, or connection capacity is saturated.

With concurrency 10 on each stage, the queue holds up to 40 waiting events. A full queue makes ingestion workers wait, connecting the achieved rates of the two stages. Equal ingestion and decision throughput therefore does not imply equal standalone service capacity.

These results cannot be linearly extrapolated to concurrency 50 or 100.

## 5. Decision latency and reliability

| Decision metric | Empty | Seed 1M | Seed 5M |
|---|---:|---:|---:|
| Attempts | 1,000,000 | 1,000,000 | 1,000,000 |
| Successes | 1,000,000 | 1,000,000 | 1,000,000 |
| Failures | 0 | 0 | 0 |
| Minimum, ms | 77.64 | 82.35 | 69.93 |
| Average, ms | 331.59 | 329.59 | 331.39 |
| p50, ms | 320 | 320 | 320 |
| p95, ms | 482 | 482 | 482 |
| p99, ms | 629 | 570 | 629 |
| Maximum, ms | 1,358.55 | 1,360.93 | 1,362.70 |

The central and tail latency measurements are stable. The 1M phase has a 9.38% lower p99 than baseline, but the 5M phase returns to baseline p99. This is not a monotonic improvement or degradation with history size.

Equal p95 values mean equal reported histogram percentile buckets. Small underlying differences can be hidden by bucket resolution.

Decision latency measures the API request, excluding preceding ingestion and time waiting in the queue. Adding ingestion p95 and decision p95 would not produce a valid end-to-end p95.

All three million decision requests were counted as successful by the client. The client accepts HTTP 200 or 202 with the expected response format; the suite does not validate every decision outcome or confirm every rule's execution. Zero reported failures is consequently an API-level observation.

## 6. Ingestion latency and failure analysis

### 6.1 Latency

| Ingestion metric | Empty | Seed 1M | Seed 5M |
|---|---:|---:|---:|
| Recorded operation count | 1,011,671 | 1,011,977 | 1,011,940 |
| Minimum, ms | 16.39 | 67.67 | 19.12 |
| Average, ms | 261.82 | 265.21 | 262.80 |
| p50, ms | 234 | 235 | 235 |
| p95, ms | 558 | 560 | 559 |
| p99, ms | 785 | 790 | 732 |
| Maximum, ms | 1,752.42 | 1,679.67 | 1,679.63 |

Samples are record-level ingestion operations, not individual HTTP attempts. A sample includes retries and retry delays for its record. Ultimately failed records also contribute samples.

Average ingestion latency rises by only 1.29% in the 1M phase and 0.37% in the 5M phase. The p95 changes by 0.36% and 0.18%, respectively. There is no strong history-dependent latency penalty in these measurements.

Ingestion has a lower mean than decisions but a higher p95 in every run. Its slow tail includes retry behavior, so latency should not be interpreted as pure database insert time.

### 6.2 Terminal record failures and replacement

| Reliability metric | Empty | Seed 1M | Seed 5M |
|---|---:|---:|---:|
| Successful source records | 1,000,000 | 1,000,000 | 1,000,000 |
| Ultimately failed source records | 11,671 | 11,977 | 11,940 |
| Total source records attempted | 1,011,671 | 1,011,977 | 1,011,940 |
| Record-level failure percentage | 1.1536% | 1.1835% | 1.1799% |
| Final failure classification | HTTP 502 | HTTP 502 | HTTP 502 |
| Extra retries on successful records | 228,015 | 230,115 | 229,687 |

The failure-rate denominator includes both successful and ultimately failed source records.

The pipeline replaces failed records with later eligible records until it obtains one million successful ingestions. Therefore, the evaluated set is not necessarily the exact first million source records after the seed boundary.

An HTTP failure does not by itself prove that no server-side side effect occurred. Ambiguous outcomes and idempotency behavior require server-side investigation if exact reconciliation is needed.

### 6.3 Retry accounting

The recorded retry count includes extra attempts only for records that eventually succeeded. It excludes attempts spent on records that ultimately failed and does not provide retry causes for successful operations.

A conservative lower bound on ingestion HTTP attempts is:

    successful records + recorded extra retries + failed records

| Phase | Minimum ingestion HTTP attempts |
|---|---:|
| Empty | 1,239,686 |
| Seed 1M | 1,242,092 |
| Seed 5M | 1,241,627 |

These are lower bounds, not exact totals. Under the current three-attempt limit, total attempts are at most these bounds plus two additional attempts for each ultimately failed record.

Successful records alone required approximately 1.228 to 1.230 HTTP attempts per successful ingestion. The retry overhead is substantial even though the final throughput is stable.

Because a successful record can contribute one or two retries, the number of successful records that retried is bounded as follows:

| Phase | Minimum successful records that retried | Maximum |
|---|---:|---:|
| Empty | 114,008 | 228,015 |
| Seed 1M | 115,058 | 230,115 |
| Seed 5M | 114,844 | 229,687 |

The actual retry distribution cannot be reconstructed from these summaries.

### 6.4 Cross-run reliability conclusion

Across the three phases:

- 3,000,000 ingestions succeeded.
- 35,588 source records ultimately failed ingestion.
- 687,817 additional attempts were recorded on successful records.
- 3,000,000 decision requests succeeded at the API level.

The similar error rates at all tested volumes suggest a recurring ingestion-path condition rather than a major failure increase caused by larger history. They do not rule out a database-related cause shared by all phases.

The summaries do not identify the component producing HTTP 502. Proxy logs, ingestion logs, request IDs, timeout settings, and database metrics are needed before assigning root cause.

## 7. Performance within each phase

Each segment contains 100,000 completed decision attempts. In these runs all decision attempts succeeded.

| Through | Empty requests/sec | Seed 1M requests/sec | Seed 5M requests/sec |
|---|---:|---:|---:|
| 100,000 | 26.44 | 23.18 | 23.81 |
| 200,000 | 31.76 | 32.19 | 29.87 |
| 300,000 | 32.01 | 31.64 | 31.52 |
| 400,000 | 30.03 | 27.74 | 29.91 |
| 500,000 | 28.35 | 32.24 | 28.58 |
| 600,000 | 31.97 | 32.17 | 31.62 |
| 700,000 | 31.66 | 31.69 | 30.84 |
| 800,000 | 27.27 | 29.68 | 27.50 |
| 900,000 | 32.25 | 28.92 | 32.03 |
| 1,000,000 | 31.18 | 31.42 | 31.00 |

The first segment includes initial pipeline work and is the slowest in every phase. It should not be treated as a pure API steady-state measurement.

A time-weighted calculation for the last 900,000 evaluations, excluding the first segment, gives:

| Phase | Last 900K evaluations/sec |
|---|---:|
| Empty | 30.62 |
| Seed 1M | 30.77 |
| Seed 5M | 30.25 |

This calculation divides 900,000 by the sum of the last nine segment durations; it does not average the displayed rates. It is a descriptive sensitivity check, not an official acceptance metric or a formal warm-up correction.

The 5M result is about 1.21% below baseline by this measure. That remains a small difference from single runs with different transaction slices.

Later dips recover, and there is no monotonic degradation. Potential explanations include transaction mix, retry bursts, database maintenance, resource contention, or source I/O, but none is established by the segment data. The reports have no per-segment latency, failure, CPU, or database-wait breakdown.

The supplied segment durations sum to the pipeline durations within 0.001 seconds of rounding, so they account for the reported measured runtime.

## 8. Seeding behavior

| Seed metric | Seed 1M | Seed 5M |
|---|---:|---:|
| Records | 1,000,000 | 5,000,000 |
| Batches | 2,000 | 10,000 |
| Records per batch | 500 | 500 |
| Duration, seconds | 2,287.125 | 9,251.445 |
| Records/sec | 437.23 | 540.46 |

The 5M seed is approximately 23.61% faster per record. Five times the records took approximately 4.05 times as long.

Seeding uses batch ingestion and does not submit explicit decision requests for the seed history. Its throughput cannot be compared directly with the single-record, decision-coupled evaluation pipeline.

The summaries do not report seed concurrency, seed retry counts, or per-batch latency. The current code defaults to seed concurrency 10, but the supplied JSON alone does not confirm the configured value for these runs.

Seed timing is excluded from performance acceptance. Operationally, it remains a material part of the run duration.

## 9. Database size and row estimates

### 9.1 Physical size

| Storage measurement | Empty | Seed 1M | Seed 5M |
|---|---:|---:|---:|
| Before evaluation, bytes | 11,134,195 | 2,834,793,715 | 14,159,832,307 |
| After evaluation, bytes | 10,515,236,083 | 13,262,357,747 | 24,658,019,571 |
| Before evaluation, GB | 0.011 | 2.835 | 14.160 |
| After evaluation, GB | 10.515 | 13.262 | 24.658 |
| Evaluation growth, bytes | 10,504,101,888 | 10,427,564,032 | 10,498,187,264 |
| Evaluation growth, GB | 10.504 | 10.428 | 10.498 |

The pre-evaluation footprint scales approximately with the seed volume: about 2.83 GB per million seed records in these runs.

The measured stage adds approximately 10.43 to 10.50 GB per million evaluated transactions. This consistency is useful for understanding the benchmark's observed storage cost.

The larger footprint of evaluated transactions is consistent with decision-related data and supporting structures being written in addition to ingestion data. Exact attribution requires per-table and per-index size measurements.

A rough observed cost is 10.4 to 10.5 decimal KB of aggregate database growth per successfully evaluated transaction. This is not an individual row size and should not be linearly extrapolated without retention, cleanup, index, and workload information. PostgreSQL database size is also not a measurement of all external storage such as backups or separately retained WAL.

### 9.2 Estimated rows

| Estimated user-table rows | Empty | Seed 1M | Seed 5M |
|---|---:|---:|---:|
| Before evaluation | 66 | 3,002,988 | 15,001,303 |
| After evaluation | 14,219,470 | 17,085,275 | 29,067,745 |
| Estimated increase | 14,219,404 | 14,082,287 | 14,066,442 |

These sums use PostgreSQL's estimated live tuples across user tables. They include more than transaction rows and can vary with statistics freshness.

The approximate three-million and fifteen-million pre-evaluation estimates are compatible with seed transactions plus audit and outbox data, but exact table composition is not established.

### 9.3 Audit and outbox counts

| Verification | Empty | Seed 1M | Seed 5M |
|---|---:|---:|---:|
| Required minimum in each table | 1,000,000 | 2,000,000 | 6,000,000 |
| Ingestion audit count | 1,000,000 | 2,000,000 | 6,000,000 |
| Outbox count | 1,000,000 | 2,002,000 | 6,010,000 |
| Outbox above minimum | 0 | 2,000 | 10,000 |
| Verification | Pass | Pass | Pass |

The extra outbox count matches seed batch count in both seeded runs. That is evidence consistent with one additional event per batch, but event-type inspection is required to confirm the mechanism.

The checks verify minimum counts, not unique transaction-to-entry mapping, correct payloads, or downstream delivery. They also do not establish that failed client operations had no database effects.

## 10. Acceptance calculation

The implementation requires, independently for ingestion and decisions:

    Seeded throughput / baseline throughput >= 0.80
    Seeded p95 / baseline p95 <= 1.20

The baseline establishes:

| Condition | Required value |
|---|---:|
| Ingestion throughput | at least 24.12/sec |
| Decision throughput | at least 24.12/sec |
| Ingestion p95 | at most 669.6 ms |
| Decision p95 | at most 578.4 ms |

| Acceptance metric | Empty | Seed 1M | Seed 5M |
|---|---:|---:|---:|
| Ingestion throughput retention | 100.000% | 100.597% | 100.033% |
| Decision throughput retention | 100.000% | 100.597% | 100.033% |
| Ingestion p95 ratio | 1.0000 | 1.0036 | 1.0018 |
| Decision p95 ratio | 1.0000 | 1.0000 | 1.0000 |
| Performance result | Pass | Pass | Pass |

These results are reconstructed from the supplied phase reports; a combined summary.json was not supplied.

The acceptance check does not require zero ingestion failures or decision failures. It does not impose an absolute throughput floor, test p99, or evaluate overall pipeline rate. Decision throughput counts failed attempts too, although that particular issue does not distort these runs because reported decision failures are zero.

The baseline's acceptance against itself is automatic for valid nonzero metrics. A phase-level performance pass should not be interpreted as a reliability or correctness certification.

## 11. What can and cannot be concluded

| Claim | Assessment |
|---|---|
| API performance stayed stable through a 5M seed at concurrency 10 | Supported by these runs |
| All measured decision requests were counted successful | Supported |
| Audit/outbox minimum count checks passed | Supported |
| Ingestion failures disappeared after retries | False; about 1.18% of source attempts ultimately failed |
| The 5M database caused a large slowdown | Not supported |
| All original source records were successfully evaluated | Not supported; failed ingestions were replaced |
| Maximum capacity is 30 requests/sec | Not established |
| Higher concurrency will scale linearly | Not established |
| Every fraud outcome was correct | Not tested by these summaries |
| Async outbox delivery is reliable | Not established |
| A full month of history will behave the same | Not established; phase not supplied |
| Database volume is the only experimental difference | Not established |

Other limitations include different transaction slices, unknown infrastructure load, unavailable execution revision/configuration details, no repeated trials, no separate warm-up stage, no account-cardinality or rule-trigger distributions, and no server resource measurements.

One million observations per phase provide detailed measurements of that phase, but do not substitute for independent repeated experiments under controlled conditions.

## 12. Follow-up investigation and measurement plan

These are proposed next steps, not changes made by this report.

### Priority 1: explain and measure ingestion failures

Correlate failed ingestion request IDs with proxy and ingestion-service logs. Capture the response producer, upstream timing, retry attempt number, final outcome, and database-side errors where present. Determine whether ambiguous client failures can coexist with committed writes.

Add counters for every HTTP attempt, including attempts for ultimately failed records, and distinguish first-attempt successes from recovered retries. Preserve error categories for retry attempts that eventually succeed.

Success criterion: an evidence-backed cause for the recurring HTTP 502 failures and a complete attempt-level reliability picture.

### Priority 2: separate harness overhead from service performance

Record source-selection start/end, first ingestion start, first decision start, last ingestion completion, and last decision completion. Track queue depth and source-read time.

This would explain the roughly 10- and 13-minute timing gaps in seeded phases without relying on rounded-rate reconstruction.

### Priority 3: measure complete transaction latency and correctness

Track each record from ingestion start through decision completion, including queue wait. Validate the decision response's completion state and expected outcome semantics. Track scenario/rule execution counts to characterize workload differences.

Do not derive end-to-end percentiles by adding stage percentiles.

### Priority 4: strengthen acceptance criteria

Keep the current history-volume comparisons, but add explicit operationally agreed limits for ingestion failures, decision failures, retries, and absolute throughput. Require completed decision semantics if that is the intended contract.

The exact reliability and latency targets must come from service requirements; they cannot be inferred from these reports alone.

### Priority 5: improve repeatability and capacity evidence

Repeat each volume with comparable infrastructure and a documented workload. Record software revision, machine/database configuration, source fingerprint, seed concurrency, account distribution, and scenario trigger counts.

Measure CPU, memory, disk latency, database waits, locks, connection utilization, and query statistics. Then vary concurrency in controlled runs to locate the actual capacity limit.

### Priority 6: complete coverage and storage attribution

Analyze the full-month phase when available. Obtain table/index size breakdowns and outbox event-type counts. Exercise asynchronous workers separately if delivery performance is part of the production objective.

## 13. Final assessment

At the tested concurrency, the database-volume performance objective is met for the three supplied phases: increasing seed history to five million records did not materially change measured API throughput or latency.

Overall runtime grows modestly, with initial source-selection overhead a credible contributor. Database growth during each million-record measured workload stays close to 10.5 GB.

The most significant unresolved result is recurring ingestion failure and retry overhead. It is present at all tested volumes and remains outside the current performance acceptance gate. The appropriate reading is stable measured performance with an outstanding ingestion reliability issue, pending the full-month result and additional operational evidence.

## Appendix: calculation examples

    Record failure percentage:
    failed source records / (successful + failed source records) * 100

    Example, Seed 5M:
    11,940 / 1,011,940 * 100 = 1.1799%

    Evaluation storage growth, Seed 5M:
    24,658,019,571 - 14,159,832,307 = 10,498,187,264 bytes

    Throughput retention, Seed 5M:
    30.16 / 30.15 = 1.0003317, or 100.033%

    Ingestion p95 ratio, Seed 5M:
    559 / 558 = 1.0017921

    Decision p95 ratio, Seed 5M:
    482 / 482 = 1.0

    Runtime increase, Seed 5M:
    (33,951.607 / 33,172.475 - 1) * 100 = 2.3487%

    Last-nine-segment rate:
    900,000 / sum(segment elapsed seconds for segments 2 through 10)

All calculations use the supplied values. Values derived from displayed rates inherit their rounding.
