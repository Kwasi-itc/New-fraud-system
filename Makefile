.PHONY: production-replay replay-production production-replay-async production-replay-ec2 replay-production-ec2 production-replay-ec2-async callback-server create-decisions create-async-decisions consolidate-postgres-db

TRANSACTIONS ?= 1000
MULTIPLIER ?= 3600
DATA_ROOT ?= /Users/kwilson/Desktop/ITC/fraud_data
MAX_IN_FLIGHT ?= 50
CHECKPOINT_EVERY ?= 100
DECISION_MODE ?= sync
ASYNC_WAIT_TIMEOUT_MS ?= 0
ASYNC_CALLBACK_PORT ?= 8099
ASYNC_CALLBACK_URL ?= http://host.docker.internal:$(ASYNC_CALLBACK_PORT)/callbacks/async-decisions
ASYNC_CALLBACK_WAIT_TIMEOUT ?= 120
CALLBACK_HOST ?= 0.0.0.0
CALLBACK_PORT ?= $(ASYNC_CALLBACK_PORT)
CALLBACK_OUTPUT ?= /tmp/fraud-data-local-async-callbacks.ndjson
CALLBACK_LOG ?= /tmp/fraud-data-local-callback-server.log
COUNT ?= 100
PROGRESS_EVERY ?= 100
DECISION_CREATE_OUTPUT ?= /tmp/fraud-decision-create-results.ndjson
DECISION_CREATE_EVENT_DATE ?=
DECISION_CREATE_RAW_TIMESTAMP ?=
DURATION ?=
HOURS ?=
DAYS ?=
WEEKS ?=
BASE_URL ?= http://ec2-54-246-247-31.eu-west-1.compute.amazonaws.com
DATA_MODEL_URL ?=
INGESTION_URL ?=
DECISION_ENGINE_URL ?=
DECISION_CREATE_URL ?= http://127.0.0.1:8082
TENANT_ID ?=
SCENARIO_ID ?=
TENANT_NAME ?= EC2 Production Replay Smoke Test
PUBLICATION_TIMEOUT ?= 900
SERVICE_AUTH_TOKEN ?=
TARGET_DB ?= fraud
BACKUP_DIR ?=
REUSE_DUMPS ?= true
DROP_LEGACY_DBS ?= true
ALLOW_EXISTING_TARGET ?= 0

production-replay:
	FRAUD_DATA_ROOT="$(DATA_ROOT)" \
	PRODUCTION_REPLAY_TRANSACTIONS="$(TRANSACTIONS)" \
	PRODUCTION_REPLAY_MULTIPLIER="$(MULTIPLIER)" \
	PRODUCTION_REPLAY_MAX_IN_FLIGHT="$(MAX_IN_FLIGHT)" \
	PRODUCTION_REPLAY_CHECKPOINT_EVERY="$(CHECKPOINT_EVERY)" \
	PRODUCTION_REPLAY_DECISION_MODE="$(DECISION_MODE)" \
	PRODUCTION_REPLAY_ASYNC_WAIT_TIMEOUT_MS="$(ASYNC_WAIT_TIMEOUT_MS)" \
	PRODUCTION_REPLAY_ASYNC_CALLBACK_URL="$(ASYNC_CALLBACK_URL)" \
	PRODUCTION_REPLAY_ASYNC_CALLBACK_PORT="$(ASYNC_CALLBACK_PORT)" \
	PRODUCTION_REPLAY_ASYNC_CALLBACK_WAIT_TIMEOUT="$(ASYNC_CALLBACK_WAIT_TIMEOUT)" \
	PRODUCTION_REPLAY_DURATION="$(DURATION)" \
	PRODUCTION_REPLAY_HOURS="$(HOURS)" \
	PRODUCTION_REPLAY_DAYS="$(DAYS)" \
	PRODUCTION_REPLAY_WEEKS="$(WEEKS)" \
	PRODUCTION_REPLAY_TENANT_ID="$(TENANT_ID)" \
	./backend/stress-tests/production_replay/run_local_replay.sh

replay-production: production-replay

production-replay-async: DECISION_MODE=async
production-replay-async: production-replay

