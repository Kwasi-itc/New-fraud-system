#!/bin/sh
# Execute in the private Compose test container. A nonzero result means the
# profile was not met or validation failed; retain JSON even on failure.
set -eu
test "${DECISION_MYSQL_TEST_ADDRESS:-}" = mysql:3306
test "${DECISION_MYSQL_REQUIRE_INTEGRATION:-}" = 1
case "${1:-}" in ramp|target|tuned) profile=$1 ;; *) echo 'choose ramp, target or tuned' >&2; exit 2 ;; esac
export DECISION_EVALUATION_LOAD_PROFILE="/backend/decision-engine-service/scripts/evaluation-load.shared-$profile.json"
export DECISION_EVALUATION_LOAD_OUTPUT="${2:?provide a new absolute output file}"
case "$DECISION_EVALUATION_LOAD_OUTPUT" in /*) ;; *) exit 2 ;; esac
go test -mod=readonly -p 2 -count=1 -timeout=600s -json ./internal/store/mysql -run '^TestManualEvaluationLoadAcceptance$'
