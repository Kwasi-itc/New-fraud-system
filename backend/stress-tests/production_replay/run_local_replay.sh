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
DECISION_MODE="${PRODUCTION_REPLAY_DECISION_MODE:-${DECISION_MODE:-async}}"
ASYNC_WAIT_TIMEOUT_MS="${PRODUCTION_REPLAY_ASYNC_WAIT_TIMEOUT_MS:-${ASYNC_WAIT_TIMEOUT_MS:-0}}"
ASYNC_CALLBACK_URL="${PRODUCTION_REPLAY_ASYNC_CALLBACK_URL:-${ASYNC_CALLBACK_URL:-}}"
ASYNC_CALLBACK_PORT="${PRODUCTION_REPLAY_ASYNC_CALLBACK_PORT:-${ASYNC_CALLBACK_PORT:-8099}}"
ASYNC_CALLBACK_WAIT_TIMEOUT="${PRODUCTION_REPLAY_ASYNC_CALLBACK_WAIT_TIMEOUT:-${ASYNC_CALLBACK_WAIT_TIMEOUT:-120}}"
LIVE_DECISION_MODE="${PRODUCTION_REPLAY_LIVE_DECISION_MODE:-${LIVE_DECISION_MODE:-async_only}}"
LIVE_ASYNC_FALLBACK_ENABLED="${PRODUCTION_REPLAY_LIVE_ASYNC_FALLBACK_ENABLED:-${LIVE_ASYNC_FALLBACK_ENABLED:-true}}"
INGESTION_TRIGGER_DECISION_MODE="${PRODUCTION_REPLAY_INGESTION_TRIGGER_DECISION_MODE:-${INGESTION_TRIGGER_DECISION_MODE:-async_only}}"
INGESTION_TRIGGER_OVERLOAD_MODE="${PRODUCTION_REPLAY_INGESTION_TRIGGER_OVERLOAD_MODE:-${INGESTION_TRIGGER_OVERLOAD_MODE:-defer_async}}"
LIVE_ASYNC_OBJECT_TYPES="${PRODUCTION_REPLAY_LIVE_ASYNC_OBJECT_TYPES:-${LIVE_ASYNC_OBJECT_TYPES:-}}"
TENANT_DATA_READ_MODE="${PRODUCTION_REPLAY_TENANT_DATA_READ_MODE:-${TENANT_DATA_READ_MODE:-direct_db}}"
ENABLE_SEPARATE_READ_POOL="${PRODUCTION_REPLAY_ENABLE_SEPARATE_READ_POOL:-${ENABLE_SEPARATE_READ_POOL:-false}}"
READ_DATABASE_URL="${PRODUCTION_REPLAY_READ_DATABASE_URL:-${READ_DATABASE_URL:-}}"
READ_DATABASE_MAX_CONNS="${PRODUCTION_REPLAY_READ_DATABASE_MAX_CONNS:-${READ_DATABASE_MAX_CONNS:-0}}"
READ_DATABASE_MIN_CONNS="${PRODUCTION_REPLAY_READ_DATABASE_MIN_CONNS:-${READ_DATABASE_MIN_CONNS:-0}}"
WORKER_DATABASE_URL="${PRODUCTION_REPLAY_WORKER_DATABASE_URL:-${WORKER_DATABASE_URL:-}}"
WORKER_DATABASE_MAX_CONNS="${PRODUCTION_REPLAY_WORKER_DATABASE_MAX_CONNS:-${WORKER_DATABASE_MAX_CONNS:-0}}"
WORKER_DATABASE_MIN_CONNS="${PRODUCTION_REPLAY_WORKER_DATABASE_MIN_CONNS:-${WORKER_DATABASE_MIN_CONNS:-0}}"
RULE_EVALUATION_CONCURRENCY="${PRODUCTION_REPLAY_RULE_EVALUATION_CONCURRENCY:-${RULE_EVALUATION_CONCURRENCY:-0}}"
SCENARIO_EVALUATION_CONCURRENCY="${PRODUCTION_REPLAY_SCENARIO_EVALUATION_CONCURRENCY:-${SCENARIO_EVALUATION_CONCURRENCY:-0}}"
AGGREGATE_REMOTE_CONCURRENCY_LIMIT="${PRODUCTION_REPLAY_AGGREGATE_REMOTE_CONCURRENCY_LIMIT:-${AGGREGATE_REMOTE_CONCURRENCY_LIMIT:-0}}"
AGGREGATE_QUERY_CONCURRENCY_LIMIT="${PRODUCTION_REPLAY_AGGREGATE_QUERY_CONCURRENCY_LIMIT:-${AGGREGATE_QUERY_CONCURRENCY_LIMIT:-16}}"
ALLOW_UNSAFE_INGESTION_HTTP_REPLAY="${PRODUCTION_REPLAY_ALLOW_UNSAFE_INGESTION_HTTP_REPLAY:-${ALLOW_UNSAFE_INGESTION_HTTP_REPLAY:-false}}"
DURATION="${PRODUCTION_REPLAY_DURATION:-${DURATION:-}}"
HOURS="${PRODUCTION_REPLAY_HOURS:-${HOURS:-}}"
DAYS="${PRODUCTION_REPLAY_DAYS:-${DAYS:-}}"
WEEKS="${PRODUCTION_REPLAY_WEEKS:-${WEEKS:-}}"
TENANT_ID="${PRODUCTION_REPLAY_TENANT_ID:-${TENANT_ID:-}}"
EXPERIMENT_LABEL="${PRODUCTION_REPLAY_EXPERIMENT_LABEL:-${EXPERIMENT_LABEL:-}}"
SMOKE_MANIFEST="/tmp/fraud-data-local-smoke.json"
SAMPLE_DIR="/tmp/fraud-data-local-sample"
SETUP_LOG="/tmp/fraud-data-local-setup.log"
REPLAY_LOG="/tmp/fraud-data-local-replay.log"
ASYNC_TRACKING_LOG="/tmp/fraud-data-local-async-decisions.ndjson"
ASYNC_CALLBACK_LOG="/tmp/fraud-data-local-async-callbacks.ndjson"
ASYNC_CALLBACK_SERVER_LOG="/tmp/fraud-data-local-callback-server.log"
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

