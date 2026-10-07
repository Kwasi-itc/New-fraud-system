# Database-volume performance suite

This command runs all four phases in order with the `internal` scenario set:

1. Empty database: ingest and evaluate the fixed 1,000,000-record evaluation corpus.
2. Seed the first 1,000,000 records of the evaluation month, then use the same evaluation corpus.
3. Seed the first 5,000,000 records of that month, then use the same evaluation corpus.
4. Seed the complete preceding month, then use the same evaluation corpus from its following month.

Run each selected phase once. The declared RDS instance class defaults to `db.r7g.large`;
it is recorded as user-supplied information because SQL access cannot verify AWS hardware.
Keep the RDS instance, application images/settings, internal rules, source data, and
ingestion/evaluation concurrency unchanged. Tenant IDs are newly created per phase;
account and transaction distributions are retained through deterministic pseudonymisation.

The default `--evaluation-cohort fixed` reserves the first 5M records in the evaluation
month (`--evaluation-offset 5000000`) and takes the next 1M for every phase. This prevents
overlap with seeds and changing transaction mix from being confused with volume effects.
`--same-month` must agree with the month following `--seed-month` when both are supplied.
For a small empty-only smoke run use `--evaluation-offset 0`. The legacy
`--evaluation-cohort phase-specific` uses each phase's original next-record selection;
different evaluation fingerprints disqualify a controlled volume comparison.

The runner uses separate bounded ingestion and decision worker pools. A decision is submitted only after its corresponding ingestion succeeds. Failed ingestions are replaced with later source records until the exact decision-request target is reached, but any such failure now fails suite acceptance. Configured concurrency is an upper bound; queue backpressure and source selection can reduce active requests.

Before recreating any database, `preparing_evaluation_sources` materializes each distinct
month/offset selection once. Fixed-cohort phases reuse that same prepared source. The
remaining selected records are retained for replacement of failed ingestions; preparation
does not truncate the source at the evaluation target. Phase JSON records the prepared
target fingerprint, record count, preparation time and bytes, and acceptance checks the
actual submitted cohort against the prepared target. Preparation and the potentially large
month/prefix scan are excluded from workload timers and periodic workload observation.
During measurement a single reader thread supplies ordered batches of at most 128 records,
so file reads/JSON decoding do not stall HTTP requests or progress timers. Cancellation
joins outstanding reads before closing files. Allow additional temporary disk space for
these prepared sources alongside the sort chunks; they are removed with the sort directory,
not retained as restart/resume files.

Per-record ingestion audit and outbox writes remain enabled for seeding and measured ingestion.
After measurement, tenant-scoped SQL streams exact transaction and decision identities into
the local keyed-token ledger. Verification checks seed and completed evaluation identities,
exactly one persisted transaction, successful audit and ingestion outbox row per expected
object, and exact agreement between returned decision IDs and persisted decision IDs.
Missing, additional or duplicate effects fail verification. Scenario conditions that do not
trigger legitimately produce no decision row; 1M evaluations need not mean 1M decisions.

Record cardinality is reported **separately for seed and evaluated transactions** with
min/max/average, nearest-rank p50/p95, a constant-count indicator and category totals.
Categories are tenant transaction rows, successful ingestion audit, per-record ingestion outbox,
decisions, rule executions, single-ingestion idempotency keys and decision outbox. Seed
batches also create one shared idempotency key and one `batch.ingestion.completed` outbox
row per batch. These are counted and verified separately, with
`average_records_including_shared` to allocate their cost across seeded transactions;
they are never counted once for every transaction in the batch. Reference data,
River jobs and downstream delivery records are excluded explicitly. Database bytes are
also captured before seeding, before evaluation and after evaluation to distinguish seed
growth from evaluation growth. Table/index sizes and vacuum/analyze timestamps accompany
PostgreSQL boundary snapshots.

Exact reconciliation happens outside throughput timing and can scan all selected tenant
records; it is not run every five seconds. Output streams into bounded-cache SQLite, so
memory does not grow with all identities, but local disk usage does. Allow disk capacity for
the seed and evaluation ledgers. `--verification-timeout` defaults to 7200 seconds (two hours), with
read-only PostgreSQL sessions and a lock timeout. This verifies persisted identities and
counts, not rule detection accuracy or outbox delivery.
The deadline covers PostgreSQL evidence collection and the local SQLite comparison together.
Override it with `--verification-timeout 7200` on older deployments, or another finite,
positive number of seconds. There is no option to disable the deadline while verifying;
zero and infinite timeouts are rejected. Durable verification is required for acceptance.

