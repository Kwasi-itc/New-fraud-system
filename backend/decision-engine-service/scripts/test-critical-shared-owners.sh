#!/bin/sh
set -eu
test "${DECISION_MYSQL_TEST_ADDRESS:-}" = mysql:3306
test "${DECISION_MYSQL_REQUIRE_INTEGRATION:-}" = 1
test "${DECISION_ENGINE_REQUIRE_INTEGRATION:-}" = 1
go test -mod=readonly -race -p 2 -count=1 -timeout=600s -json ./internal/mysqlruntime ./internal/store/mysql -run 'TestSharedOwnerModelComposition|TestMySQLVerifiedSessionComposition|TestBrowserRealOwnerEditParity|TestActualDecisionServiceRevocationBeforePersistence|TestScreeningOwnerMutationsAndProjectionDrain'
