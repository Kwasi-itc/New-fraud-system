# Draft System Performance and Resilience Test Plan

This plan defines how to measure the fraud system across APIs, networks, PostgreSQL, queues, workers, and hardware. The intended result is a defensible capacity statement: which workloads the system can sustain, on which resources, while meeting latency, correctness, and recovery requirements.

Status: implementation started. The campaign definitions and run durations below remain proposed work. The foundation described next is implemented and locally tested; no destructive database benchmark, remote infrastructure change, or fault injection has been performed.

## Implementation progress

The next increment adds a [fixed-arrival queue campaign](QUEUE_CAMPAIGN.md)
for prepared transaction inputs and existing test tenants. It supports ordered
steady/burst/recovery stages on the ingestion-to-async-decision path, bounded
in-flight work, explicit generator drops, scheduled-arrival completion deadlines,
partial reports, response identity reconciliation and optional existing telemetry.
Focused fake-client tests cover its behavior. Live queue stability/recovery gates,
all-queue coverage, fault controls and hardware qualification remain unimplemented;
this is an experiment runner, not completion of the campaign below.

The first implementation increment adds verified decision response/async completion handling, failure-aware acceptance, bounded pipeline execution, consumer supervision, retry accounting, atomic progress/failure reports, observer environment capture, and optional host/service metric sampling. A standalone read-only observation command permits testing collection without resetting a database. Usage and metric limitations are documented in [Database scale suite](../production_replay/DATABASE_SCALE_SUITE.md).

| Work package | Current state | Remaining work |
|---|---|---|
| I01 Configuration and inventory | Local observer environment, source revision/dirty-state and harness hashes captured; new options validated. | Full versioned campaign schema, deployed configuration, remote/container identity, and verified path/queue registry. |
| I02 Harness correctness | Completion evidence, deferred policy/deadlines, failure gates, cancellation, partial reports and a disk-backed evaluation identity ledger with duplicate-input/response-ID detection implemented. | Seed identity coverage, exact scenario identity coverage, independent durable-effect reconciliation, stronger disposable-target verification, setup/seeding deadlines and automatic service restoration. |
| I03 Load scheduling | Existing bounded concurrency pipeline retained; separate queue campaign adds fixed-arrival stages, scheduled-arrival timing, explicit generator drops and input-cohort reporting. | Automated rate search, repeated qualification, mixed-path workloads and live pilot validation. |
| I04 Telemetry | Local CPU/memory/disk/network counters, service runtime/pool snapshots and optional read-only PostgreSQL/public River collection implemented; incomplete requested collection fails acceptance. Database collector tested with fake subprocesses only. | Live SQL/query-plan validation, case_queue/non-River adapters, remote exporters, reset-aware rates, full-run server histograms, tracing and observer-overhead qualification. |
| I05 Fault controls | Planned. | Scoped controls, persistent restoration journal and watchdog. |
| I06 Reconciliation and report | Schema-versioned partial/final reports and response-identity reconciliation gates implemented. | Independent durable-effect reconciliation, campaign qualification gates, correlated visual reports and hardware sizing outputs. |
| I07 Campaign cases | Plan and focused foundation tests present. | Versioned executable experiment definitions and real disposable-service pilots. |

This progress table is the implementation record. Later sections describe the intended completed framework, not a claim that every capability already exists. Local tests and an observer-only pilot do not establish deployed performance or full business-outcome correctness.

The six phases provide the campaign sequence. The execution specifications following them define the paths, workload profiles, metric formulas, database and service cases, detailed runbooks, safeguards, implementation contracts, and release evidence. Proposed diagnostic defaults are distinct from business acceptance targets. Missing targets must not silently receive permissive defaults.

Use these links to navigate the plan:

- [Scope and architecture](#scope-and-architectural-basis), [acceptance inputs](#questions-and-acceptance-inputs), and [measurement contract](#measurement-contract).
- [Phase 1](#phase-1-establish-trustworthy-measurement), [Phase 2](#phase-2-establish-component-and-database-baselines), [Phase 3](#phase-3-measure-queues-and-delivery), [Phase 4](#phase-4-measure-network-and-dependency-resilience), [Phase 5](#phase-5-derive-hardware-and-configuration-requirements), and [Phase 6](#phase-6-validate-the-full-system-over-time).
- [Execution paths](#execution-paths-and-completion-boundaries), [workload profiles](#workload-specification-and-source-preparation), and [rate schedules](#rate-schedules-and-sample-adequacy).
- [Metric definitions](#metric-dictionary-and-reconciliation-rules), [instrumentation](#instrumentation-implementation-and-verification), and [queue registry](#queue-registry-and-configuration-verification).
- [Database experiments](#dedicated-database-experiments), [screening and analyst tests](#screening-and-analyst-workload-coverage), and [detailed runbooks](#detailed-experiment-runbooks).
- [Network and host diagnostics](#network-and-host-diagnostics-beyond-http-timing) and [hardware sizing](#hardware-sizing-calculations-and-output-contract).
- [Run configuration and artifacts](#run-configuration-and-artifact-contracts), [stopping and restoration](#stopping-conditions-and-restoration), and [test validity](#test-validity-and-statistical-interpretation).
- [Implementation packages](#implementation-work-packages-and-tests) and [campaign priorities and completion checklist](#campaign-priorities-and-completion-checklist).

## Scope and architectural basis

Cover data-model, ingestion, decision-engine, screening, and case-manager services, their relevant workers, and their shared logical PostgreSQL database. Include analyst API traffic in mixed workloads. Browser rendering is outside the initial scope; backend latency experienced by analyst workflows is included.

Repository evidence establishes the following starting points. Phase 1 must verify the effective deployed configuration rather than assume these defaults are active.

| Existing element | Implication for the plan |
|---|---|
| [Shared database requirements](../../../AGENTS.md) and [Compose configuration](../../../docker-compose.yml) | Measure application queries, durable jobs, and all service connection pools against one database resource budget. |
| [Database scale suite](../production_replay/database_scale_suite.py) | Retain its four historical-volume experiments, after repairing success accounting and supervision. |
| [Ingestion worker](../../ingestion-service/cmd/worker/main.go) | Inventory River upload and deferred-ingestion queues and their configured consumers. |
| [Decision worker](../../decision-engine-service/cmd/worker/main.go) | Inventory River queues and enabled legacy tasks separately. A polling setting alone does not describe all active work. |
| [Screening case outbox](../../screening-service/internal/store/postgres/case_event_outbox.go) and [case maintenance](../../case-manager-service/internal/store/postgres/maintenance.go) | Measure downstream delivery, retry, and claim contention where enabled. |
| [Runtime metrics collector](../capture_runtime_read_metrics.py) | Reuse existing decision runtime and ingestion read metrics, then extend coverage. |
| [Stress test protocol](../STRESS_TEST_PROTOCOL.md) | Give every experiment an objective, controlled variables, workload, measurements, acceptance rules, procedure, interpretation, and artifacts. |

Service URLs and table names do not prove that an integration is operational. Trace a known transaction through each intended path and identify missing producers, consumers, or destinations before performance testing. Report an unimplemented or disconnected path as a coverage gap, not a successful test.

## Questions and acceptance inputs

The campaign must answer five questions:

1. What is the maximum confirmed successful completion rate for each important workload?
2. Where is time spent, and which resource limits capacity?
3. Can queues absorb expected bursts and recover within the required deadline?
4. What resources support normal load, growth, and the required degraded operating condition?
5. Does accepted work remain accounted for through retries, worker failures, and dependency outages?

Record the following inputs in the test configuration before qualification runs. Discovery runs can proceed while targets remain unresolved, but cannot claim production acceptance.

| Input | Draft treatment |
|---|---|
| Simple direct-decision target | Preserve the existing protocol target of 1,000 evaluations/second with zero errors, timeouts, or dropped requests. This applies to its specified simple workload only. |
| Normal and peak production arrival rates | To be established from traffic evidence or a stated business forecast. |
| Synchronous latency and asynchronous completion deadlines | To be specified per path, including p95 and p99. Do not substitute HTTP acceptance latency for completion latency. |
| Burst size and duration | To be specified from observed or expected traffic. |
| Queue age and recovery limits | To be specified per critical queue and downstream delivery. |
| Resource headroom and failure tolerance | To be specified; test one unavailable replica where the intended deployment supports it. |
| Data retention and growth horizon | To be specified for transactions, decisions, audit, jobs, outbox, and logs. |
| Hardware profiles and run budget | Use the current test environment as baseline; identify affordable alternatives before provisioning. |

## Measurement contract

Use distinct counters for scheduled arrivals, submitted requests, HTTP acceptance, committed transactions, completed decisions, and required delivered side effects. Keep logical transactions separate from HTTP attempts, retries, scenario executions, and queue jobs.

Assign a run identity and propagate transaction, job, and trace correlation through asynchronous boundaries. Maintain an identity ledger for reconciliation. Do not label timeouts as definite server-side failures without checking persisted state. Multiple jobs or decisions may legitimately belong to one transaction; define expected cardinality per workload.

Record timestamps for scheduled arrival, submission, API receipt, ingestion commit, job availability, first claim, each attempt, decision persistence, and required delivery. Report submission-to-completion and scheduled-arrival-to-completion separately. Split queue wait, execution time, retry delay, and delivery time. Never add component percentiles or overlapping spans to estimate end-to-end percentiles.

Use UTC timestamps for cross-host alignment and monotonic clocks for local durations. Record clock synchronization quality. If skew prevents reliable stage timing, mark that timing inconclusive.

| Layer | Required evidence |
|---|---|
| Generator | Intended and actual arrival rate, unsent work, scheduling lag, active connections, CPU, memory, network throughput. |
| API and runtime | Status classes, timeouts, latency histograms, active work, admission waits/rejections, CPU, RSS/heap, GC, goroutines, restarts, sampled profiles. |
| Network | DNS/connect/TLS timing where available, connection reuse, request/response bytes, RTT, retransmissions, resets, interface drops, proxy timing where deployed. |
| Database pools | Per-service and per-replica pool limits, active/idle connections, acquisition wait, canceled waits, connection creation. |
| PostgreSQL | Query fingerprints and durations, wait events, locks/deadlocks, CPU, physical I/O, WAL, checkpoints, temporary spills, vacuum, table/index sizes, exact reconciliation counts. |
| Queues and workers | Ready, running, retrying and terminal states; oldest ready and outstanding age; arrival, claim and successful completion rates; busy workers; attempts; recovery delay. |
| Business outcomes | Persisted transactions, expected decisions, required deliveries, terminal failures, duplicates, missing identities, and tenant consistency. |

Proposed collection defaults: host/service samples every 5 seconds; queue aggregates every 5 seconds; database aggregate snapshots every 15 seconds and at stage boundaries. Retune after measuring overhead. Avoid repeated full-table counts on million-row tables during timed load; use bounded or indexed observations and perform exact reconciliation after drainage. Measure short request and wait durations with event histograms rather than inferring them from 5-second samples.

Use sampled traces for routine runs and targeted profiling for diagnostic runs. Keep identity data in the ledger or traces, not unbounded metric labels. Validate instrumentation overhead with paired enabled/disabled trials. PostgreSQL views and permissions must match the deployed version; [PostgreSQL 16 monitoring](https://www.postgresql.org/docs/16/monitoring-stats.html) documents the baseline Compose version's statistics. Trace spans provide correlated operation timing across services; see the [OpenTelemetry observability primer](https://opentelemetry.io/docs/concepts/observability-primer/).

## Phase 1 Establish trustworthy measurement

Deliver an environment manifest, verified execution-path inventory, repaired harness, and instrumented smoke run.

Capture source revision and dirty state, image digests, migration versions, host/container resources, database version/settings, disk limits, network placement, service replicas, effective read modes, pool sizes, queue names, worker counts, retry/timeouts, auth/TLS/logging settings, and generator resources. Redact credentials. Verify alternate read/worker connection URLs still target the intended shared logical database.

Repair the current suite before treating it as a qualification test:

- Distinguish synchronous completion from deferred acceptance and follow deferred work to a terminal outcome with a deadline.
- Include failure counts and unresolved outcomes in acceptance; report successful-completion throughput separately from request-attempt rate.
- Supervise worker tasks and collectors concurrently, bound waiting, and save partial results on failure or interruption.
- Reconcile exact expected transaction identities and required effects, beyond aggregate audit/outbox counts.
- Record all retry attempts, source replacements, selected time ranges, and effective concurrency.
- Verify database identity and disposable-environment boundaries before reset; include every active consumer in lifecycle management, including case-manager-worker.
- Reject an explicitly selected empty seed month; detect duplicate source identities and source exhaustion.

Add focused harness tests for all-decision failure, deferred acceptance, worker exceptions, exhausted sources, retries, duplicate identities, missing telemetry, reset targeting, and interrupted-run reporting. Exercise at least one real asynchronous transaction through completion in the disposable environment.

Exit condition: a small run has a complete identity ledger, correlated stage timings, correctly classified intentional failures, and verifiable cleanup. No production-capacity claim is possible before this gate.

## Phase 2 Establish component and database baselines

Keep hardware, software, configuration, and input distribution fixed while changing one dimension. Restore equivalent seeded state between comparative trials. Record cache state; use separate warm and restart/cold-start experiments rather than assuming database recreation clears all caches.

| Test | Workload and variable | Required result |
|---|---|---|
| C01 Ingestion | Single-record and batch ingestion; sweep arrival rate and batch size separately. | Commit capacity, audit/outbox write cost, pool waits, errors and retries. |
| C02 Simple decisions | Existing one-scenario simple payload workload; sweep arrival rate. | Confirm highest sustainable rate and evaluate the existing 1,000/second target. |
| C03 Historical decisions | Count, average, distinct-count and long-history rules; vary one rule shape at a time. | Query cost, rows examined where measurable, read-call count, pool wait, and successful decision rate. |
| C04 Scenario fan-out | Increase scenarios and rules while retaining the same transaction distribution. | CPU, query/request amplification, parallelism, and persistence cost. |
| C05 Historical volume | Existing empty, 1M, 5M, and full-month phases. Add a larger size only when justified by growth requirements. | Volume sensitivity with precise source-window and workload differences disclosed. |
| C06 Controlled volume comparison | Replay the same fixed evaluation corpus against multiple reproducible historical snapshots. | Separate historical-volume effects from changes in evaluation transactions. |
| C07 Data skew and tenants | Uniform accounts versus hot accounts/merchants; one versus multiple tenants. | Lock/index contention and per-tenant latency/fairness. |
| C08 Read path | Compare supported ingestion HTTP and direct database read modes with equivalent semantics. | Network/query cost and correctness differences; verify freshness if replicas are used. |

Preserve pseudonymization and deterministic source selection. Use known-answer fixtures for rule correctness, including time boundaries and concurrent event ordering. Separately report expected fraud outcomes; a valid decline is not a technical failure. Production-shaped traffic complements these fixtures but cannot itself establish detection accuracy without expected outcomes.

Exit condition: repeatable component baselines and an evidence-supported candidate bottleneck for each workload. Correlation alone is not a root-cause finding; confirm with a controlled resource or configuration change.

## Phase 3 Measure queues and delivery

Create a queue registry containing logical purpose, storage, producer, consumer, configured queue name, claim method, concurrency, retry/backoff, timeout, recovery mechanism, terminal states, and delivery contract.

Start with ingestion upload/deferred queues; decision scheduled, async, callback, workflow, screening, scoring, and outbox queues where enabled; data-model index jobs; screening case delivery; and case-manager maintenance/outbox work. Map only verified active paths into the run. Document unused or missing paths explicitly.

| Test | Experiment | Required checks |
|---|---|---|
| Q01 Steady load | Hold several arrival rates below and near consumer capacity. | Arrival/completion balance, oldest age, backlog slope, pool wait, and API latency. |
| Q02 Burst and drain | Inject a bounded burst, then return to baseline traffic. | Peak backlog, deadline compliance, observed drain time, and foreground impact. |
| Q03 Consumer outage | Stop one consumer, then restart it; separately stop all consumers of a selected queue. | Durable retention, restart/reclaim behavior, accounted outcomes, recovery deadline. |
| Q04 Crash boundaries | Interrupt after claim, after database commit, and after downstream success before acknowledgement where test hooks permit. | Recovery and duplicate business-effect prevention under ambiguous outcomes. |
| Q05 Poison and slow jobs | Mix invalid, permanently failing, and slow work with healthy work. | Bounded retry, visible terminal failure, no starvation of healthy work. |
| Q06 Worker scaling | Sweep worker counts and replicas against a fixed database connection budget. | Scaling efficiency and point at which database or dependency pressure worsens throughput. |
| Q07 Tenant fairness | Add a noisy tenant while other tenants retain fixed load. | Per-tenant queue age, completion latency and resource consumption. |
| Q08 Backlog size | Compare empty and large pre-existing queues with realistic state distributions. | Claim-query cost and index/vacuum behavior as queue tables grow. |

The primary capacity measure is successful business completion per second, not jobs claimed or attempts processed. Distinguish scheduled future work from overdue ready work. Record both database job state and downstream business completion.

For planning only, approximate drain time as backlog divided by successful completion rate minus ongoing arrival rate, when that difference is positive and rates remain stable. Validate this estimate experimentally rather than use it as acceptance evidence.

Exit condition: every critical configured queue has a measured sustainable rate, bounded retry behavior, tested recovery path, and foreground-impact assessment.

## Phase 4 Measure network and dependency resilience

Target one connection at a time: generator to API; decision to ingestion in HTTP read mode; service to data model; service/worker to PostgreSQL; and enabled screening, workflow, scoring or callback destinations. Include ingress proxies or load balancers only if present in the intended deployment.

The following values are proposed diagnostic starting points, not assertions about production conditions. Record whether delay/loss applies in one or both directions and measure actual RTT after injection.

| Test | Proposed treatment | Required evidence |
|---|---|---|
| N01 Latency | Baseline, then added one-way delays of 5, 20 and 50 ms on one selected path. | End-to-end sensitivity, per-request dependency-call amplification and pool occupancy. |
| N02 Jitter and loss | Separately add bounded jitter, then 0.1% and 1% packet loss where the test environment supports it. | Retransmissions, retries, timeout classification and recovery. |
| N03 Bandwidth | Limit one path to 50% and 25% of measured baseline capacity. | Bytes per transaction, achievable throughput and queue growth. |
| N04 Connections | Compare reuse against controlled connection churn and resets. | DNS/connect/TLS contribution, reconnect storms and resource limits. |
| N05 Dependency outage | Make one dependency unavailable for 30 then 120 seconds, restoring it between trials. | Admission behavior, bounded timeouts/retries, retained work and post-restoration drain. |
| N06 Database recovery | Restart disposable PostgreSQL or exercise a supported test failover. | Reconnection, uncertain commit reconciliation, job recovery and duplicate effects. |

Use deterministic dependency stubs to isolate latency/error behavior, followed by integration runs against the actual test dependencies. Label stub results as component evidence. Record effective retry and timeout settings at every hop so nested retries are visible.

Exit condition: network sensitivity and failure behavior are quantified for each critical path, with restoration verified after every experiment.

## Phase 5 Derive hardware and configuration requirements

Use representative normal/peak workloads selected from earlier phases. Measure one resource dimension at a time, then test the most important interactions. Do not run the full Cartesian product.

| Dimension | Proposed comparison | Decision supported |
|---|---|---|
| API compute | Baseline CPU allocation and feasible smaller/larger allocations. | Whether CPU provisioning improves useful completion rate. |
| API memory | Baseline and feasible constrained/expanded limits. | Working-set needs, GC behavior, cache benefits and OOM boundary. |
| Database compute/memory | Available test instance profiles or controlled local profiles. | Query capacity and cache sensitivity. |
| Storage | Available IOPS/throughput profiles; track WAL, commit latency and queue writes. | Storage requirements and growth reserve. |
| API replicas | One, two, then four where practical. | Scaling efficiency and shared-database pressure. |
| Worker concurrency | Small, baseline and larger allocations per critical queue. | Consumer capacity without foreground degradation. |
| Pool configuration | Several database-wide budgets, apportioned across services and replicas. | Lowest waits without oversaturating PostgreSQL. |
| Placement | Intended placement plus a representative remote path. | Network contribution to resource sizing. |

Sum every distinct pool limit across API/worker replicas, plus administrative, migration and monitoring allowance. Record direct connections versus any pooler; avoid counting the same shared pool twice. Keep this within the usable shared-database connection budget, then validate behavior under load.

Measure storage growth per successful transaction by category: tenant rows, indexes, audit, decision/rule history, jobs, and outbox. Project retention separately, and include WAL, temporary space, maintenance reserve, and backup requirements where applicable. Label projections and identify assumptions; do not extrapolate CPU or storage linearly without validation.

Deliver a sizing table with exact workload, confirmed normal/peak capacity, p95/p99 completion time, queue recovery, resource allocations, headroom, and limitations. Report the minimum tested passing configuration and a proposed operating configuration with the specified headroom. If cost is reported, use actual deployment-region prices or billing evidence at the time of sizing.

## Phase 6 Validate the full system over time

Enable all consumers and destinations required by the selected deployed path. Include background reads, case operations, and publication/index work in the agreed production mix. Measure their effect on the shared database and live decisions.

| Test | Proposed procedure | Required result |
|---|---|---|
| E01 Sustained mixed load | Confirm normal and peak rates with a realistic traffic mix. | Completion, latency, fairness and queue requirements hold together. |
| E02 Burst recovery | Run normal traffic, apply the agreed burst, then continue normal traffic. | Backlog returns to its pre-burst range by the recovery deadline. |
| E03 Soak | Start with 8 hours; extend to 24 hours for release qualification if required. | No unexplained worsening in memory, latency, queue age, connection use or storage growth. |
| E04 Reduced capacity | Remove one replica from the intended redundant topology. | Required service levels and recovery hold under the defined degraded condition. |
| E05 Background maintenance | Run index preparation and representative database maintenance/retention while load continues. | Lock duration, I/O competition and latency impact remain within their budgets. |

An 8- or 24-hour run cannot prove stability over a longer retention cycle. Seed representative aged data and separately exercise cleanup if its natural schedule falls outside the run.

## Standard execution procedure

1. Select a named experiment and freeze its versioned configuration, targets, variable, seed snapshot, and input corpus.
2. Verify disposable targets, effective routing, active workers, readiness, disk capacity, and cleanup capability. Record pre-run service state.
3. Start collectors and verify their timestamps and coverage. Calibrate the load generator independently; use a separate host where feasible and document colocation otherwise.
4. Run a correctness smoke test and reconcile identities before starting expensive trials.
5. Warm up for a proposed minimum of 5 minutes; continue until the declared stabilization criteria hold, or label the run non-steady. Measure cold-start behavior separately.
6. Run 10-minute exploratory rate steps. Bracket the failure boundary, then refine it; record source growth and restore comparable state between trials.
7. Confirm candidate capacity with three independent 30-minute trials. Rotate comparison order to reduce temporal bias and report variability instead of only the best run.
8. Stop arrivals, continue collectors, and observe drainage until the explicit deadline. Persist pending and terminal outcomes rather than discard them.
9. Reconcile expected effects and perform detailed database counts/query-plan analysis outside the timed window where possible.
10. Remove injected faults, restore intended service state, verify readiness, and save cleanup results even when the test fails.

Use arrival-rate workloads for external demand and closed-concurrency workloads for concurrency sensitivity. Arrival-rate scheduling avoids reducing offered traffic merely because requests become slower; see [k6 open and closed models](https://grafana.com/docs/k6/latest/using-k6/scenarios/concepts/open-vs-closed/). Record actual scheduled/submitted rates and scheduling lag, and invalidate capacity conclusions when generator limitations prevent the intended load.

Require bounded experiment duration, bounded queue reconciliation, and persistent failure artifacts. During intentional overload, expected rejection is a measured response, not automatically an invalid test; it does disqualify that rate from a zero-error sustainable-capacity claim.

## Pass criteria and reporting

Classify each experiment as pass, fail, saturation found, blocked by missing implementation, or inconclusive due to test validity. An expected failure-injection response can pass its own recovery contract without meeting normal-operation latency targets during the outage.

| Gate | Qualification requirement |
|---|---|
| Validity | Intended workload delivered, required collectors healthy, configuration known, instrumentation/generator limitations disclosed. |
| Correctness | Every accepted identity accounted for; no unexplained missing work, unintended duplicate effects or tenant inconsistency. |
| Performance | Successful completion rate and absolute per-path latency/deadlines meet the configured targets. Report errors, retries and deferred work separately. |
| Queues | No sustained age/backlog growth at qualifying steady load; recovery reaches the declared deadline and pre-burst range. Define trend window and noise tolerance before the run. |
| Resources | Agreed headroom remains; no unexpected OOM/restart, pool exhaustion, or unexplained memory/disk growth. |
| Recovery | Interrupted/ambiguous work reconciles, service levels recover, and injected faults are removed. |

Retain the database suite's 80% throughput retention and 1.2x p95 ratios as supplemental regression checks. They cannot replace absolute targets or correctness gates. Missing acceptance inputs yield discovery results rather than a production pass.

Each run should save environment and effective configuration, workload/seed fingerprints, lifecycle ledger, time-series metrics, latency histograms, error/retry detail, queue snapshots, database evidence, sampled traces/profiles, fault timeline, cleanup result, and a human-readable report. Preserve evidence from failed runs. Define artifact retention and storage limits; redact secrets and avoid raw sensitive payloads.

The report should show synchronized charts of offered/submitted/completed rates, latency, queue age/depth, pool wait, CPU/memory, and database I/O. Include a stage-duration breakdown, reconciliation outcome, highest confirmed sustainable rate, first failing rate, and evidence supporting the suspected limiting resource. Compare histograms or raw samples rather than averaging percentile values.

## Implementation order and deliverables

| Order | Deliverable | Proposed responsibility | Completion evidence |
|---|---|---|---|
| 1 | Run schema, acceptance inputs, topology and queue registry | Performance lead with service owners | Effective configuration and one traced transaction per active path. |
| 2 | Harness accounting, supervision and partial reporting | Backend/test engineer | Focused automated tests plus a reconciled smoke run. |
| 3 | Collectors, correlation and overhead validation | Backend and platform engineers | Aligned traces/metrics and quantified observer overhead. |
| 4 | Component, volume and queue campaigns | Performance engineer with service owners | Repeatable baselines and recovery evidence. |
| 5 | Network experiments and resource sweeps | Platform engineer with performance engineer | Verified fault restoration and measured sizing candidates. |
| 6 | Mixed-load, soak and degraded-capacity qualification | Service owners and platform team | Acceptance report, resource recommendation and remaining gaps. |

These are responsibility categories, not assigned individuals or staffing commitments. Duration depends on instrumentation gaps, source volume, infrastructure availability, and the chosen matrix. The first implementation milestone is a trustworthy instrumented pilot; estimate the broader execution schedule from its observed setup and run costs.

Keep experiment definitions, collectors, fault controls, outcome tracking, and reporting separate from workload code. Reuse the current replay, scenario, volume, and metrics utilities where their contracts fit. Preserve the existing four-phase command as a focused experiment; do not turn it into a single script responsible for every infrastructure concern.

## Execution paths and completion boundaries

Create a path record for each row below. It must include the actual route, service identities, consumer identities, tables touched, remote calls, completion evidence, and enabled configuration. A path is eligible for load testing only after a smoke transaction traverses it successfully.

| Path | Starting action | Completion boundary | Isolation experiment |
|---|---|---|---|
| P01 Immediate ingestion | Submit a valid object through the ingestion API. | Object and its required audit/outbox effects committed. | Ingestion with decision triggering excluded from the timed path. |
| P02 Immediate decision | Submit a pre-ingested object for synchronous decision evaluation. | Required scenario results persisted, with no unresolved execution. | Pre-seeded fixed corpus and fixed live scenario versions. |
| P03 Deferred ingestion | Submit through the configured overload/deferred path. | Deferred job reaches successful business commit, not merely queue insertion. | Force bounded admission pressure while downstream workers remain healthy. |
| P04 Deferred decision | Explicit async request or verified synchronous fallback. | Execution has a reconciled terminal outcome; callback is a separately measured boundary when required. | Async worker and callback delivery measured independently. |
| P05 Scheduled evaluation | Create or enable a supported scheduled evaluation. | Due work starts within its lateness budget and reaches the expected terminal outcome. | Separate scheduling lateness from queue wait after enqueue. |
| P06 Screening | Trigger screening against a known fixture/provider response. | Screening result and required review/case delivery persisted. | Local deterministic provider, then actual test provider where applicable. |
| P07 Workflow and case intake | Trigger a rule/workflow that requires a case effect. | Intended case mutation committed and externally required acknowledgement recorded. | Repeat the same delivery identity to verify deduplication. |
| P08 Analyst operations | Browse case queue/session and read or mutate a case. | Correct bounded response or committed mutation with tenant/inbox permissions enforced. | Isolated analyst traffic, then concurrent transaction/background traffic. |
| P09 Model publication | Publish a scenario/model requiring index preparation. | Required preparation completes and the intended version becomes usable. | Concurrent publication and live decisions, with explicit version expectations. |
| P10 Upload and batch work | Submit a supported transaction upload or batch. | All input rows accounted for under the documented partial/all-or-nothing contract. | File parsing, enqueue, ingestion, and downstream processing timed separately. |

For P03/P04, acceptance must distinguish durable acknowledgement, execution start, completion, and delivery. If the deployed ingestion outbox has no verified route to decision execution, do not label ingestion plus a harness-generated direct decision call as the production asynchronous path.

Record whether each path is required for qualification, optional and enabled, disabled by design, unsupported, or blocked by missing implementation. The case-manager router currently maps internal AI-review and auto-assignment run endpoints to a `NotImplemented` handler; verify intended supported alternatives before including those endpoints in capacity claims. See [case-manager routes](../../case-manager-service/internal/httpapi/router.go).

## Workload specification and source preparation

Freeze a workload manifest for every comparison. Record input fingerprint, random seed, tenant/account cardinality, timestamp span, payload-size percentiles, amount distribution, scenario-trigger frequencies, rule versions, dependency response distributions, and expected fan-out. Collect these from the source profile when available; otherwise label synthetic assumptions.

| Profile | Purpose | Proposed construction |
|---|---|---|
| W01 Known answers | Verify correctness before performance. | Small deterministic set covering valid, invalid, duplicate, retry, late and out-of-order events, and exact rule/time boundaries. |
| W02 Minimal | Isolate fixed API and persistence overhead. | One tenant, one simple rule, constant payload shape, fresh identities. |
| W03 Representative | Qualify the intended production mix. | Sanitized source distribution plus measured analyst/background traffic and expected outcome proportions. |
| W04 Aggregate intensive | Stress historical data reads. | Account counts, amount averages, distinct merchants, short and long time windows, controlled selectivity. |
| W05 Hot identities | Expose contention and unfairness. | Diagnostic distributions with 50% then 90% of events targeting a small declared account/merchant set; do not call these production proportions. |
| W06 Multiple tenants | Measure isolation under load. | One, ten, then one hundred tenants if feasible; both equal shares and one noisy tenant. Include equal object identifiers in different tenants. |
| W07 Large payload and files | Measure parsing, allocation, network and persistence cost. | Source p50/p95/p99 payloads plus valid near-limit payloads and separate deliberate over-limit rejection cases. |
| W08 Aged data | Exercise mature storage and queues. | Historical transaction/index volume plus realistic completed, pending, retrying and terminal job populations and retention boundaries. |
| W09 Mixed operations | Expose resource competition. | Concurrent transaction processing, analyst reads/mutations, case delivery, and scheduled/index work at independently configured rates. |

Keep source-invalid cases in a named negative-test stream. Do not quietly remove unexpected ingestion failures from a qualification corpus and replace them with easier records. For compatibility runs of the existing replacement behavior, report both the original input count and replaced identities; classify those separately from fixed-corpus qualification.

Control the relationship between historical timestamps and replay time. Preserve original event time for historical rules, and use explicit test time where supported for scheduled work. Record timezone and boundary semantics. Validate that concurrent ingestion does not accidentally introduce future records into aggregates intended to consider only earlier events.

Seed transaction, decision, audit, queue and case history according to the path being tested. Transaction-only seeding is insufficient to emulate mature decision history or a large case queue. Seed through owned service contracts or isolated, schema-versioned test fixtures; do not use arbitrary cross-service writes that bypass business invariants.

Use fresh identities or restore the same snapshot between repetitions so idempotency caches do not turn intended writes into repeated cached responses. Fingerprint schema/index definitions and planner statistics as well as input files. Separate post-load statistics stabilization, cold-start, and naturally evolving maintenance tests.

## Rate schedules and sample adequacy

Rate is always qualified by unit: logical transactions, decision requests, executed scenarios, jobs, or HTTP attempts per second. Specify each independent traffic stream and its arrival distribution. A request triggering six jobs is one transaction and potentially six queue arrivals.

| Schedule | Proposed definition | Appropriate use |
|---|---|---|
| Smoke | 100 known-answer records with bounded completion waits. | Environment and harness validation; no percentile capacity claims. |
| Baseline | Fixed low rate with stable queues and resources. | Measurement overhead and stage-cost characterization. |
| Rate search | Start at a demonstrated passing rate, increase geometrically until failure, then refine the bracket to within a predeclared 5% rate interval. | Capacity discovery with fresh/comparable state. |
| Qualification | Three 30-minute measured runs at the candidate rate, after warmup. | Confirm stable capacity; increase duration/sample size where required. |
| Burst | Hold normal traffic, add the declared burst, then continue normal traffic until drainage. | Recovery while new work continues. |
| Soak | 8-hour pilot and, if required, 24-hour confirmation. | Long-duration drift, maintenance and retention behavior. |
| Fault | Stable pre-fault interval, time-bounded fault, restoration, and bounded recovery interval. | Defined resilience hypotheses. |

Use constant arrivals for controlled capacity and a recorded or declared bursty arrival distribution for realistic queue behavior. Preserve idle periods when testing cold caches or connection expiry. Model analyst sessions and think time separately from machine-generated transaction arrivals.

Calculate expected samples before a run: arrival rate multiplied by measured duration, adjusted for stream proportion and scenario trigger probability. Rare paths may need dedicated runs. Treat p99 based on only a few tail observations as weak evidence; report count and uncertainty. Zero failures in a finite run is an observation, not proof of zero underlying failure probability.

Define a latency success curve using deadlines as well as percentiles: fraction of logical inputs successfully completed by each relevant deadline. Keep failed and unresolved inputs in the denominator for deadline attainment. Report successful-completion latency separately so censored/failed work does not create artificially attractive tail results.

## Metric dictionary and reconciliation rules

These definitions are the contract between load generation, collectors, and reporting. Counter deltas are computed within a declared interval and reset when a process/database statistics reset is detected. An absent value is unknown, never automatically zero.

| Metric | Definition and unit | Interpretation constraint |
|---|---|---|
| Offered rate | Scheduled logical inputs divided by interval seconds. | Includes generator work never submitted. |
| Submitted rate | Logical inputs with an actual first submission divided by interval seconds. | Retries are excluded. |
| Attempt rate | All outbound attempts divided by interval seconds. | Includes retry amplification. |
| Useful throughput | Successful logical completions observed in the interval divided by seconds. | Identify cohort/backlog origin; pre-existing backlog completions are not new-input success. |
| Cohort completion ratio | Successful completed inputs from the selected cohort divided by submitted cohort inputs. | Evaluate at a declared observation deadline. |
| Deadline attainment | Cohort inputs completed successfully by their deadline divided by cohort inputs due for measurement. | Unresolved and terminal technical failures miss the deadline. |
| Retry amplification | Total attempts divided by distinct logical operations. | Report per hop and queue, not just the client total. |
| Submission delay | First actual submission time minus scheduled arrival time, in ms. | Identifies generator and local scheduling delay. |
| Pool wait | Acquisition start to acquired connection, in ms. | Cumulative averages cannot supply a p95 without event instrumentation. |
| Initial ready wait | First claim time minus initial eligible/available time, in ms. | Future scheduled work is not ready until its due time. |
| Retry delay | Time between failed attempt completion and the next eligible/claimed attempt. | Separate intentional backoff from additional ready wait where possible. |
| Queue growth | Change in outstanding cohort/state counts per second over the specified window. | Reconcile arrivals, terminal departures and explicit cancellations; state transitions alone are not departures. |
| Ready age | Observation time minus earliest eligible time among currently ready jobs. | Distinguish from total age since original submission. |
| Recovery time | Fault removal until all declared recovery predicates hold continuously for the confirmation window. | API readiness alone does not establish backlog recovery. |
| CPU demand | Incremental CPU-seconds divided by useful completions. | Use only comparable stable workloads; distinguish host and container scope. |
| Write growth | Bytes added per successful logical transaction, by table/index category. | Include negative cleanup effects separately and avoid dividing by failed attempts. |

At reconciliation, partition each submitted logical input into exactly one primary outcome: successful by deadline, successful late, terminal technical failure, expected negative-test rejection, unresolved, or confirmed not accepted. Store unexpected duplicate effects separately because duplicates may coexist with nominal success. For timed-out submissions, query durable identity evidence before choosing the outcome. Any unclassified identity is a reconciliation defect.

For each path, independently reconcile required effects and their cardinalities: transaction revision, decisions for expected applicable scenarios, jobs, audit events, cases, callbacks and outbox delivery. Do not assume all transactions must create a case or all scenarios must trigger. Verify duplicate requests produce the documented response and no extra unintended business effects.

The existing ingestion [read metrics collector](../../ingestion-service/internal/httpapi/read_metrics.go) limits retained latency samples to 512. Its reported percentiles must be labeled with their sampling/window semantics and cannot be treated as full-run percentiles. Capture run-wide histograms or another bounded, mergeable distribution with declared precision. Preserve per-replica distributions and merge compatible histograms; do not average p95 values.

## Instrumentation implementation and verification

| Capability | Existing basis | Work required before qualification |
|---|---|---|
| Client outcomes | Replay API client and suite counters. | Logical/attempt ledger, deferred polling, deadline accounting, all retry attempts, bounded completion tracking. |
| Decision runtime | Admin runtime-metrics endpoint and current capture utility. | Verify available fields, per-instance collection, restart handling, and missing execution-stage metrics. |
| Ingestion reads | Admin read-metrics endpoint and database pool snapshot. | Full-run latency distributions, write/admission telemetry, pool wait distributions where needed. |
| Screening | Existing metrics route. | Inspect schema and semantics, map job/provider/case-delivery coverage; do not assume format compatibility. |
| Host/container | No collector assumed installed by this plan. | CPU, throttling, memory, pressure, disk/network counters and process-to-service mapping. |
| Queues | River and application job/outbox storage. | Version-aware adapters, state mapping, bounded query plans and per-attempt lifecycle evidence. |
| PostgreSQL | Native statistics and service pool counters. | Version/permission checks, query-statistics availability, snapshots, reset detection, safe plan capture. |
| Distributed traces | No complete cross-service tracing assumed. | Correlation propagation, operation spans, async links, sampling configuration, export health. |

Expose diagnostic interfaces only in the intended test/administrative boundary. Use read-only database credentials for observation and separate administrative access for setup. Avoid sending raw payloads, credentials, or account identifiers into traces or logs.

If selected, `pg_stat_statements` requires its configured preload and database extension; arrange this before measured runs, including any required restart. Record availability instead of silently omitting query evidence. See [PostgreSQL statement statistics](https://www.postgresql.org/docs/16/pgstatstatements.html).

For Go CPU, allocation, goroutine and contention diagnosis, use supported profiles in controlled diagnostic runs and measure observer overhead. Profile only long enough to answer the hypothesis; high-overhead profiling need not run throughout every qualification test. See [Go diagnostics](https://go.dev/doc/diagnostics).

Collector self-tests must cover process restart, cumulative-counter reset, endpoint timeout, missing field, unit mismatch, clock skew, full disk, and partial artifact write. Separate collector health from application health in the report. Capture provider metrics at their native interval and do not present them as higher-resolution measurements.

## Queue registry and configuration verification

The following are source defaults, not deployed facts. Record the pinned River version, schema/migration version, queue configuration, producer insertion behavior, retention and retry policy. Do not assume optional commercial queue features exist in the installed version.

| Owner | Queue name in source defaults | Worker setting to inspect |
|---|---|---|
| Ingestion | `upload_logs` | `UPLOAD_LOG_QUEUE_WORKERS` and `UPLOAD_LOG_QUEUE_NAME` |
| Ingestion | `deferred_ingests` | `DEFERRED_INGEST_QUEUE_WORKERS` and `DEFERRED_INGEST_QUEUE_NAME` |
| Decision | `scheduled_executions` | `SCHEDULED_EXECUTION_QUEUE_WORKERS` and `SCHEDULED_EXECUTION_QUEUE_NAME` |
| Decision | `async_decision_executions` | `ASYNC_EXECUTION_QUEUE_WORKERS` and `ASYNC_EXECUTION_QUEUE_NAME` |
| Decision | `async_decision_execution_callbacks` | `ASYNC_EXECUTION_CALLBACK_QUEUE_WORKERS` and corresponding queue-name setting |
| Decision | `workflow_executions` | `WORKFLOW_DISPATCH_QUEUE_WORKERS` and corresponding queue-name setting |
| Decision | `screening_executions` | `SCREENING_DISPATCH_QUEUE_WORKERS` and corresponding queue-name setting |
| Decision | `scoring_requests` | `SCORING_DISPATCH_QUEUE_WORKERS` and corresponding queue-name setting |
| Decision | `outbox_events` | `OUTBOX_QUEUE_WORKERS` and `OUTBOX_QUEUE_NAME` |

Sources: [ingestion configuration](../../ingestion-service/internal/app/config.go) and [decision configuration](../../decision-engine-service/internal/app/config.go). Add data-model index work, screening dataset/case work and case-manager maintenance/delivery from their actual implementations; do not model them as River queues without verification.

Inspect configured admission limits as separate queues/waits: ingestion write/read/aggregate concurrency, HTTP client pools, decision live/rule/scenario/remote aggregate concurrency, database pools, and the harness queue itself. Measure wait or rejection at each boundary. Increasing a worker count does not increase capacity when a smaller pool or admission limit remains binding.

Add these queue-specific cases to Q01 through Q08:

| Test | Procedure | Acceptance evidence |
|---|---|---|
| Q09 Transactional enqueue | Force a controlled rollback around owned state change and job creation, then test commit. | No committed business state requiring work is orphaned; rolled-back work does not execute. Record actual atomicity boundaries. |
| Q10 Scheduling | Mix due, future and overdue work; restart the scheduler/worker. | No unintended early execution; bounded due-to-start delay and accounted missed schedules. |
| Q11 Retention and rescue | Age terminal jobs and create interrupted running jobs in an isolated fixture. | Retention preserves required audit evidence and recovery makes abandoned work observable/processable. |
| Q12 Shutdown and rollout | Drain/stop then replace a worker; separately test abrupt termination and compatible mixed versions. | In-flight behavior, graceful timeout and old/new job compatibility match the declared contract. |

## Dedicated database experiments

| Test | Controlled change | Measurements and checks |
|---|---|---|
| D01 Connection budget | Sweep total budget and allocation across APIs/workers with fixed arrivals. | Acquisition wait, active sessions, query latency and useful throughput; identify the saturation knee. |
| D02 Query shape | Vary account cardinality, filter selectivity, history windows and data skew. | Fingerprints, execution plans, reads/rows, temporary spills and expected results. |
| D03 Write amplification | Compare ingestion-only, decisions, rule histories and required deliveries on equivalent corpora. | WAL bytes, commits, fsync/commit latency, table/index growth and useful throughput. |
| D04 Lock contention | Concurrent operations on one hot account/case versus independent objects. | Lock types/duration, blocked sessions, deadlocks, transaction duration and retry outcomes. |
| D05 Planner statistics | Compare declared fresh and stale-statistics snapshots on a growing test dataset. | Estimate-versus-observed row counts and plan changes; no undeclared index/statistics changes mid-comparison. |
| D06 Maintenance | Run representative vacuum, retention deletion, index preparation and checkpoint activity separately. | Foreground p99, I/O, WAL, lock duration, replication lag where applicable, and recovery. |
| D07 Storage pressure | Apply bounded I/O constraint or reduced free-space reserve on disposable infrastructure. | Error classification, commit durability, queue retention and restoration; stop before harming unrelated storage. |
| D08 Read freshness | Where a replica/read-only route exists, introduce lag and test immediate post-ingest decisions. | Freshness contract, missing-history behavior, fallback and correctness. Mark not applicable if no such route exists. |
| D09 Backup and restore | Restore a declared test backup into isolated infrastructure and replay a known reconciliation cohort. | Measured restore duration, data recovery point, migration compatibility and resumed queue behavior. |

Capture query plans for representative parameters at each relevant scale. `EXPLAIN ANALYZE` executes the statement; use read queries or disposable diagnostic copies and keep its cost outside timed qualification. Avoid counting a cache hit as proof that indexes scale. Separate server execution, lock wait, pool acquisition and network time when available.

Maintain consistent lock ordering and short transactions in any fixes derived from these tests. Follow service ownership and schema-qualified SQL. Schema/index optimizations require fresh baseline and changed-condition runs; a faster isolated query does not establish improved full-system capacity.

## Screening and analyst workload coverage

The initial internal scenario set does not exercise all screening or case-management behavior. Add the following cases when those paths are required by the intended deployment.

| Test | Workload | Checks |
|---|---|---|
| C09 Screening response shapes | No matches, one match, many matches and supported enrichments against deterministic fixtures. | Provider time versus local matching/persistence cost, payload size and correct result cardinality. |
| C10 Screening provider failure | Slow response, rate limit, invalid response and temporary outage. | Timeout/retry budgets, terminal state, bounded backlog and useful completion. |
| C11 Screening dataset update | Refresh a representative dataset while screening traffic continues. | Version/freshness evidence, I/O competition, consistent results and update recovery. |
| C12 Case queue reads | Multiple inboxes, permissions, filters and pages at increasing case counts. | Bounded reads, stable pagination, no cross-tenant/inbox leakage, latency independent of full investigation history where expected. |
| C13 Case mutations | Assign, close, comment, snooze and supported bulk actions with hot-case conflicts. | Atomicity, lock waits, conflict semantics, audit consistency and unaffected unrelated cases. |
| C14 Case intake and delivery | Concurrent duplicate workflow/screening events and receiver outage. | Intended case cardinality, deduplication, event ordering and eventual accounted delivery. |
| C15 Evidence and reports | Supported upload/finalize/download and report operations with realistic sizes. | Storage/network cost, authorization, interrupted-operation cleanup and bounded response sizes. |
| C16 Model/index preparation | Multiple supported publications during ingestion/decisions. | Preparation duration, lock scope, publication correctness and foreground latency. |

Keep stub-based provider benchmarks separate from actual dependency capacity. Do not load third-party endpoints as part of local discovery; use an authorized test endpoint when integration qualification is in scope. Record authentication, TLS and production-representative logging settings so a development-mode result is not presented as production-equivalent.

## Detailed experiment runbooks

Each runbook inherits the environment, ledger, telemetry, restoration and artifact requirements above. Record exact rates, sizes and deadlines before starting. Numerical examples are diagnostic defaults; they do not resolve missing business requirements.

### C06 Historical volume with a fixed evaluation corpus

Hypothesis: increasing stored history changes useful decision capacity independently of evaluation-input distribution.

1. Select one fixed evaluation corpus and build historical snapshots with 0, 1M and 5M transactions, excluding all evaluation identities. Include the required historical effect categories and document any intentionally absent history.
2. Verify identical live scenarios, schema/index definitions, resource limits and source distribution. Freeze the test event-time interpretation.
3. For each snapshot, run correctness fixtures, warm up with a separate declared corpus, then restore equivalent measurement starting state if warmup changed relevant history.
4. Apply the same measured corpus and arrival schedule. Preserve dispatch order but record actual commit order and any rule sensitivity to that order.
5. Measure successful logical completion, absolute latency, read/query amplification, pool waits, I/O and database growth.
6. Drain and reconcile. Repeat three times and rotate snapshot order. Show per-trial results and variation.

Pass condition: required absolute service levels and correctness hold at the specified sizes. Report relative retention separately. If the selected fixed corpus is too small for the run, extend it deterministically with fresh identities or shorten the declared experiment before execution; do not wrap and replay idempotent writes silently.

### Q03 Consumer outage and recovery

Hypothesis: accepted work survives a consumer outage and completes within the recovery budget without impairing unrelated service paths beyond their stated limits.

1. Choose one verified queue, identify all consumers, and measure its passing steady rate. Use a proposed 60% of that rate for the first outage trial so recovery capacity exists.
2. Run 10 minutes of stable load and save baseline queue age, depth and foreground latency.
3. Stop only the selected consumer set for a proposed 60 seconds. Continue arrivals and record acknowledgements, backlog growth and any upstream rejections.
4. Restart consumers and verify their effective configuration. Continue arrivals at the same rate until recovery or the declared deadline.
5. Measure restart-to-first-claim, successful completion rate, oldest age, pool pressure and time to return to the baseline backlog range.
6. Stop arrivals, finish bounded reconciliation, and compare all accepted identities with business outcomes and required deliveries.

Pass condition: no unexplained loss or unintended duplicate effects, correct admission behavior, and recovery within the configured deadline. Repeat with one consumer lost from a replicated pool before testing loss of all consumers. If arrival rate meets/exceeds post-restart completion capacity, classify inability to drain as a capacity finding rather than a recovery bug without further evidence.

### Q04 Ambiguous delivery and crash recovery

Hypothesis: failure after an external effect but before local acknowledgement does not produce an unintended duplicate business effect.

1. Use a deterministic test receiver that records a delivery identity and can delay/drop its acknowledgement after committing the effect.
2. Submit a known cohort, cause acknowledgement loss, and terminate the worker at the supported test boundary. Use explicit test hooks where needed; random termination alone cannot prove coverage of a precise crash point.
3. Restart the worker and observe job rescue/retry. Record attempt identities and receiver deduplication decisions.
4. Reconcile receiver effects, local job state, outbox state and original transactions.

Pass condition: each logical effect has the intended cardinality and all work reaches an observable outcome. Do not assert exactly-once transport; prove the business effect contract under repeated delivery. Repeat for rollback-before-commit and failure-after-local-commit boundaries where the path supports them.

### N01 Network sensitivity on historical reads

Hypothesis: additional decision-to-ingestion latency reveals excessive serial dependency calls or connection limits.

1. Select the supported HTTP read mode and an aggregate workload with known results. Identify the exact test interface/proxy and endpoints; preserve telemetry and control connectivity.
2. Measure untreated RTT, bytes, call count per decision, connection reuse and useful capacity.
3. Apply one-way added delay of 5, 20 then 50 ms in separate restored trials. Verify actual resulting RTT before each measurement.
4. At one fixed passing arrival rate, compare dependency call duration, overall completion, goroutines, pool occupancy and queue age. Separately search capacity if needed.
5. Remove the impairment after every trial and run a short untreated control to confirm restoration.

Pass condition: the declared service level holds within the supported network envelope. Outside that envelope, produce a sensitivity curve and classified failure behavior. Do not infer serial call count by dividing end-to-end latency by injected delay when calls overlap or retry.

### D01 Shared connection budget

Hypothesis: pool allocation can improve completion without increasing the database-wide connection ceiling.

1. Inventory all distinct pools and database reserved/administrative capacity. Confirm actual pool defaults when a configured value of zero delegates to library behavior.
2. Choose one fixed total budget and compare at least three allocations between foreground APIs and background workers. Keep replicas, workload and database hardware fixed.
3. For each allocation, run normal traffic plus a controlled backlog drain. Measure API pool wait, worker pool wait, active PostgreSQL sessions, query/lock time and useful completion.
4. Identify whether idle capacity exists in one pool while another starves. Repeat the best allocation with a higher total budget only if database resources support the hypothesis.
5. Confirm the selected allocation in the mixed-workload trial and with one replica unavailable where required.

Pass condition: configured latency/completion and drain targets hold within the connection limit. A lower pool-wait number alone is insufficient if database query time or foreground tail latency worsens.

### E03 Soak and background maintenance

Hypothesis: the selected hardware/configuration remains stable while normal growth, retries and maintenance occur.

1. Seed aged data, verify expected retention jobs, and define the duration, arrival schedule, maintenance events and expected storage growth envelope.
2. Warm up, then run the declared workload for 8 hours initially. Capture comparable 30-minute windows and preserve all restarts/configuration changes.
3. Trigger only declared maintenance events if their natural cadence would fall outside the run. Measure service levels before, during and after each event.
4. Track heap after comparable GC points where observable, RSS, goroutines, connections, ready age and latency against workload/growth. Do not interpret normal cache fill as a leak without evidence.
5. Stop arrivals, drain within the deadline and reconcile. Repeat for 24 hours if required by release qualification.

Pass condition: no unexplained upward resource/latency/backlog trend, no unexpected restart/OOM, and all stated service and recovery targets hold. Quantify observed growth and unsupported longer-term conclusions.

## Network and host diagnostics beyond HTTP timing

Extend network tests with controlled DNS failure/latency, stale keepalive resets, delayed response bodies, and connection establishment failure where those mechanisms are relevant. Distinguish connection refused, DNS failure, TLS failure, client timeout, server timeout and explicit overload. Map retries at each layer to avoid misattributing a retry storm to original user demand.

Use operating-system traffic controls or a dedicated test proxy according to the host environment. Record tool version, treatment direction, selected ports/endpoints, and whether a proxy changes protocol/TLS/connection behavior. Managed database hosts may not permit OS-level fault injection; place the treatment at a controlled client path and label the limitation. Do not claim to simulate a real failover merely by dropping a TCP connection.

For CPU capture cores/vCPUs, model, architecture, frequency behavior where available, container quotas/throttling, host contention and burst-credit behavior for burstable instances. For memory capture container limits, RSS, Go heap, page cache, swap/pressure and OOM events. For disk capture volume class, provisioned limits, capacity/free bytes, latency, queue depth, IOPS, throughput and filesystem/container-storage placement. For networking capture effective bandwidth, retransmits, drops, socket counts, file descriptors and ephemeral-port pressure.

Keep host and container scope explicit. A displayed CPU value of 100% may mean one saturated core rather than the whole machine. Do not compare unlike CPU generations solely by vCPU count, or identify a database cache miss solely from operating-system disk I/O without the relevant database evidence.

## Hardware sizing calculations and output contract

Sizing must present observed configurations first and projections second. Derive demand from stable successful work and validate the chosen configuration with actual tests.

| Calculation | Formula | Conditions |
|---|---|---|
| First CPU estimate | Required cores approximately equal target logical completions/second multiplied by measured CPU-seconds/completion, divided by chosen utilization fraction. | Same workload/code, stable service demand, and declared utilization target; validate parallelism and contention. |
| Concurrency estimate | Mean in-flight work approximately equals arrival rate multiplied by mean residence time. | Stable population and consistent boundary; use means, not p95, and include queue wait when inside the boundary. |
| Backlog growth during outage | New retained work approximately equals arrival rate multiplied by outage duration. | Adjust for rejected work, fan-out, scheduled arrivals and surviving consumers. |
| Recovery capacity | Required successful completion rate at least equals ongoing arrival rate plus backlog divided by recovery deadline. | Planning approximation; actual capacity may change with backlog/data size. |
| Retained application storage | Sum over categories of measured daily byte growth multiplied by category retention days. | Add indexes and mature-state effects; avoid double-counting included sizes and model retention cleanup. |
| Network demand | Bytes per logical input multiplied by input rate, measured separately for each path. | Include actual dependency fan-out, retries and protocol overhead. |
| Scaling efficiency | Measured throughput gain divided by resource/replica increase. | Same workload and service targets; identify a moved bottleneck. |

Memory sizing must use measured working sets, caches, heap/GC and container overhead rather than a generic bytes-per-request rule. Add explicitly justified reserves for bursts, maintenance and the required degraded condition. Keep durable database storage, WAL retention, backups, logs and benchmark artifacts as separate capacity lines.

The final hardware recommendation table must include service/worker role, replica count, CPU/memory allocation, pool limits, database compute/memory, storage capacity/IOPS/throughput, network placement, tested rate, p95/p99, error/deadline rate, maximum queue age, recovery time, and validated headroom. State the smallest configuration tested; do not call it the theoretical minimum. If no tested configuration qualifies, report the limiting evidence and next experiment instead of issuing a speculative production specification.

## Run configuration and artifact contracts

Implement a versioned configuration schema before automating the campaign. The following is an illustrative contract, not a currently supported CLI/configuration file. Null qualification inputs make a qualification run invalid; discovery mode can retain them and emit no production pass.

```yaml
schema_version: 1
mode: discovery
experiment: Q03
path: P04
workload: W03
environment_manifest: environment.json
source_manifest: workload.json
seed_fingerprint: null
arrival:
  model: constant_rate
  logical_inputs_per_second: null
  generator_schedule_lag_limit_ms: null
timing:
  warmup_seconds: 300
  baseline_seconds: 600
  outage_seconds: 60
  recovery_deadline_seconds: null
targets:
  successful_completions_per_second: null
  completion_p95_ms: null
  completion_p99_ms: null
  max_unexpected_failure_ratio: null
  max_ready_age_seconds: null
  max_unexplained_missing_identities: 0
  max_unintended_duplicate_effects: 0
resources:
  effective_pool_budget: null
  minimum_free_disk_bytes: null
  maximum_outstanding_jobs: null
fault:
  target_consumer: null
  restoration_deadline_seconds: null
evidence:
  require_reconciliation: true
  preserve_partial_results: true
```

Reject unknown keys, incompatible paths, invalid units, unsupported fault adapters and nonpositive deadlines where required. Save the resolved configuration and defaults actually used. Record separate discovery versus qualification status; do not infer qualification from a zero process exit code alone.

Proposed artifact structure:

```text
run-directory/
  resolved-config.json
  environment.json
  workload.json
  topology-and-queues.json
  stage-events.ndjson
  outcomes/
    ledger-parts/
    reconciliation.json
  metrics/
    generator.ndjson
    services.ndjson
    hosts.ndjson
    database.ndjson
    queues.ndjson
    histograms/
  diagnostics/
    traces/
    profiles/
    query-plans/
    sanitized-logs/
  faults-and-restoration.json
  validity.json
  summary.json
  report.md
```

Bound memory through streaming/partitioned ledgers and bounded collector buffers. Partition artifacts by run and time; estimate disk requirements during the pilot. Persist stage transitions before disruptive actions, flush outcomes periodically, and publish summary files atomically. Check artifact completeness on readback. Recovery after harness restart must not replay ambiguous mutations or reapply a fault without inspecting its recorded and observed state.

## Stopping conditions and restoration

Before each disruptive test, record the exact test targets, original network/resource/service configuration, expected fault duration, restoration method and independent verification method. Reject a target outside the disposable environment. Do not drop the application database or apply broad host-level impairments affecting unrelated workloads.

Configure bounded stop conditions for free disk reserve, maximum retained backlog, experiment duration, completion deadline, collector loss, and uncontrollable target scope. A queue-growth threshold can intentionally be crossed during a capacity search only up to the separate hard resource bound. Capture evidence first where safe, then stop new arrivals and restore the fault; do not wait indefinitely for drainage.

Use time-bounded fault controls with a restoration watchdog independent of the main load-generator task where feasible. Maintain a management path that the injected network fault cannot disable. Test restoration on a small pilot before larger injections.

The teardown sequence is: stop new arrivals; remove fault; verify actual network/service state; preserve outstanding-work evidence; allow bounded drainage; stop collectors after final snapshots; reconcile; restore the declared post-run configuration. Abrupt harness termination, cancellation and cleanup failure must leave a durable status indicating which work or fault may remain. A failed restoration cannot receive a pass even if the measured application metrics met their targets.

## Test validity and statistical interpretation

Predeclare measurement windows, rate tolerance, stabilization criteria, maximum missing telemetry, generator scheduling-lag tolerance, histogram precision, and comparison thresholds. Suggested pilot stabilization is three consecutive one-minute windows with useful rate within 5% of the interval mean and no increasing ready-age trend; adopt or revise this explicitly before qualification. Bursty and fault tests use their own declared phase predicates rather than this steady-state criterion.

Compare equivalent workload cohorts and hardware profiles. Report each confirmation run, median result and range; add confidence estimates only with a method suitable for correlated time-series data. Do not pool unrelated warmup, overload, recovery and steady-state windows into one latency percentile. Keep system errors, deliberate negative tests and valid fraud declines separate.

Detect collector restarts and cumulative-counter resets. Do not subtract counters across reset boundaries or use a negative delta as a performance measurement. Missing cloud metrics, unsupported host counters or unavailable query statistics must reduce the scope of the conclusion rather than silently disappear.

Investigate a bottleneck using a paired intervention: collect evidence, change one suspected constraint, restore equivalent initial state and rerun. If capacity does not improve as predicted, revise the hypothesis. CPU saturation, high cache hit rate, or pool occupancy alone does not establish the root cause.

## Implementation work packages and tests

| Package | Proposed change | Required verification |
|---|---|---|
| I01 Configuration and inventory | Versioned run schema, environment capture and queue/path registry. | Missing targets, configuration drift, invalid units, secret redaction and shared-database identity checks. |
| I02 Harness correctness | Deferred completion, outcome ledger, retry accounting, worker supervision and bounded waits. | All failures, accepted-but-never-completed work, uncertain commits, duplicate effects, source exhaustion and task exception tests. |
| I03 Load scheduling | Fixed arrivals plus existing concurrency mode, reproducible workload selection and schedule-lag capture. | Rate accuracy against a deterministic local target, constrained generator, cancellation and no silent dropped inputs. |
| I04 Telemetry | Existing endpoint adapters, host/database/queue collectors, histograms and correlation. | Unit/reset semantics, missing data, bounded polling cost, histogram merging and measured overhead. |
| I05 Fault controls | Scoped network/dependency/worker disruptions and restoration journal. | Timeout, cancellation, repeated cleanup, management-path survival and independent restoration verification. |
| I06 Reconciliation and report | Cohort/effect reconciliation, gates, timelines, partial reports and sizing outputs. | Fixtures with missing/late/duplicated outcomes, ignored queue work, wrong denominators, incomplete artifacts and absent acceptance targets. |
| I07 Campaign cases | Versioned workload and experiment definitions with per-case setup/teardown. | Small real-service pilot for every enabled path before scaling. |

Define a collector interface with start, sample, health and stop operations; a workload interface with prepare, schedule, submit and drain; a fault interface with scope validation, apply, verify and restore; and a reconciler that consumes the immutable input ledger plus durable outcomes. Interfaces must surface errors explicitly. Do not implement an unavailable collector or path as a successful no-op.

Use fake clients/clocks for deterministic harness tests, disposable PostgreSQL integration tests for transaction/queue contracts, small containerized tests for full path smoke coverage, and dedicated infrastructure for capacity/fault campaigns. Keep correctness test results distinct from performance qualification.

## Campaign priorities and completion checklist

Execute in increasing cost and disruption. These tiers determine scheduling priority; they do not waive critical-path requirements.

| Tier | Included work | Gate to proceed |
|---|---|---|
| A Measurement pilot | I01 through I04 foundations, W01/W02, P01/P02 and one required async path. | Outcomes and telemetry reconcile; intentional failure is detected. |
| B Core capacity | C01 through C08 and D01 through D04, plus required screening/case smoke cases. | Stable component baselines and first supported capacity findings. |
| C Delivery and resilience | Q01 through Q12 and N01 through N06 for enabled critical paths. | Queue/dependency recovery and cleanup demonstrated. |
| D Sizing and mature data | Hardware sweeps, D05 through D09 as applicable, C09 through C16 for required paths. | Candidate operating configuration and complete workload coverage. |
| E Qualification | E01 through E05 with fixed targets and candidate resources. | All required gates pass or explicit blockers remain. |

Estimate execution cost after the pilot using setup/seed time plus each trial's warmup, measured duration, drain/recovery, repetition count and artifact processing. Add soak and restoration verification separately. Reuse immutable source preprocessing, but do not reuse mutable database state across comparisons without declaring the effect. Parallel experiments sharing the database or host invalidate isolation unless resource competition is the intentional experiment.

The final review must answer all of the following:

- Are the intended deployment, active execution paths, data shapes and required queues covered?
- Are effective settings, source version, resources and environment limitations reproducible?
- Are accepted inputs reconciled through the correct completion and delivery boundaries?
- Are latency, successful throughput and deadlines reported with valid denominators and adequate samples?
- Does each critical queue meet normal-load and recovery requirements without unacceptable foreground impact?
- Are network sensitivity, connection budget, storage behavior and maintenance costs understood?
- Is the recommended hardware configuration actually tested at the required workload and degraded condition?
- Are fault restoration, harness cancellation and partial reporting verified?
- Are unsupported paths, missing telemetry, unresolved targets and untested growth assumptions explicit?

Deliver the review with the exact qualified configuration, capacity envelope by workload, queue recovery envelope, supported network conditions, resource sizing table, reproducible run manifests, ranked bottleneck evidence, and remaining work. A path with missing implementation or an unresolved critical requirement remains a qualification blocker rather than being hidden in an overall average.