Use `--skip-verification` to omit post-run PostgreSQL evidence collection and the SQLite
durable comparison for every selected phase. The pipeline still records and checks its
response identities, and storage snapshots and requested telemetry remain enabled.
Reports record `durable_reconciliation.status: skipped`, `valid: null`, and unknown
audit/outbox counts as null. All selected phases can complete, but suite acceptance
remains false with `durable_verification_skipped`; the command exits with code 2.
Performance measurements remain available and must be described as unverified.

## Completion and failure accounting

Reports use schema version 2. Acceptance requires zero ingestion failures, zero decision failures, zero unresolved decisions, and the exact successful completion target, in addition to the performance ratios. Decision throughput acceptance uses `successful_evaluations_per_second`. Legacy attempt-rate fields remain available for comparison; they are not successful completion rates.

The default `--deferred-policy reject` aborts the measured phase if the decision service defers work, including an HTTP 202 or a response marked deferred. This preserves the synchronous experiment's scope. A successful HTTP 200 must contain the expected object's scenario results, with tenant/object identity checked for triggered decisions. Valid fraud declines remain successful technical outcomes.

For an explicit mixed synchronous/asynchronous experiment, use `--deferred-policy wait`. The runner then starts `decision-engine-worker` in managed mode and follows each accepted execution through its tenant-scoped status endpoint. Success requires a completed execution and valid result evidence; failed, malformed, mismatched, or timed-out executions cannot pass. With `--no-manage-services`, the caller must run the appropriate worker. Callback delivery and independent database reconciliation are not verified by this completion check.

| Option | Default | Meaning |
|---|---|---|
| `--pipeline-timeout` | 86400 seconds | Deadline for each measured pipeline; cancels remaining tasks and saves partial counters. Does not bound sorting, migrations or seeding. |
| `--decision-completion-timeout` | 60 seconds | Maximum observation time after deferred acceptance, including status calls. |
| `--decision-poll-interval` | 0.5 seconds | Delay between status observations while deferred work remains pending. |
| `--capture-metrics` | Off | Capture host/process and service metrics around and during each measured pipeline. Incomplete requested capture fails acceptance. |
| `--capture-database-metrics` | Off | Requires `--capture-metrics`; add read-only PostgreSQL counters, connection waits, selected settings and capped active-job samples from `public.river_job`. Requires `psql`. |
| `--metrics-interval` | 5 seconds | Delay between observation cycles; sample duration is recorded separately. |
| `--metrics-timeout` | 5 seconds | Total deadline per service metrics request. |

All timeouts/intervals must be finite and positive. Async observation occupies an evaluation worker slot, so configured evaluation concurrency now bounds outstanding logical operations, including their polling. Compare runs with the same deferred policy; enabling a worker changes the workload competing for PostgreSQL resources.

Python 3.10 and later are supported. Asynchronous deadline handlers catch both
`asyncio.TimeoutError` and the built-in `TimeoutError`, which are distinct in 3.10.
Expiration of a sampling/progress interval schedules another observation; it is not
a workload failure. Actual request, database and completion deadlines still produce
failed evidence, and cancellation reaps background tasks and subprocesses.

`decision.request_latency` measures the original HTTP request. `decision.latency` measures the outcome-observation duration, including deferred waiting where enabled. Both include failed observations; unresolved work is explicitly counted. HTTP failures are conservatively unresolved because the server may have committed work before the response failed. There is no automatic resubmission of ambiguous decisions.

Ingestion retry counts include retries on ultimately failed records. Partial summaries show unfinished ingestion, unresolved started decisions and successfully ingested records still awaiting decision submission. Segments include the final incomplete block.

Each measured phase now writes `<phase>.identities.sqlite3`, a local test artifact containing keyed identity tokens and stage transitions. It rejects duplicate evaluation input identities and decision IDs reused across responses for the same tenant. Triggered decisions contribute their validated IDs; non-triggered scenario results may complete without a decision ID. The phase's `identity_reconciliation` summary requires exactly the requested number of completed identities, no additional identities, no duplicates and no unfinished work. Failed ingestions remain visible even when replaced by later records.

