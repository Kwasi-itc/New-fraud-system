#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKSPACE_DIR="$(cd "$BACKEND_DIR/.." && pwd)"
ENV_FILE="${PRODUCTION_REPLAY_ENV_FILE:-}"
DATA_ROOT="${FRAUD_DATA_ROOT:-/Users/kwilson/Desktop/ITC/fraud_data}"
SEED_DATA_ROOT="${PRODUCTION_REPLAY_SEED_DATA_ROOT:-${FRAUD_DATA_SEED_ROOT:-${DATA_ROOT%/}_seed}}"
SEED_START_TIME="${PRODUCTION_REPLAY_SEED_START_TIME:-${SEED_START_TIME:-}}"
SEED_END_TIME="${PRODUCTION_REPLAY_SEED_END_TIME:-${SEED_END_TIME:-}}"
VENV_DIR="${PRODUCTION_REPLAY_VENV:-/tmp/fraud-production-replay-venv}"
TRANSACTIONS="${PRODUCTION_REPLAY_TRANSACTIONS:-${TRANSACTIONS:-1000}}"
TRANSACTION_OFFSET="${PRODUCTION_REPLAY_TRANSACTION_OFFSET:-${TRANSACTION_OFFSET:-0}}"
MULTIPLIER="${PRODUCTION_REPLAY_MULTIPLIER:-${MULTIPLIER:-3600}}"
MAX_IN_FLIGHT="${PRODUCTION_REPLAY_MAX_IN_FLIGHT:-${MAX_IN_FLIGHT:-50}}"
CHECKPOINT_EVERY="${PRODUCTION_REPLAY_CHECKPOINT_EVERY:-${CHECKPOINT_EVERY:-100}}"
SEED_BATCH_SIZE="${PRODUCTION_REPLAY_SEED_BATCH_SIZE:-${SEED_BATCH_SIZE:-500}}"
SEED_MAX_IN_FLIGHT="${PRODUCTION_REPLAY_SEED_MAX_IN_FLIGHT:-${SEED_MAX_IN_FLIGHT:-10}}"
SEED_PROGRESS_EVERY="${PRODUCTION_REPLAY_SEED_PROGRESS_EVERY:-${SEED_PROGRESS_EVERY:-100}}"
SEED_REQUEST_TIMEOUT="${PRODUCTION_REPLAY_SEED_REQUEST_TIMEOUT:-${SEED_REQUEST_TIMEOUT:-300}}"
AGGREGATE_FACT_BACKFILL_TIMEOUT="${PRODUCTION_REPLAY_AGGREGATE_FACT_BACKFILL_TIMEOUT:-${AGGREGATE_FACT_BACKFILL_TIMEOUT:-7200}}"
PUBLICATION_TIMEOUT="${PRODUCTION_REPLAY_PUBLICATION_TIMEOUT:-${PUBLICATION_TIMEOUT:-3600}}"
PUBLISH_RULES_AFTER_SEED="${PRODUCTION_REPLAY_PUBLISH_RULES_AFTER_SEED:-${PUBLISH_RULES_AFTER_SEED:-false}}"
REUSE_EXISTING_SETUP="${PRODUCTION_REPLAY_REUSE_EXISTING_SETUP:-${REUSE_EXISTING_SETUP:-false}}"
REUSE_EXISTING_SEED="${PRODUCTION_REPLAY_REUSE_EXISTING_SEED:-${REUSE_EXISTING_SEED:-false}}"
SKIP_SEED="${PRODUCTION_REPLAY_SKIP_SEED:-${SKIP_SEED:-false}}"
PRESERVE_RUNNING_VALKEY="${PRODUCTION_REPLAY_PRESERVE_RUNNING_VALKEY:-${PRESERVE_RUNNING_VALKEY:-false}}"
PROFILE_INPUT="${PRODUCTION_REPLAY_PROFILE_INPUT:-${PROFILE_INPUT:-}}"
DECISION_MODE="${PRODUCTION_REPLAY_DECISION_MODE:-${DECISION_MODE:-sync}}"
ASYNC_WAIT_TIMEOUT_MS="${PRODUCTION_REPLAY_ASYNC_WAIT_TIMEOUT_MS:-${ASYNC_WAIT_TIMEOUT_MS:-0}}"
ASYNC_CALLBACK_URL="${PRODUCTION_REPLAY_ASYNC_CALLBACK_URL:-${ASYNC_CALLBACK_URL:-}}"
ASYNC_CALLBACK_PORT="${PRODUCTION_REPLAY_ASYNC_CALLBACK_PORT:-${ASYNC_CALLBACK_PORT:-8099}}"
ASYNC_CALLBACK_WAIT_TIMEOUT="${PRODUCTION_REPLAY_ASYNC_CALLBACK_WAIT_TIMEOUT:-${ASYNC_CALLBACK_WAIT_TIMEOUT:-120}}"
LIVE_DECISION_MODE="${PRODUCTION_REPLAY_LIVE_DECISION_MODE:-${LIVE_DECISION_MODE:-}}"
LIVE_ASYNC_FALLBACK_ENABLED="${PRODUCTION_REPLAY_LIVE_ASYNC_FALLBACK_ENABLED:-${LIVE_ASYNC_FALLBACK_ENABLED:-false}}"
LIVE_ASYNC_OBJECT_TYPES="${PRODUCTION_REPLAY_LIVE_ASYNC_OBJECT_TYPES:-${LIVE_ASYNC_OBJECT_TYPES:-}}"
TENANT_DATA_READ_MODE="${PRODUCTION_REPLAY_TENANT_DATA_READ_MODE:-${TENANT_DATA_READ_MODE:-direct_db}}"
ENABLE_SEPARATE_READ_POOL="${PRODUCTION_REPLAY_ENABLE_SEPARATE_READ_POOL:-${ENABLE_SEPARATE_READ_POOL:-false}}"
READ_DATABASE_URL="${PRODUCTION_REPLAY_READ_DATABASE_URL:-${READ_DATABASE_URL:-}}"
READ_DATABASE_MAX_CONNS="${PRODUCTION_REPLAY_READ_DATABASE_MAX_CONNS:-${READ_DATABASE_MAX_CONNS:-0}}"
READ_DATABASE_MIN_CONNS="${PRODUCTION_REPLAY_READ_DATABASE_MIN_CONNS:-${READ_DATABASE_MIN_CONNS:-0}}"
WORKER_DATABASE_URL="${PRODUCTION_REPLAY_WORKER_DATABASE_URL:-${WORKER_DATABASE_URL:-}}"
WORKER_DATABASE_MAX_CONNS="${PRODUCTION_REPLAY_WORKER_DATABASE_MAX_CONNS:-${WORKER_DATABASE_MAX_CONNS:-4}}"
WORKER_DATABASE_MIN_CONNS="${PRODUCTION_REPLAY_WORKER_DATABASE_MIN_CONNS:-${WORKER_DATABASE_MIN_CONNS:-0}}"
RULE_EVALUATION_CONCURRENCY="${PRODUCTION_REPLAY_RULE_EVALUATION_CONCURRENCY:-${RULE_EVALUATION_CONCURRENCY:-0}}"
SCENARIO_EVALUATION_CONCURRENCY="${PRODUCTION_REPLAY_SCENARIO_EVALUATION_CONCURRENCY:-${SCENARIO_EVALUATION_CONCURRENCY:-0}}"
AGGREGATE_REMOTE_CONCURRENCY_LIMIT="${PRODUCTION_REPLAY_AGGREGATE_REMOTE_CONCURRENCY_LIMIT:-${AGGREGATE_REMOTE_CONCURRENCY_LIMIT:-0}}"
AGGREGATE_QUERY_CONCURRENCY_LIMIT="${PRODUCTION_REPLAY_AGGREGATE_QUERY_CONCURRENCY_LIMIT:-${AGGREGATE_QUERY_CONCURRENCY_LIMIT:-0}}"
FRONTEND_FORWARDED_PORT="${PRODUCTION_REPLAY_FRONTEND_FORWARDED_PORT:-${FRONTEND_FORWARDED_PORT:-3000}}"
DATA_MODEL_FORWARDED_PORT="${PRODUCTION_REPLAY_DATA_MODEL_FORWARDED_PORT:-${DATA_MODEL_FORWARDED_PORT:-8080}}"
INGESTION_FORWARDED_PORT="${PRODUCTION_REPLAY_INGESTION_FORWARDED_PORT:-${INGESTION_FORWARDED_PORT:-8081}}"
DECISION_ENGINE_FORWARDED_PORT="${PRODUCTION_REPLAY_DECISION_ENGINE_FORWARDED_PORT:-${DECISION_ENGINE_FORWARDED_PORT:-8082}}"
ALLOW_UNSAFE_INGESTION_HTTP_REPLAY="${PRODUCTION_REPLAY_ALLOW_UNSAFE_INGESTION_HTTP_REPLAY:-${ALLOW_UNSAFE_INGESTION_HTTP_REPLAY:-false}}"
DURATION="${PRODUCTION_REPLAY_DURATION:-${DURATION:-}}"
HOURS="${PRODUCTION_REPLAY_HOURS:-${HOURS:-}}"
DAYS="${PRODUCTION_REPLAY_DAYS:-${DAYS:-}}"
WEEKS="${PRODUCTION_REPLAY_WEEKS:-${WEEKS:-}}"
TENANT_ID="${PRODUCTION_REPLAY_TENANT_ID:-${TENANT_ID:-}}"
EXPERIMENT_LABEL="${PRODUCTION_REPLAY_EXPERIMENT_LABEL:-${EXPERIMENT_LABEL:-}}"
SMOKE_MANIFEST="/tmp/fraud-data-local-smoke.json"
SEED_MANIFEST="/tmp/fraud-data-local-seed.json"
SAMPLE_DIR="/tmp/fraud-data-local-sample"
SEED_SAMPLE_DIR="/tmp/fraud-data-local-seed-sample"
SETUP_LOG="/tmp/fraud-data-local-setup.log"
SEED_LOG="/tmp/fraud-data-local-seed.log"
REPLAY_LOG="/tmp/fraud-data-local-replay.log"
PUBLICATION_LOG="/tmp/fraud-data-local-publication.log"
ASYNC_TRACKING_LOG="/tmp/fraud-data-local-async-decisions.ndjson"
ASYNC_CALLBACK_LOG="/tmp/fraud-data-local-async-callbacks.ndjson"
ASYNC_CALLBACK_SERVER_LOG="/tmp/fraud-data-local-callback-server.log"
ASYNC_BACKLOG_BEFORE="/tmp/fraud-data-local-async-backlog-before.json"
ASYNC_BACKLOG_AFTER="/tmp/fraud-data-local-async-backlog-after.json"
CALLBACK_SERVER_PID=""
AUTO_CALLBACK_SERVER=0
START_DECISION_WORKER=0
POSTGRES_STARTUP_TIMEOUT="${PRODUCTION_REPLAY_POSTGRES_STARTUP_TIMEOUT:-1800}"
DATA_MODEL_URL="http://127.0.0.1:$DATA_MODEL_FORWARDED_PORT"
INGESTION_URL="http://127.0.0.1:$INGESTION_FORWARDED_PORT"
DECISION_ENGINE_URL="http://127.0.0.1:$DECISION_ENGINE_FORWARDED_PORT"

