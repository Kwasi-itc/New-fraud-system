#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
ENV_FILE="${PRODUCTION_REPLAY_ENV_FILE:-}"
DATA_ROOT="${FRAUD_DATA_ROOT:-/Users/kwilson/Desktop/ITC/fraud_data}"
SEED_DATA_ROOT="${PRODUCTION_REPLAY_SEED_DATA_ROOT:-${FRAUD_DATA_SEED_ROOT:-${DATA_ROOT%/}_seed}}"
VENV_DIR="${PRODUCTION_REPLAY_VENV:-/tmp/fraud-production-replay-venv}"
TRANSACTIONS="${PRODUCTION_REPLAY_TRANSACTIONS:-${TRANSACTIONS:-1000}}"
TRANSACTION_OFFSET="${PRODUCTION_REPLAY_TRANSACTION_OFFSET:-${TRANSACTION_OFFSET:-0}}"
MULTIPLIER="${PRODUCTION_REPLAY_MULTIPLIER:-${MULTIPLIER:-360}}"
MAX_IN_FLIGHT="${PRODUCTION_REPLAY_MAX_IN_FLIGHT:-${MAX_IN_FLIGHT:-50}}"
CHECKPOINT_EVERY="${PRODUCTION_REPLAY_CHECKPOINT_EVERY:-${CHECKPOINT_EVERY:-100}}"
SEED_BATCH_SIZE="${PRODUCTION_REPLAY_SEED_BATCH_SIZE:-${SEED_BATCH_SIZE:-500}}"
SEED_MAX_IN_FLIGHT="${PRODUCTION_REPLAY_SEED_MAX_IN_FLIGHT:-${SEED_MAX_IN_FLIGHT:-10}}"
SEED_PROGRESS_EVERY="${PRODUCTION_REPLAY_SEED_PROGRESS_EVERY:-${SEED_PROGRESS_EVERY:-100}}"
SEED_REQUEST_TIMEOUT="${PRODUCTION_REPLAY_SEED_REQUEST_TIMEOUT:-${SEED_REQUEST_TIMEOUT:-300}}"
REUSE_EXISTING_SETUP="${PRODUCTION_REPLAY_REUSE_EXISTING_SETUP:-${REUSE_EXISTING_SETUP:-false}}"
REUSE_EXISTING_SEED="${PRODUCTION_REPLAY_REUSE_EXISTING_SEED:-${REUSE_EXISTING_SEED:-false}}"
DECISION_MODE="${PRODUCTION_REPLAY_DECISION_MODE:-${DECISION_MODE:-async}}"
ASYNC_WAIT_TIMEOUT_MS="${PRODUCTION_REPLAY_ASYNC_WAIT_TIMEOUT_MS:-${ASYNC_WAIT_TIMEOUT_MS:-0}}"
ASYNC_CALLBACK_URL="${PRODUCTION_REPLAY_ASYNC_CALLBACK_URL:-${ASYNC_CALLBACK_URL:-}}"
LIVE_DECISION_MODE="${PRODUCTION_REPLAY_LIVE_DECISION_MODE:-${LIVE_DECISION_MODE:-}}"
LIVE_ASYNC_FALLBACK_ENABLED="${PRODUCTION_REPLAY_LIVE_ASYNC_FALLBACK_ENABLED:-${LIVE_ASYNC_FALLBACK_ENABLED:-true}}"
LIVE_ASYNC_OBJECT_TYPES="${PRODUCTION_REPLAY_LIVE_ASYNC_OBJECT_TYPES:-${LIVE_ASYNC_OBJECT_TYPES:-}}"
TENANT_DATA_READ_MODE="${PRODUCTION_REPLAY_TENANT_DATA_READ_MODE:-${TENANT_DATA_READ_MODE:-direct_db}}"
ENABLE_SEPARATE_READ_POOL="${PRODUCTION_REPLAY_ENABLE_SEPARATE_READ_POOL:-${ENABLE_SEPARATE_READ_POOL:-false}}"
READ_DATABASE_MAX_CONNS="${PRODUCTION_REPLAY_READ_DATABASE_MAX_CONNS:-${READ_DATABASE_MAX_CONNS:-0}}"
READ_DATABASE_MIN_CONNS="${PRODUCTION_REPLAY_READ_DATABASE_MIN_CONNS:-${READ_DATABASE_MIN_CONNS:-0}}"
ALLOW_UNSAFE_INGESTION_HTTP_REPLAY="${PRODUCTION_REPLAY_ALLOW_UNSAFE_INGESTION_HTTP_REPLAY:-${ALLOW_UNSAFE_INGESTION_HTTP_REPLAY:-false}}"
DURATION="${PRODUCTION_REPLAY_DURATION:-${DURATION:-}}"
HOURS="${PRODUCTION_REPLAY_HOURS:-${HOURS:-}}"
DAYS="${PRODUCTION_REPLAY_DAYS:-${DAYS:-}}"
WEEKS="${PRODUCTION_REPLAY_WEEKS:-${WEEKS:-}}"
BASE_URL="${PRODUCTION_REPLAY_BASE_URL:-${BASE_URL:-http://ec2-54-246-247-31.eu-west-1.compute.amazonaws.com}}"
TENANT_ID="${PRODUCTION_REPLAY_TENANT_ID:-${TENANT_ID:-}}"
TENANT_NAME="${PRODUCTION_REPLAY_TENANT_NAME:-${TENANT_NAME:-EC2 Production Replay Smoke Test}}"
PUBLICATION_TIMEOUT="${PRODUCTION_REPLAY_PUBLICATION_TIMEOUT:-${PUBLICATION_TIMEOUT:-900}}"
AUTH_TOKEN="${SERVICE_AUTH_TOKEN:-}"
EXPERIMENT_LABEL="${PRODUCTION_REPLAY_EXPERIMENT_LABEL:-${EXPERIMENT_LABEL:-}}"

