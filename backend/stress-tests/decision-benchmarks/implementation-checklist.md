# Implementation checklist

Statuses: **Not Started**, **In Progress**, **Blocked**, **Complete**. Complete means the acceptance criteria were met, not merely that code exists. Existing reusable tools do not make a new benchmark case complete. Implementation followed the instruction to continue. Disposable local execution was approved through the execution permission flow. Statuses below apply to the full acceptance criteria, not just the implemented subset; see VALIDATION.md for exact evidence.

| ID | Stage / task | Status | Depends on | Acceptance criteria / retained evidence |
|---|---|---|---|---|
| A01 | Trace decision, evaluation, persistence and authorization | Complete | — | Static audit references concrete functions and distinguishes request, scenario and rule outputs |
| A02 | Inventory capabilities and runtime gates | Complete | A01 | Catalog checked against validator, evaluator, aggregate compiler and SQL readers; disabled/partial paths identified |
| A03 | Inspect tests, replay, load and telemetry infrastructure | Complete | A01 | Reuse map and limitations recorded; no legacy results promoted to current baseline |
| A04 | Publish matrix, result design and risks | Complete | A01–A03 | Six requested deliverables available through README; source identity and no-execution limitation explicit |
| B01 | Review design/location/dependencies | Complete | A04 | User approves matrix and proposed reuse/location; changes to scope recorded |
| B02 | Approve execution environment and limits | In Progress | B01 | Named isolated DB/endpoints, auth profile, CPU/RAM/disk/time/row/load ceilings, telemetry access, cleanup ownership and optional fault scope recorded |
| B03 | Agree service objectives | Blocked | User-provided objectives | Numeric latency/error/completion objectives and workload scope supplied; otherwise discovery only, no production-capacity pass claim |
| C01 | Freeze source/environment manifest and safety preflight | In Progress | B01,B02 | Commit + dirty source digest + image digests; explicit disposable DB/endpoint identity; cross-service migrations verified; all replica pools fit one connection budget; application DB refused |
| C02 | Add versioned experiment/result contract | In Progress | C01 | Manifest validation, canonical hashes, reproducible seed, counters/units/missing-data semantics and unique artifact paths; schema compatibility tests reject malformed records |
| C03 | Adapt existing setup/clients and owned cleanup journal | Complete | C02 | Authenticated model/scenario/rule setup through owner APIs; preparation jobs awaited; live model/iteration verified; partial setup resumable or safely retained; no arbitrary cross-service SQL mutations |
| C04 | Implement L1 reference fixtures and oracle | Complete | C03 | Every L1 case plus normalization/boundary variants checks response and persistence; correct rejection cases distinguished; tests fail on wrong score/rule/tenant/status and no-trigger-as-approve mistakes |
| C05 | Implement simplest synchronous baseline | Complete | C04 | Exactly S=1/R=1 on actual API, declared input model, trigger=true, warm-up excluded, unique inputs, all responses checked; separately named all-scenarios-one-scenario control |
| C06 | Integrate bounded raw collection and observation | In Progress | C05 | Success/failure/all-attempt latency cohorts, p90 added, counters reset-aware, generator lag/drop/resource metrics; instrumentation-on/off diagnostic comparison quantifies overhead without removing correctness checks |
| C07 | Add evaluator microbenchmarks if diagnostics justify | Not Started | C04 | Separate Go benchmark of real AST/evidence path with allocations and recorded cache state; no claim of API throughput; intended cases pass semantic tests |
| C08 | L2 rules, AST depth and short-circuit cases | In Progress | C05,C06 | 2/5/10/25/50/100 rules; hit/miss/mixed; identical/distinct formulas; nested AND/OR/NOT and snoozes; exact expected score and evidence checks |
| C09 | L3 scenario fan-out and partial failures | In Progress | C08 | 1/2/5/10/25/50 scenarios as budget allows; correct selection/result set; distinct outcomes; controlled sibling failure reconciles committed rows and demonstrates retry policy |
| C10 | Small aggregate oracle and reader parity | In Progress | C04 | COUNT/DISTINCT/SUM/AVG/MIN/MAX under allowed configuration; empty/null/duplicate/boundary/nested NOT cases; HTTP and direct_db agree or discrepancy blocks that cohort; unsupported functions explicitly reject |
| C11 | Deterministic scale seeder and query-plan capture | In Progress | C10,B02 | Bounded ingestion batches, exact counts/hashes/distribution, no app DB access, prepared indexes, stored plans and dataset bytes; cleanup restricted to owned environment |
| C12 | L4 10k/100k history | Not Started | C11,C06 | Small oracle passes first; approved scale runs save plans/query stats with warm/cold definitions and exact match selectivity |
| C13 | L4 1m/5m history extension | Not Started | C12,B02 | Explicit disk/runtime headroom and scale approval; actual seeded counts verified; no skipped volumes described as measured |
| C14 | L5 mixed/tenant/payload/ingestion adapter | In Progress | C09,C10 | Manifest mix realized and reported; cross-tenant lookalikes isolated; fixed-state and evolving-history contracts distinct; exact payload/response sizes |
| C15 | L6 decision-only fixed-arrival adapter | Complete | C06,C09 | Reuses system scheduler, bounded inflight, records dropped/late arrivals, response correctness enforced; synthetic fake-client tests verify pacing/accounting/drain behavior |
| C16 | Async/fault/recovery extension (optional) | Not Started | B02,C15 | Acceptance and terminal completion separate; bounded polling/reconciliation; only approved disposable fault targets; failed/timeout/retry/partial-commit cases and restoration proven |
| C17 | Reports and baseline comparisons | In Progress | C02,C06 | Machine-readable raw data and HTML/Markdown report; incompatible cohorts rejected; no averaging percentiles; invalid runs visible; correctness regressions dominate performance changes |
| D01 | Run correctness qualification | In Progress | C04,C08–C10 | All approved fixtures pass; tenant/authorization/rollback/concurrency checks retained; any gap named rather than hidden |
| D02 | Execute repeated baseline/scaling campaigns | In Progress | D01,C12,C15,C17 | At least 3 valid repeats per selected cell; warm-up separated; generator healthy; resource/counter evidence saved; source/config/data equivalence checked |
| D03 | Execute approved scale/mixed/soak/recovery | Not Started | D02,C13,C14; C16 if selected | Approved limits respected; anomalies repeated; state changes reconciled; unavailable metrics explicit |
| D04 | Attribute bottlenecks and propose changes | In Progress | D02,D03 | Each recommendation links measurements/plan/profile, confidence and correctness/security trade-offs; no production optimization before baseline |
| D05 | Rebenchmark approved optimizations | Not Started | Separate optimization approval,D04 | Identical semantic oracle and controlled A/B environment; improvements reported with repeated-run variability and regressions |

