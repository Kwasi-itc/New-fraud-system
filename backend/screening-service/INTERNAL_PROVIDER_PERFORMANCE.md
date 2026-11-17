# Phase 8 complete-decision performance qualification

Measured 2026-11-14/15 UTC. Existing tasks: PERF-01, INL-01/02/03, OPS-01/02. **Correctness/delivery verification passed; capacity acceptance failed.** The previous 536.96 provider searches/second is not complete decision throughput. No production capacity is accepted.

## Workload and measurement boundary

The final opt-in integration test sends **10,000 HTTP evaluations with 16 closed-loop clients** through actual service-token authentication, tenant/scenario consistency checks, two payload fraud rules, explicit provider selection, PostgreSQL indexed candidate retrieval/scoring, error-only inline screening, forced hit outcomes, transactional decision/execution/evidence persistence and actual decision/screening workers. Every decision enqueues a workflow and outbox events; controlled HTTP receivers record deliveries. Real signed/current human sessions prepare saved searches and monitoring subscriptions.

The initial dataset has 40,000 fictional records, 75% Person and 25% Organization. Requests use 50% no-candidate names, 40% exact-name hits and 10% one-character spelling hits, with two amount/country rules. Concurrent intake stages/publishes a complete **40,020-record** generation and rescreens 20 previously clean monitored queries. Evidence from both old/new generation sets is checked against published versions; no orphan attribution is permitted. The workload is mechanically generated, not representative official-source identity prevalence or customer traffic.

Fresh Go 1.27.4 binaries run for decision, screening and case APIs and decision/screening workers. PostgreSQL 16.15 uses tmpfs data on Docker Desktop, sharing a host with 16 logical CPUs and 7,702,478,848 bytes of Docker memory. No per-container CPU/memory cap is set. Application child processes use GOMAXPROCS=2. Local container HTTP is plaintext; model reads and outbound receivers are controlled dependencies. This does not qualify external identity issuance, TLS/WAN latency, real provider HTTP, aggregate rules, EC2/RDS storage or production topology.

Configured maximum pools: decision API 16, screening API 4, decision worker 4, screening worker 4, case API 8, fixture/observer 16. The sum is a test budget, not all simultaneously open connections or a production budget. Decision queues explicitly use four workflow/eight outbox workers, 5 ms fetch cooldown and 100 ms fallback poll. Other configured queues/embedded projection loops remain part of the real service behavior; global running-job counts must not be interpreted as utilization of only those twelve workers.

API timing ends after the response/committed decision. Complete-effects timing also waits for all persisted workflow/outbox receipts, full concurrent source publication and all 20 current-generation rescreens. Database observations are scheduled every 100 ms; Docker resource samples are timestamped and filtered to this decision/effects window. Source setup/build and post-test idle samples are excluded from reported load-window CPU/memory. Sampling, SQL profiling and test assertions add overhead.

## Final result

| Measure | Result |
| --- | --- |
| Requests / persisted decisions / inline evidence / completed screening executions | 10,000 / 10,000 / 10,000 / 10,000 |
| Rule execution rows / expected screening hit decisions | 20,000 / 5,000 |
| API throughput | **87.600 decisions/s** over 114.154 s |
| Complete decision plus effects throughput | **87.487 decisions/s** over 114.302 s |
| API p50 / p95 / p99 | **170.912 / 290.738 / 359.474 ms** |
| Evaluation errors / incomplete / retries / discarded or cancelled jobs | **0 / 0 / 0 / 0** |
| Workflow executions / HTTP receipts | 10,000 / 10,000 |
| Outbox events / HTTP receipts | 30,000 / 30,000 |
| Remaining workflows / outbox events | 0 / 0 |
| Current-generation rescreens completed / saved generation sets | 20 / 2 |
| Database observations / peak active / peak lock-waiting sessions | 1,142 / 23 / 2 |
| Peak queued / running River jobs | 107 / 169 across all participating queues |
| Database CPU samples: mean / peak | 730.44% / 828.04%; 100% means one core |
| Database peak sampled Docker memory | Approximately 1.656 GiB |
| Combined test/API/worker runner CPU: mean / peak | 417.43% / 470.49% |
| Combined runner peak sampled memory | Approximately 335.1 MiB |
| Resource samples within the load window | 27 per container |
| Decision API pool | 16 total/max connections; 80,025 acquisitions; 16 empty acquisitions; 1.445 s cumulative empty-acquire wait; no cancelled acquisitions |

