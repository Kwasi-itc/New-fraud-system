#!/bin/sh
# Both databases must be the private Compose fixtures; no application DSN.
set -eu
test "$DECISION_MYSQL_REQUIRE_INTEGRATION" = 1
test "$DECISION_MYSQL_TEST_ADDRESS" = mysql:3306
test "$DECISION_ENGINE_REQUIRE_INTEGRATION" = 1
test "$DECISION_ENGINE_TEST_DATABASE_URL" = 'postgres://test_owner:isolated-test-only@postgres:5432/decision_test_admin?sslmode=disable'
sh scripts/test-load-fixtures.sh
go test -mod=readonly -race -p 2 -count=1 -timeout=600s -json ./internal/service ./internal/store/mysql -run 'TestDecisionUsesConfiguredNarrowTransactionsWithoutFallback$|TestDecisionServiceEvaluateScenarioReturns|TestOutboxPublicationPolicyDoesNotSkipDecisions$|TestEnabledOutboxPropagatesWriteAndQueueErrors$|TestIntegrationDecisionOutboxPublicationAtomicity$|TestMySQLCombinedTransactionRequiredJobsCapabilitiesAndRollback$|TestMySQLActualDecisionServiceThresholdsEvidenceEffectsAndRequiredJobRollback$|TestActualDecisionServicePostgresMySQLThresholdEvidenceParity$|TestActualDecisionServicePostgresMySQLConfiguredEffectParity$'