if [[ -z "$LIVE_DECISION_MODE" ]]; then
  if [[ "$DECISION_MODE" == "async" ]]; then
    LIVE_DECISION_MODE="async_only"
  else
    LIVE_DECISION_MODE="sync"
  fi
fi

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
  local compose_args=(
    --project-directory "$WORKSPACE_DIR"
    --file "$WORKSPACE_DIR/docker-compose.yml"
  )
  if [[ -n "$ENV_FILE" ]]; then
    compose_args+=(--env-file "$ENV_FILE")
  fi
  docker compose "${compose_args[@]}" "$@"
}

capture_async_backlog() {
  local output_path="$1"
  local label="$2"
  local url="$DECISION_ENGINE_URL/v1/tenants/$TENANT_ID/async-decision-executions/status-summary"
  if ! curl --fail --silent --show-error "$url" >"$output_path"; then
    : >"$output_path"
    printf 'warning: unable to read async decision backlog %s replay\n' "$label" >&2
    return
  fi
  python3 - "$output_path" "$label" <<'PY'
import json
import sys
from pathlib import Path

payload = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
summary = payload.get("status_summary", {})
print(
    "Async decision backlog " + sys.argv[2] + " replay: "
    + " ".join(f"{key}={summary.get(key, 0)}" for key in ("pending", "queued", "running", "completed", "failed"))
)
PY
}

