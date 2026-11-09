APP_NAME=marble-datamodel-service
DATA_MODEL_TEST_DATABASE_URL ?= postgres://datamodel:datamodel@localhost:5434/datamodel?sslmode=disable

.PHONY: run run-worker build test test-integration test-mysql test-mysql-workers test-mysql-endpoints migrate-up migrate-down docker-up docker-down

run:
	go run ./cmd/server

run-worker:
	go run ./cmd/worker

build:
	go build ./...

test:
	go test ./...

test-integration:
	DATA_MODEL_TEST_DATABASE_URL=$(DATA_MODEL_TEST_DATABASE_URL) go test -run Integration ./...

# Disposable fixture only; this does not use the development application DB.
test-mysql:
	docker compose -p data-model-mysql-tests -f compose.mysql-test.yaml up --abort-on-container-exit --exit-code-from test

test-mysql-workers:
	docker compose -p data-model-mysql-worker-tests -f compose.mysql-test.yaml -f compose.mysql-worker-test.yaml up --abort-on-container-exit --exit-code-from test

# Requires the local MySQL API on port 8080 and Docker for physical assertions.
test-mysql-endpoints:
	node scripts/mysql-endpoint-audit.mjs

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down 1

docker-up:
	docker compose up --build

docker-down:
	docker compose down -v
