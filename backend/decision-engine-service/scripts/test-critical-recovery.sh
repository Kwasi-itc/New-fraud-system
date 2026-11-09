#!/bin/sh
# Private Compose fixtures only; actual event receiver acceptance is separate.
set -eu
test "${DECISION_MYSQL_TEST_ADDRESS:-}" = mysql:3306
test "${DECISION_MYSQL_REQUIRE_INTEGRATION:-}" = 1
sh scripts/test-scoring-deferral.sh
go test -mod=readonly -race -p 2 -count=1 -timeout=600s -json ./internal/mysqlexecution -run 'TestMySQLCallbackDispatch|TestMySQLEvaluationWorkerScopeAndCancellation|TestDispatchReconciliationNeverBlindlyRepeats'
go test -mod=readonly -race -p 2 -count=1 -timeout=600s -json ./internal/store/mysql -run 'TestMySQLEffectCancellation|TestMySQLExecutionCancellation|TestMySQLCallbackCancellation|TestMySQLReceiptAwareMaintenance'
go vet -p 2 ./internal/mysqlexecution ./internal/mysqlruntime ./internal/store/mysql