capture_fact_runtime_metrics() {
  local output_dir="$1"
  local label="$2"
  local ingestion_output="$output_dir/ingestion-aggregate-fact-metrics-$label.json"
  local decision_output="$output_dir/decision-runtime-metrics-$label.json"
  mkdir -p "$output_dir"
  if ! curl --fail --silent --show-error \
    "$INGESTION_URL/v1/admin/aggregate-fact-metrics" >"$ingestion_output"; then
    : >"$ingestion_output"
    printf 'warning: unable to capture ingestion aggregate-fact metrics at %s\n' "$label" >&2
  fi
  if ! curl --fail --silent --show-error \
    "$DECISION_ENGINE_URL/v1/admin/runtime-metrics" >"$decision_output"; then
    : >"$decision_output"
    printf 'warning: unable to capture decision runtime metrics at %s\n' "$label" >&2
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

wait_for_postgres() {
  local elapsed=0
  local container_id
  local health

  printf 'Waiting for PostgreSQL to finish startup or crash recovery...\n'
  while [[ "$elapsed" -lt "$POSTGRES_STARTUP_TIMEOUT" ]]; do
    if compose exec -T postgres pg_isready -U fraud -d fraud >/dev/null 2>&1; then
      container_id="$(compose ps -q postgres)"
      health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}healthy{{end}}' "$container_id" 2>/dev/null || true)"
      if [[ "$health" == "healthy" ]]; then
        printf 'PostgreSQL is ready\n'
        return
      fi
    fi
    sleep 5
    elapsed=$((elapsed + 5))
    if (( elapsed % 30 == 0 )); then
      printf 'PostgreSQL is still recovering after %ss; continuing to wait...\n' "$elapsed"
    fi
  done

  printf 'error: PostgreSQL did not become ready within %ss\n' "$POSTGRES_STARTUP_TIMEOUT" >&2
  compose logs --tail 100 postgres >&2 || true
  exit 1
}