if [[ -z "$TENANT_ID" ]]; then
  printf 'error: TENANT_ID is required; pass TENANT_ID=<existing-tenant-id> to make production-replay\n' >&2
  exit 1
fi

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
if [[ "$LIVE_DECISION_MODE" != "sync" && "$LIVE_DECISION_MODE" != "async_only" ]]; then
  printf 'error: LIVE_DECISION_MODE must be sync or async_only; got %s\n' "$LIVE_DECISION_MODE" >&2
  exit 1
fi
if [[ "$LIVE_ASYNC_FALLBACK_ENABLED" != "true" && "$LIVE_ASYNC_FALLBACK_ENABLED" != "false" ]]; then
  printf 'error: LIVE_ASYNC_FALLBACK_ENABLED must be true or false; got %s\n' "$LIVE_ASYNC_FALLBACK_ENABLED" >&2
  exit 1
fi
if [[ "$INGESTION_TRIGGER_DECISION_MODE" != "sync" && "$INGESTION_TRIGGER_DECISION_MODE" != "async_only" ]]; then
  printf 'error: INGESTION_TRIGGER_DECISION_MODE must be sync or async_only; got %s\n' "$INGESTION_TRIGGER_DECISION_MODE" >&2
  exit 1
fi
if [[ "$INGESTION_TRIGGER_OVERLOAD_MODE" != "defer_async" && "$INGESTION_TRIGGER_OVERLOAD_MODE" != "reject" ]]; then
  printf 'error: INGESTION_TRIGGER_OVERLOAD_MODE must be defer_async or reject; got %s\n' "$INGESTION_TRIGGER_OVERLOAD_MODE" >&2
  exit 1
fi
if [[ "$TENANT_DATA_READ_MODE" != "ingestion_http" && "$TENANT_DATA_READ_MODE" != "direct_db" ]]; then
  printf 'error: TENANT_DATA_READ_MODE must be ingestion_http or direct_db; got %s\n' "$TENANT_DATA_READ_MODE" >&2
  exit 1
fi
if [[ "$ENABLE_SEPARATE_READ_POOL" != "true" && "$ENABLE_SEPARATE_READ_POOL" != "false" ]]; then
  printf 'error: ENABLE_SEPARATE_READ_POOL must be true or false; got %s\n' "$ENABLE_SEPARATE_READ_POOL" >&2
  exit 1
fi
if [[ ! "$RULE_EVALUATION_CONCURRENCY" =~ ^[0-9]+$ ]]; then
  printf 'error: RULE_EVALUATION_CONCURRENCY must be zero or a positive integer; got %s\n' "$RULE_EVALUATION_CONCURRENCY" >&2
  exit 1
