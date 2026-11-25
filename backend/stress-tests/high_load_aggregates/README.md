# High-Load Aggregates Phase 0 Benchmark Harness

Isolated PostgreSQL benchmark harness for [HIGH_LOAD_AGGREGATES_PLAN.md](../../decision-engine-service/HIGH_LOAD_AGGREGATES_PLAN.md) (§3 Phase 0).

## Safety Guardrail

This harness **only** runs against an isolated disposable database:
- `DATABASE_URL` (or `--db-name`) must contain one of the disposable markers: `disposable`, `bench`, `test`, `tmp`.
- By default, `--manage-container` starts an ephemeral `postgres:16` Docker container (`high-load-aggregates-bench-db`) on `127.0.0.1:55439` with database `high_load_aggregates_disposable` and tears it down or leaves it isolated from all application databases.

## Agreed Phase 0 SLO Gate

- **Target Aggregate Latency**: `p99 <= 25 ms` at `200` concurrent decision evaluations for a **1M-row** hot merchant (`7d` window).
- **Write Overhead**: `< 10%` `BatchIngest` (500-row batch) throughput regression relative to baseline.
- Phase 3 (progressive time-bucket rollups) is built **only** if Phases 1–2 miss this SLO gate on this harness.

## Usage

```bash
python3 backend/stress-tests/high_load_aggregates/run_benchmark.py --manage-container
```

Outputs:
- `backend/stress-tests/high_load_aggregates/BASELINE_REPORT.md`
- `backend/stress-tests/high_load_aggregates/baseline_results.json`