print_effective_postgres_settings() {
  printf 'Effective PostgreSQL runtime settings:\n'
  compose exec -T postgres psql -U fraud -d fraud -At -c "
    SELECT name || '=' || current_setting(name)
    FROM unnest(ARRAY[
      'synchronous_commit',
      'max_wal_size',
      'min_wal_size',
      'checkpoint_timeout',
      'checkpoint_completion_target',
      'wal_compression',
      'shared_buffers',
      'bgwriter_lru_maxpages'
    ]) AS settings(name);
  "
}

print_effective_valkey_settings() {
  local appendonly
  local appendfsync
  local save_schedule
  appendonly="$(compose exec -T valkey valkey-cli --raw CONFIG GET appendonly | sed -n '2p')"
  appendfsync="$(compose exec -T valkey valkey-cli --raw CONFIG GET appendfsync | sed -n '2p')"
  save_schedule="$(compose exec -T valkey valkey-cli --raw CONFIG GET save | sed -n '2p')"
  if [[ -z "$save_schedule" ]]; then
    save_schedule="disabled"
  fi
  printf 'Effective Valkey runtime settings: appendonly=%s appendfsync=%s save=%s\n' \
    "$appendonly" "$appendfsync" "$save_schedule"
}

require_command curl
require_command docker
require_command python3

if [[ -n "$ENV_FILE" && ! -f "$ENV_FILE" ]]; then
  printf 'error: replay environment file does not exist: %s\n' "$ENV_FILE" >&2
  exit 1
fi

if [[ "$REUSE_EXISTING_SETUP" == "true" && -z "$TENANT_ID" ]]; then
  printf 'error: TENANT_ID is required when REUSE_EXISTING_SETUP=true\n' >&2
  exit 1
fi

if [[ ! -d "$DATA_ROOT" ]]; then
  printf 'error: fraud data directory does not exist: %s\n' "$DATA_ROOT" >&2
  exit 1
fi
if [[ "$SKIP_SEED" != "true" && ! -d "$SEED_DATA_ROOT" ]]; then
  printf 'error: fraud seed data directory does not exist: %s\n' "$SEED_DATA_ROOT" >&2
  exit 1
fi
if [[ -n "$PROFILE_INPUT" && ! -f "$PROFILE_INPUT" ]]; then
  printf 'error: replay profile input does not exist: %s\n' "$PROFILE_INPUT" >&2
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
if [[ ! "$AGGREGATE_FACT_BACKFILL_TIMEOUT" =~ ^[0-9]+([.][0-9]+)?$ || "$AGGREGATE_FACT_BACKFILL_TIMEOUT" =~ ^0+([.]0+)?$ ]]; then
  printf 'error: AGGREGATE_FACT_BACKFILL_TIMEOUT must be a positive number; got %s\n' "$AGGREGATE_FACT_BACKFILL_TIMEOUT" >&2
  exit 1
fi
if [[ ! "$PUBLICATION_TIMEOUT" =~ ^[0-9]+([.][0-9]+)?$ || "$PUBLICATION_TIMEOUT" =~ ^0+([.]0+)?$ ]]; then
  printf 'error: PUBLICATION_TIMEOUT must be a positive number; got %s\n' "$PUBLICATION_TIMEOUT" >&2
  exit 1
fi
if [[ -n "$SEED_START_TIME" || -n "$SEED_END_TIME" ]]; then
  if [[ -z "$SEED_START_TIME" || -z "$SEED_END_TIME" ]]; then
    printf 'error: SEED_START_TIME and SEED_END_TIME must be supplied together\n' >&2
    exit 1
  fi
fi
if [[ "$PUBLISH_RULES_AFTER_SEED" != "true" && "$PUBLISH_RULES_AFTER_SEED" != "false" ]]; then
  printf 'error: PUBLISH_RULES_AFTER_SEED must be true or false; got %s\n' "$PUBLISH_RULES_AFTER_SEED" >&2
  exit 1
