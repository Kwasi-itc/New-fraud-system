# Fixed-arrival queue campaign

`queue_campaign.py` implements scheduled ingestion, synchronous decisions and
asynchronous decisions, with steady-load and burst/recovery stages. It reuses the
existing API client, tenant-scoped completion verifier, identity ledger and
optional runtime/PostgreSQL collectors.

It sends real ingestion and decision requests. Use a prepared test tenant with
the transaction model, related reference data, published scenarios and running
decision workers. It does not create a tenant, reset a database, start workers,
change worker counts. An explicit optional outage can stop and restore one
verified disposable Docker worker, as described below.

## Input and prerequisites

Run commands from `backend/stress-tests`. Install
`production_replay/requirements.txt` in your Python environment.

Provide an NDJSON file containing one transaction **fields object** per line,
with a unique nonempty `object_id`. Fields must match the prepared tenant model;
for example, the structure is:

```json
{"object_id":"queue-test-000001","transaction_id":"queue-test-000001","date":"2026-10-01T00:00:00Z","amount":10,"account_ref":"existing-test-account"}
```

Supply all other fields needed by your model and rules. Existing source CSVs
and the database-scale manifest are not accepted directly. Prepare distinct
transactions for each run; reusing IDs can change the ingestion workload. No
real benchmark dataset is bundled. `prepare_inputs.py` can export manifest records
or generate fixture records; see the README. A row is limited to 1 MiB, and input is read
incrementally. The runner consumes one row for every offered arrival, including
arrivals dropped by the generator. It fails on early input exhaustion.

Authentication is read from `SERVICE_AUTH_TOKEN`, or the environment variable
named by `--auth-token-env`. Pass URLs explicitly for remote services. Set
`--expected-scenarios` to the applicable result count for your published workload.

## Validate and run

First validate a small schedule without sending requests or creating artifacts:

```shell
python -m system_tests.queue_campaign --input transactions.ndjson --output system_tests/runs/queue-smoke --tenant-id TEST_TENANT --stage smoke:10:10 --expected-scenarios 4 --deadline 5 --dry-run
```

Remove `--dry-run` to run the 100-transaction smoke test. Choose a new output
directory for each run; existing directories are refused. Dry-run checks the
scheduled prefix's row structure, length and duplicate identities, but not server
compatibility. Duplicate JSON keys and non-finite numbers are rejected. Input
fingerprints are checked after execution; a changed file cannot pass.

Choose `--path ingestion`, `--path ingest-sync`, or `--path ingest-async` (default).
Ingestion-only completion means a verified ingestion response; it does not promise
a decision. Sync requires synchronous completion and rejects deferred fallback.
Async requires deferred acceptance followed by verified completion. Each path
has its own acceptance meaning and should be compared separately.

A proposed burst diagnostic (rates and deadlines are examples, not established
capacity or production targets):

```shell
python -m system_tests.queue_campaign --input transactions.ndjson --output system_tests/runs/queue-burst --tenant-id TEST_TENANT --stage baseline:10:60 --stage burst:30:20 --stage recovery:10:120 --expected-scenarios 4 --deadline 15 --completion-timeout 60 --max-inflight 500 --capture-metrics
```

This requires 2,400 unique records and maintains baseline traffic during recovery.
Each stage is `NAME:logical-transactions-per-second:seconds`; names must be unique.
For a steady-load experiment, specify a single stage. Compare several separate
runs with equivalent data and configuration to explore capacity; automated rate
search and repeated-run qualification are not implemented.

## Measurement and acceptance

Arrival slots retain their original monotonic schedule. A saturated
`--max-inflight` limit or lateness above `--max-scheduling-lag` (default 0.1 seconds)
causes a visible generator drop. The generator does not lower the requested rate
to wait for a free slot. The in-flight limit includes ingestion and completion
polling; choose it to accommodate the intended outstanding cohort and account
for the HTTP pool and polling load.