The ledger uses a bounded SQLite cache and disk indexes rather than retaining all identities in Python memory. Raw transaction fields, object IDs, decision IDs and the per-artifact token key are not saved. Tokens cannot be joined across runs or used for restart/resume. Normal failure/cancellation commits partial state; abrupt process termination can lose up to 255 transitions since the last checkpoint. Ledger I/O is included in measured throughput and needs overhead qualification. Seed and durable transaction/decision identities are checked after measurement; rule detection accuracy, callbacks and business delivery remain outside this verification.

When database capture is enabled, `psql` ignores startup files, disables password prompts and uses read-only sessions with connection, statement, lock and process timeouts. Errors are recorded by type/exit code without stderr. Five active River states are sampled independently, ordered by scheduled time, up to 1,001 jobs each. A capped state marks every returned queue count as a **lower bound**; queues absent from a capped prefix have unknown backlog. Age is time since the earliest sampled scheduled time, clamped at zero, rather than enqueue age or execution duration. `case_queue`, non-River queues and terminal job history are not collected yet. Validate query plans and sampling overhead on the disposable target before a capacity campaign; bounded output does not guarantee a cheap query plan. Activity visibility depends on the database role.

With both capture flags, each phase records PostgreSQL snapshots before evaluation and
immediately after the pipeline, **before exact verification queries**. During-evaluation
samples go into `<phase>.metrics.ndjson`; boundaries and `postgres_evaluation_delta` go
into phase JSON and `summary.json`. Counters include transactions/rollbacks, buffer reads
and hits, tuples, temp spills, deadlocks and read/write timing. Activity groups include
wait event names, blocked connection counts and oldest transaction age. Settings include
PostgreSQL version, max connections, shared buffers, work memory and I/O timing mode.
Differences, per-second rates and buffer-hit ratios are calculated only when timestamps
and `stats_reset` permit valid subtraction. Zero I/O time with `track_io_timing=off` is
not evidence of zero disk latency. SQL counters include observer/background database work.
Any failed required boundary snapshot or invalid boundary delta fails telemetry acceptance.
RDS CPU, memory, provisioned IOPS and physical storage latency remain unavailable through
this SQL-only access; they cannot be supplied by host `psutil` samples.

Throughput fields use explicit windows, also saved in `throughput_windows_seconds`:

| Rate | Numerator | Measurement window |
|---|---|---|
| Ingestion `requests_per_second` | Successful logical ingestions | First ingestion start to final ingestion completion, including failures/retries |
| Decision `evaluations_per_second` | Completed evaluation attempts, success or failure | First decision start to final decision completion |
| Decision `successful_evaluations_per_second` | Verified successful evaluations | Same decision window |
| End-to-end `pipeline_successes_per_second` | Verified successful evaluations | First ingestion start to final pipeline completion |

Sorting, setup, seeding, resource snapshots and exact reconciliation are excluded from
these rates. Ingestion and decision overlap: the pipeline can make their rates similar
because decisions consume successfully ingested records. Successful decision EPS remains
the primary metric; this is a coupled pipeline test, not an isolated maximum decision-capacity
test. Average/p50/p95/p99 latency, failures, retries and observed concurrency accompany rates.

## Progress and interrupted runs

The output directory is printed before preprocessing. `summary.json` records the current stage and phase; each active phase has its own JSON snapshot. Pipeline counters are saved every five seconds and on orderly cancellation or failure. Completed phases remain available when a later phase fails. JSON files are replaced atomically, so a reader sees a complete previous or new snapshot.

An interrupted or failed run has `acceptance.passed=false` and `acceptance.evaluated=false`. Error types and in-harness module/function/line locations are saved without exception text, local variables, full filesystem paths or raw server payloads. Abrupt process termination can leave the latest snapshot marked running; the snapshots are not a restart/resume ledger. Counter snapshots do not replace transaction-level outcome reconciliation.