fi
if [[ "$PUBLISH_RULES_AFTER_SEED" == "true" && "$SKIP_SEED" == "true" ]]; then
  printf 'error: PUBLISH_RULES_AFTER_SEED=true requires a seed phase\n' >&2
  exit 1
fi
if [[ "$PUBLISH_RULES_AFTER_SEED" == "true" && "$REUSE_EXISTING_SETUP" == "true" ]]; then
  printf 'error: PUBLISH_RULES_AFTER_SEED=true requires a fresh model-only setup; REUSE_EXISTING_SETUP must be false\n' >&2
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
if [[ "$SKIP_SEED" != "true" && "$SKIP_SEED" != "false" ]]; then
  printf 'error: SKIP_SEED must be true or false; got %s\n' "$SKIP_SEED" >&2
  exit 1
fi

if [[ "$PRESERVE_RUNNING_VALKEY" != "true" && "$PRESERVE_RUNNING_VALKEY" != "false" ]]; then
  printf 'error: PRESERVE_RUNNING_VALKEY must be true or false; got %s\n' "$PRESERVE_RUNNING_VALKEY" >&2
  exit 1
fi
if [[ "$SKIP_SEED" == "true" && "$REUSE_EXISTING_SEED" == "true" ]]; then
  printf 'error: SKIP_SEED=true cannot be combined with REUSE_EXISTING_SEED=true\n' >&2
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
if [[ "$DECISION_MODE" == "sync" && "$LIVE_DECISION_MODE" == "async_only" ]]; then
  printf 'error: refusing a mislabeled sync replay because LIVE_DECISION_MODE=async_only would queue the decisions.\n' >&2
  printf 'set LIVE_DECISION_MODE=sync in the selected Docker environment file for a real synchronous run.\n' >&2
  exit 1
fi
NORMALIZED_LIVE_ASYNC_OBJECT_TYPES="$(printf '%s' "$LIVE_ASYNC_OBJECT_TYPES" | tr '[:upper:]' '[:lower:]')"
NORMALIZED_LIVE_ASYNC_OBJECT_TYPES=",${NORMALIZED_LIVE_ASYNC_OBJECT_TYPES},"
NORMALIZED_LIVE_ASYNC_OBJECT_TYPES="${NORMALIZED_LIVE_ASYNC_OBJECT_TYPES//[[:space:]]/}"
if [[ "$DECISION_MODE" == "sync" && "$NORMALIZED_LIVE_ASYNC_OBJECT_TYPES" == *",transactions,"* ]]; then
  printf 'error: refusing a mislabeled sync replay because LIVE_ASYNC_OBJECT_TYPES includes transactions.\n' >&2
  printf 'remove transactions from LIVE_ASYNC_OBJECT_TYPES in the selected Docker environment file.\n' >&2
  exit 1
fi
if [[ "$DECISION_MODE" == "async" && "$LIVE_DECISION_MODE" == "sync" ]]; then
  printf 'warning: DECISION_MODE=async still queues decisions even though LIVE_DECISION_MODE=sync.\n' >&2
fi
if [[ "$LIVE_ASYNC_FALLBACK_ENABLED" != "true" && "$LIVE_ASYNC_FALLBACK_ENABLED" != "false" ]]; then
  printf 'error: LIVE_ASYNC_FALLBACK_ENABLED must be true or false; got %s\n' "$LIVE_ASYNC_FALLBACK_ENABLED" >&2
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
for forwarded_port in "$FRONTEND_FORWARDED_PORT" "$DATA_MODEL_FORWARDED_PORT" "$INGESTION_FORWARDED_PORT" "$DECISION_ENGINE_FORWARDED_PORT"; do
  if [[ ! "$forwarded_port" =~ ^[0-9]+$ ]] || (( forwarded_port < 1 || forwarded_port > 65535 )); then
    printf 'error: forwarded ports must be integers from 1 through 65535; got %s\n' "$forwarded_port" >&2
    exit 1
  fi
done
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
  printf 'Replay configuration: transactions=%s offset=%s multiplier=%sx max_in_flight=%s decision_mode=%s live_decision_mode=%s read_mode=%s separate_read_pool=%s async_fallback=%s\n' \
    "$TRANSACTIONS" "$TRANSACTION_OFFSET" "$MULTIPLIER" "$MAX_IN_FLIGHT" "$DECISION_MODE" "$LIVE_DECISION_MODE" "$TENANT_DATA_READ_MODE" "$ENABLE_SEPARATE_READ_POOL" "$LIVE_ASYNC_FALLBACK_ENABLED"