production-replay-ec2:
	FRAUD_DATA_ROOT="$(DATA_ROOT)" \
	PRODUCTION_REPLAY_TRANSACTIONS="$(TRANSACTIONS)" \
	PRODUCTION_REPLAY_MULTIPLIER="$(MULTIPLIER)" \
	PRODUCTION_REPLAY_MAX_IN_FLIGHT="$(MAX_IN_FLIGHT)" \
	PRODUCTION_REPLAY_CHECKPOINT_EVERY="$(CHECKPOINT_EVERY)" \
	PRODUCTION_REPLAY_DECISION_MODE="$(DECISION_MODE)" \
	PRODUCTION_REPLAY_ASYNC_WAIT_TIMEOUT_MS="$(ASYNC_WAIT_TIMEOUT_MS)" \
	PRODUCTION_REPLAY_ASYNC_CALLBACK_URL="$(ASYNC_CALLBACK_URL)" \
	PRODUCTION_REPLAY_DURATION="$(DURATION)" \
	PRODUCTION_REPLAY_HOURS="$(HOURS)" \
	PRODUCTION_REPLAY_DAYS="$(DAYS)" \
	PRODUCTION_REPLAY_WEEKS="$(WEEKS)" \
	PRODUCTION_REPLAY_BASE_URL="$(BASE_URL)" \
	PRODUCTION_REPLAY_DATA_MODEL_URL="$(DATA_MODEL_URL)" \
	PRODUCTION_REPLAY_INGESTION_URL="$(INGESTION_URL)" \
	PRODUCTION_REPLAY_DECISION_ENGINE_URL="$(DECISION_ENGINE_URL)" \
	PRODUCTION_REPLAY_TENANT_ID="$(TENANT_ID)" \
	PRODUCTION_REPLAY_TENANT_NAME="$(TENANT_NAME)" \
	PRODUCTION_REPLAY_PUBLICATION_TIMEOUT="$(PUBLICATION_TIMEOUT)" \
	SERVICE_AUTH_TOKEN="$(SERVICE_AUTH_TOKEN)" \
	./backend/stress-tests/production_replay/run_remote_replay.sh

replay-production-ec2: production-replay-ec2

production-replay-ec2-async: DECISION_MODE=async
production-replay-ec2-async: production-replay-ec2

callback-server:
	@mkdir -p "$$(dirname "$(CALLBACK_OUTPUT)")" "$$(dirname "$(CALLBACK_LOG)")"
	@echo "callback server: http://$(CALLBACK_HOST):$(CALLBACK_PORT)"
	@echo "callback output: $(CALLBACK_OUTPUT)"
	@echo "callback log: $(CALLBACK_LOG)"
	PYTHONPATH=backend/stress-tests python3 -m production_replay.callback_server \
		--host "$(CALLBACK_HOST)" \
		--port "$(CALLBACK_PORT)" \
		--output "$(CALLBACK_OUTPUT)" \
		2>&1 | tee -a "$(CALLBACK_LOG)"

create-decisions:
	DECISION_CREATE_EVENT_DATE="$(DECISION_CREATE_EVENT_DATE)" \
	DECISION_CREATE_RAW_TIMESTAMP="$(DECISION_CREATE_RAW_TIMESTAMP)" \
	PYTHONPATH=backend/stress-tests python3 -m production_replay.create_decisions \
		--decision-engine-url "$(if $(DECISION_ENGINE_URL),$(DECISION_ENGINE_URL),$(DECISION_CREATE_URL))" \
		--tenant-id "$(TENANT_ID)" \
		--scenario-id "$(SCENARIO_ID)" \
		--count "$(COUNT)" \
		--wait-timeout-ms "$(ASYNC_WAIT_TIMEOUT_MS)" \
		--callback-url "$(ASYNC_CALLBACK_URL)" \
		--progress-every "$(PROGRESS_EVERY)" \
		--output "$(DECISION_CREATE_OUTPUT)"

create-async-decisions: create-decisions

consolidate-postgres-db:
	TARGET_DB="$(TARGET_DB)" \
	BACKUP_DIR="$(BACKUP_DIR)" \
	REUSE_DUMPS="$(REUSE_DUMPS)" \
	DROP_LEGACY_DBS="$(DROP_LEGACY_DBS)" \
	ALLOW_EXISTING_TARGET="$(ALLOW_EXISTING_TARGET)" \
	bash ./backend/tools/db_consolidation/consolidate_to_fraud_db.sh
