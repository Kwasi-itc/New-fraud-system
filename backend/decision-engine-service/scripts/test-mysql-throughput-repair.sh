#!/bin/sh
set -eu
sh scripts/test-mysql.sh --run-file testdata/mysql-throughput-wait-repair-pattern.txt ./internal/service
output=$(cat evidence/mysql-integration/latest.txt)
pattern=$(cat testdata/mysql-throughput-native-pattern.txt)
printf 'go test -mod=readonly -race -p 1 -count=1 -timeout=15m -json -run %s ./internal/store/mysql\n' "$pattern" >> "$output/selection.txt"
set +e
go test -mod=readonly -race -p 1 -count=1 -timeout=15m -json -run "$pattern" ./internal/store/mysql >> "$output/raw.jsonl" 2>> "$output/stderr.log"
native_exit=$?
printf '%s\n' "$native_exit" > "$output/tests.exit"
absolute_output=$(cd "$output" && pwd)
cd ../mysqlstore
go vet ./... > "$absolute_output/mysqlstore-vet.log" 2>&1
shared_vet=$?
go test -mod=readonly -race -p 1 -count=1 -json ./... > "$absolute_output/mysqlstore-tests.jsonl" 2> "$absolute_output/mysqlstore-stderr.log"
shared_tests=$?
printf '%s\n' "$shared_vet" > "$absolute_output/mysqlstore-vet.exit"
printf '%s\n' "$shared_tests" > "$absolute_output/mysqlstore-tests.exit"
printf 'Throughput repair evidence: %s; native=%s shared_vet=%s shared_tests=%s\n' "$absolute_output" "$native_exit" "$shared_vet" "$shared_tests"
test "$native_exit" -eq 0 && test "$shared_vet" -eq 0 && test "$shared_tests" -eq 0