fi
printf 'Replay tuning: rule_eval=%s scenario_eval=%s aggregate_remote=%s aggregate_query=%s read_db_max_conns=%s\n' \
  "$RULE_EVALUATION_CONCURRENCY" "$SCENARIO_EVALUATION_CONCURRENCY" "$AGGREGATE_REMOTE_CONCURRENCY_LIMIT" "$AGGREGATE_QUERY_CONCURRENCY_LIMIT" "$READ_DATABASE_MAX_CONNS"
if [[ "$SKIP_SEED" == "true" ]]; then
  printf 'Seed configuration: skipped; replay records will be the only transaction history\n'
else
  printf 'Seed configuration: data_root=%s start=%s end=%s batch_size=%s max_in_flight=%s request_timeout=%ss backfill_timeout=%ss rules_after_seed=%s reuse_existing_setup=%s reuse_existing_seed=%s\n' \
    "$SEED_DATA_ROOT" "${SEED_START_TIME:-all}" "${SEED_END_TIME:-all}" "$SEED_BATCH_SIZE" "$SEED_MAX_IN_FLIGHT" "$SEED_REQUEST_TIMEOUT" "$AGGREGATE_FACT_BACKFILL_TIMEOUT" "$PUBLISH_RULES_AFTER_SEED" "$REUSE_EXISTING_SETUP" "$REUSE_EXISTING_SEED"
fi
if [[ -n "$ENV_FILE" ]]; then
  printf 'Docker environment file: %s (service values are preserved unless explicitly overridden on the Make command line)\n' "$ENV_FILE"
fi

if [[ "$DECISION_MODE" == "async" || "$LIVE_DECISION_MODE" == "async_only" ]]; then
  START_DECISION_WORKER=1
fi

if [[ "$ENABLE_SEPARATE_READ_POOL" == "true" && -z "$READ_DATABASE_URL" ]]; then
  READ_DATABASE_URL="postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable"
fi

printf 'Preparing local fraud databases from existing images...\n'
export LIVE_DECISION_MODE="$LIVE_DECISION_MODE"
export DECISION_MODE="$DECISION_MODE"
export TRANSACTION_OFFSET="$TRANSACTION_OFFSET"
export EXPERIMENT_LABEL="$EXPERIMENT_LABEL"
export ENABLE_SEPARATE_READ_POOL="$ENABLE_SEPARATE_READ_POOL"
export LIVE_ASYNC_FALLBACK_ENABLED="$LIVE_ASYNC_FALLBACK_ENABLED"
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
export DATA_MODEL_URL
export INGESTION_URL
export DECISION_ENGINE_URL
compose up -d --no-build postgres
wait_for_postgres
print_effective_postgres_settings
compose run --rm data-model-migrate
compose run --rm ingestion-migrate
compose run --rm decision-engine-migrate
compose run --rm screening-migrate

printf 'Starting local fraud services from existing images...\n'
if [[ "$START_DECISION_WORKER" == "0" ]]; then
  printf 'Stopping the decision worker so queued async work does not distort the synchronous replay...\n'
  compose stop decision-engine-worker >/dev/null 2>&1 || true
fi
SERVICES=(
  data-model-service
  ingestion-service
  decision-engine-service
  data-model-worker
)
if [[ "$PRESERVE_RUNNING_VALKEY" == "true" ]]; then
  if [[ "$(compose exec -T valkey valkey-cli --raw PING 2>/dev/null || true)" != "PONG" ]]; then
    printf 'error: PRESERVE_RUNNING_VALKEY=true requires an already-running healthy Valkey container\n' >&2
    exit 1
  fi
  printf 'Preserving the running Valkey container; starting application services without dependencies...\n'
  compose up -d --no-build --no-deps "${SERVICES[@]}"
else
  compose up -d --no-build "${SERVICES[@]}"
fi

wait_for_service "data-model-service" "$DATA_MODEL_URL/readyz"
wait_for_service "ingestion-service" "$INGESTION_URL/readyz"
wait_for_service "decision-engine-service" "$DECISION_ENGINE_URL/readyz"
print_effective_valkey_settings

if [[ ! -x "$VENV_DIR/bin/python" ]]; then
  printf 'Creating replay Python environment...\n'
  python3 -m venv --system-site-packages "$VENV_DIR"
fi

if ! "$VENV_DIR/bin/python" -c 'import httpx, openpyxl' >/dev/null 2>&1; then
  "$VENV_DIR/bin/python" -m pip install -r "$SCRIPT_DIR/requirements.txt"
fi

if [[ "$START_DECISION_WORKER" == "1" ]]; then
  capture_async_backlog "$ASYNC_BACKLOG_BEFORE" "before"
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

