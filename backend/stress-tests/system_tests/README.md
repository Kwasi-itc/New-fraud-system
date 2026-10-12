# System tests

System performance and resilience campaigns live here. Run the following commands
from `backend/stress-tests` so both Python packages are importable.

| File | Purpose |
|---|---|
| [EXPLANATION.md](EXPLANATION.md) | Plain-language walkthrough: why the test matters, how it works and what is complete |
| [SYSTEM_PERFORMANCE_TEST_PLAN.md](SYSTEM_PERFORMANCE_TEST_PLAN.md) | Full campaign plan and implementation status |
| [QUEUE_CAMPAIGN.md](QUEUE_CAMPAIGN.md) | Queue experiment prerequisites, commands, metrics and limitations |
| [queue_campaign.py](queue_campaign.py) | Fixed-arrival ingestion, sync and async decision runner |
| [campaign.py](campaign.py) | Versioned discovery campaigns, input and shared connection-budget preflight |
| [prepare_inputs.py](prepare_inputs.py) | Synthetic inputs or replay manifest export |
| [worker_outage.py](worker_outage.py) | Scoped worker outage with restoration journal and watchdog |
| [report.py](report.py) | HTML comparison and reset-aware telemetry summaries |
| [VALIDATION.md](VALIDATION.md) | Recorded live evidence and remaining coverage |
| [queue_recovery.py](queue_recovery.py) | Read-only named River queue collection and sample-based recovery gate |
| [tests/test_queue_campaign.py](tests/test_queue_campaign.py) | Local fake-client tests; no running services required |
| [tests/test_queue_recovery.py](tests/test_queue_recovery.py) | Collector contracts, recovery windows and combined acceptance tests |

Install dependencies and inspect the runner:

```shell
python -m pip install -r system_tests/requirements.txt
python -m system_tests.queue_campaign --help
```

Run system harness correctness tests:

```shell
python -m unittest discover -s system_tests/tests -t .
```

Run both the shared replay and system harness tests:

```shell
python -m unittest discover -s production_replay/tests -t .
python -m unittest discover -s system_tests/tests -t .
```

Use `system_tests/runs/<unique-run-name>` for generated artifacts; `runs/` is
ignored by Git. The campaign reuses API clients, completion verification,
reporting, identity tracking and observation utilities from `production_replay`.
The existing database-scale runner and its documentation remain in that package.

## Run isolated live smoke tests

For configurable generated traffic against a local database, see
[Local stress setup](LOCAL_STRESS.md). It creates its own PostgreSQL container,
tenant and four scenarios, then runs ingestion or synchronous/asynchronous
decision traffic. It needs no RDS endpoint or replay datasets.

With Docker running, these commands create their own disposable fixtures. They
do not use the application's database. Choose a fresh output directory each time.

```shell
python -m system_tests.postgres_pilot --output system_tests/runs/postgres-pilot
python -m system_tests.service_pilot --output system_tests/runs/service-pilot
python -m system_tests.watchdog_pilot --output system_tests/runs/watchdog-pilot
```

The PostgreSQL pilot checks queue SQL against a minimal PostgreSQL 16 fixture
and saves an execution plan. The service pilot builds the actual services, runs
ordered migrations against one shared disposable PostgreSQL database, creates a
synthetic tenant and four scenarios, exercises all three paths, stops/restores a
decision worker during traffic, checks sampled queue recovery, and checks stored
record counts. It removes its verified containers afterward; built images remain.
Builds may need registry/module network access. Logs and lifecycle records remain
under the selected output directory, including when a run fails.

The separate watchdog pilot stops an inert disposable worker fixture, forcibly
terminates its controller, and checks that the independent watchdog restores it
after lease expiry. Allow approximately two minutes. It verifies restoration
independence, not business processing during a crash.

`--reuse-images-from system-tests-<prior-project>` reuses a prior pilot's local
images; this does not rebuild changed application code. Record that distinction
when comparing runs. Fault injection is restricted to labeled disposable worker
containers; see [QUEUE_CAMPAIGN.md](QUEUE_CAMPAIGN.md).

## Prepare and run a discovery campaign

For a prepared test tenant, create separate inputs for each run. These synthetic
records match the replay fixture's internal transaction model; adjust/export
realistic records for other models. They do not create reference data or scenarios.

```shell
python -m system_tests.prepare_inputs --synthetic-prefix discovery-ingestion --count 10 --output system_tests/runs/inputs/ingestion.ndjson
python -m system_tests.prepare_inputs --synthetic-prefix discovery-sync --count 10 --output system_tests/runs/inputs/sync.ndjson
python -m system_tests.prepare_inputs --synthetic-prefix discovery-async --count 30 --output system_tests/runs/inputs/async.ndjson
```

Alternatively, use `prepare_inputs --manifest PATH --count N --skip N --output PATH`
to export the replay manifest's adapters. Export order follows manifest streams,
files and rows; it is not a global chronological merge. Existing output files are
refused. Use new IDs when repeating experiments against the same tenant.

Edit [examples/campaign.json](examples/campaign.json): supply your prepared tenant,
service URLs and expected scenario count. Input paths are relative to the config
file. Every stage is `name:transactions-per-second:seconds`.

```shell
python -m system_tests.campaign --config system_tests/examples/campaign.json --validate-only
python -m system_tests.campaign --config system_tests/examples/campaign.json --output system_tests/runs/discovery-001
```

Validation checks every scheduled input prefix, duplicate identities across runs
in the same tenant, fingerprints and semantic options before traffic. It cannot
establish server compatibility. The campaign runs experiments sequentially and
stops on failure by default. `continue_on_failure: true` permits later experiments;
it cannot convert failed experiments into a successful campaign.

Optional `connection_budget` declares one shared database budget:

```json
{"max_connections":100,"reserve":20,"pools":[
  {"role":"apis","replicas":3,"max_connections":8},
  {"role":"workers","replicas":2,"max_connections":4}
]}
```

Account for every service/replica and observer, maintenance and migration headroom.
The validator checks arithmetic, not effective deployment configuration.

Open `report.html` inside the campaign output. It compares stage cohorts without
averaging percentiles, identifies missing evidence, and includes reset-aware
telemetry summaries when available. Profile names are labels; they do not change
or verify hardware. `summary.json` always marks `production_qualified: false`:
the implemented campaign mode is discovery only. See [VALIDATION.md](VALIDATION.md)
for completed smoke tests and the remaining broader plan.
