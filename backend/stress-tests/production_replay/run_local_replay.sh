#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKSPACE_DIR="$(cd "$BACKEND_DIR/.." && pwd)"
DATA_ROOT="${FRAUD_DATA_ROOT:-/Users/kwilson/Desktop/ITC/fraud_data}"
VENV_DIR="${PRODUCTION_REPLAY_VENV:-/tmp/fraud-production-replay-venv}"
SMOKE_MANIFEST="/tmp/fraud-data-local-smoke.json"
SAMPLE_DIR="/tmp/fraud-data-local-sample"
SETUP_LOG="/tmp/fraud-data-local-setup.log"
REPLAY_LOG="/tmp/fraud-data-local-replay.log"
COMPOSE_OVERRIDE="$SCRIPT_DIR/docker-compose.local-replay.yml"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'error: required command not found: %s\n' "$1" >&2
    exit 1
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

printf 'Starting local fraud services from existing images...\n'
docker compose --project-directory "$WORKSPACE_DIR" \
  --file "$WORKSPACE_DIR/docker-compose.yml" \
  --file "$COMPOSE_OVERRIDE" \
  up -d --no-build \
  data-model-service \
  ingestion-service \
  decision-engine-service \
  data-model-worker

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

(
  cd "$BACKEND_DIR"
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay.local_sample \
    --base-manifest "$SCRIPT_DIR/manifests/fraud-data.json" \
    --data-root "$DATA_ROOT" \
    --output-dir "$SAMPLE_DIR" \
    --output-manifest "$SMOKE_MANIFEST"
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

printf 'Replaying 1,000 production-format transactions...\n'
set +e
(
  cd "$BACKEND_DIR"
  PYTHONPATH=stress-tests "$VENV_DIR/bin/python" -m production_replay run \
    --manifest "$SMOKE_MANIFEST" \
    --execute \
    --tenant-id "$TENANT_ID" \
    --multiplier 3600 \
    --max-in-flight 50 \
    --checkpoint-every 100
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
exit "$REPLAY_STATUS"
