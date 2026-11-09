#!/bin/sh
# Full evaluator/composition regression, then native changed-owner contracts.
# Historical timing workloads run separately while other fixture jobs are idle.
set -eu
sh scripts/test-mysql.sh ./internal/service ./internal/runtime/ast_eval ./internal/mysqlexecution ./internal/mysqlruntime ./internal/migrationaudit
output=$(cat evidence/mysql-integration/latest.txt)
pattern=$(cat testdata/mysql-throughput-native-pattern.txt)
printf 'go test -mod=readonly -race -p 1 -count=1 -timeout=15m -json -run %s ./internal/store/mysql\n' "$pattern" >> "$output/selection.txt"
set +e
go test -mod=readonly -race -p 1 -count=1 -timeout=15m -json -run "$pattern" ./internal/store/mysql >> "$output/raw.jsonl" 2>> "$output/stderr.log"
native_exit=$?
printf '%s\n' "$native_exit" > "$output/tests.exit"
printf 'Throughput regression evidence: %s; native=%s\n' "$output" "$native_exit"
exit "$native_exit"
