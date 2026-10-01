# Fixed-arrival queue campaign

`queue_campaign.py` implements the first executable queue experiment from
`SYSTEM_PERFORMANCE_TEST_PLAN.md`: scheduled ingestion followed by asynchronous
decision execution, with steady-load and burst/recovery stages. It reuses the
existing API client, tenant-scoped completion verifier, identity ledger and
optional runtime/PostgreSQL collectors.

It sends real ingestion and decision requests. Use a prepared test tenant with
the transaction model, related reference data, published scenarios and running
decision workers. It does not create a tenant, reset a database, start workers,
change worker counts, or inject failures.

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
real benchmark dataset is bundled. A row is limited to 1 MiB, and input is read
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
scheduled prefix's row structure and length, not server compatibility or unique
identities. Runtime ledger checks detect duplicate submitted identities.

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
  offered/submitted/completion rates. Short runs may have only an initial sample;
  the final summary is authoritative.
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

## Coverage limits

Client outstanding work is not database queue depth. The last successful cohort
completion after a stage ends is not proof that the whole queue recovered.
Per-stage latency follows the input cohort even if it completes in a later stage;
the overall throughput includes final drainage. Failed/unresolved records do not
enter successful latency percentiles but remain in acceptance denominators.

Hardware measurements describe the observer host, not remote service hardware.
Database counters are raw cumulative snapshots, not reset-aware rates. This does
not yet qualify Q01/Q02 queue stability or hardware sizing: queue-age/recovery
gates, independent durable-effect reconciliation, callback/workflow/screening/case
delivery, non-River queue collection, worker faults, remote host metrics and
automated hardware comparisons remain future work. No live capacity or fault
campaign has been run as part of this implementation.

Tests use fake service clients to cover async completion, tenant mismatch,
saturation, deadlines, cancellation by timeout, ambiguous failures, duplicate
identities, input validation and dry-run behavior:

```shell
python -m unittest system_tests.tests.test_queue_campaign
```
