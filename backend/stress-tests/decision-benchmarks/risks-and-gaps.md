# Risks, gaps and review decisions

Evidence level: static source inspection. No current throughput, latency, query plan, runtime setting or resource measurement was collected. Prior saved results are not validated for this modified checkout.

## Reliability and comparability risks

| ID / priority | Evidence and implication | Required resolution / acceptance |
|---|---|---|
| G01 / prerequisite | Dirty working tree includes decision, model, auth and harness changes. Commit alone does not identify source | Freeze source/build manifest before execution; retain dirty digest and per-service image provenance |
| G02 / prerequisite | Catalog/validation include unsupported historical statistics and disabled broad-read helpers; legacy load variants use them | Capability smoke gate rejects invalid successful-workload definitions. Preserve explicit rejection tests; never silently substitute a rule |
| G03 / prerequisite | Rule creation is weaker than commit validation; aggregate runtime allow-list is separate; publication may wait for indexes | Setup must validate, commit, prepare and publish, then evaluate actual fixture; capture effective API/worker allow-lists |
| G04 / high | Multi-scenario writes commit independently, while one sibling failure can fail the whole HTTP request | Test partial commits and uncertain timeouts; reconcile before any retry. Do not assert all-scenario atomicity or exactly-once behavior |
| G05 / high | History reads are outside the write transaction; request caches share some first-read values, not a global snapshot | Fixed-state baseline and evolving-state ledger separate. Concurrent reference/history mutation requires a documented range/ordering oracle |
| G06 / high | Payload validation normalizes values and fills nullable omissions; empty fields triggers a stored-object read | Test API normalization separately from raw evaluator semantics; maintain full valid payload model and explicit stored/inline variants |
| G07 / high | Legacy v1 auth accepts shared service credential or disabled mode, without user tenant membership checks on evaluate routes | Keep trusted-service boundary explicit. Authenticated benchmark configuration and cross-tenant resource tests; include user gateway tests if user-level authorization is claimed |
| G08 / high | Existing service pilot disables auth and uses constrained tmpfs DB; older stress README describes separate databases | Reuse lifecycle only with reviewed authenticated profile and one shared isolated PostgreSQL database. Durable storage required for representative scale. Do not copy outdated split-DB guidance |
| G09 / high | Metadata invalidation observed in process; 30s TTL; no verified distributed invalidation | Capture replica count; test publication/model changes with multiple replicas and wait for verified state before baseline. Do not claim immediate global consistency |
| G10 / high | SQL NULL/filter semantics differ from scalar comparisons; catalog support can exceed compiler support | Independent SQL-aware oracle, null/empty datasets, NOT/IN/prefix cases, direct/HTTP parity. Any mismatch blocks that aggregate cohort |
| G11 / medium | Rule persistence is per-row; evidence generation and cache keys can grow with R, tree size and payload bytes | Measure stages, allocations and write/query counts before any batching/cache redesign |
| G12 / medium | `past_decision_count` loads object history and decisions themselves increase it | Independent sequence oracle or fresh equivalent histories; cannot repeatedly reuse one object and call state unchanged |
| G13 / medium | Process metrics are cumulative/overlapping, rolling percentiles use 512 samples, no stable per-rule duration | Raw client distributions authoritative; counter reset handling; per-rule unknown reported honestly. Diagnostic logging overhead quantified |
| G14 / medium | Fixed arrival generator can saturate due to verification, disk or sockets | Observe scheduling lag/drop/CPU/I/O, bounded buffers, separate generator host if needed; retain invalid run rather than claim engine capacity |
| G15 / medium | SQL/index availability and shared DB constraints cannot be proven by source migration files alone | Approved environment catalog + migration compatibility checks and representative plans; schema-qualified access and tenant relationships tested |
| G16 / medium | Untriggered all-scenario results omit scenario identity; response checks cannot identify each miss directly | Single-scenario prequalification plus expected selection/count and bounded persistence reconciliation; document residual identity limit |
| G17 / medium | Switch helper can evaluate multiple branches; evidence traversal may evaluate otherwise unselected expressions; duration parser is limited | Explicit nested/branch/error and malformed-duration fixtures before using these helpers in successful benchmark cohorts |
| G18 / prerequisite for qualification | No agreed SLO or dedicated resource ceiling supplied | Discovery can follow environment approval; production-capacity claims remain blocked pending objectives and measurements |
| G19 / high | Prepared integer comparisons preserve exact values, but JSON AST constants decode through float64 | Distinguish field-to-field fidelity from large numeric literal fidelity. Pin expected mathematical values independently and treat any rounding mismatch as a correctness issue, not benchmark noise |

## Investigation priorities, not optimization claims

No observed performance problem is established yet. These are candidates with source evidence, a measurement gate and a possible later action. No production logic should change as part of Stage A/B.

