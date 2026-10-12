# Local stress setup

Run from `backend/stress-tests` with Python 3.11+ and Docker Compose available:

```shell
python -m pip install -r system_tests/requirements.txt
python -m system_tests.local_stress --output system_tests/runs/local-001
```

The default sends 300 generated transactions at 10 transactions/second through
ingestion and synchronous decision evaluation, with at most 20 operations in
flight. Each invocation builds local service images, starts a separate randomly
named Compose project, runs ordered migrations against one shared PostgreSQL 16
database, and creates a new tenant, transaction model and four published internal
scenarios (six rules). No source datasets or RDS configuration are needed.

Choose a fresh output directory for each run. Examples:

```shell
python -m system_tests.local_stress --output system_tests/runs/local-smoke --stage smoke:2:5
python -m system_tests.local_stress --output system_tests/runs/local-ramp --stage warmup:5:10 --stage load:25:60 --max-inflight 40
python -m system_tests.local_stress --output system_tests/runs/local-async --path ingest-async --stage load:10:30
python -m system_tests.local_stress --output system_tests/runs/local-ingest --path ingestion --stage load:10:30
```

All stages are measured, including a stage named `warmup`. The name is a label.
Supported paths are `ingestion`, `ingest-sync` and `ingest-async`. Async traffic
waits for terminal execution results; acceptance is not based on HTTP 202 alone.
`--deadline`, `--completion-timeout` and `--max-scheduling-lag` control the
completion and scheduling acceptance thresholds. The runner refuses more than
one million transactions or more than 24 hours of scheduled traffic.

The existing queue campaign supplies bounded concurrency, identity tracking,
latency/throughput reports and failure accounting. Inspect:

- `setup.json`: tenant, model and scenario setup.
- `workload/summary.json`: workload results and acceptance.
- `evidence.json`: overall workload outcome.
- `lifecycle.json`: stack lifecycle and cleanup outcome; always check this too.
- `docker.log`: build, migration, runtime and teardown logs.
- `compose.json`: exact local fixture configuration.

APIs bind to random loopback ports; PostgreSQL has no published port. All services
share the fixture's database. The database uses an anonymous disk volume rather
than the smoke pilot's RAM disk. On completion or an orderly failure, cleanup
checks project labels and removes the test project's containers and volume.
Artifacts and built images remain. A process kill or host crash can prevent
cleanup; `lifecycle.json` and `compose.json` identify the project to inspect.
No existing application database is selected or reset.

`--reuse-images-from system-tests-<prior-project>` skips rebuilding, for example
to repeat a run with another path. Those images may not contain current source
changes. The project name is recorded in the prior `lifecycle.json`.

This is a local synthetic load experiment, not the four-phase RDS database-volume
suite. Records use the existing fixture generator: one account and merchant,
constant amount 10, and advancing timestamps. This exercises the internal rules
but is not a representative business distribution or a fraud-outcome oracle.
Services and PostgreSQL retain the pilot's 512 MiB per-container limits. The
database pool settings and workers are recorded in `compose.json`; results do
not establish production capacity, RDS performance or downstream delivery.

Verify harness behavior without starting services:

```shell
python -B -m unittest system_tests.tests.test_local_stress system_tests.tests.test_pilots -q
```

Validation on 2026-10-12: all 46 system harness tests passed. Live runs built the
current service sources and completed 10/10 synchronous transactions, then reused
those images and completed 10/10 asynchronous transactions. Both runs reported
successful cleanup. Artifacts are under `runs/local-stress-sync-20261012` and
`runs/local-stress-async-20261012` (ignored by Git). These small runs verify wiring
and completion; sustained load and resource saturation have not been measured.
