#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKSPACE_DIR="$(cd "$BACKEND_DIR/.." && pwd)"
DATA_ROOT="${FRAUD_DATA_ROOT:-/Users/kwilson/Desktop/ITC/fraud_data}"
VENV_DIR="${PRODUCTION_REPLAY_VENV:-/tmp/fraud-production-replay-venv}"
TRANSACTIONS="${PRODUCTION_REPLAY_TRANSACTIONS:-${TRANSACTIONS:-1000}}"
MULTIPLIER="${PRODUCTION_REPLAY_MULTIPLIER:-${MULTIPLIER:-3600}}"
MAX_IN_FLIGHT="${PRODUCTION_REPLAY_MAX_IN_FLIGHT:-${MAX_IN_FLIGHT:-50}}"
CHECKPOINT_EVERY="${PRODUCTION_REPLAY_CHECKPOINT_EVERY:-${CHECKPOINT_EVERY:-100}}"
DECISION_MODE="${PRODUCTION_REPLAY_DECISION_MODE:-${DECISION_MODE:-sync}}"
ASYNC_WAIT_TIMEOUT_MS="${PRODUCTION_REPLAY_ASYNC_WAIT_TIMEOUT_MS:-${ASYNC_WAIT_TIMEOUT_MS:-0}}"
ASYNC_CALLBACK_URL="${PRODUCTION_REPLAY_ASYNC_CALLBACK_URL:-${ASYNC_CALLBACK_URL:-}}"
ASYNC_CALLBACK_PORT="${PRODUCTION_REPLAY_ASYNC_CALLBACK_PORT:-${ASYNC_CALLBACK_PORT:-8099}}"
ASYNC_CALLBACK_WAIT_TIMEOUT="${PRODUCTION_REPLAY_ASYNC_CALLBACK_WAIT_TIMEOUT:-${ASYNC_CALLBACK_WAIT_TIMEOUT:-120}}"
DURATION="${PRODUCTION_REPLAY_DURATION:-${DURATION:-}}"
HOURS="${PRODUCTION_REPLAY_HOURS:-${HOURS:-}}"
DAYS="${PRODUCTION_REPLAY_DAYS:-${DAYS:-}}"
WEEKS="${PRODUCTION_REPLAY_WEEKS:-${WEEKS:-}}"
SMOKE_MANIFEST="/tmp/fraud-data-local-smoke.json"
SAMPLE_DIR="/tmp/fraud-data-local-sample"
SETUP_LOG="/tmp/fraud-data-local-setup.log"
REPLAY_LOG="/tmp/fraud-data-local-replay.log"
ASYNC_TRACKING_LOG="/tmp/fraud-data-local-async-decisions.ndjson"
ASYNC_CALLBACK_LOG="/tmp/fraud-data-local-async-callbacks.ndjson"
ASYNC_CALLBACK_SERVER_LOG="/tmp/fraud-data-local-callback-server.log"
COMPOSE_OVERRIDE="$SCRIPT_DIR/docker-compose.local-replay.yml"
CALLBACK_SERVER_PID=""
AUTO_CALLBACK_SERVER=0

cleanup() {
  if [[ -n "$CALLBACK_SERVER_PID" ]]; then
    kill "$CALLBACK_SERVER_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'error: required command not found: %s\n' "$1" >&2
    exit 1
  fi
}

