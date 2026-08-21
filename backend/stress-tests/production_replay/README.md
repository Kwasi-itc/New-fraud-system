# Production Replay Harness

This standalone Python harness profiles internal reference and transaction dumps, prepares one isolated tenant, and replays all configured transaction streams against the existing ingestion and decision-engine APIs. It does not require changes to any service.

## One-Command Local Test

From `New-fraud-system`, run:

```bash
make production-replay
```

The Make target accepts the common replay parameters:

```bash
make production-replay TRANSACTIONS=2000 MULTIPLIER=3
make production-replay TRANSACTIONS=3000 MULTIPLIER=4x
make production-replay TRANSACTIONS=all MULTIPLIER=360x
make production-replay-async TRANSACTIONS=1000 MULTIPLIER=360x
```

Docker and service settings may be placed in `.env`, or in another Make-compatible environment file selected with `ENV_FILE`. The replay wrapper passes that same file to Docker Compose and does not replace an explicitly configured service mode with a mode derived from the replay request. Keep run-specific inputs such as `TRANSACTIONS`, `MULTIPLIER`, `TENANT_ID`, and data paths on the `make` command line:

```bash
make production-replay-async \
  ENV_FILE=/home/ubuntu/system.env \
  TENANT_ID='<existing-replay-tenant>' \
  DATA_ROOT=/home/ubuntu/fraud_data \
  TRANSACTIONS=500000 \
  MULTIPLIER=100
```

Every wrapper run now uses two transaction datasets in this order:

1. It prepares the tenant, then batch-ingests every transaction under `SEED_DATA_ROOT` through the ingestion service. This phase does not submit any decision requests.
2. Only after the seed completes successfully, it replays the requested `TRANSACTIONS` or source-time window from `DATA_ROOT`, including the normal decision request for each measured transaction.

For an empty-database baseline with no historical transaction seed, set `PRESEED=false`. When `REUSE_EXISTING_SETUP=false` and `TENANT_ID` is omitted, the harness creates a fresh tenant and prints its generated ID. Reference records and managed scenarios are still prepared, but no transaction from `SEED_DATA_ROOT` is read or ingested:

```bash
make production-replay \
  ENV_FILE=.env \
  PRESEED=false \
  REUSE_EXISTING_SETUP=false \
  REUSE_EXISTING_SEED=false \
  DATA_ROOT=/Users/kwilson/Desktop/ITC/fraud_data \
  TRANSACTIONS=10000 \
  MULTIPLIER=1 \
  DECISION_MODE=sync
```

The resulting `seed-summary.json` uses `status: skipped` and reports zero seed records and writes. `PRESEED=false` cannot be combined with seed reuse or seed resume.

`SEED_DATA_ROOT` defaults to `$(DATA_ROOT)_seed`. For example, `DATA_ROOT=/home/ubuntu/fraud_data` automatically uses `/home/ubuntu/fraud_data_seed` for history and `/home/ubuntu/fraud_data` for the measured replay. Set it explicitly when the directories are elsewhere:

```bash
make production-replay \
  DATA_ROOT=/home/ubuntu/fraud_data \
  SEED_DATA_ROOT=/home/ubuntu/fraud_data_seed \
  TRANSACTIONS=500000 \
  MULTIPLIER=100
```

The seed phase defaults to batches of 500 with 4 concurrent batch requests and a 900-second request timeout. The local wrapper also starts `ingestion-worker` so deferred ingestion and CSV jobs have a consumer during the seed and measured replay. Batch seed requests themselves still execute on `ingestion-service`; lowering their concurrency prevents long-running database checkpoints from causing a large group of requests to hit the timeout together. `SEED_BATCH_SIZE` may be set from 1 through 500; `SEED_MAX_IN_FLIGHT`, `SEED_PROGRESS_EVERY`, and `SEED_REQUEST_TIMEOUT` control its concurrency, progress reporting, and per-request timeout.

