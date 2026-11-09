#!/bin/sh
set -eu
test "$DECISION_MYSQL_REQUIRE_INTEGRATION" = 1
test "$DECISION_MYSQL_TEST_ADDRESS" = mysql:3306
test "$DECISION_ENGINE_REQUIRE_INTEGRATION" = 1
test "$DECISION_ENGINE_TEST_DATABASE_URL" = 'postgres://test_owner:isolated-test-only@postgres:5432/decision_test_admin?sslmode=disable'
go test -mod=readonly -race -p 2 -count=1 -timeout=600s -json ./internal/store/mysql ./internal/clients/workflowreceipt -run 'TestMySQLCaseWorkflowReceiptActualOwner|TestWorkflowReceiptActualOwner'
go vet -p 2 ./internal/clients/workflowreceipt ./internal/store/mysql
cd ../case-manager-service
go test -mod=readonly -race -p 2 -count=1 -timeout=600s -json ./internal/service ./internal/httpapi/...
CASE_MANAGER_TEST_DATABASE_URL="$DECISION_ENGINE_TEST_DATABASE_URL" go test -mod=readonly -race -p 2 -count=1 -timeout=600s -json ./internal/store/postgres -run 'TestPostgresWorkflowReceipt|TestManualCreateReceiptConcurrencyAndAuthorization|TestExplorationLinksPivotsPathsAndAuthorization|TestPostgresAuthorizationAndInvestigation|TestPhase4EvidenceAuthorizationCompletionAndRetention|TestPhase4BulkAuthorizationReplayAndAtomicAudit|TestPhase5AnalyticsReconciliationAndAuthorization'
go vet -p 2 ./internal/service ./internal/store/mysql ./mysqlworkflow ./internal/httpapi/...