Stage A completion is static evidence and reviewable design. Stage C completion requires the agreed tooling and qualification tests, with optional extensions explicitly included or excluded. Stage D completion requires saved raw results and evidence-based analysis; it cannot be satisfied by filling result records with estimates.

Proposed implementation boundaries:

- `decision-benchmarks/`: manifests, decision-only adapters, semantic oracle, schema, generated-run entry points and reports; generated `runs/` ignored when implemented.
- `production_replay/`: reuse API clients, identities, completion verification and observers through existing public module functions. Extract shared helpers only if needed and covered by contract tests.
- `system_tests/`: reuse fixed-arrival scheduling, campaign preflight, telemetry and disposable lifecycle. Extend authenticated fixture configuration rather than copying disabled-auth defaults.
- `integration-tests/decision_engine_rule_fixtures/`: small functional contracts; never large seeds in ordinary CI.
- `decision-engine-service/internal/runtime/ast_eval/`: optional Go diagnostic benchmarks beside implementation tests.

Dependency proposal: use the repository's existing Python/httpx, unittest/pytest, PostgreSQL observation and Go test stack. `production_replay/requirements.txt` currently specifies httpx>=0.27,<1, openpyxl>=3.1,<4, psutil>=6,<8 and Windows tzdata; `system_tests/requirements.txt` includes it. Reuse `BenchmarkValidation` for preparation diagnostics. k6 is optional where an existing profile fits; no new load framework or dependency is currently justified. These ranges do not freeze installed versions: capture the resolved environment after approval. No dependencies were installed in Stage A.

## Evidence and remaining acceptance work, 2026-10-29

B01: continuation authorized the documented implementation. B02: local low-rate disposable stacks were approved; scale/fault budgets and production objectives remain unspecified. C03-C05 and C15: live scalar qualification, persistence checks, single-rule baseline, bounded pacing and 48 focused regression tests passed. C04 covers the seven implemented L1 families; it does not claim exhaustive coverage of every capability in the audit.

C01: source contents, environment and images recorded; shared-stack migrations exercised. Full replica connection-budget qualification remains open. C02: strict experiment parser/hashes and result contract tests exist; exhaustive result-schema validation remains open. C06: raw/telemetry collection implemented; instrumentation overhead and complete resource attribution remain open. C08-C09: configurable rule/scenario fan-out and nesting implemented; snoozes, large sweeps and controlled partial failures remain open. C10: all six aggregate oracles passed direct_db execution; authenticated HTTP reader parity remains unresolved. C11: bounded seeding, exact counts, indexes, sizes and representative plans implemented; scale dataset fingerprints remain open.

C14: multiple tenants, payload padding and disjoint-key ingestion supported; state-changing selected history and full realized-mix reporting remain open. C17: raw JSON/NDJSON and Markdown/HTML reports implemented; automated cross-environment compatibility rejection remains open. D01-D02: two small campaigns passed, including three repeats of the simplest baseline; concurrency capacity, rollback/fault and scale qualification are not complete. D04: source-based hypotheses and observability gaps documented; no latency-dominating component established by these short runs. No speculative optimization was applied. C07 and C16 are optional diagnostics/extensions and were not implemented.
