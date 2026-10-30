# Decision and rules benchmarks

Audit completed 2026-10-28; the subsequent instruction to continue authorized tooling implementation. The synchronous API framework and disposable local campaigns are implemented. See [validation evidence](VALIDATION.md) for measured scope and remaining gaps. No application evaluation logic or dependencies were changed. The original proposal below is retained as design context.

| Document | Contents |
|---|---|
| [Implementation audit](implementation-audit.md) | Actual request, evaluation, storage, dependency and test paths |
| [Capability matrix](capability-matrix.md) | Implemented operators, runtime restrictions and validation differences |
| [Benchmark plan](benchmark-plan.md) | Progressive matrix, reference cases, data and execution controls |
| [Implementation checklist](implementation-checklist.md) | Sequenced work, dependencies, statuses and acceptance criteria |
| [Results and reporting](results-and-reporting.md) | Proposed versioned result contract and comparison rules |
| [Risks and gaps](risks-and-gaps.md) | Evidence limits, investigation priorities and review decisions |

Audit source: local checkout based on `a2d241a53e082b1ff654634c41e83ecfb7328941`, with substantial existing tracked modifications and untracked files. Findings describe files as inspected, including modified decision service, payload preparation, auth and system-test infrastructure; the commit alone cannot reproduce this checkout. Before execution, capture a source manifest, dirty diff digest and built image digests. No claims are made about which source version a running deployment uses.

Documentation follows the existing `backend/stress-tests` separation from functional integration tests. Proposed tooling belongs in this directory, reusing sibling `production_replay` and `system_tests` modules. Small rule contracts belong in `backend/integration-tests/decision_engine_rule_fixtures`; evaluator diagnostics belong beside the Go evaluator tests. Existing runners remain authoritative for their own use cases; this design adds decision/rule correctness and a common experiment manifest.

## Usage

From the repository root using the existing stress-test Python environment:

```powershell
python backend/stress-tests/decision-benchmarks/benchmark.py validate --config backend/stress-tests/decision-benchmarks/examples/smoke.json
python -m unittest discover -s backend/stress-tests/decision-benchmarks/tests -v
python backend/stress-tests/decision-benchmarks/benchmark.py local --config backend/stress-tests/decision-benchmarks/examples/smoke.json --output backend/stress-tests/decision-benchmarks/runs/my-smoke --execute
python backend/stress-tests/decision-benchmarks/benchmark.py report --output backend/stress-tests/decision-benchmarks/runs/my-smoke
```

`validate` is offline contract validation; production API qualification runs before each timed trial. `local` creates new authenticated Docker services and one shared disposable PostgreSQL database. It accepts no application database URL. Use a new output directory. The existing lifecycle runner checks ownership before cleanup; retain `lifecycle.json`. Do not edit backend source during build/execution: a launch-to-workload source check rejects changes. Image reuse is optional and does not establish source provenance.

`examples/qualification.json` adds scalar, nested aggregate, tenant and ingestion cases. `examples/progressive.json` is an unexecuted larger matrix. Default local budgets cap seeded/background history at 10,000 rows and scheduled duration at 600 seconds. Increasing these requires suitable resources; the 512 MiB artifact disk guard is not a database capacity guarantee.

The strict fields are defined in `decision_benchmarks/model.py`: counts, families, numeric operators, logical nesting/predicate position, distinct thresholds, alternating outcomes, trigger/match ratios, seed, history size/cardinality, padding, tenants, arrival rate/mode, concurrency, timeout, warm-up and repetitions. `nonnull` means `not(is_null(field))`, not a separate existence operator. `match_ratio` selects canonical payload classes; actual outcomes depend on configuration and are independently verified.

Fixed arrivals preserve scheduled times and record unsent generator drops, which invalidate measured trials. Closed-loop concurrency is diagnostic. Every response is checked; persisted decisions/evidence are reconciled outside timing. No decision/ingestion retries occur. Background ingestion uses an excluded merchant key so selected aggregate history stays constant while the table grows.

Reports retain invalid trials, individual latency histograms and repeat ranges. Raw NDJSON, manifests, setup journals, query plans, source/image/environment records and reports are saved under ignored `runs/`. Archive these externally for long-term comparison. Missing telemetry is not zero. Authenticated HTTP aggregate-reader parity, million-row runs, capacity, soak and fault qualification remain open; local runs use explicit `direct_db` mode. No production service objectives are assumed.