If setup completed but the seed was interrupted, rerun with the same tenant and batch size using `REUSE_EXISTING_SETUP=true` and `RESUME_SEED=true`. Setup is verified without being recreated. The seed source is scanned in its original deterministic order, and ingestion idempotency keys make already completed batches read-only replays while unfinished batches are inserted. Keeping the same batch size is required because it preserves those original batch keys:

```bash
make production-replay \
  TENANT_ID='<existing-replay-tenant>' \
  REUSE_EXISTING_SETUP=true \
  RESUME_SEED=true \
  SEED_BATCH_SIZE=500 \
  SEED_MAX_IN_FLIGHT=4 \
  SEED_REQUEST_TIMEOUT=900
```

Resume progress and `seed-summary.json` report `replayed_records` for records that were already safely committed and `inserted_records` for records added by the resumed process. `RESUME_SEED=true` cannot be combined with `REUSE_EXISTING_SEED=true`, because the latter intentionally skips all seed writes.

To stop seeding and test against whatever historical data is already present, set both reuse flags. The harness verifies the existing setup and looks up the first expected seed transaction by its indexed object ID, performs no setup or seed mutations, and proceeds to the measured replay:

```bash
make production-replay \
  TENANT_ID='<existing-replay-tenant>' \
  REUSE_EXISTING_SETUP=true \
  REUSE_EXISTING_SEED=true \
  TRANSACTIONS=500000 \
  MULTIPLIER=100
```

The resulting `seed-summary.json` uses `status: reused_existing`; `records` and `batches` are `null` because the harness deliberately avoids a potentially expensive full-table count. This mode confirms that seed data exists, but it does not claim the interrupted seed is complete.

For an async run on top of an existing prepared tenant, the continuation target forces the async request mode and both reuse flags. It does not override `LIVE_DECISION_MODE` or other Docker/service settings from the selected environment file. It starts the decision worker, prints the tenant's async execution status before and after the replay, and stores those snapshots in the result directory:

```bash
make production-replay-continue-async \
  ENV_FILE=/home/ubuntu/system.env \
  TENANT_ID='<existing-replay-tenant>' \
  DATA_ROOT=/home/ubuntu/fraud_data \
  SEED_DATA_ROOT=/home/ubuntu/fraud_data_seed \
  TRANSACTIONS=500000 \
  MULTIPLIER=100
```

Existing queued executions are not deleted or recreated; the worker resumes them and processes newly accepted executions from the continuation run as queue capacity becomes available.

To resume an interrupted measured replay in the same run directory, pass its
checkpoint together with the existing-setup and existing-seed safeguards. The
checkpoint validates the tenant, source fingerprint, multiplier, and decision
mode before any replay request is sent:

```bash
make production-replay-continue-sync \
  REPLAY_RESUME_FROM=backend/stress-tests/production-replay-runs/replay-<run-id>/checkpoint.json \
  TENANT_ID='<tenant-id>' \
  TRANSACTIONS=10000 \
  MULTIPLIER=1
```

Locally generated transaction samples inherit stable timestamps from their
source CSV files, so regenerating an unchanged selection preserves its source
fingerprint across interrupted runs.

For a synchronous continuation, use `production-replay-continue-sync`. It forces the sync request mode and both reuse flags while preserving Docker/service settings from the selected environment file. When `LIVE_DECISION_MODE=sync`, it stops the local decision worker before measuring so an old async backlog does not compete with the synchronous run:

```bash
make production-replay-continue-sync \
  ENV_FILE=.env \
  TENANT_ID='<existing-replay-tenant>' \
  DATA_ROOT=/home/ubuntu/fraud_data \
  SEED_DATA_ROOT=/home/ubuntu/fraud_data_seed \
  TRANSACTION_OFFSET=10000 \
  TRANSACTIONS=10000 \
  MULTIPLIER=100
```

`LIVE_DECISION_MODE` must resolve to `sync` from the selected Docker environment or the sync fallback, and `LIVE_ASYNC_OBJECT_TYPES` must not include `transactions`. The wrapper refuses conflicting settings, preventing another mislabeled sync result.

