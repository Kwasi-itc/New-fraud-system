set -eu
test "$DECISION_MYSQL_REQUIRE_INTEGRATION" = 1
test "$DECISION_ENGINE_REQUIRE_INTEGRATION" = 1
test "$DECISION_MYSQL_TEST_ADDRESS" = mysql:3306
go version
go test -mod=readonly -race -p 2 -count=1 -timeout=600s -json ./internal/mysqlruntime ./internal/store/mysql -run 'Test(SharedOwnerModelComposition|BrowserRealOwnerEditParity|ActualDecisionServiceRevocationBeforePersistence|ScreeningOwnerMutationsAndProjectionDrain)$'
cd ../data-model-service
export DATA_MODEL_MYSQL_TEST_ADDRESS=mysql:3306
export DATA_MODEL_MYSQL_TEST_ROOT_PASSWORD=isolated-test-root-only
export DATA_MODEL_REQUIRE_MYSQL_TEST=1
go test -mod=readonly -race -p 2 -count=1 -timeout=300s -json ./internal/store/mysql/metadata -run 'TestPhysicalCreationAllocated/service_allocated_native_index_worker_decision$'
