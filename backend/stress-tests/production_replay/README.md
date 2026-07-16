# Production Replay Harness

This standalone Python harness profiles internal reference and transaction dumps, prepares one isolated tenant, and replays all configured transaction streams against the existing ingestion and decision-engine APIs. It does not require changes to any service.

## One-Command Local Test

From `New-fraud-system`, run:

```bash
./backend/stress-tests/production_replay/run_local_replay.sh
```

This starts the required Docker services from existing images using `--no-build`, prepares its Python environment, creates a local tenant, loads the final reference data from `/Users/kwilson/Desktop/ITC/fraud_data`, replays exactly 1,000 production-format transactions across all six streams, and prints a compact ingestion and decision summary. A harness-local Compose override points ingestion's migration, API, and worker at the same tenant-data database as data-model; it does not modify the base Compose file. The command leaves Docker and the local tenant running for inspection. If a required Docker image does not exist, it fails instead of building it.

## Safety Model

- `profile` is always read only.
- `setup` only calls services when `--execute` is present.
- `run` only sends events when `--execute`, `--tenant-id`, and a positive `--multiplier` are all present.
- Raw source rows are not copied into run artifacts or emitted in error logs.
- Ingestion retries reuse one deterministic idempotency key. Decision callbacks are not retried.
- Technical errors are observed and summarized; they do not stop the remaining replay.

The `fraud-data.json` manifest covers the final June 2026 extract. It discovers the files that actually exist rather than trusting directory names for coverage.

## Requirements

- Python 3.11 or newer
- `httpx`
- `openpyxl`
- data-model, ingestion, decision-engine, and data-model index worker available in the isolated environment
- one correctly shared tenant-schema data plane for data-model and ingestion

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
  --data-model-url "$DATA_MODEL_URL" \
  --ingestion-url "$INGESTION_URL" \
  --decision-engine-url "$DECISION_ENGINE_URL"
```

For every transaction, the harness calls ingestion and waits for success before calling the decision callback. Independent transactions run concurrently. Events with the same source timestamp are launched together, and all streams are globally merged before scheduling.

The six configured streams are `genpay` inflow, `genpayv2` inflow, and `uniwallet`/`uniwalletv2` inflow and outflow. They all use the shared final CSV schema. Inflow maps to `incoming`, outflow maps to `outgoing`, and the retained source fields identify the concrete processor and payment source.

Every source row receives a deterministic object ID derived from its stream, file, row number, and source transaction identifier. Repeated source transaction identifiers are therefore versioned rather than overwritten, while rerunning the same source row reuses the same ingestion idempotency key.

Results are written below `stress-tests/production-replay-runs/`, which is ignored by Git. The summary separates ingestion and decision errors and leaves acceptance thresholds unset until they are defined.

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

An interruption after the previous checkpoint can repeat the current checkpoint window. Ingestion remains idempotent; direct decision callbacks in that incomplete window can be repeated. Reduce `--checkpoint-every` if that recovery window must be smaller.

## Adding Streams

Add one manifest entry per direction, channel, or processor. Streams using `production_transaction_csv_v1` can be combined immediately when they have the same headers and timestamp format. Different formats require a new adapter registered in `adapters/__init__.py`; they do not require scheduler or service changes.

The channel metadata controls which conditional card, bank, cash-out, cash-reporting, and electronic-transfer rules are installed. All configured streams replay on one merged timeline.

## Tests

```bash
PYTHONPATH=stress-tests python3 -m unittest discover \
  -s stress-tests/production_replay/tests \
  -t stress-tests
```