To avoid replaying records already used by an earlier measured run, set `TRANSACTION_OFFSET` to the number of records previously selected. The offset is applied after globally ordering all six transaction streams by source timestamp. For example, this selects source positions 10,001 through 20,000:

```bash
make production-replay-continue-async \
  ENV_FILE=.env \
  TENANT_ID='e12add53-58ca-4769-b7d7-57720ab05e39' \
  DATA_ROOT=/home/ubuntu/fraud_data \
  SEED_DATA_ROOT=/home/ubuntu/fraud_data_seed \
  TRANSACTION_OFFSET=10000 \
  TRANSACTIONS=10000 \
  MULTIPLIER=100
```

For later consecutive runs, increase the offset by the number selected in each successful earlier run. `TRANSACTION_OFFSET` is a run parameter and does not belong in the Docker `.env`. It requires a numeric `TRANSACTIONS` value and cannot be combined with `TRANSACTIONS=all` or a duration selector.

You can replay a source-time window instead of choosing a transaction count:

```bash
make production-replay HOURS=6 MULTIPLIER=360x
make production-replay DAYS=2 MULTIPLIER=360x
make production-replay WEEKS=1 MULTIPLIER=360x
make production-replay DURATION=12h MULTIPLIER=360x
```

When `DURATION`, `HOURS`, `DAYS`, or `WEEKS` is set, the transaction count is ignored. The duration sample starts at the earliest source transaction timestamp and includes all configured streams up to that source-time cutoff.

`MULTIPLIER` also accepts a `*` suffix, for example `MULTIPLIER='360*'`. Quote it in your shell so it is not treated as a filename pattern.

Both modes submit each event to `POST /v1/tenants/{tenant_id}/ingestion-events/record-ingested`; the request payload sets `mode` to `async` or `sync`. `production-replay` sends synchronous requests by default and the async targets send asynchronous requests. `LIVE_DECISION_MODE` remains an independent Docker/service setting: when it is present in the selected environment file, the wrapper preserves it. For EC2 async replay, use:

```bash
make production-replay-ec2-async TRANSACTIONS=1000 MULTIPLIER=360x
```

When the selected environment file does not define the service settings, the local wrapper uses these async-oriented fallbacks for an async target:

- `LIVE_DECISION_MODE=async_only`
- `LIVE_ASYNC_FALLBACK_ENABLED=true`
- `TENANT_DATA_READ_MODE=ingestion_http`

Without an explicit environment-file value, the synchronous target falls back to `LIVE_DECISION_MODE=sync`.

For an explicit synchronous comparison run:

```bash
make production-replay TENANT_ID=<tenant-id> \
  LIVE_DECISION_MODE=sync \
  LIVE_ASYNC_FALLBACK_ENABLED=true \
  TENANT_DATA_READ_MODE=ingestion_http \
  DECISION_MODE=sync
```

For local async replay, the wrapper starts a small host callback receiver automatically when `ASYNC_CALLBACK_URL` is not provided:

```bash
make production-replay-async TRANSACTIONS=1000 MULTIPLIER=360x
```

The local receiver is passed to Docker workers as `http://host.docker.internal:8099/callbacks/async-decision`. The replay output directory includes `async-decisions.ndjson` and `async-callback-summary.json`. The summary reports how many async-mode requests completed inline versus how many were deferred, plus per-deferred-execution callback timing.

Both async targets accept `ASYNC_WAIT_TIMEOUT_MS=<milliseconds>`. When omitted, the service default async wait window is used. Local async replay also accepts `ASYNC_CALLBACK_PORT=<port>` and `ASYNC_CALLBACK_WAIT_TIMEOUT=<seconds>`.

To ask the decision engine to call back after each async execution completes or fails, pass a callback URL that is reachable from the decision-engine worker:

```bash
make production-replay-ec2-async TRANSACTIONS=1000 MULTIPLIER=360x ASYNC_CALLBACK_URL=https://example.com/fraud/async-callback
```

