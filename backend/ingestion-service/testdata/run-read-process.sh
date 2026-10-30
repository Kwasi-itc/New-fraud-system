#!/bin/sh
set -eu
test "${INGESTION_REQUIRE_MYSQL_TEST:-}" = 1
test "${INGESTION_MYSQL_TEST_ADDRESS:-}" = mysql:3306
test -n "${INGESTION_DATA_MODEL_FIXTURE_EXPORTER:-}"
test "$#" = 1
mkdir "$1"
go build -race -mod=readonly -o /tmp/ingestion-mysql-api ./cmd/mysql-api
go build -race -mod=readonly -o /tmp/ingestion-mysql-worker ./cmd/mysql-worker
go test -json -race -mod=readonly -count=1 -timeout=240s ./internal/httpapi/... ./internal/mysqlruntime \
  > "$1/handlers.jsonl" 2> "$1/handlers-stderr.log"
go test -json -race -mod=readonly -count=1 -timeout=360s ./internal/store/mysql \
  -run 'TestProcessRead|TestProcessCrossTenant|TestProcessConcurrentReads|TestProcessPublicationRace|TestProcessAPIRuntimeGrants|TestProcessUploadLifecycle|TestFullSharedReadPlans' \
  > "$1/raw.jsonl" 2> "$1/stderr.log"
go vet -mod=readonly ./internal/store/mysql ./internal/httpapi/... ./internal/mysqlruntime