compose() {
  docker compose --project-directory "$WORKSPACE_DIR" \
    --file "$WORKSPACE_DIR/docker-compose.yml" \
    --file "$COMPOSE_OVERRIDE" \
    "$@"
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
  for ((attempt = 1; attempt <= 120; attempt++)); do
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
require_command docker
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
if [[ ! "$ASYNC_CALLBACK_PORT" =~ ^[0-9]+$ || "$ASYNC_CALLBACK_PORT" -le 0 ]]; then
  printf 'error: ASYNC_CALLBACK_PORT must be a positive integer; got %s\n' "$ASYNC_CALLBACK_PORT" >&2
  exit 1
fi
if [[ ! "$ASYNC_CALLBACK_WAIT_TIMEOUT" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
  printf 'error: ASYNC_CALLBACK_WAIT_TIMEOUT must be zero or a positive number; got %s\n' "$ASYNC_CALLBACK_WAIT_TIMEOUT" >&2
  exit 1
fi

if [[ -n "$REPLAY_DURATION" ]]; then
  printf 'Replay configuration: duration=%s multiplier=%sx max_in_flight=%s decision_mode=%s\n' \
    "$REPLAY_DURATION" "$MULTIPLIER" "$MAX_IN_FLIGHT" "$DECISION_MODE"
else
  printf 'Replay configuration: transactions=%s multiplier=%sx max_in_flight=%s decision_mode=%s\n' \
    "$TRANSACTIONS" "$MULTIPLIER" "$MAX_IN_FLIGHT" "$DECISION_MODE"
fi

printf 'Preparing local fraud databases from existing images...\n'
compose up -d --no-build postgres
compose run --rm data-model-migrate
compose run --rm ingestion-migrate
compose run --rm decision-engine-migrate
compose run --rm screening-migrate

printf 'Starting local fraud services from existing images...\n'
SERVICES=(
  data-model-service
  ingestion-service
  decision-engine-service
  data-model-worker
)
if [[ "$DECISION_MODE" == "async" ]]; then
  SERVICES+=(decision-engine-worker)
fi
compose up -d --no-build "${SERVICES[@]}"

wait_for_service "data-model-service" "http://127.0.0.1:8080/readyz"
wait_for_service "ingestion-service" "http://127.0.0.1:8081/readyz"
wait_for_service "decision-engine-service" "http://127.0.0.1:8082/readyz"

if [[ ! -x "$VENV_DIR/bin/python" ]]; then
  printf 'Creating replay Python environment...\n'
  python3 -m venv --system-site-packages "$VENV_DIR"
fi

if ! "$VENV_DIR/bin/python" -c 'import httpx, openpyxl' >/dev/null 2>&1; then
  "$VENV_DIR/bin/python" -m pip install -r "$SCRIPT_DIR/requirements.txt"
fi

if [[ "$DECISION_MODE" == "async" && -z "$ASYNC_CALLBACK_URL" ]]; then
  printf 'Starting local async callback receiver on port %s...\n' "$ASYNC_CALLBACK_PORT"
  PYTHONPATH="$BACKEND_DIR/stress-tests" "$VENV_DIR/bin/python" -m production_replay.callback_server \
    --host 0.0.0.0 \
    --port "$ASYNC_CALLBACK_PORT" \
    --output "$ASYNC_CALLBACK_LOG" \
    >"$ASYNC_CALLBACK_SERVER_LOG" 2>&1 &
  CALLBACK_SERVER_PID="$!"
  wait_for_service "async-callback-receiver" "http://127.0.0.1:$ASYNC_CALLBACK_PORT/readyz"
  ASYNC_CALLBACK_URL="http://host.docker.internal:$ASYNC_CALLBACK_PORT/callbacks/async-decision"
  AUTO_CALLBACK_SERVER=1
  printf 'Async callback URL for Docker workers: %s\n' "$ASYNC_CALLBACK_URL"
fi

(
  cd "$BACKEND_DIR"
  SAMPLE_ARGS=(
    --base-manifest "$SCRIPT_DIR/manifests/fraud-data.json" \
    --data-root "$DATA_ROOT" \
    --output-dir "$SAMPLE_DIR" \
    --output-manifest "$SMOKE_MANIFEST"
  )
  if [[ -n "$REPLAY_DURATION" ]]; then
    SAMPLE_ARGS+=(--duration "$REPLAY_DURATION")
  else
    SAMPLE_ARGS+=(--transactions "$TRANSACTIONS")
  fi
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay.local_sample "${SAMPLE_ARGS[@]}"
)

printf 'Creating a local replay tenant and loading reference data...\n'
(
  cd "$BACKEND_DIR"
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay setup \
    --manifest "$SMOKE_MANIFEST" \
    --execute \
    --tenant-name "Local Production Replay Smoke Test" \
    --publication-timeout 900
) | tee "$SETUP_LOG"

TENANT_ID="$(awk '/^tenant:/ {print $2}' "$SETUP_LOG" | tail -n 1)"
if [[ -z "$TENANT_ID" ]]; then
  printf 'error: setup completed without returning a tenant ID\n' >&2
  exit 1
fi

printf 'Building the frontend for replay tenant %s...\n' "$TENANT_ID"
export NEXT_PUBLIC_DATA_MODEL_TENANT_ID="$TENANT_ID"
compose build frontend
compose up -d --no-deps frontend
printf 'Frontend is available at http://127.0.0.1:3000 for tenant %s\n' "$TENANT_ID"

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
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay run \
    --manifest "$SMOKE_MANIFEST" \
    --execute \
    --tenant-id "$TENANT_ID" \
    --multiplier "$MULTIPLIER" \
    --max-in-flight "$MAX_IN_FLIGHT" \
    --checkpoint-every "$CHECKPOINT_EVERY" \
    --decision-mode "$DECISION_MODE" \
    --async-wait-timeout-ms "$ASYNC_WAIT_TIMEOUT_MS" \
    --async-callback-url "$ASYNC_CALLBACK_URL" \
    --async-tracking-output "$ASYNC_TRACKING_LOG"
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

CALLBACK_REPORT_STATUS=0
if [[ "$DECISION_MODE" == "async" && "$AUTO_CALLBACK_SERVER" == "1" ]]; then
  printf '\nAsync callback timing result:\n'
  set +e
  PYTHONPATH="$BACKEND_DIR/stress-tests" "$VENV_DIR/bin/python" -m production_replay.callback_report \
    --submissions "$ASYNC_TRACKING_LOG" \
    --callbacks "$ASYNC_CALLBACK_LOG" \
    --summary "$RUN_DIR/async-callback-summary.json" \
    --wait-timeout "$ASYNC_CALLBACK_WAIT_TIMEOUT"
  CALLBACK_REPORT_STATUS="$?"
  set -e
fi

printf '\nLocal replay result:\n'
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
if [[ "$REPLAY_STATUS" -ne 0 ]]; then
  exit "$REPLAY_STATUS"
fi
exit "$CALLBACK_REPORT_STATUS"