fi
if [[ ! "$SCENARIO_EVALUATION_CONCURRENCY" =~ ^[0-9]+$ ]]; then
  printf 'error: SCENARIO_EVALUATION_CONCURRENCY must be zero or a positive integer; got %s\n' "$SCENARIO_EVALUATION_CONCURRENCY" >&2
  exit 1
fi
if [[ ! "$AGGREGATE_REMOTE_CONCURRENCY_LIMIT" =~ ^[0-9]+$ ]]; then
  printf 'error: AGGREGATE_REMOTE_CONCURRENCY_LIMIT must be zero or a positive integer; got %s\n' "$AGGREGATE_REMOTE_CONCURRENCY_LIMIT" >&2
  exit 1
fi
if [[ ! "$AGGREGATE_QUERY_CONCURRENCY_LIMIT" =~ ^[0-9]+$ ]]; then
  printf 'error: AGGREGATE_QUERY_CONCURRENCY_LIMIT must be zero or a positive integer; got %s\n' "$AGGREGATE_QUERY_CONCURRENCY_LIMIT" >&2
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

if [[ -n "$REPLAY_DURATION" ]]; then
  printf 'Replay configuration: duration=%s multiplier=%sx max_in_flight=%s decision_mode=%s live_decision_mode=%s read_mode=%s separate_read_pool=%s async_fallback=%s\n' \
    "$REPLAY_DURATION" "$MULTIPLIER" "$MAX_IN_FLIGHT" "$DECISION_MODE" "$LIVE_DECISION_MODE" "$TENANT_DATA_READ_MODE" "$ENABLE_SEPARATE_READ_POOL" "$LIVE_ASYNC_FALLBACK_ENABLED"
else
  printf 'Replay configuration: transactions=%s multiplier=%sx max_in_flight=%s decision_mode=%s live_decision_mode=%s read_mode=%s separate_read_pool=%s async_fallback=%s\n' \
    "$TRANSACTIONS" "$MULTIPLIER" "$MAX_IN_FLIGHT" "$DECISION_MODE" "$LIVE_DECISION_MODE" "$TENANT_DATA_READ_MODE" "$ENABLE_SEPARATE_READ_POOL" "$LIVE_ASYNC_FALLBACK_ENABLED"
fi
printf 'Replay tuning: rule_eval=%s scenario_eval=%s aggregate_remote=%s aggregate_query=%s read_db_max_conns=%s\n' \
  "$RULE_EVALUATION_CONCURRENCY" "$SCENARIO_EVALUATION_CONCURRENCY" "$AGGREGATE_REMOTE_CONCURRENCY_LIMIT" "$AGGREGATE_QUERY_CONCURRENCY_LIMIT" "$READ_DATABASE_MAX_CONNS"

if [[ "$ENABLE_SEPARATE_READ_POOL" == "true" && -z "$READ_DATABASE_URL" ]]; then
  READ_DATABASE_URL="postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable"
fi