if [[ "$START_DECISION_WORKER" == "1" ]]; then
  printf 'Starting the decision worker; any existing queued executions will continue processing.\n'
  compose up -d --no-build decision-engine-worker
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
    SAMPLE_ARGS+=(--transactions "$TRANSACTIONS" --offset "$TRANSACTION_OFFSET")
  fi
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay.local_sample "${SAMPLE_ARGS[@]}"

  if [[ "$SKIP_SEED" != "true" ]]; then
    SEED_SAMPLE_ARGS=(
      --base-manifest "$SCRIPT_DIR/manifests/fraud-data.json" \
      --data-root "$SEED_DATA_ROOT" \
      --reference-data-root "$DATA_ROOT" \
      --output-dir "$SEED_SAMPLE_DIR" \
      --output-manifest "$SEED_MANIFEST"
    )
    if [[ -n "$SEED_START_TIME" ]]; then
      SEED_SAMPLE_ARGS+=(--start-time "$SEED_START_TIME" --end-time "$SEED_END_TIME")
    else
      SEED_SAMPLE_ARGS+=(--transactions all)
    fi
    PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay.local_sample "${SEED_SAMPLE_ARGS[@]}"
  fi
)

if [[ "$REUSE_EXISTING_SETUP" == "true" ]]; then
  printf 'Verifying the existing local replay tenant and repairing aggregate facts if needed...\n'
else
  printf 'Creating a local replay tenant and loading reference data...\n'
fi
(
  cd "$BACKEND_DIR"
  SETUP_ARGS=(
    --manifest "$SMOKE_MANIFEST"
    --execute
    --tenant-name "Local Production Replay Smoke Test"
    --publication-timeout "$PUBLICATION_TIMEOUT"
    --aggregate-fact-backfill-timeout "$AGGREGATE_FACT_BACKFILL_TIMEOUT"
  )
  if [[ "$PUBLISH_RULES_AFTER_SEED" == "true" ]]; then
    SETUP_ARGS+=(--defer-scenarios)
  fi
  if [[ "$REUSE_EXISTING_SETUP" == "true" ]]; then
    SETUP_ARGS+=(--tenant-id "$TENANT_ID")
    SETUP_ARGS+=(--reuse-existing)
  elif [[ -n "$TENANT_ID" ]]; then
    TENANT_LOOKUP_STATUS="$(curl --silent --output /dev/null --write-out '%{http_code}' \
      "$DATA_MODEL_URL/v1/tenants/$TENANT_ID")"
    case "$TENANT_LOOKUP_STATUS" in
      200)
        SETUP_ARGS+=(--tenant-id "$TENANT_ID")
        ;;
      404)
        printf 'Requested tenant %s does not exist; creating a new tenant with a server-generated ID.\n' "$TENANT_ID" >&2
        ;;
      *)
        printf 'error: unable to verify requested tenant %s; data-model-service returned HTTP %s\n' \
          "$TENANT_ID" "$TENANT_LOOKUP_STATUS" >&2
        exit 1
        ;;
    esac
  fi
  if [[ -n "$PROFILE_INPUT" ]]; then
    SETUP_ARGS+=(--profile-input "$PROFILE_INPUT")
  fi
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay setup "${SETUP_ARGS[@]}"
) | tee "$SETUP_LOG"

TENANT_ID="$(awk '/^tenant:/ {print $2}' "$SETUP_LOG" | tail -n 1)"
if [[ -z "$TENANT_ID" ]]; then
  printf 'error: setup completed without returning a tenant ID\n' >&2
  exit 1
fi
SETUP_RUN_DIR="$(awk -F': ' '/^setup output:/ {print $2}' "$SETUP_LOG" | tail -n 1)"
if [[ -z "$SETUP_RUN_DIR" || ! -f "$SETUP_RUN_DIR/profile.json" ]]; then
  printf 'error: setup completed without a reusable source profile\n' >&2
  exit 1
fi
capture_fact_runtime_metrics "$SETUP_RUN_DIR" "before-seed"

SEED_RUN_DIR=""
if [[ "$SKIP_SEED" == "true" ]]; then
  printf 'Skipping pre-seeding for tenant %s; replay will use its existing transaction history.\n' "$TENANT_ID"
else
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
    PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -u -m production_replay seed "${SEED_ARGS[@]}"
  ) | tee "$SEED_LOG"

  SEED_RUN_DIR="$(awk -F': ' '/^seed output:/ {print $2}' "$SEED_LOG" | tail -n 1)"
  if [[ -z "$SEED_RUN_DIR" || ! -f "$SEED_RUN_DIR/summary.json" ]]; then
    printf 'error: transaction seed completed without a summary file\n' >&2
    exit 1
  fi
  capture_fact_runtime_metrics "$SEED_RUN_DIR" "after-seed"
fi