`environment.json` captures observer OS/Python, CPU counts, memory, filesystem, dependencies,
Git revision/dirty-state and harness hashes. Each phase's `deployment` captures running
container image IDs/references, start time/restarts, configured resource limits, networks,
and an explicit allowlist of runtime controls. Database credentials and arbitrary environment
variables are excluded. Primary/read/worker database targets must match the test endpoint.
The Compose override forces synchronous mode and directs all three database URLs to the
test database. `--no-manage-services` records deployment inspection as unavailable.
The merged Compose project is validated before stopping services or recreating the database.
The current base file still requires its case-manager authentication variables during parsing
(`CASE_SERVICE_AUTH_TOKEN`, `CASE_SERVICE_TENANT_IDS`, `CASE_USER_JWT_ISSUER`,
`CASE_USER_JWT_KEYS_FILE`). Supply valid existing configuration; the benchmark does not
invent authentication credentials or weaken those production requirements. Case-manager
runtime services stay stopped during this experiment.
Run configuration records the declared RDS class and one run per phase; evaluation source
fingerprints/time ranges and published scenario IDs are saved per phase. `run-config.json`
also saves the internal scenario definitions, including trigger/rule formulas and thresholds.

Every phase also runs `checking_runtime_pools` after readiness and before setup/seeding,
even without capture flags. The decision and ingestion metrics endpoints must report a
positive effective primary pool maximum. If a positive `DATABASE_MAX_CONNS` is explicitly
configured for that container, it must match. A mismatch or unavailable/invalid endpoint
fails the run and saves `runtime_pools` evidence; container environment alone is not proof
that a deployed image supports its settings. Minimum connections, secondary pools and
worker pools are not verified through these API endpoints.

The base Compose file exposes independent controls: `DECISION_DATABASE_MAX_CONNS` /
`DECISION_DATABASE_MIN_CONNS`, `INGESTION_DATABASE_MAX_CONNS` /
`INGESTION_DATABASE_MIN_CONNS`, and `DECISION_WORKER_DATABASE_MAX_CONNS` /
`DECISION_WORKER_DATABASE_MIN_CONNS`. These map to each service's `DATABASE_*` variables.
Zero preserves the connection-string/driver defaults. The decision API and worker now
honor positive maximum/minimum settings and reject invalid effective limits. Budget all
API, read and worker pools and all replicas against the shared database; these controls do
not reserve connections or automatically enforce a database-wide budget. Rebuild the
affected images before rerunning; the runner recreates containers but does not build images.

## Read-only observation pilot

Install `production_replay/requirements.txt` in the selected Python environment. It includes `psutil` for host observation and timezone data on Windows. From `backend/stress-tests`, capture the current observer host without touching services or databases:

```bash
python -m production_replay.benchmark_observation \
  --output database-scale-runs/observation-pilot \
  --samples 3 --interval 5
```

The destination must be new. To include the existing service metrics endpoints, add their explicitly selected URLs:

```bash
python -m production_replay.benchmark_observation \
  --output database-scale-runs/observation-with-services \
  --samples 12 --interval 5 --timeout 5 \
  --decision-engine-url http://127.0.0.1:8082 \
  --ingestion-url http://127.0.0.1:8081
```

Authentication comes from `SERVICE_AUTH_TOKEN`, or the environment variable named by `--auth-token-env`. The observer performs GET requests only and never resets a database or starts/stops services. It uses its own HTTP connection pool and caps metrics responses at 1 MiB. Network/schema failures are recorded rather than substituted with zero metrics. Exit code 2 indicates incomplete observation.

Artifacts are `environment.json`, `observations.ndjson`, and `observation-summary.json`. The JSON lines include UTC timestamps, monotonic elapsed time, sampling duration, host/process CPU time, memory, disk and network cumulative counters, output filesystem space, and a numeric projection of known service metric groups. Dynamic endpoint labels are hashed, and arbitrary string values are omitted. The service percentiles retain their endpoint's sampling semantics; they are not full-run percentiles.

Host disk/network counters and consecutive-sample rates apply to the observer host.
CPU utilization is derived from consecutive CPU counters. These do not measure remote RDS
hardware or attribute all host activity to the fraud services. Remote exporters, tracing,
network fault injection and non-River queue coverage remain outside this volume test.

## Verification

From `backend/stress-tests`, run the replay tests with:

```bash
python -B -m unittest discover -s production_replay/tests -t . -q
```

The default tests use fake services/HTTP transports and local temporary artifacts. They cover completion contracts, tenant/result mismatch, async failure/deadline, cancellation, partial artifacts, stage timing, fixed corpus selection, missing-baseline rejection, metrics failures, statistics resets, credential-safe deployment inspection and durable reconciliation.