if [[ -z "$LIVE_DECISION_MODE" ]]; then
  if [[ "$DECISION_MODE" == "async" ]]; then
    LIVE_DECISION_MODE="async_only"
  else
    LIVE_DECISION_MODE="sync"
  fi
fi

export DECISION_MODE="$DECISION_MODE"
export TRANSACTION_OFFSET="$TRANSACTION_OFFSET"
export LIVE_DECISION_MODE="$LIVE_DECISION_MODE"
export LIVE_ASYNC_FALLBACK_ENABLED="$LIVE_ASYNC_FALLBACK_ENABLED"
export LIVE_ASYNC_OBJECT_TYPES="$LIVE_ASYNC_OBJECT_TYPES"
export TENANT_DATA_READ_MODE="$TENANT_DATA_READ_MODE"
export ENABLE_SEPARATE_READ_POOL="$ENABLE_SEPARATE_READ_POOL"
export READ_DATABASE_MAX_CONNS="$READ_DATABASE_MAX_CONNS"
export READ_DATABASE_MIN_CONNS="$READ_DATABASE_MIN_CONNS"
export EXPERIMENT_LABEL="$EXPERIMENT_LABEL"

BASE_URL="${BASE_URL%/}"
DATA_MODEL_URL="${PRODUCTION_REPLAY_DATA_MODEL_URL:-${DATA_MODEL_URL:-}}"
INGESTION_URL="${PRODUCTION_REPLAY_INGESTION_URL:-${INGESTION_URL:-}}"
DECISION_ENGINE_URL="${PRODUCTION_REPLAY_DECISION_ENGINE_URL:-${DECISION_ENGINE_URL:-}}"
DATA_MODEL_URL="${DATA_MODEL_URL:-$BASE_URL:8080}"
INGESTION_URL="${INGESTION_URL:-$BASE_URL:8081}"
DECISION_ENGINE_URL="${DECISION_ENGINE_URL:-$BASE_URL:8082}"

