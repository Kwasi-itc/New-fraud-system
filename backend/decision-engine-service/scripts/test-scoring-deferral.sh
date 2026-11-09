#!/bin/sh
set -eu
test "${DECISION_MYSQL_TEST_ADDRESS:-}" = mysql:3306
test "${DECISION_MYSQL_REQUIRE_INTEGRATION:-}" = 1
go test -mod=readonly -race -p 2 -count=1 -timeout=600s -json \
  ./internal/store/mysql ./internal/mysqlexecution ./internal/mysqlruntime \
  -run 'TestMySQLAdditionalScoringDeferral|TestMySQLDecisionEffectsUncertainCommit|TestDeferredScoringRejectsConfigurationAndPropagatesFailures|TestMySQLApplicationActualRoutesScopesReadinessAndWorkerLifecycle|TestMySQLRequiredEffectJobsAtomicOwnerBindingAndReceipts'
go vet -p 2 ./internal/store/mysql ./internal/mysqlexecution ./internal/mysqlruntime