You can still run the wrapper directly:

```bash
./backend/stress-tests/production_replay/run_local_replay.sh
```

This starts the required Docker services from existing images using `--no-build`, prepares its Python environment and tenant, loads reference data from `DATA_ROOT`, optionally batch-ingests historical transactions from `SEED_DATA_ROOT` without decisions, rebuilds the frontend with the replay tenant ID, and then replays the configured number of production-format transactions from `DATA_ROOT` across all six streams. It prints compact seed, ingestion, and decision summaries. The harness uses the base Compose file directly and relies on the system-level single `fraud` database configuration; it no longer applies replay-specific database overrides. The command leaves Docker, the frontend, and the local tenant running for inspection. If a required backend Docker image does not exist, it fails instead of building it.

## Safety Model

- `profile` is always read only.
- `setup` only calls services when `--execute` is present.
- `setup --reuse-existing` only reads and verifies the existing tenant, transaction model, and live replay scenarios; it does not recreate setup resources.
- `seed` only sends ingestion requests when `--execute` and `--tenant-id` are present. It uses deterministic idempotency keys and never calls the decision endpoint.
- `seed --reuse-existing` verifies one expected historical transaction using its indexed object ID and performs no seed writes.
- `seed --skip` records an intentionally skipped seed with zero writes and does not contact the ingestion service.
- `run` only sends events when `--execute`, `--tenant-id`, and a positive `--multiplier` are all present.
- Replay artifacts intentionally include normalized request bodies for successful calls and complete service response bodies for successful and failed calls; treat the replay output directory as sensitive data.
- Ingestion retries reuse one deterministic idempotency key. Decision callbacks are not retried.
- Technical errors are observed and summarized; they do not stop the remaining replay.

The `fraud-data.json` manifest covers the final June 2026 extract. It discovers the files that actually exist rather than trusting directory names for coverage.

## Requirements

- Python 3.11 or newer
- `httpx`
- `openpyxl`
- data-model, ingestion, decision-engine, and data-model index worker available in the isolated environment
- the base Compose single `fraud` database configuration applied to data-model, ingestion, decision-engine, and screening
- a seed dataset containing the same six `transactions/<processor>/<direction>/*.csv` stream directories as the measured dataset; reference files continue to come from `DATA_ROOT`

## Validated Source Inventory

The final read-only profile contains 9,952,558 logical transaction rows in 180 daily files. Parsed event time spans `2026-06-01T00:00:00Z` through `2026-06-30T23:59:58Z`.

- average source rate: 3.8397 events/second
- p95 source rate: 10 events/second
- p99 source rate: 14 events/second
- peak source rate: 91 events/second
- incoming events: 7,186,697
- outgoing events: 2,765,861
- repeated source transaction identifiers within one file: 9, retained as separate versioned rows

At `100x`, the source timing compresses to roughly 7.2 hours and multiplies instantaneous rates by 100. `--max-in-flight` remains the hard client-side concurrency bound.

Run commands from `New-fraud-system/backend`:

```bash
cd backend
python3 -m pip install -r stress-tests/production_replay/requirements.txt
```

## Replay Matrix

To compare the operational modes from the stress-failure checklist, use the matrix runner.

Plan only:

```bash
cd backend
python3 stress-tests/production_replay_matrix.py --target local
```

Execute the local matrix:

```bash
cd backend
python3 stress-tests/production_replay_matrix.py \
  --target local \
  --execute \
  --tenant-id <existing-tenant-id> \
  --transactions 1000 \
  --multiplier 3600 \
  --capture-metrics
```

The default matrix covers:

- `LIVE_DECISION_MODE=async_only` replay posture
- `LIVE_ASYNC_FALLBACK_ENABLED=true`
- `TENANT_DATA_READ_MODE=ingestion_http`
- synchronous event writes to ClickHouse
- bounded event aggregates through ingestion-service

Each run writes:

- replay stdout/stderr logs
- the replay run directory reported by the wrapper
- `experiment-settings.json`
- optional metrics capture manifests when `--capture-metrics` is used

The matrix artifacts are written below:

```text
stress-tests/production-replay-matrix-runs/
```

For operational-table comparison runs, a separate PostgreSQL read pool can still be configured with:

- `READ_DATABASE_URL`
- `READ_DATABASE_MAX_CONNS`
- `READ_DATABASE_MIN_CONNS`

Production replay transactions use `storage_class=event`. Fresh setup explicitly
selects `account_ref` and `merchant_id` for ClickHouse projections because the
managed scenarios commonly constrain history by those fields. Reused setup must
already have the same projection choices; setup will not retrofit an event table
after ingestion has locked its schema. Transactions are always routed through
ingestion-service to ClickHouse, including when
`TENANT_DATA_READ_MODE=direct_db`; that mode remains direct only for operational
PostgreSQL tables. A separate PostgreSQL read pool does not affect transaction
aggregates on this path.

For remote runs, the wrapper records the requested service-mode assumptions in `experiment-settings.json`, but it does not reconfigure the remote services itself.

## 1. Profile

```bash
PYTHONPATH=stress-tests python3 -m production_replay profile \
  --manifest stress-tests/production_replay/manifests/fraud-data.json \
  --output /tmp/production-replay-profile.json
```

The profile reports source duration, per-second rates, missing fields, duplicate transaction IDs, categories, multiline fields, reference-data conflicts, and merchant/product join coverage.

## 2. Setup

Without `--execute`, this profiles only:

```bash
PYTHONPATH=stress-tests python3 -m production_replay setup \
  --manifest stress-tests/production_replay/manifests/fraud-data.json
```

Prepare a fresh tenant:

```bash
PYTHONPATH=stress-tests python3 -m production_replay setup \
  --manifest stress-tests/production_replay/manifests/fraud-data.json \
  --execute \
  --data-model-url "$DATA_MODEL_URL" \
  --ingestion-url "$INGESTION_URL" \
  --decision-engine-url "$DECISION_ENGINE_URL"
```

Use `--tenant-id <uuid>` to prepare an existing clean tenant. Existing compatible tables, fields, links, lists, and entries are reused. Incompatible definitions or colliding managed scenario names stop setup.

Setup creates:

- `merchants`, `merchant_products`, and `transactions` object types and their links
- deduplicated merchant and merchant-product reference records
- normalized staff-number, email, MSISDN, and merchant-name custom lists
- only the scenario-catalog rules supported by the transaction fields and configured streams
- publication index jobs, with a default 15-minute preparation timeout

The setup output includes the tenant ID needed for replay.

## 3. Seed

The Make and shell wrappers run this phase automatically. To run it directly, first create a full manifest whose transaction globs point to the seed dataset, then execute:

```bash
PYTHONPATH=stress-tests python3 -m production_replay seed \
  --manifest /tmp/fraud-data-local-seed.json \
  --execute \
  --tenant-id '<setup-tenant-id>' \
  --batch-size 500 \
  --max-in-flight 10 \
  --data-model-url "$DATA_MODEL_URL" \
  --ingestion-url "$INGESTION_URL"
```

The seed command streams all configured transaction files and sends only `POST /v1/tenants/{tenant_id}/ingest/transactions/batch`. It does not profile or sort the seed files, pace them by source time, or call `record-ingested`, so no decisions are requested by the replay harness during this phase. A summary records the number of seeded records and batches and explicitly reports `decision_requests: 0`.

## 4. Replay

Without `--execute`, this profiles only. A real replay requires an explicit speed:

```bash
PYTHONPATH=stress-tests python3 -m production_replay run \
  --manifest stress-tests/production_replay/manifests/fraud-data.json \
  --execute \
  --tenant-id '<setup-tenant-id>' \
  --multiplier 10 \
  --max-in-flight 500 \
  --checkpoint-every 10000 \
  --async-wait-timeout-ms 0 \
  --async-callback-url "$ASYNC_CALLBACK_URL" \
  --data-model-url "$DATA_MODEL_URL" \
  --ingestion-url "$INGESTION_URL" \
  --decision-engine-url "$DECISION_ENGINE_URL"
```

