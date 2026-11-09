#!/bin/sh
# Full evaluator/publication regression plus native consumer SQL contracts.
# Reuse the existing complete inventory, vet, race and raw evidence protocol.
set -eu
sh scripts/test-mysql.sh ./internal/service ./internal/runtime/ast_eval ./internal/migrationaudit
output=$(cat evidence/mysql-integration/latest.txt)
pattern=$(cat testdata/mysql-aggregate-native-pattern.txt)
printf 'go test -mod=readonly -race -p 1 -count=1 -timeout=5m -json -run %s ./internal/store/mysql\n' "$pattern" >> "$output/selection.txt"
set +e
go test -mod=readonly -race -p 1 -count=1 -timeout=5m -json -run "$pattern" ./internal/store/mysql >> "$output/raw.jsonl" 2>> "$output/stderr.log"
aggregate_exit=$?
printf '%s\n' "$aggregate_exit" > "$output/tests.exit"
printf 'Aggregate evidence: %s; native=%s\n' "$output" "$aggregate_exit"
exit "$aggregate_exit"
