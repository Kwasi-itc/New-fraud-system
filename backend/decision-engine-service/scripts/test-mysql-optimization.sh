#!/bin/sh
# The existing runner collects full inventory, full-module vet and race JSON.
# Add the focused SQL owner check without rerunning unrelated store fixtures.
set -eu
if [ "$#" -eq 0 ]; then set -- ./internal/service ./internal/mysqlexecution ./internal/mysqlapi ./internal/mysqlruntime; fi
sh scripts/test-mysql.sh "$@"
output=$(cat evidence/mysql-integration/latest.txt)
pattern='^Test(EvaluationRoutingFreshCoherentAuthorizedAndBounded|MySQLProviderScoringResultIsIdempotentAndGenerationFenced)$'
printf 'go test -mod=readonly -race -p 1 -count=1 -timeout=5m -json -run %s ./internal/store/mysql\n' "$pattern" >> "$output/selection.txt"
set +e
go test -mod=readonly -race -p 1 -count=1 -timeout=5m -json -run "$pattern" ./internal/store/mysql >> "$output/raw.jsonl" 2>> "$output/stderr.log"
routing_exit=$?
printf '%s\n' "$routing_exit" > "$output/tests.exit"
set -e
absolute_output=$(cd "$output" && pwd)
cd ../mysqlstore
set +e
go vet -p 2 ./... > "$absolute_output/mysqlstore-vet.log" 2>&1
shared_vet=$?
go test -mod=readonly -race -p 1 -count=1 -json -run '^Test(Configuration|InterpolationIsExplicitAndPinsSafeCharset)$' . > "$absolute_output/mysqlstore-tests.jsonl" 2> "$absolute_output/mysqlstore-stderr.log"
shared_tests=$?
printf '%s\n' "$shared_vet" > "$absolute_output/mysqlstore-vet.exit"
printf '%s\n' "$shared_tests" > "$absolute_output/mysqlstore-tests.exit"
printf 'Optimization evidence: %s; routing=%s shared_vet=%s shared_tests=%s\n' "$absolute_output" "$routing_exit" "$shared_vet" "$shared_tests"
test "$routing_exit" -eq 0 && test "$shared_vet" -eq 0 && test "$shared_tests" -eq 0
