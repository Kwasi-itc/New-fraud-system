#!/bin/sh
# Run inside the private compose test service, never against application data.
set -eu
test "$DECISION_MYSQL_REQUIRE_INTEGRATION" = 1
test "$DECISION_MYSQL_TEST_ADDRESS" = mysql:3306
go test -mod=readonly -race -p 2 -count=1 -timeout=120s -json ./internal/loadacceptance
go test -mod=readonly -race -p 2 -count=1 -timeout=600s -json ./internal/service ./internal/store/mysql -run 'TestDecision.*Cache|TestDecisionJobBatch|TestEvaluationProfile|TestEvaluationSQLProfile|Test.*LoadHarnessSmoke$|TestRequiredEffectProducerDoesNotLockUnrelatedOwners$|TestMySQLRequiredEffectJobsAtomicOwnerBindingAndReceipts$'
go vet -p 2 ./internal/loadacceptance ./internal/service ./internal/store/mysql
