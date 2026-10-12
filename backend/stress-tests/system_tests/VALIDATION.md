# Validation record and remaining work

Recorded 2026-10-01. The core discovery suite is executable. The full system
performance/resilience plan is **not complete**, and no production capacity or
hardware recommendation has been established.

## Evidence collected

| Check | Result | Scope |
|---|---|---|
| Automated test discovery | 141 passed | Replay and system harness behavior, contracts and failure handling |
| PostgreSQL 16 pilot | Passed; cleanup verified | Minimal River-shaped fixture, cap/zero/future-age/paused/missing-queue checks, wrong database rejection and read-only enforcement |
| Query plan | Saved; observed 1.378 ms execution in this run | Small fixture with its own index; not a deployed-index or representative performance claim |
| Real ingestion | 10/10 completed within configured deadline | Synthetic tenant, actual service images and migrations |
| Real synchronous decisions | 10/10 completed | Four fixture scenarios; synchronous completion required |
| Real asynchronous decisions | 10/10 completed | Actual decision worker and River queue |
| Worker outage | 24/24 completed | Two-second scoped worker stop during continued traffic, followed by restoration |
| Queue recovery | Passed over 15 samples; no invalid samples/gaps | Peak observed active jobs 7; healthy confirmation 4.797 seconds after recovery-stage start |
| Durable fixture counts | 54 transactions, 54 audit records, 54 distinct audit object IDs, 54 outbox rows, 34 completed async executions | Read-only independent counts in the new tiny tenant; not general identity-level or downstream-delivery reconciliation |
| Restoration and cleanup | Passed | Watchdog verified restored worker; fixture containers removed |
| Controller hard kill | Passed; cleanup verified | Independent watchdog restored a stopped inert worker after its controlling Python process was forcibly terminated |

Recovery limits were active jobs <= 5 and oldest due age <= 5 seconds, held across
one second of healthy samples within six seconds after recovery began. These are
diagnostic fixture settings, not production SLOs. A count gate can pass with some
outstanding work; it is not a zero-backlog assertion. Sampled observations cannot
exclude spikes between samples.

Local evidence (ignored by Git; regenerate using README commands):

- `runs/postgres-pilot-20261001/summary.json`, `observations.json`, `query-plan.json`.
- `runs/service-pilot-outage-local-images-20261001/lifecycle.json`, `evidence.json`,
  `outage-queues.ndjson`, individual run summaries and the worker restoration journal.
- `runs/watchdog-pilot-20261001/summary.json`, `fault.json`, `fault.watchdog.json`.

The final application pilot reused images built by `system-tests-9d3daada546e`.
It used poll-mode workers and enabled the fixture's required `count_distinct`
aggregate. Earlier attempts exposed missing aggregate configuration and a
migration URL incompatibility with the pool setting; the fixture now configures
the aggregate and removes the runtime pool URL parameter for migrations. Those
failed runs were retained and cleaned up. This configures the test fixture; it
does not change application deployment defaults. All services shared one logical
PostgreSQL database, with ordered service migrations and no application volumes.

## What is still required

| Area | Remaining work |
|---|---|
| Workload coverage | Concurrent mixed paths, direct-decision variants, callback/workflow/screening/case paths and their completion contracts |
| Queue coverage | Inventory every enabled queue; add non-River adapters and downstream-delivery reconciliation |
| Durable correctness | Tenant-scoped, identity-level expected/actual comparisons at representative scale; prove no loss/duplicates through retries |
| Fault coverage | Dependency/network/database faults, retries, host/daemon loss and prolonged outages |
| Capacity | Automated rate search/repetition, realistic historical data, long steady/burst/soak runs and generator calibration |
| Hardware | Effective deployment inventory, remote/container telemetry, representative query plans, observer overhead and controlled resource comparisons |
| Qualification | Business arrival rates, latency and recovery targets, growth horizon, required enabled paths and failure tolerance |

The broad C/Q/N/D/P cases in the plan are not marked complete by these smoke
tests. Profile labels do not provision hardware. Connection budgets validate
declared arithmetic, not running pool settings. Reports intentionally keep
`production_qualified: false`; discovery results must not become an implicit
production approval.

Use [README.md](README.md) to reproduce the pilots and configure larger discovery
experiments. Use [EXPLANATION.md](EXPLANATION.md) for the plain-language explanation.

## Commit integration verification — 2026-10-12

The remaining discovery tooling was reconciled with the current main branch:
ledger storage/retention protections and PostgreSQL storage/counter observations
are retained alongside direct ingestion completion and reusable read-only queries.

- System harness discovery: 46 tests passed.
- Replay discovery: 169 tests ran, with 166 passing, one skipped and two timeout
  test failures. The pipeline unfinished-count failure also reproduced against
  the unchanged committed baseline. The reconciliation timeout assertion was
  intermittent; all eight timeout compatibility tests passed on the follow-up run.
- This verification did not execute live load, outage or database pilot scenarios.