Ingestion uses one attempt. Ambiguous ingestion/decision errors are unresolved,
not retried or treated as confirmed non-acceptance. Each submitted operation has
an overall `--completion-timeout`, including ingestion, async submission and
polling. After the schedule, outstanding operations finish within their remaining
deadlines. An operation can complete successfully yet miss the separately declared
`--deadline`, measured from its scheduled arrival.

A run passes its narrow response-level gate only if every planned input is
submitted and completes by that deadline, the response identity ledger reconciles,
and any requested telemetry is complete. A synchronous response cannot qualify
as exercising the async queue path. Valid fraud declines are technical successes.
Exit codes: 0 passed, 2 completed but failed acceptance, 1 runtime failure,
130 user interruption; argument validation also uses 2.

Artifacts:

- `summary.json`: atomic progress/final report, counts, per-stage input cohorts,
  approximate full-run latency distributions and acceptance.
- `timeline.ndjson`: roughly one-second cumulative counts for deriving interval
  offered/submitted/completion rates, including a final snapshot after drainage.
- `identities.sqlite3`: bounded-memory response identity ledger. Raw identities
  and its ephemeral hashing key are not persisted. It is not a restart ledger.
- `config.json` and `environment.json`: schedule, settings, source revision,
  harness hashes, endpoint identities and observer host information.
- `metrics.ndjson`: optional host/process and service snapshots.

`--capture-database-metrics` additionally requires `--capture-metrics`, installed
`psql`, and `PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER` plus suitable authentication
for the **same shared application test database**. Its sessions are read-only.
The inherited collector samples capped active-state prefixes of `public.river_job`;
counts may be lower bounds. Missing queues in a capped prefix are unknown, not
empty. Query plans and observer overhead still require validation on the target.

## Check named River queues and recovery

The optional named-queue check adds database evidence to the transaction results.
It queries only explicitly selected River queues, verifies the connected database
name and queue registrations before sending transactions, and records snapshots
in `queues.ndjson`. Install `psql` and configure its `PG*` environment for the same
shared test database used by the services. The name check alone does not prove
the service URLs use that database; verify deployed configuration beforehand.

Example (choose actual deployment queue names and your own acceptance thresholds):

```shell
python -m system_tests.queue_campaign --input transactions.ndjson --output system_tests/runs/queue-recovery --tenant-id TEST_TENANT --stage baseline:10:60 --stage burst:30:20 --stage recovery:10:120 --expected-scenarios 4 --deadline 15 --max-inflight 500 --queue async_decision_executions --queue-database TEST_DATABASE --queue-recovery-stage recovery --queue-recovery-deadline 90 --queue-healthy-seconds 15 --queue-max-active 10 --queue-max-due-age 5
```

This requires the selected queue to reach at most 10 active jobs and at most
5 seconds of due-job waiting age, staying within both limits across at least
15 seconds of consecutive observations. That confirmation must occur within
90 seconds after the recovery stage begins. A later unhealthy observation resets
the confirmation; all selected queues must be healthy together. The transaction
completion gate must also pass. Threshold violations during the earlier burst
are recorded but do not by themselves fail recovery.

| Option | Meaning |
|---|---|
| `--queue NAME` | Repeat for up to 16 registered queues in the same River schema |
| `--queue-schema` | Schema containing `river_job` and `river_queue`; default `public` |
| `--queue-database` | Required exact database name checked against `current_database()` |
| `--queue-recovery-stage` | Existing stage whose start begins the recovery clock |
| `--queue-recovery-deadline` | Time allowed to confirm recovery; must fit within that stage |
| `--queue-healthy-seconds` | Required healthy observation window |
| `--queue-max-active` | Active-job threshold per queue, including running, pending and future jobs |
| `--queue-max-due-age` | Maximum age of due available/retryable/scheduled jobs, in seconds |
| `--queue-interval` | Delay between observations; default 1 second, plus collection duration |
| `--queue-timeout` | Database query/process timeout; default 5 seconds |
| `--queue-max-sample-gap` | Maximum allowed gap between samples; default 7 seconds |
| `--queue-sample-cap` | Maximum exact count per queue/state; default 1,000 |