| Priority / candidate | Source evidence and hypothesis | Measurement required | Possible later change / trade-offs and verification |
|---|---|---|---|
| 1: persistence amplification | `RuleExecutionRepository.CreateMany` loops inserts; larger R adds round trips and evidence bytes | rules_eval vs tx_rule_exec_insert/commit, SQL calls, bytes/WAL, CPU and p99 across R | Bounded batch insert if dominant; preserve returned order/evidence, transaction rollback and tenant ownership. Re-run R sweep, failure injection and tenant tests |
| 1: aggregate access | Arbitrary approved filters can select many rows; exact schema/index state unknown | Plans, buffers, query latency and selectivity at each H; pool/I/O pressure | Index/query-shape change only after evidence; adds write/disk/migration cost. Preserve NULL/time semantics; test ingestion interference and migration locks |
| 2: repeated AST/evidence/key work | AST unmarshalling, evidence recursion, JSON/hex key containing full payload | Go CPU/allocation profiles, large-payload and identical/distinct-node sweeps | Safe immutable compiled/memoized representation if warranted; invalidation, model revision and tenant key correctness are mandatory. Re-run cache concurrency/alias/error fixtures |
| 2: decision history | `past_decision_count` materializes per-object history | Object-history sweep and query projection/bytes/profile | Owner repository COUNT contract if supported by evidence; preserve all outcome/null/history semantics, concurrent observations and tenant isolation |
| 2: fan-out and pool budget | API x scenario x rule concurrency can increase demand on one shared DB | Per-replica pool waits/active queries, remote limiter, throughput knee and ingestion interference | Tune bounded concurrency/pools within global budget; trades peak throughput for tail latency/fairness. Retest cancellation, overload/recovery and hot-tenant fairness |
| 3: metadata/HTTP/reference cost | Model HTTP reads, optional ingestion HTTP, relationship hops | Preparation/read times, calls, cache misses, direct/HTTP parity at equal data | Evaluate existing direct_db mode or bounded projections/cache only if measured; stronger schema coupling/invalidation duties. No cross-service mutation bypass; retain auth and tenant tests |
| 3: scenario selection | Live-scenario tenant/type query and metadata cache | Cold/warm query plans with many unrelated scenarios/tenants | Index/filter/cache refinement if plan shows cost; publication invalidation and selection correctness must remain unchanged |
| 3: observability | No reliable per-rule production latency and limited rolling samples | Determine what client/phase/DB evidence cannot answer | Add narrowly scoped optional instrumentation only after separate approval; quantify overhead and avoid high-cardinality sensitive labels |

Confidence is high in the cited code structure, low in its importance to latency until measured. Expected benefits are directional hypotheses; none has a promised improvement percentage.

## Execution findings, 2026-10-29

The continuation instruction authorized implementation; explicit tool approval authorized low-rate disposable local execution. See [validation](VALIDATION.md) and the updated checklist. Original review questions below remain relevant to larger campaigns, rather than blocking the completed local work.

Authenticated HTTP aggregate-reader parity remains a concrete integration gap: [ingestion HTTP client](../../decision-engine-service/internal/clients/ingestion/http_client.go) creates requests without an Authorization header. The benchmark retains token authentication and explicitly uses direct_db. This code finding predicts an authenticated reader failure but was not fault-injected in the live campaigns; do not report parity as passed or silently disable auth.

Two low-rate campaigns passed response/persistence checks and verified disposable cleanup. They do not establish scalable throughput, overlapping concurrent decisions, recovery behavior or a latency-dominating component. The qualification campaign recorded a maximum of one in-flight decision in its nominal concurrency-two trial. Instrumentation overhead, service/DB CPU, exclusive query timing and production connection-budget evidence remain missing. Available query plans cover small synthetic histories and cannot justify index changes at scale.

Keep generated runs outside source control but archive complete artifacts and the corresponding dirty checkout. Source hashes alone cannot recreate uncommitted files. The initial development smoke changed harness code during execution and must not be used as a frozen performance baseline; the subsequent qualification captured source before building and checked it before traffic.

## Review decisions

Stage B should resolve these concrete choices before implementation/execution. The user explicitly requested approval before Stage C; this is the reason for the gate, not an inferred repository policy.

1. **Location and reuse:** proposed `backend/stress-tests/decision-benchmarks` for decision-specific manifests/adapters/oracles, existing integration fixtures for small contracts, reuse `production_replay` observers and `system_tests` scheduler/lifecycle. No new dependency proposed.
2. **Initial scope:** approve L1 scalar correctness and persisted single-scenario/single-rule baseline first, then L2/L3, then small aggregate parity and scale. Async, downstream actions and fault injection are optional extensions, separately budgeted.
3. **Environment:** name a dedicated disposable shared PostgreSQL environment and service endpoints. Specify authentication/trusted-caller profile, hardware/storage, replicas, worker topology, network placement and all pool limits. A tenant in the application DB does not satisfy isolation.
4. **Execution budget:** approve initial 10k/100k histories; choose whether 1m/5m, bursts and 2-hour soak are permitted, along with disk/RAM/load/time stop limits and cleanup ownership. Rates are calibrated from the pilot, not guessed.
5. **Telemetry:** determine container/host CPU/memory access, DB statistics/query-plan permissions, existing pg_stat_statements availability and retention policy. Missing observers constrain attribution and must remain visible in reports.
6. **Qualification objectives:** supply latency/error/completion SLOs and representative workload mix if production capacity is a goal. Without them, results remain discovery baselines. Confirm proposed initial mix or provide a sanitized representative distribution.

Unresolved empirical questions include HTTP/direct_db normalization parity across all aggregate result types, deployment index readiness, actual multi-replica cache staleness, full tenant-consistent FK coverage after all migrations, and whether the chosen generator can sustain verified arrivals. These are planned verification tasks, not assumptions answered by static inspection.
