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

You can replay a source-time window instead of choosing a transaction count:

```bash
make production-replay HOURS=6 MULTIPLIER=360x
make production-replay DAYS=2 MULTIPLIER=360x
make production-replay WEEKS=1 MULTIPLIER=360x
make production-replay DURATION=12h MULTIPLIER=360x
```

When `DURATION`, `HOURS`, `DAYS`, or `WEEKS` is set, the transaction count is ignored. The duration sample starts at the earliest source transaction timestamp and includes all configured streams up to that source-time cutoff.

`MULTIPLIER` also accepts a `*` suffix, for example `MULTIPLIER='360*'`. Quote it in your shell so it is not treated as a filename pattern.

Replay and backfill runs should default to async decision execution. Both modes submit each event to `POST /v1/tenants/{tenant_id}/ingestion-events/record-ingested`; the request payload sets `mode` to `async` or `sync`. The CLI and shell wrappers default to `--decision-mode async`; use `--decision-mode sync` or `DECISION_MODE=sync` only for explicit comparison runs. For EC2, use:

```bash
make production-replay-ec2-async TRANSACTIONS=1000 MULTIPLIER=360x
```

For replay and backfill incident posture, the local replay wrapper now also defaults the service-side decision posture to:

- `LIVE_DECISION_MODE=async_only`
- `LIVE_ASYNC_FALLBACK_ENABLED=true`
- `TENANT_DATA_READ_MODE=direct_db`

Override those only for explicit comparison runs:

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

This starts the required Docker services from existing images using `--no-build`, prepares its Python environment, prepares the requested local tenant, loads the final reference data from `/Users/kwilson/Desktop/ITC/fraud_data`, rebuilds and starts the frontend with the replay tenant ID, replays the configured number of production-format transactions across all six streams, and prints a compact ingestion and decision summary. The harness uses the base Compose file directly and relies on the system-level single `fraud` database configuration; it no longer applies replay-specific database overrides. The command leaves Docker, the frontend, and the local tenant running for inspection. If a required backend Docker image does not exist, it fails instead of building it.

## Safety Model

- `profile` is always read only.
- `setup` only calls services when `--execute` is present.
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
- `TENANT_DATA_READ_MODE=direct_db`
- shared write/read DB pool
- separate read pool enabled in ingestion-service

Each run writes:

- replay stdout/stderr logs
- the replay run directory reported by the wrapper
- `experiment-settings.json`
- optional metrics capture manifests when `--capture-metrics` is used

The matrix artifacts are written below:

```text
stress-tests/production-replay-matrix-runs/
```

For local compose-backed runs, separate read-pool comparison is implemented by setting:

- `READ_DATABASE_URL`
- `READ_DATABASE_MAX_CONNS`
- `READ_DATABASE_MIN_CONNS`

When `ENABLE_SEPARATE_READ_POOL=true` and `READ_DATABASE_URL` is unset, the wrapper reuses the same Postgres URL as the write pool but still creates a distinct read pool so contention can be capped independently.

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

## 3. Replay

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

Results are written below `stress-tests/production-replay-runs/`, which is ignored by Git. The summary separates ingestion and decision errors, includes `sampled_error_breakdown` and full-run `error_breakdown`, and leaves acceptance thresholds unset until they are defined. Each run writes `errors.ndjson` so ingestion write failures can be separated from callback or decision failures after the run. Each error record includes the complete service response status and body when a response was received; its short `error` description remains bounded for readable summaries. The run also writes `successes.ndjson`, with one record for every successful ingestion or decision request. A success record includes request and response timestamps, latency, attempt count, method, path, safe request headers, request body, HTTP status, and the complete response body. Authorization headers are never included.

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
