# Results schema and reporting plan

Target schema version: `decision-benchmark/v1`. The tables below remain the full design; the implemented subset is described here. This document is not an executable JSON Schema. Unavailable values are null or absent with a documented limitation, never fabricated zero. Time fields name units.

## Implemented subset (2026-10-29)

Campaign artifacts include environment/source manifests, lifecycle journal, Docker log, generated Compose and JSON/Markdown/HTML reports. Each trial saves a versioned hashed experiment, setup journals, independent aggregate oracle, correctness NDJSON, separate warm-up/measured request streams and summaries, telemetry, reconciliation and aggregate query plans where applicable. Background ingestion has separate streams.

Trial summaries contain version, benchmark ID, status, configuration, validity, limitations, qualification count, warmup, measurement, observation and reconciliation; production qualification is always false. Failed setup retains an error and may omit later fields. Measurement includes offered/submitted/completed/correct/failed/timeout/unresolved/drop/scenario/rule counters, elapsed/schedule/drain seconds, peak inflight, six latency cohorts and rates with explicit denominators. Histograms reuse replay buckets and add p90; percentiles are approximate.

Raw requests retain object/tenant index, payload hash/bytes, schedule/dispatch/response/completion offsets, HTTP status/response bytes and decision/evidence hashes. Errors and generator drops remain explicit. Full persisted evidence is checked before disposable cleanup; hashes remain afterward. Manifest parsing is strict; result dictionaries have contract tests but no exhaustive independent JSON Schema validator. Reports group exact configuration hashes only and do not certify environment/source compatibility across campaigns.

Still unavailable from the target contract: per-rule timing, exclusive per-request SQL attribution, container/DB CPU and network attribution, complete pool saturation metrics, full confusion matrices, writer-overhead calibration, automatic SLO capacity classification, async completion and fault reconciliation. Available host/process/service/DB observations do not substitute for these missing metrics.

## Artifact set

Each immutable run directory contains `manifest.json`, `environment.json`, `setup.json`, `requests.ndjson`, `correctness.ndjson`, `telemetry.ndjson`, `query-plans/`, `summary.json`, and a human summary/report. Include artifact SHA-256 digests and schema versions. Write summaries atomically; journal setup and final status. A failed or interrupted run retains its raw data and status. Store no credentials or real customer payloads; use synthetic IDs and canonical fixture references.

Use existing replay JSON writers, observation and system-test reporting helpers. Raw request records should be streamed with bounded buffering; record writer backpressure and lost records. No request may silently disappear from the accounting. Histogram-only retention for large runs requires an explicit storage policy and known precision; correctness evidence and all failures remain traceable.

## Run-level contract

| Object / fields | Type / required semantics |
|---|---|
| `schema_version`, `run_id`, `benchmark_id`, `description`, `repetition`, `status` | Strings/integer; status planned/running/completed/failed/interrupted/invalid; status completed is not production qualification |
| `source.commit`, `dirty`, `source_manifest_sha256`, `image_digests` | Commit plus exact checkout/build provenance, per-service images; missing provenance invalidates cross-version claims |
| `configuration.manifest_sha256`, `seed`, `endpoint_kind`, `read_mode`, `execution_mode` | Frozen fixture and schedule identity; single/all route; ingestion_http/direct_db; sync/async; effective API and worker config |
| `configuration.scenarios` | IDs, iteration IDs, trigger AST/hash, object type, thresholds, ordered rules with stable ID/formula/hash/modifier/snooze status; configured/eligible counts; AST nodes/depth/distinct queries |
| `configuration.dependencies` | Workflow/screening/scoring profile, reference/custom-list/GeoIP versions, auth mode/trusted caller boundary; no secret values |
| `dataset` | Exact historical row count or null+reason, bytes/index bytes, model revision/hash, distribution, null fraction, cardinalities, matching rows, anchor/window bounds, current-object inclusion, seed digest, immutable/evolving state |
| `payload` | Encoded bytes min/p50/max, declared field count, synthetic generator/version, input stream hash, planned/realized match mix; response/evidence bytes separately |
| `load` | Closed/fixed arrival mode, target request rate (null for pure closed loop), VUs, max inflight, scenario/rule/aggregate concurrency, offered schedule, actual send rate, measured duration, warm-up/drain durations, completion deadline |
| `environment` | OS/CPU/cores/GOMAXPROCS/RAM/container limits, topology/replicas, DB version/settings/storage, migrations per service, effective pool caps and shared budget, HTTP limits/timeouts, TLS/auth/log levels, generator host/runtime, available observers |
| `cache_state` | Metadata cold/warm/unknown, DB buffer/OS state and exact procedure; never a single unexplained cold flag |
| `counts` | Offered slots, submitted logical inputs, HTTP attempts, responses, transport failures, timeouts, generator drops, accepted async, terminal completed/failed/unresolved, correct decisions, persisted scenario decisions, rule executions, trigger misses |
| `rates` | Submitted inputs/s, terminal inputs/s, correct inputs/s, persisted scenario decisions/s, errors/submitted, timeouts/submitted, retries/original submissions; per-cohort denominators included |
| `latencies` | p50/p90/p95/p99/max/count plus method/precision for all attempts, valid successful requests, failures, scheduled-to-completion, async acceptance/queue/completion and optional rule/DB phases |
| `correctness` | Cases checked, expected rejections, unexpected rejection/acceptance, incorrect matches, missed matches, wrong score/outcome, missing/extra scenarios or rules, tenant violations, persistence mismatches, unresolved commits; oracle version/hash |
| `resources` | App/generator CPU and memory/RSS time series summaries, DB CPU/I/O/connections, pool acquire waits/saturation, query calls/time, queue depth/oldest age/retries, network bytes/dependency latency; per-source availability |
| `quality` | Warm-up stabilization, repeats/cohort, generator headroom/lag/drops, telemetry gaps/resets, instrumentation overhead, missing evidence, validity reasons, SLO supplied flag; `production_qualified:false` absent separate qualification |
| `artifacts`, `notes` | Paths/digests; setup failures, anomalies, exclusions, unmeasured limitations and safety-stop reason |

