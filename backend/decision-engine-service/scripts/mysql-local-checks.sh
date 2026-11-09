#!/bin/sh
# Run inside compose.mysql.test.yaml's disposable test service.
set -eu
go vet ./cmd/mysql-api ./cmd/mysql-dev-install ./internal/mysqlruntime ./internal/mysqlexecution ./internal/mysqlrelease ./internal/httpapi
go test -mod=readonly -race -p 1 -count=1 -timeout=240s -v \
  -run 'TestLocal|TestMissingRuntimeProviders|TestMySQLApplicationActualRoutes|TestMySQLReleaseRuntimeGrant' \
  ./internal/mysqlruntime ./internal/mysqlexecution ./internal/store/mysql
