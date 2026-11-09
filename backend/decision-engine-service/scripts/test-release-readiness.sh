#!/bin/sh
set -eu
test "${DECISION_MYSQL_TEST_ADDRESS:-}" = mysql:3306
test "${DECISION_MYSQL_REQUIRE_INTEGRATION:-}" = 1
go test -mod=readonly -race -p 2 -count=1 -timeout=600s -json ./internal/mysqlrelease ./cmd/mysql-release
go test -mod=readonly -race -p 2 -count=1 -timeout=300s -json ./internal/store/mysql -run '^TestMySQLReleaseRuntimeGrantBoundaryAndWorkerDispatch$'
go vet -p 2 ./internal/mysqlrelease ./internal/store/mysql ./cmd/mysql-release
