#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
DATA_ROOT="${FRAUD_DATA_ROOT:-/Users/kwilson/Desktop/ITC/fraud_data}"
VENV_DIR="${PRODUCTION_REPLAY_VENV:-/tmp/fraud-production-replay-venv}"
TRANSACTIONS="${PRODUCTION_REPLAY_TRANSACTIONS:-${TRANSACTIONS:-1000}}"
MULTIPLIER="${PRODUCTION_REPLAY_MULTIPLIER:-${MULTIPLIER:-360}}"
MAX_IN_FLIGHT="${PRODUCTION_REPLAY_MAX_IN_FLIGHT:-${MAX_IN_FLIGHT:-50}}"
CHECKPOINT_EVERY="${PRODUCTION_REPLAY_CHECKPOINT_EVERY:-${CHECKPOINT_EVERY:-100}}"
DECISION_MODE="${PRODUCTION_REPLAY_DECISION_MODE:-${DECISION_MODE:-sync}}"
ASYNC_WAIT_TIMEOUT_MS="${PRODUCTION_REPLAY_ASYNC_WAIT_TIMEOUT_MS:-${ASYNC_WAIT_TIMEOUT_MS:-0}}"
ASYNC_CALLBACK_URL="${PRODUCTION_REPLAY_ASYNC_CALLBACK_URL:-${ASYNC_CALLBACK_URL:-}}"
DURATION="${PRODUCTION_REPLAY_DURATION:-${DURATION:-}}"
HOURS="${PRODUCTION_REPLAY_HOURS:-${HOURS:-}}"
DAYS="${PRODUCTION_REPLAY_DAYS:-${DAYS:-}}"
WEEKS="${PRODUCTION_REPLAY_WEEKS:-${WEEKS:-}}"
BASE_URL="${PRODUCTION_REPLAY_BASE_URL:-${BASE_URL:-http://ec2-54-246-247-31.eu-west-1.compute.amazonaws.com}}"
TENANT_ID="${PRODUCTION_REPLAY_TENANT_ID:-${TENANT_ID:-}}"
TENANT_NAME="${PRODUCTION_REPLAY_TENANT_NAME:-${TENANT_NAME:-EC2 Production Replay Smoke Test}}"
PUBLICATION_TIMEOUT="${PRODUCTION_REPLAY_PUBLICATION_TIMEOUT:-${PUBLICATION_TIMEOUT:-900}}"
AUTH_TOKEN="${SERVICE_AUTH_TOKEN:-}"

BASE_URL="${BASE_URL%/}"
DATA_MODEL_URL="${PRODUCTION_REPLAY_DATA_MODEL_URL:-${DATA_MODEL_URL:-}}"
INGESTION_URL="${PRODUCTION_REPLAY_INGESTION_URL:-${INGESTION_URL:-}}"
DECISION_ENGINE_URL="${PRODUCTION_REPLAY_DECISION_ENGINE_URL:-${DECISION_ENGINE_URL:-}}"
DATA_MODEL_URL="${DATA_MODEL_URL:-$BASE_URL:8080}"
INGESTION_URL="${INGESTION_URL:-$BASE_URL:8081}"
DECISION_ENGINE_URL="${DECISION_ENGINE_URL:-$BASE_URL:8082}"

SMOKE_MANIFEST="/tmp/fraud-data-remote-smoke.json"
SAMPLE_DIR="/tmp/fraud-data-remote-sample"
SETUP_LOG="/tmp/fraud-data-remote-setup.log"
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
if [[ "$DECISION_MODE" != "sync" && "$DECISION_MODE" != "async" ]]; then
  printf 'error: DECISION_MODE must be sync or async; got %s\n' "$DECISION_MODE" >&2
  exit 1
fi
if [[ ! "$ASYNC_WAIT_TIMEOUT_MS" =~ ^[0-9]+$ ]]; then
  printf 'error: ASYNC_WAIT_TIMEOUT_MS must be zero or a positive integer; got %s\n' "$ASYNC_WAIT_TIMEOUT_MS" >&2
  exit 1
fi

printf 'Remote replay endpoints:\n'
printf '  data-model:      %s\n' "$DATA_MODEL_URL"
printf '  ingestion:       %s\n' "$INGESTION_URL"
printf '  decision-engine: %s\n' "$DECISION_ENGINE_URL"
if [[ -n "$REPLAY_DURATION" ]]; then
  printf 'Replay configuration: duration=%s multiplier=%sx max_in_flight=%s decision_mode=%s\n' \
    "$REPLAY_DURATION" "$MULTIPLIER" "$MAX_IN_FLIGHT" "$DECISION_MODE"
else
  printf 'Replay configuration: transactions=%s multiplier=%sx max_in_flight=%s decision_mode=%s\n' \
    "$TRANSACTIONS" "$MULTIPLIER" "$MAX_IN_FLIGHT" "$DECISION_MODE"
fi

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
    SAMPLE_ARGS+=(--transactions "$TRANSACTIONS")
  fi
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay.local_sample "${SAMPLE_ARGS[@]}"
)

printf 'Creating a remote replay tenant and loading reference data...\n'
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
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay setup "${SETUP_ARGS[@]}"
) | tee "$SETUP_LOG"

TENANT_ID="$(awk '/^tenant:/ {print $2}' "$SETUP_LOG" | tail -n 1)"
if [[ -z "$TENANT_ID" ]]; then
  printf 'error: setup completed without returning a tenant ID\n' >&2
  exit 1
fi

if [[ -n "$REPLAY_DURATION" ]]; then
  printf 'Replaying production-format transactions from the first %s of source time...\n' "$REPLAY_DURATION"
elif [[ "$TRANSACTIONS" == "all" ]]; then
  printf 'Replaying all production-format transactions...\n'
else
  printf 'Replaying %s production-format transactions...\n' "$TRANSACTIONS"
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

printf '\nRemote replay result:\n'
"$VENV_DIR/bin/python" - "$RUN_DIR/summary.json" <<'PY'
import json
import sys
from pathlib import Path

summary = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
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
}
print(json.dumps(result, indent=2))
PY

printf '\nTenant: %s\n' "$TENANT_ID"
printf 'Results: %s\n' "$RUN_DIR"
exit "$REPLAY_STATUS"