SMOKE_MANIFEST="/tmp/fraud-data-remote-smoke.json"
SEED_MANIFEST="/tmp/fraud-data-remote-seed.json"
SAMPLE_DIR="/tmp/fraud-data-remote-sample"
SEED_SAMPLE_DIR="/tmp/fraud-data-remote-seed-sample"
SETUP_LOG="/tmp/fraud-data-remote-setup.log"
SEED_LOG="/tmp/fraud-data-remote-seed.log"
REPLAY_LOG="/tmp/fraud-data-remote-replay.log"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'error: required command not found: %s\n' "$1" >&2
    exit 1
  fi
}

normalize_multiplier() {
  local value="$1"
  value="${value%x}"
  value="${value%X}"
  value="${value%\*}"
  printf '%s' "$value"
}

duration_selector() {
  local selected=0
  [[ -n "$DURATION" ]] && selected=$((selected + 1))
  [[ -n "$HOURS" ]] && selected=$((selected + 1))
  [[ -n "$DAYS" ]] && selected=$((selected + 1))
  [[ -n "$WEEKS" ]] && selected=$((selected + 1))
  if [[ "$selected" -gt 1 ]]; then
    printf 'error: define only one of DURATION, HOURS, DAYS, or WEEKS\n' >&2
    exit 1
  fi
  if [[ -n "$DURATION" ]]; then
    printf '%s' "$DURATION"
  elif [[ -n "$HOURS" ]]; then
    printf '%sh' "$HOURS"
  elif [[ -n "$DAYS" ]]; then
    printf '%sd' "$DAYS"
  elif [[ -n "$WEEKS" ]]; then
    printf '%sw' "$WEEKS"
  fi
}

wait_for_service() {
  local name="$1"
  local url="$2"
  local attempt
  for ((attempt = 1; attempt <= 60; attempt++)); do
    if curl --fail --silent --show-error "$url" >/dev/null 2>&1; then
      printf '%s is ready\n' "$name"
      return
    fi
    sleep 1
  done
  printf 'error: %s did not become ready at %s\n' "$name" "$url" >&2
  exit 1
}

require_command curl
require_command python3

if [[ ! -d "$DATA_ROOT" ]]; then
  printf 'error: fraud data directory does not exist: %s\n' "$DATA_ROOT" >&2
  exit 1
fi
if [[ ! -d "$SEED_DATA_ROOT" ]]; then
  printf 'error: fraud seed data directory does not exist: %s\n' "$SEED_DATA_ROOT" >&2
  exit 1
fi

MULTIPLIER="$(normalize_multiplier "$MULTIPLIER")"
REPLAY_DURATION="$(duration_selector)"
if [[ "$TRANSACTIONS" != "all" && ! "$TRANSACTIONS" =~ ^[0-9]+$ ]]; then
  printf 'error: TRANSACTIONS must be a positive integer or all; got %s\n' "$TRANSACTIONS" >&2
  exit 1
fi
if [[ "$TRANSACTIONS" != "all" && "$TRANSACTIONS" -le 0 ]]; then
  printf 'error: TRANSACTIONS must be positive; got %s\n' "$TRANSACTIONS" >&2
  exit 1
fi
if [[ ! "$TRANSACTION_OFFSET" =~ ^[0-9]+$ ]]; then
  printf 'error: TRANSACTION_OFFSET must be zero or a positive integer; got %s\n' "$TRANSACTION_OFFSET" >&2
  exit 1
fi
if [[ "$TRANSACTION_OFFSET" -gt 0 && "$TRANSACTIONS" == "all" ]]; then
  printf 'error: TRANSACTION_OFFSET requires a numeric TRANSACTIONS value\n' >&2
  exit 1
fi
if [[ "$TRANSACTION_OFFSET" -gt 0 && -n "$REPLAY_DURATION" ]]; then
  printf 'error: TRANSACTION_OFFSET cannot be combined with DURATION, HOURS, DAYS, or WEEKS\n' >&2
  exit 1