## Per-request and semantic record

Each request record identifies run/case/logical-input/attempt/tenant/object, fixture hash, arrival schedule offset, dispatch offset, response/terminal offsets, endpoint, HTTP status/error category, timeout/retry/deferred flag, bytes and selected scenario result identities where exposed. Use monotonic clocks for durations and UTC timestamps only for correlation. Include time from scheduling to dispatch and from dispatch to response. Keep original scheduled arrival on retries.

Correctness records map the request to expected and actual trigger state, rule hit/no_hit/snoozed sets, score/outcome, scenario/iteration identity and persistence reconciliation status. Never interpret `triggered:false` as `approve`. Multiple scenarios may produce multiple decisions per input. Verify untriggered result counts with the known selected set; identity limitations must be explicit. Record matched-rule confusion counts, not just request success.

Retry records distinguish client retry, ingestion retry, River attempt and explicit rerun. Sync decision writes have ambiguous commit outcome on network timeout; default client policy is no blind retry. Mark unresolved until bounded tenant/object/run reconciliation establishes what was stored. Async acceptance is not terminal completion. Late completions belong to their original measured cohort and are reported in drain metrics, not silently dropped.

Accounting checks include: offered = submitted + generator_dropped for a completed fixed schedule (add not-yet-offered/cancelled for interruption); attempts = responses + transport failures + outstanding at snapshot; terminal logical inputs + unresolved = submitted after drain. Do not double-count a retry as another logical transaction. For all-triggered successful inputs with fixed S/R and no snooze count change, expected decision rows = inputs*S and rule rows = inputs*S*R; reconcile actual rows rather than inferring writes solely from HTTP 200.

## Measurement sources and limits

| Metric | Collection plan | Interpretation |
|---|---|---|
| End-to-end latency/throughput | Existing monotonic client timing, bounded raw records/histograms; exact actual wall windows | Include auth/transport/serialization/persistence; success-only and all-attempt cohorts both required |
| Rule evaluation | Debug phase timings in separate diagnostic runs; optional Go profiles/microbenchmarks after approval | Current `rules_eval_us` is concurrent batch wall time. Per-rule time is unavailable as a stable general production metric; report null rather than divide by R |
| Preparation/cache/read metrics | `/v1/admin/runtime-metrics`, sampled before/during/after, per replica | Cumulative counters: delta with resets detected. Last-512 evaluation percentiles are diagnostic snapshots, not run p99; cannot subtract percentiles |
| DB query count/time | Approved `pg_stat_statements` if already available, query-plan files, database/host observer; otherwise bounded diagnostic SQL logging | Record collection scope and contamination from setup/polling/other services. Do not install extensions silently. Client read-call count is not necessarily SQL query count |
| Pool pressure | pgx acquired/max, acquire counts/durations/cancellation/empty waits, all services/workers | Pool stats are process wide; overlapping request deltas are not attribution. Budget includes observer/maintenance reserve |
| CPU/memory/I/O/network | Container/host observer for application, generator and PostgreSQL | Record sampling interval, units and CPU core normalization. Missing RDS/host access is missing data, not zero usage |
| Queue/recovery | Existing River queue collector, execution attempts/status and bounded completion polling | Submission-to-terminal and poll detection delay separate; sampled oldest age can miss transient peaks |
| External dependencies | Existing HTTP/read timing and controlled dependency logs where available | `direct_db` still has model-service reads; disabling configured dependencies changes workload semantics |

Start with a one-second observer interval as a proposal; validate overhead in isolated repeated diagnostics and adjust with recorded justification. Detailed debug logs and plans are outside primary measured runs unless their enabled cost is the intended production profile. Correctness verification stays enabled in all runs.

## Report and comparisons

1. Run identity, validity and correctness come first, then environment/configuration and data state. Clearly label static findings, measurements, hypotheses and missing data.
2. Tabulate every selected matrix cell with S/R, types, dataset/matching rows, payload size, rate/concurrency/duration, throughput, p50/p90/p95/p99/max, errors/retries, correctness and resource availability. Show failures/invalid cells; do not silently omit them.
3. Produce curves for rules, scenarios, AST complexity, history size/selectivity, concurrency and offered rate. Plot throughput and latency together with generator drops, error rates and pool/DB pressure. Show per-repeat points and median plus range/IQR; at least 3 repeats, preferably 5 near the knee. Never average p99 values into a claimed combined p99. Combined distributions require compatible raw samples/histograms.
4. Compare matched cohorts only: same route/read mode, schema/config/build, cache procedure, data distribution/history policy, resources, match mix and auth. Change one dimension or explicitly label a factorial/mixed experiment. Paired baseline/candidate order should be interleaved; calculate per-pair throughput and latency ratios with uncertainty.
5. Attribution uses phase timings, query plans/statistics and profiles. Parallel phase times are not additive to request latency. Rising latency alone does not establish a DB bottleneck; require query/wait/resource evidence. Show throughput knee as observed discovery; do not call it an SLO capacity limit without agreed objectives.
6. Recommendations have an issue ID, exact supporting run/plan/profile links, likely cause/confidence, proposed change, expected direction (not invented percentage), trade-offs, correctness/tenant/security risks and a verification matrix. Performance wins with unexplained semantic mismatches are rejected.

Historical June/July run files may be linked as prior context only after validating provenance and compatibility. This audit provides no new measured result rows or charts.