PUBLICATION_RUN_DIR=""
if [[ "$PUBLISH_RULES_AFTER_SEED" == "true" ]]; then
  printf 'Creating and publishing replay rules after the historical seed; index jobs and aggregate-fact backfills are timed separately...\n'
  (
    cd "$BACKEND_DIR"
    PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -u -m production_replay publish \
      --manifest "$SMOKE_MANIFEST" \
      --execute \
      --tenant-id "$TENANT_ID" \
      --publication-timeout "$PUBLICATION_TIMEOUT" \
      --aggregate-fact-backfill-timeout "$AGGREGATE_FACT_BACKFILL_TIMEOUT"
  ) | tee "$PUBLICATION_LOG"
  PUBLICATION_RUN_DIR="$(awk -F': ' '/^publication output:/ {print $2}' "$PUBLICATION_LOG" | tail -n 1)"
  if [[ -z "$PUBLICATION_RUN_DIR" || ! -f "$PUBLICATION_RUN_DIR/summary.json" ]]; then
    printf 'error: delayed rule publication completed without a timing summary\n' >&2
    exit 1
  fi
  capture_fact_runtime_metrics "$PUBLICATION_RUN_DIR" "after-publication-and-backfill"
fi

printf 'Building the frontend for replay tenant %s...\n' "$TENANT_ID"
export NEXT_PUBLIC_DATA_MODEL_TENANT_ID="$TENANT_ID"
compose build frontend
compose up -d --no-deps frontend
printf 'Frontend is available at http://127.0.0.1:%s for tenant %s\n' "$FRONTEND_FORWARDED_PORT" "$TENANT_ID"

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
  REPLAY_ARGS=(
    --manifest "$SMOKE_MANIFEST"
    --execute
    --tenant-id "$TENANT_ID"
    --multiplier "$MULTIPLIER"
    --max-in-flight "$MAX_IN_FLIGHT"
    --checkpoint-every "$CHECKPOINT_EVERY"
    --decision-mode "$DECISION_MODE"
    --async-wait-timeout-ms "$ASYNC_WAIT_TIMEOUT_MS"
    --async-callback-url "$ASYNC_CALLBACK_URL"
    --async-tracking-output "$ASYNC_TRACKING_LOG"
    --profile-input "$SETUP_RUN_DIR/profile.json"
  )
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -u -m production_replay run "${REPLAY_ARGS[@]}"
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
capture_fact_runtime_metrics "$RUN_DIR" "after-replay"
cp "$SETUP_RUN_DIR/ingestion-aggregate-fact-metrics-before-seed.json" "$RUN_DIR/" 2>/dev/null || true
cp "$SETUP_RUN_DIR/decision-runtime-metrics-before-seed.json" "$RUN_DIR/" 2>/dev/null || true
if [[ -n "$SEED_RUN_DIR" ]]; then
  cp "$SEED_RUN_DIR/ingestion-aggregate-fact-metrics-after-seed.json" "$RUN_DIR/" 2>/dev/null || true
  cp "$SEED_RUN_DIR/decision-runtime-metrics-after-seed.json" "$RUN_DIR/" 2>/dev/null || true
fi
if [[ "$SKIP_SEED" == "true" ]]; then
  "$VENV_DIR/bin/python" - "$RUN_DIR/seed-summary.json" <<'PY'
import json
import sys
from pathlib import Path

Path(sys.argv[1]).write_text(
    json.dumps(
        {
            "status": "skipped",
            "reason": "preseeding_disabled",
            "records": 0,
            "batches": 0,
            "decision_requests": 0,
        },
        indent=2,
    )
    + "\n",
    encoding="utf-8",
)
PY
else
  cp "$SEED_RUN_DIR/summary.json" "$RUN_DIR/seed-summary.json"
fi

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
        "preseeding_skipped": os.getenv("PRODUCTION_REPLAY_SKIP_SEED") == "true",
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

if [[ "$START_DECISION_WORKER" == "1" ]]; then
  capture_async_backlog "$ASYNC_BACKLOG_AFTER" "after"
  [[ -s "$ASYNC_BACKLOG_BEFORE" ]] && cp "$ASYNC_BACKLOG_BEFORE" "$RUN_DIR/async-backlog-before.json"
  [[ -s "$ASYNC_BACKLOG_AFTER" ]] && cp "$ASYNC_BACKLOG_AFTER" "$RUN_DIR/async-backlog-after.json"
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
if [[ "$SKIP_SEED" == "true" ]]; then
  printf 'Seed results: skipped\n'
else
  printf 'Seed results: %s\n' "$SEED_RUN_DIR"
fi
printf 'Results: %s\n' "$RUN_DIR"
if [[ "$REPLAY_STATUS" -ne 0 ]]; then
  exit "$REPLAY_STATUS"
fi
exit "$CALLBACK_REPORT_STATUS"