The after-drain runtime endpoint reports idle API connections, which is not an in-load saturation measurement. Time-series DB activity and cumulative acquisition waits are retained separately. Docker memory usage is a sampled container measure, not a pure PostgreSQL heap or production RSS guarantee. Worker-specific CPU, full per-worker pool time series, managed storage IOPS and arrival-rate-controlled overload behavior remain deployment qualification gaps.

Against the requested targets, complete throughput is **412.513 decisions/s below 500**, and p95 **240.738 ms above 50 ms**. A zero-error closed-loop run does not prove an open-loop overload envelope. The host's substantial database/application CPU usage and longer candidate queries support further profiling and deployment sizing; they do not prove a single isolated bottleneck or a production scaling factor.

## Defects investigated and remediated

An initial small full-decision run achieved only about 7.18 decisions/s. EXPLAIN showed stale planner statistics immediately after bulk staging/publication: one controlled plan took **327.64 ms before ANALYZE versus 11.93 ms after**. The earlier provider-only benchmark had manually refreshed statistics; the runtime publication path had not.

Screening migration **22** adds a tightly scoped SECURITY DEFINER statistics routine for the three source projections, with fixed search path, narrow lock/statement settings, a nonblocking advisory lock and no PUBLIC execution. Restricted private/collector capabilities receive only routine EXECUTE, not ownership or broad maintenance authority. Large publications (≥5,000 records) invoke it before publication mutation locks. Actual restricted-login publication of 5,001 private records verifies the contract. Concurrent refresh skips are explicit in the routine's boolean result/runbook; staging/production must monitor statistics and plans under multiple simultaneous publishers. The original retrieval SQL/scorers remain unchanged; an experimental query barrier was discarded after measuring worse performance.

A subsequent **1,000-request short run** produced 204.61 API decisions/s and p95 106.56 ms but **failed complete-effects acceptance**: two outbox workers at the default 100 ms fetch cooldown did not drain the 3,000 events within 90 seconds. Its report is retained as a failed run, not advertised as completed decision throughput.

River cooldown/fallback polling are now configurable with validated bounds; existing defaults remain **100 ms / 1 s**. The final test explicitly provisions 4 workflow/8 outbox workers with **5 ms / 100 ms** timing and verifies all 30,000 deliveries. Application production settings were not changed. The longer sustained result is lower than the short burst because it includes continued worker/DB pressure; the two runs are different configurations/durations and are not a controlled claim that one optimization alone caused a throughput gain.

Under final load, the candidate query runs 10,000 times with mean **27.96 ms**, maximum **170.36 ms**, no JIT and no temporary blocks written. Concurrent publication statistics maintenance takes approximately **5.07 s** once. The runtime cache shows high reuse; the API pool's modest cumulative waits do not justify blindly increasing it. SQL profiles, pre/post plans and raw observations are retained for analysis.

## Required deployment qualification

- Use approved representative sources, tenant/scenario counts, aliases/identifiers, rule complexity including aggregates, hit prevalence and real downstream contracts.
- Run sustained fixed-arrival-rate and burst/soak tests on the intended EC2/RDS topology with TLS, actual storage and all replicas/background work. Measure saturation, rejection/backpressure, recovery and duplicate delivery behavior.
- Capture per-process/per-worker CPU, heap, pool wait/acquisition time, per-queue job/age/retry distributions and RDS CPU/I/O/locks/plans. Agree the one-database connection budget and headroom before increasing concurrency.
- Profile candidate retrieval and persistence/projection/background queue costs under the same workload before choosing the next optimization. Requalify any query/index/cache/concurrency change with correctness/isolation and representative quality evidence.
- Obtain an explicit SLO/capacity decision. The current result does not meet 500 completed decisions/s or p95 ≤50 ms; neither the earlier provider number nor infrastructure extrapolation can close PERF-01.

The [activation runbook](INTERNAL_PROVIDER_ACTIVATION.md) keeps rollout gated. Machine evidence and exact test-source locations are indexed in the [acceptance register](INTERNAL_PROVIDER_ACCEPTANCE.md).