An opt-in SQL contract test uses only localhost and the disposable database
`fraud_scale_observation_test`: set `FRAUD_SCALE_TEST_PG_PORT` before running tests.
It replaces its fixture schemas in that database and verifies observation SQL, tenant isolation,
shared batch accounting and an indexed River sample plan with 20,000 terminal jobs.
It does not run application migrations or the full services. A real disposable-system load
run is still required to establish RDS capacity, plans at millions of records, or observer overhead.

## Privacy

The suite removes unused source fields before writing sort files or sending requests. `source_account_no`, `source_trans_id`, `thirdparty_id`, `terminal_id`, `account_name`, `payment_msisdn`, `narration`, `raw_account_ref`, and `raw_account_name` are dropped. `account_ref`, which the internal rules require for grouping, is deterministically pseudonymised with keyed HMAC-SHA256. PII-derived object and transaction identifiers are also replaced with namespaced HMAC tokens. Raw PII and the HMAC key are never written to reports.

For large sources, preprocessing creates smaller sanitized CSV copies once and avoids repeating HMAC work during each suite run:

```bash
./backend/stress-tests/sanitize_database_scale_sources.sh \
  --source-root /path/to/fraud_data \
  --output-root /path/to/fraud_data_sanitized \
  --pii-key-file "$PWD/pii-hmac.key" \
  --workers 6
```

Run it once for each source tree, then pass the sanitized roots to the suite with `--pre-sanitized-source`. The sanitizer preserves all transaction rows, retains only the eight CSV columns needed by the adapter, HMAC-tokenizes the account grouping key, blanks the unused source transaction identifier, and never changes the source tree. The blank identifier makes the adapter use its non-PII file-and-row identity fallback.

Create a private key file once:

```bash
umask 077
openssl rand -hex 32 > pii-hmac.key
```

## Run

The host needs `python3`, Docker Compose, and PostgreSQL client commands (`psql`, `dropdb`, and `createdb`). Use the absolute path to the RDS CA certificate. Put the database password in an environment variable rather than the command line:

```bash
export FRAUD_DB_PASSWORD='replace-me'

./backend/stress-tests/run_database_scale_suite.sh \
  --manifest backend/stress-tests/production_replay/manifests/fraud-data.json \
  --data-root /home/ubuntu/fraud_data \
  --seed-data-root /home/ubuntu/fraud_data_seed \
  --pre-sanitized-source \
  --pg-host dev-fraud-database-1.cluster-cinofplxsbbb.eu-west-1.rds.amazonaws.com \
  --pg-port 5432 \
  --pg-user postgres \
  --pg-password-env FRAUD_DB_PASSWORD \
  --pg-admin-database postgres \
  --pg-sslrootcert "$PWD/global-bundle.pem" \
  --database-name fraud_scale_test \
  --allow-drop-database fraud_scale_test \
  --pii-key-file "$PWD/pii-hmac.key" \
  --ingestion-concurrency 50 \
  --evaluation-concurrency 50
```

The destructive guard requires `--allow-drop-database` to exactly match `--database-name`, refuses `postgres`, `template0`, and `template1`, terminates connections only for that exact database, and passes the exact name as a command argument to `dropdb`/`createdb`.

The suite merges both source trees before selecting months. Use `--same-month YYYY-MM`
and `--seed-month YYYY-MM` to choose them explicitly. `--phase empty 1m 5m 1month` or
repeated `--phase` flags select phases; omission runs all. Duplicate selections are rejected.
Results go under `backend/stress-tests/database-scale-runs/`. `summary.json` compares with
the named empty-database baseline only. Without that baseline, measurements are saved but
regression acceptance is marked not evaluated. Acceptance requires identical evaluation
fingerprints, at least 80% throughput retention and at most 1.2x p95 for ingestion and
decisioning, together with zero unexplained failures/unresolved work, exact durable
reconciliation and complete requested telemetry. A single run per phase gives a volume
comparison without estimating run-to-run variation. Database recreation does not guarantee
a cold RDS cache, and the database remains after the final phase for inspection.

The internal set has four scenarios and six rules: high-value account activity; odd-hour amount and burst checks; rapid account and multi-merchant activity; and a 30-day account amount-spike check.
