# Database-volume performance suite

This command runs all four phases in order with the `internal` scenario set:

1. Empty database: ingest and evaluate 1,000,000 records.
2. Seed 1,000,000 records, then ingest and evaluate the next 1,000,000 records from the same month.
3. Seed 5,000,000 records, then ingest and evaluate the next 1,000,000 records from the same month.
4. Seed one complete month, then ingest and evaluate 1,000,000 records from the following month.

The runner uses separate bounded ingestion and decision worker pools. A decision is submitted only after its corresponding ingestion succeeds. Failed ingestions are replaced with later source records until the exact decision-request target is reached, but any such failure now fails suite acceptance. Configured concurrency is an upper bound; queue backpressure and source selection can reduce active requests.

Per-record ingestion audit and outbox writes remain enabled for both seeding and measured ingestion. After each phase, the runner counts both tables and fails the phase if either contains fewer entries than the expected successful ingestion count. It also reports per-transaction record cardinality across ingestion-audit rows, ingestion outbox events, decisions, and rule executions, including min/max/average/p50/p95 and category totals. This is a total-count check, not identity-level reconciliation or proof of delivery.

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

`decision.request_latency` measures the original HTTP request. `decision.latency` measures the outcome-observation duration, including deferred waiting where enabled. Both include failed observations; unresolved work is explicitly counted. HTTP failures are conservatively unresolved because the server may have committed work before the response failed. There is no automatic resubmission of ambiguous decisions.

Ingestion retry counts include retries on ultimately failed records. Partial summaries show unfinished ingestion, unresolved started decisions and successfully ingested records still awaiting decision submission. Segments include the final incomplete block.

Each measured phase now writes `<phase>.identities.sqlite3`, a local test artifact containing keyed identity tokens and stage transitions. It rejects duplicate evaluation input identities and decision IDs reused across responses for the same tenant. Triggered decisions contribute their validated IDs; non-triggered scenario results may complete without a decision ID. The phase's `identity_reconciliation` summary requires exactly the requested number of completed identities, no additional identities, no duplicates and no unfinished work. Failed ingestions remain visible even when replaced by later records.

The ledger uses a bounded SQLite cache and disk indexes rather than retaining all identities in Python memory. Raw transaction fields, object IDs, decision IDs and the per-artifact token key are not saved. Tokens cannot be joined across runs or used for restart/resume. Normal failure/cancellation commits partial state; abrupt process termination can lose up to 255 transitions since the last checkpoint. Ledger I/O is included in measured throughput and needs overhead qualification. This reconciles observed responses only: seed records, unobserved duplicate server effects, exact scenario identity coverage, persisted decisions, callbacks and business delivery still need independent verification.

When database capture is enabled, `psql` ignores startup files, disables password prompts and uses read-only sessions with connection, statement, lock and process timeouts. Errors are recorded by type/exit code without stderr. Five active River states are sampled independently, ordered by scheduled time, up to 1,001 jobs each. A capped state marks every returned queue count as a **lower bound**; queues absent from a capped prefix have unknown backlog. Age is time since the earliest sampled scheduled time, clamped at zero, rather than enqueue age or execution duration. `case_queue`, non-River queues and terminal job history are not collected yet. Validate query plans and sampling overhead on the disposable target before a capacity campaign; bounded output does not guarantee a cheap query plan. PostgreSQL counters include `stats_reset` for later reset-aware rate calculations; this collector does not calculate rates. Activity visibility depends on the database role.

Each phase also records read-only PostgreSQL snapshots immediately before evaluation and after verification. These snapshots contain the database's cumulative `pg_stat_database` counters, active connection state/wait groups, selected PostgreSQL settings, and capped active River queues. During-evaluation samples are written to `<phase>.metrics.ndjson`; the before/after snapshots are in the phase JSON and `summary.json`. Compare the cumulative counters with the recorded `stats_reset` and timestamps; they are not hardware CPU, memory, IOPS, or remote RDS metrics.

Throughput fields use explicit windows: ingestion `requests_per_second` is successful ingestion completions divided by the first-ingestion-start to last-ingestion-completion window; decision `evaluations_per_second` is all decision attempts divided by the first-decision-start to last-decision-completion window; decision `successful_evaluations_per_second` is successful decision completions over that same decision window; and `pipeline_successes_per_second` is successful decisions divided by the end-to-end pipeline window from first ingestion start to final decision completion. Use successful decision EPS as the primary business metric, with ingestion and end-to-end rates as supporting measures.

## Progress and interrupted runs

The output directory is printed before preprocessing. `summary.json` records the current stage and phase; each active phase has its own JSON snapshot. Pipeline counters are saved every five seconds and on orderly cancellation or failure. Completed phases remain available when a later phase fails. JSON files are replaced atomically, so a reader sees a complete previous or new snapshot.

An interrupted or failed run has `acceptance.passed=false` and `acceptance.evaluated=false`. Error types are saved without exception text or raw server payloads. Abrupt process termination can leave the latest snapshot marked running; the snapshots are not a restart/resume ledger. Counter snapshots do not replace transaction-level outcome reconciliation.

`environment.json` captures the observer machine's OS/Python, CPU counts, memory, output filesystem, dependency versions, Git revision/dirty-state summary and harness file hashes. Service URLs are recorded without credentials, paths or query strings. It explicitly identifies remote hardware, deployed image digests and effective service configuration as missing. Local hardware is not assumed to be the service/database hardware.

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

Host disk/network counters are cumulative totals across the observer host, not service-attributed rates or measurements of link RTT, TLS cost, retransmissions, remote disks, or container quotas. Raw counter reset handling/rate derivation, remote exporters, database query/queue adapters, distributed tracing and controlled fault injection remain planned work. The current observer is the collection foundation, not complete architecture coverage.

## Verification

From `backend/stress-tests`, run the replay tests with:

```bash
python -B -m unittest discover -s production_replay/tests -t . -q
```

These tests use fake services/HTTP transports and local temporary artifacts. They do not recreate the PostgreSQL database. They cover completion contracts, tenant/result mismatch, async failure/deadline, producer/consumer cancellation, all-failure acceptance, partial artifacts, metrics errors and local observation. A real disposable-system load run is still required to establish capacity or verify the full deployed path.

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

The suite merges transaction files from `--data-root` and the optional `--seed-data-root` before selecting months. This supports production layouts where the preceding full seed month is stored separately from the evaluation month. It automatically selects a month containing enough records for the 5M phase and a populated consecutive month pair for the final phase. Use `--same-month YYYY-MM` and `--seed-month YYYY-MM` to choose them explicitly. Results are written under `backend/stress-tests/database-scale-runs/`; `summary.json` compares each phase with the empty-database baseline. Performance acceptance requires at least 80% throughput retention and no more than a 20% p95-latency increase for both ingestion and decision evaluation in every phase, alongside the completion, reliability and requested-telemetry gates described above.

The internal set has four scenarios and six rules: high-value account activity; odd-hour amount and burst checks; rapid account and multi-merchant activity; and a 30-day account amount-spike check.