For every transaction, the harness calls ingestion and waits for success before submitting a `record-ingested` decision request. The payload always includes the selected `mode`, `wait_timeout_ms`, `callback_url`, and `source`; async mode defers immediately, while sync mode attempts inline evaluation and may return either an inline `200` response or a deferred `202` response. Independent transactions run concurrently. Events with the same source timestamp are launched together, and all streams are globally merged before scheduling.

The six configured streams are `genpay` inflow, `genpayv2` inflow, and `uniwallet`/`uniwalletv2` inflow and outflow. They all use the shared final CSV schema. Inflow maps to `incoming`, outflow maps to `outgoing`, and the retained source fields identify the concrete processor and payment source.

Every source row receives a deterministic object ID derived from its stream, file, row number, and source transaction identifier. Repeated source transaction identifiers are therefore versioned rather than overwritten, while rerunning the same source row reuses the same ingestion idempotency key.

Results are written below `stress-tests/production-replay-runs/`, which is ignored by Git. Each wrapper-created replay directory includes `seed-summary.json`, copied from the completed seed phase. The replay summary separates ingestion and decision errors, includes `sampled_error_breakdown` and full-run `error_breakdown`, and leaves acceptance thresholds unset until they are defined. Each run writes `errors.ndjson` so ingestion write failures can be separated from callback or decision failures after the run. Each error record includes the complete service response status and body when a response was received; its short `error` description remains bounded for readable summaries. The run also writes `successes.ndjson`, with one record for every successful ingestion or decision request. A success record includes request and response timestamps, latency, attempt count, method, path, safe request headers, request body, HTTP status, and the complete response body. Authorization headers are never included.

`successes.ndjson` can be large because a successful transaction normally produces both an ingestion entry and a decision entry, and the request body contains production-shaped fields. Protect and expire this file like the source data and allow sufficient disk space for long replays. Success records are buffered in small batches and flushed before checkpoints to reduce measurement overhead.

Use a fresh setup tenant for each independent measured run. Ingestion itself is idempotent for a repeated source event, but the direct decision callback is intentionally not retried or deduplicated by this harness.

### Resume

The harness drains all in-flight requests before writing each checkpoint. Resume validates the tenant, multiplier, manifest, and source-file fingerprint, rebuilds the temporary sorted stream, and continues after the last drained cursor:

```bash
PYTHONPATH=stress-tests python3 -m production_replay run \
  --manifest stress-tests/production_replay/manifests/fraud-data.json \
  --execute \
  --tenant-id '<setup-tenant-id>' \
  --multiplier 10 \
  --resume-from stress-tests/production-replay-runs/replay-<run-id>/checkpoint.json \
  --data-model-url "$DATA_MODEL_URL" \
  --ingestion-url "$INGESTION_URL" \
  --decision-engine-url "$DECISION_ENGINE_URL"
```

An interruption after the previous checkpoint can repeat the current checkpoint window. Ingestion remains idempotent; direct decision callbacks in that incomplete window can be repeated. The success log is appended on resume, so repeated requests in that window can also appear more than once. Reduce `--checkpoint-every` if that recovery window must be smaller.

## Adding Streams

Add one manifest entry per direction, channel, or processor. Streams using `production_transaction_csv_v1` can be combined immediately when they have the same headers and timestamp format. Different formats require a new adapter registered in `adapters/__init__.py`; they do not require scheduler or service changes.

The channel metadata controls which conditional card, bank, cash-out, cash-reporting, and electronic-transfer rules are installed. All configured streams replay on one merged timeline.

## Tests

```bash
PYTHONPATH=stress-tests python3 -m unittest discover \
  -s stress-tests/production_replay/tests \
  -t stress-tests
```