printf 'Preparing local fraud databases from existing images...\n'
export LIVE_DECISION_MODE="$LIVE_DECISION_MODE"
export LIVE_ASYNC_FALLBACK_ENABLED="$LIVE_ASYNC_FALLBACK_ENABLED"
export INGESTION_TRIGGER_DECISION_MODE="$INGESTION_TRIGGER_DECISION_MODE"
export INGESTION_TRIGGER_OVERLOAD_MODE="$INGESTION_TRIGGER_OVERLOAD_MODE"
export LIVE_ASYNC_OBJECT_TYPES="$LIVE_ASYNC_OBJECT_TYPES"
export TENANT_DATA_READ_MODE="$TENANT_DATA_READ_MODE"
export READ_DATABASE_URL="$READ_DATABASE_URL"
export READ_DATABASE_MAX_CONNS="$READ_DATABASE_MAX_CONNS"
export READ_DATABASE_MIN_CONNS="$READ_DATABASE_MIN_CONNS"
export WORKER_DATABASE_URL="$WORKER_DATABASE_URL"
export WORKER_DATABASE_MAX_CONNS="$WORKER_DATABASE_MAX_CONNS"
export WORKER_DATABASE_MIN_CONNS="$WORKER_DATABASE_MIN_CONNS"
export RULE_EVALUATION_CONCURRENCY="$RULE_EVALUATION_CONCURRENCY"
export SCENARIO_EVALUATION_CONCURRENCY="$SCENARIO_EVALUATION_CONCURRENCY"
export AGGREGATE_REMOTE_CONCURRENCY_LIMIT="$AGGREGATE_REMOTE_CONCURRENCY_LIMIT"
export AGGREGATE_QUERY_CONCURRENCY_LIMIT="$AGGREGATE_QUERY_CONCURRENCY_LIMIT"
export PRODUCTION_REPLAY_LIVE_DECISION_MODE="$LIVE_DECISION_MODE"
export PRODUCTION_REPLAY_LIVE_ASYNC_FALLBACK_ENABLED="$LIVE_ASYNC_FALLBACK_ENABLED"
export PRODUCTION_REPLAY_INGESTION_TRIGGER_DECISION_MODE="$INGESTION_TRIGGER_DECISION_MODE"
export PRODUCTION_REPLAY_INGESTION_TRIGGER_OVERLOAD_MODE="$INGESTION_TRIGGER_OVERLOAD_MODE"
export PRODUCTION_REPLAY_LIVE_ASYNC_OBJECT_TYPES="$LIVE_ASYNC_OBJECT_TYPES"
export PRODUCTION_REPLAY_TENANT_DATA_READ_MODE="$TENANT_DATA_READ_MODE"
export PRODUCTION_REPLAY_READ_DATABASE_URL="$READ_DATABASE_URL"
export PRODUCTION_REPLAY_READ_DATABASE_MAX_CONNS="$READ_DATABASE_MAX_CONNS"
export PRODUCTION_REPLAY_READ_DATABASE_MIN_CONNS="$READ_DATABASE_MIN_CONNS"
export PRODUCTION_REPLAY_WORKER_DATABASE_URL="$WORKER_DATABASE_URL"
export PRODUCTION_REPLAY_WORKER_DATABASE_MAX_CONNS="$WORKER_DATABASE_MAX_CONNS"
export PRODUCTION_REPLAY_WORKER_DATABASE_MIN_CONNS="$WORKER_DATABASE_MIN_CONNS"
export PRODUCTION_REPLAY_RULE_EVALUATION_CONCURRENCY="$RULE_EVALUATION_CONCURRENCY"
export PRODUCTION_REPLAY_SCENARIO_EVALUATION_CONCURRENCY="$SCENARIO_EVALUATION_CONCURRENCY"
export PRODUCTION_REPLAY_AGGREGATE_REMOTE_CONCURRENCY_LIMIT="$AGGREGATE_REMOTE_CONCURRENCY_LIMIT"
export PRODUCTION_REPLAY_AGGREGATE_QUERY_CONCURRENCY_LIMIT="$AGGREGATE_QUERY_CONCURRENCY_LIMIT"
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
    --tenant-id "$TENANT_ID" \
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
printf 'Frontend is available at http://127.0.0.1:5118 for tenant %s\n' "$TENANT_ID"

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

"$VENV_DIR/bin/python" - "$RUN_DIR" <<'PY'
import json
import os
import sys
from pathlib import Path

run_dir = Path(sys.argv[1])
metadata = {
    "experiment_label": os.getenv("EXPERIMENT_LABEL") or None,
    "service_modes": {
        "live_decision_mode": os.getenv("LIVE_DECISION_MODE"),
        "live_async_fallback_enabled": os.getenv("LIVE_ASYNC_FALLBACK_ENABLED"),
        "ingestion_trigger_decision_mode": os.getenv("INGESTION_TRIGGER_DECISION_MODE"),
        "ingestion_trigger_overload_mode": os.getenv("INGESTION_TRIGGER_OVERLOAD_MODE"),
        "live_async_object_types": os.getenv("LIVE_ASYNC_OBJECT_TYPES") or None,
        "tenant_data_read_mode": os.getenv("TENANT_DATA_READ_MODE"),
    },
    "ingestion_read_pool": {
        "enabled": os.getenv("ENABLE_SEPARATE_READ_POOL") == "true",
        "read_database_url": "set" if os.getenv("READ_DATABASE_URL") else None,
        "read_database_max_conns": os.getenv("READ_DATABASE_MAX_CONNS"),
        "read_database_min_conns": os.getenv("READ_DATABASE_MIN_CONNS"),
        "worker_database_url": "set" if os.getenv("WORKER_DATABASE_URL") else None,
        "worker_database_max_conns": os.getenv("WORKER_DATABASE_MAX_CONNS"),
        "worker_database_min_conns": os.getenv("WORKER_DATABASE_MIN_CONNS"),
    },
}
(run_dir / "experiment-settings.json").write_text(json.dumps(metadata, indent=2) + "\n", encoding="utf-8")
PY

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