fi
if [[ ! "$MULTIPLIER" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
  printf 'error: MULTIPLIER must be positive, with optional x or * suffix; got %s\n' "$MULTIPLIER" >&2
  exit 1
fi
if [[ ! "$MAX_IN_FLIGHT" =~ ^[0-9]+$ || "$MAX_IN_FLIGHT" -le 0 ]]; then
  printf 'error: MAX_IN_FLIGHT must be a positive integer; got %s\n' "$MAX_IN_FLIGHT" >&2
  exit 1
fi
if [[ ! "$CHECKPOINT_EVERY" =~ ^[0-9]+$ || "$CHECKPOINT_EVERY" -le 0 ]]; then
  printf 'error: CHECKPOINT_EVERY must be a positive integer; got %s\n' "$CHECKPOINT_EVERY" >&2
  exit 1
fi
if [[ ! "$SEED_BATCH_SIZE" =~ ^[0-9]+$ || "$SEED_BATCH_SIZE" -le 0 || "$SEED_BATCH_SIZE" -gt 500 ]]; then
  printf 'error: SEED_BATCH_SIZE must be between 1 and 500; got %s\n' "$SEED_BATCH_SIZE" >&2
  exit 1
fi
if [[ ! "$SEED_MAX_IN_FLIGHT" =~ ^[0-9]+$ || "$SEED_MAX_IN_FLIGHT" -le 0 ]]; then
  printf 'error: SEED_MAX_IN_FLIGHT must be a positive integer; got %s\n' "$SEED_MAX_IN_FLIGHT" >&2
  exit 1
fi
if [[ ! "$SEED_PROGRESS_EVERY" =~ ^[0-9]+$ ]]; then
  printf 'error: SEED_PROGRESS_EVERY must be zero or a positive integer; got %s\n' "$SEED_PROGRESS_EVERY" >&2
  exit 1
fi
if [[ ! "$SEED_REQUEST_TIMEOUT" =~ ^[0-9]+([.][0-9]+)?$ || "$SEED_REQUEST_TIMEOUT" =~ ^0+([.]0+)?$ ]]; then
  printf 'error: SEED_REQUEST_TIMEOUT must be a positive number; got %s\n' "$SEED_REQUEST_TIMEOUT" >&2
  exit 1
fi
if [[ "$REUSE_EXISTING_SETUP" != "true" && "$REUSE_EXISTING_SETUP" != "false" ]]; then
  printf 'error: REUSE_EXISTING_SETUP must be true or false; got %s\n' "$REUSE_EXISTING_SETUP" >&2
  exit 1
fi
if [[ "$REUSE_EXISTING_SEED" != "true" && "$REUSE_EXISTING_SEED" != "false" ]]; then
  printf 'error: REUSE_EXISTING_SEED must be true or false; got %s\n' "$REUSE_EXISTING_SEED" >&2
  exit 1
fi
if [[ "$REUSE_EXISTING_SEED" == "true" && "$REUSE_EXISTING_SETUP" != "true" ]]; then
  printf 'error: REUSE_EXISTING_SEED=true requires REUSE_EXISTING_SETUP=true to avoid mutating the prepared tenant\n' >&2
  exit 1
fi
if [[ "$DECISION_MODE" != "sync" && "$DECISION_MODE" != "async" ]]; then
  printf 'error: DECISION_MODE must be sync or async; got %s\n' "$DECISION_MODE" >&2
  exit 1
fi
if [[ ! "$ASYNC_WAIT_TIMEOUT_MS" =~ ^[0-9]+$ ]]; then
  printf 'error: ASYNC_WAIT_TIMEOUT_MS must be zero or a positive integer; got %s\n' "$ASYNC_WAIT_TIMEOUT_MS" >&2
  exit 1
fi
if [[ "$LIVE_DECISION_MODE" != "sync" && "$LIVE_DECISION_MODE" != "async_only" ]]; then
  printf 'error: LIVE_DECISION_MODE must be sync or async_only; got %s\n' "$LIVE_DECISION_MODE" >&2
  exit 1
fi
if [[ "$DECISION_MODE" == "sync" && "$LIVE_DECISION_MODE" == "async_only" ]]; then
  printf 'error: refusing a mislabeled sync replay because LIVE_DECISION_MODE=async_only would queue the decisions.\n' >&2
  printf 'configure the remote decision service with LIVE_DECISION_MODE=sync for a real synchronous run.\n' >&2
  exit 1
fi
NORMALIZED_LIVE_ASYNC_OBJECT_TYPES=",${LIVE_ASYNC_OBJECT_TYPES,,},"
NORMALIZED_LIVE_ASYNC_OBJECT_TYPES="${NORMALIZED_LIVE_ASYNC_OBJECT_TYPES//[[:space:]]/}"
if [[ "$DECISION_MODE" == "sync" && "$NORMALIZED_LIVE_ASYNC_OBJECT_TYPES" == *",transactions,"* ]]; then
  printf 'error: refusing a mislabeled sync replay because LIVE_ASYNC_OBJECT_TYPES includes transactions.\n' >&2
  printf 'remove transactions from LIVE_ASYNC_OBJECT_TYPES in the remote decision service configuration.\n' >&2
  exit 1
fi
if [[ "$LIVE_ASYNC_FALLBACK_ENABLED" != "true" && "$LIVE_ASYNC_FALLBACK_ENABLED" != "false" ]]; then
  printf 'error: LIVE_ASYNC_FALLBACK_ENABLED must be true or false; got %s\n' "$LIVE_ASYNC_FALLBACK_ENABLED" >&2
  exit 1
fi
if [[ "$TENANT_DATA_READ_MODE" != "ingestion_http" && "$TENANT_DATA_READ_MODE" != "direct_db" ]]; then
  printf 'error: TENANT_DATA_READ_MODE must be ingestion_http or direct_db; got %s\n' "$TENANT_DATA_READ_MODE" >&2
  exit 1
fi
if [[ "$ALLOW_UNSAFE_INGESTION_HTTP_REPLAY" != "true" && "$ALLOW_UNSAFE_INGESTION_HTTP_REPLAY" != "false" ]]; then
  printf 'error: ALLOW_UNSAFE_INGESTION_HTTP_REPLAY must be true or false; got %s\n' "$ALLOW_UNSAFE_INGESTION_HTTP_REPLAY" >&2
  exit 1
fi
if [[ "$TENANT_DATA_READ_MODE" == "ingestion_http" && "$ENABLE_SEPARATE_READ_POOL" != "true" ]]; then
  if python3 - "$MULTIPLIER" "$MAX_IN_FLIGHT" <<'PY'
import sys
multiplier = float(sys.argv[1])
max_in_flight = int(sys.argv[2])
sys.exit(0 if multiplier >= 50 or max_in_flight >= 25 else 1)
PY
  then
    if [[ "$ALLOW_UNSAFE_INGESTION_HTTP_REPLAY" != "true" ]]; then
      printf 'error: refusing high-pressure replay with TENANT_DATA_READ_MODE=ingestion_http and no separate read pool.\n' >&2
      printf 'set ENABLE_SEPARATE_READ_POOL=true, switch to TENANT_DATA_READ_MODE=direct_db, or set ALLOW_UNSAFE_INGESTION_HTTP_REPLAY=true for an explicit comparison run.\n' >&2
      exit 1
    fi
    printf 'warning: running an explicitly unsafe replay with TENANT_DATA_READ_MODE=ingestion_http and no separate read pool.\n' >&2
  fi
fi

printf 'Remote replay endpoints:\n'
printf '  data-model:      %s\n' "$DATA_MODEL_URL"
printf '  ingestion:       %s\n' "$INGESTION_URL"
printf '  decision-engine: %s\n' "$DECISION_ENGINE_URL"
if [[ -n "$REPLAY_DURATION" ]]; then
  printf 'Replay configuration: duration=%s multiplier=%sx max_in_flight=%s decision_mode=%s live_decision_mode=%s read_mode=%s separate_read_pool=%s async_fallback=%s\n' \
    "$REPLAY_DURATION" "$MULTIPLIER" "$MAX_IN_FLIGHT" "$DECISION_MODE" "$LIVE_DECISION_MODE" "$TENANT_DATA_READ_MODE" "$ENABLE_SEPARATE_READ_POOL" "$LIVE_ASYNC_FALLBACK_ENABLED"
else
  printf 'Replay configuration: transactions=%s offset=%s multiplier=%sx max_in_flight=%s decision_mode=%s live_decision_mode=%s read_mode=%s separate_read_pool=%s async_fallback=%s\n' \
    "$TRANSACTIONS" "$TRANSACTION_OFFSET" "$MULTIPLIER" "$MAX_IN_FLIGHT" "$DECISION_MODE" "$LIVE_DECISION_MODE" "$TENANT_DATA_READ_MODE" "$ENABLE_SEPARATE_READ_POOL" "$LIVE_ASYNC_FALLBACK_ENABLED"
fi
printf 'Seed configuration: data_root=%s batch_size=%s max_in_flight=%s request_timeout=%ss reuse_existing_setup=%s reuse_existing_seed=%s\n' \
  "$SEED_DATA_ROOT" "$SEED_BATCH_SIZE" "$SEED_MAX_IN_FLIGHT" "$SEED_REQUEST_TIMEOUT" "$REUSE_EXISTING_SETUP" "$REUSE_EXISTING_SEED"

wait_for_service "data-model-service" "$DATA_MODEL_URL/readyz"
wait_for_service "ingestion-service" "$INGESTION_URL/readyz"
wait_for_service "decision-engine-service" "$DECISION_ENGINE_URL/readyz"

if [[ ! -x "$VENV_DIR/bin/python" ]]; then
  printf 'Creating replay Python environment...\n'
  python3 -m venv --system-site-packages "$VENV_DIR"
fi

if ! "$VENV_DIR/bin/python" -c 'import httpx, openpyxl' >/dev/null 2>&1; then
  "$VENV_DIR/bin/python" -m pip install -r "$SCRIPT_DIR/requirements.txt"
fi

(
  cd "$BACKEND_DIR"
  SAMPLE_ARGS=(
    --base-manifest "$SCRIPT_DIR/manifests/fraud-data.json"
    --data-root "$DATA_ROOT"
    --output-dir "$SAMPLE_DIR"
    --output-manifest "$SMOKE_MANIFEST"
  )
  if [[ -n "$REPLAY_DURATION" ]]; then
    SAMPLE_ARGS+=(--duration "$REPLAY_DURATION")
  else
    SAMPLE_ARGS+=(--transactions "$TRANSACTIONS" --offset "$TRANSACTION_OFFSET")
  fi
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay.local_sample "${SAMPLE_ARGS[@]}"

  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay.local_sample \
    --base-manifest "$SCRIPT_DIR/manifests/fraud-data.json" \
    --data-root "$SEED_DATA_ROOT" \
    --reference-data-root "$DATA_ROOT" \
    --output-dir "$SEED_SAMPLE_DIR" \
    --output-manifest "$SEED_MANIFEST" \
    --transactions all
)

if [[ "$REUSE_EXISTING_SETUP" == "true" ]]; then
  printf 'Verifying the existing remote replay tenant without changing its setup...\n'
else
  printf 'Creating a remote replay tenant and loading reference data...\n'
fi
(
  cd "$BACKEND_DIR"
  SETUP_ARGS=(
    --manifest "$SMOKE_MANIFEST"
    --execute
    --tenant-name "$TENANT_NAME"
    --publication-timeout "$PUBLICATION_TIMEOUT"
    --data-model-url "$DATA_MODEL_URL"
    --ingestion-url "$INGESTION_URL"
    --decision-engine-url "$DECISION_ENGINE_URL"
  )
  if [[ -n "$AUTH_TOKEN" ]]; then
    SETUP_ARGS+=(--auth-token "$AUTH_TOKEN")
  fi
  if [[ -n "$TENANT_ID" ]]; then
    SETUP_ARGS+=(--tenant-id "$TENANT_ID")
  fi
  if [[ "$REUSE_EXISTING_SETUP" == "true" ]]; then
    if [[ -z "$TENANT_ID" ]]; then
      printf 'error: TENANT_ID is required when REUSE_EXISTING_SETUP=true\n' >&2
      exit 1
    fi
    SETUP_ARGS+=(--reuse-existing)
  fi
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay setup "${SETUP_ARGS[@]}"
) | tee "$SETUP_LOG"

TENANT_ID="$(awk '/^tenant:/ {print $2}' "$SETUP_LOG" | tail -n 1)"
if [[ -z "$TENANT_ID" ]]; then
  printf 'error: setup completed without returning a tenant ID\n' >&2
  exit 1
fi

if [[ "$REUSE_EXISTING_SEED" == "true" ]]; then
  printf 'Reusing the existing seed in tenant %s without performing seed writes...\n' "$TENANT_ID"
else
  printf 'Pre-seeding tenant %s with every transaction from %s (ingestion only, no decisions)...\n' "$TENANT_ID" "$SEED_DATA_ROOT"
fi
(
  cd "$BACKEND_DIR"
  SEED_ARGS=(
    --manifest "$SEED_MANIFEST"
    --tenant-id "$TENANT_ID"
    --timeout "$SEED_REQUEST_TIMEOUT"
    --data-model-url "$DATA_MODEL_URL"
    --ingestion-url "$INGESTION_URL"
    --decision-engine-url "$DECISION_ENGINE_URL"
  )
  if [[ "$REUSE_EXISTING_SEED" == "true" ]]; then
    SEED_ARGS+=(--reuse-existing)
  else
    SEED_ARGS+=(
      --execute
      --batch-size "$SEED_BATCH_SIZE"
      --max-in-flight "$SEED_MAX_IN_FLIGHT"
      --progress-every "$SEED_PROGRESS_EVERY"
    )
  fi
  if [[ -n "$AUTH_TOKEN" ]]; then
    SEED_ARGS+=(--auth-token "$AUTH_TOKEN")
  fi
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay seed "${SEED_ARGS[@]}"
) | tee "$SEED_LOG"

SEED_RUN_DIR="$(awk -F': ' '/^seed output:/ {print $2}' "$SEED_LOG" | tail -n 1)"
if [[ -z "$SEED_RUN_DIR" || ! -f "$SEED_RUN_DIR/summary.json" ]]; then
  printf 'error: transaction seed completed without a summary file\n' >&2
  exit 1
fi

if [[ -n "$REPLAY_DURATION" ]]; then
  printf 'Replaying production-format transactions from the first %s of source time...\n' "$REPLAY_DURATION"
elif [[ "$TRANSACTIONS" == "all" ]]; then
  printf 'Replaying all production-format transactions...\n'
else
  if [[ "$TRANSACTION_OFFSET" -gt 0 ]]; then
    printf 'Replaying the next %s production-format transactions after the first %s...\n' "$TRANSACTIONS" "$TRANSACTION_OFFSET"
  else
    printf 'Replaying %s production-format transactions...\n' "$TRANSACTIONS"
  fi
fi

set +e
(
  cd "$BACKEND_DIR"
  RUN_ARGS=(
    --manifest "$SMOKE_MANIFEST"
    --execute
    --tenant-id "$TENANT_ID"
    --multiplier "$MULTIPLIER"
    --max-in-flight "$MAX_IN_FLIGHT"
    --checkpoint-every "$CHECKPOINT_EVERY"
    --decision-mode "$DECISION_MODE"
    --async-wait-timeout-ms "$ASYNC_WAIT_TIMEOUT_MS"
    --async-callback-url "$ASYNC_CALLBACK_URL"
    --data-model-url "$DATA_MODEL_URL"
    --ingestion-url "$INGESTION_URL"
    --decision-engine-url "$DECISION_ENGINE_URL"
  )
  if [[ -n "$AUTH_TOKEN" ]]; then
    RUN_ARGS+=(--auth-token "$AUTH_TOKEN")
  fi
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay run "${RUN_ARGS[@]}"
) | tee "$REPLAY_LOG"
REPLAY_STATUS="${PIPESTATUS[0]}"
set -e

if [[ "$REPLAY_STATUS" -ne 0 && "$REPLAY_STATUS" -ne 2 ]]; then
  printf 'error: replay command failed with status %s\n' "$REPLAY_STATUS" >&2
  exit "$REPLAY_STATUS"
fi

RUN_DIR="$(awk -F': ' '/^replay output:/ {print $2}' "$REPLAY_LOG" | tail -n 1)"
if [[ -z "$RUN_DIR" || ! -f "$RUN_DIR/summary.json" ]]; then
  printf 'error: replay completed without a summary file\n' >&2
  exit 1
fi
cp "$SEED_RUN_DIR/summary.json" "$RUN_DIR/seed-summary.json"

"$VENV_DIR/bin/python" - "$RUN_DIR" <<'PY'
import json
import os
import sys
from pathlib import Path

run_dir = Path(sys.argv[1])
metadata = {
    "experiment_label": os.getenv("EXPERIMENT_LABEL") or None,
    "configuration": {
        "env_file": os.getenv("PRODUCTION_REPLAY_ENV_FILE") or None,
        "precedence": "make_command_line > env_file > process_environment > built_in_default",
        "transaction_offset": int(os.getenv("TRANSACTION_OFFSET", "0")),
    },
    "service_modes": {
        "request_decision_mode": os.getenv("DECISION_MODE"),
        "live_decision_mode": os.getenv("LIVE_DECISION_MODE"),
        "live_async_fallback_enabled": os.getenv("LIVE_ASYNC_FALLBACK_ENABLED"),
        "live_async_object_types": os.getenv("LIVE_ASYNC_OBJECT_TYPES") or None,
        "tenant_data_read_mode": os.getenv("TENANT_DATA_READ_MODE"),
    },
    "ingestion_read_pool": {
        "enabled": os.getenv("ENABLE_SEPARATE_READ_POOL") == "true",
        "read_database_max_conns": os.getenv("READ_DATABASE_MAX_CONNS"),
        "read_database_min_conns": os.getenv("READ_DATABASE_MIN_CONNS"),
    },
    "note": "Remote replay wrapper records requested service-mode assumptions only. It does not reconfigure remote services.",
}
(run_dir / "experiment-settings.json").write_text(json.dumps(metadata, indent=2) + "\n", encoding="utf-8")
PY

printf '\nRemote replay result:\n'
"$VENV_DIR/bin/python" - "$RUN_DIR/summary.json" "$RUN_DIR/seed-summary.json" <<'PY'
import json
import sys
from pathlib import Path

summary = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
seed = json.loads(Path(sys.argv[2]).read_text(encoding="utf-8"))
result = {
    "status": summary["status"],
    "scheduled": summary["scheduled"],
    "completed": summary["completed"],
    "ingestion": {
        "successes": summary["ingestion"]["successes"],
        "failures": summary["ingestion"]["failures"],
    },
    "decision": {
        "successes": summary["decision"]["successes"],
        "failures": summary["decision"]["failures"],
    },
    "seed": {
        "status": seed["status"],
        "records": seed["records"],
        "batches": seed["batches"],
        "decision_requests": seed["decision_requests"],
    },
}
print(json.dumps(result, indent=2))
PY

printf '\nTenant: %s\n' "$TENANT_ID"
printf 'Seed results: %s\n' "$SEED_RUN_DIR"
printf 'Results: %s\n' "$RUN_DIR"
exit "$REPLAY_STATUS"