Each queue/state query reads an ordered prefix of at most cap + 1 jobs. Below
the cap, zero and nonzero counts are explicit. Beyond it, counts are lower bounds;
they cannot establish healthy recovery. Paused queues are unhealthy. Missing,
malformed or failed observations and excessive sample gaps fail acceptance.
A queue missing from the River registry blocks the run before transaction traffic.

This is **sample-based recovery for selected queues across all tenants**. It does
not isolate jobs belonging to the test tenant, prove worker liveness from a queue
registration, detect every between-sample spike, or verify terminal jobs and
downstream effects. Running/pending jobs contribute to active counts but not the
due-age metric. Scheduled future work can prevent an active-count gate from
passing; select appropriate queues and thresholds.

The collector uses read-only sessions, statement/lock/connect/process timeouts
and no job payload reads. A bounded result does not guarantee an inexpensive
query: validate `EXPLAIN (ANALYZE, BUFFERS)` and collector overhead on disposable
representative data before treating results as capacity evidence. The SQL and
recovery behavior have local tests, a live PostgreSQL 16 contract pilot with an
execution plan, and a real-service recovery smoke test. The contract fixture's
index does not establish representative production query cost; see VALIDATION.md.
The queue gate can run without `--capture-metrics`; the older broad database
collector remains available independently.

Scheduled traffic is also bounded by `--max-duration` (default 86,400 seconds)
and `--max-operations`. Outstanding operations may finish after that traffic
window, within their individual completion timeouts.

## Coverage limits

Client outstanding work is not database queue depth. The last successful cohort
completion after a stage ends is not proof that the whole queue recovered.
Per-stage latency follows the input cohort even if it completes in a later stage;
the overall throughput includes final drainage. Failed/unresolved records do not
enter successful latency percentiles but remain in acceptance denominators.

Hardware measurements describe the observer host, not remote service hardware.
Raw database snapshots are retained; campaign reports derive reset-aware commit
rates and host CPU fractions over valid intervals. Missing intervals are excluded,
not filled with zero. Database commits include observer and unrelated traffic.
Small live pilots passed, including a worker outage; they do not qualify queue
stability at production load or hardware sizing. General identity-level durable
reconciliation, callback/workflow/screening/case delivery, non-River queue
collection, remote host metrics and automated hardware comparisons remain future
work. The service pilot's durable counts apply only to its tiny isolated fixture.

## Bounded worker outage

For an async experiment, supply all four options:

```text
--worker-container-id FULL_64_HEX_ID
--worker-project system-tests-PROJECT
--worker-outage-stage outage
--worker-outage-seconds 2
```

The named stage must exist and leave at least two seconds beyond the requested
outage duration; outages are bounded to 300 seconds. The container must have
`system-tests.disposable=true`, matching `system-tests.project` and Compose project
labels, and a Compose service name ending in `-worker`. The harness checks the
immutable identity and labels before mutations. It refuses arbitrary application
containers. The service pilot supplies these labels only to its own fixture.

A persistent fault journal and separate watchdog are armed before stopping the
worker. Normal completion, cancellation and errors attempt restoration; the
watchdog restores on lease expiry if the parent disappears. Passing requires the
requested outage duration, restoration and successful watchdog verification.
Container running state does not prove application readiness; transaction completion
and the optional queue gate provide separate evidence. Docker/host loss can prevent
both processes from restoring; the journal records the scoped recovery target.
Real normal restoration and hard parent termination are tested: the dedicated
watchdog pilot kills its controller while an inert disposable worker is stopped
and verifies restoration independently. This does not test Docker daemon or host loss.

`--minimum-free-disk-bytes` defaults to 100 MiB and is checked before traffic and
periodically during a run. This is a stop threshold, not a reserved allocation;
external disk consumption can still exhaust storage. Duration, input count and
in-flight work have separate bounds. Failure reports retain incomplete outcomes.

Tests use fake service clients to cover async completion, tenant mismatch,
saturation, deadlines, cancellation by timeout, ambiguous failures, duplicate
identities, input validation and dry-run behavior:

```shell
python -m unittest system_tests.tests.test_queue_campaign
python -m unittest system_tests.tests.test_queue_recovery
```
