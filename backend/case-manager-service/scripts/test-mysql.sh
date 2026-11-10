#!/bin/sh
set -eu
test "${CASE_MYSQL_TEST_ADDRESS:-}" = mysql:3306
test "${CASE_MYSQL_REFERENCE_ADDRESS:-}" = mysql-reference:3306
test "${CASE_REQUIRE_MYSQL_TEST:-}" = 1
test -n "${CASE_MANAGER_TEST_DATABASE_URL:-}"
output="evidence/mysql-integration/$(date -u +%Y-%m-%dT%H-%M-%SZ)"
mkdir -p "$output"
printf '%s\n' "$output" > evidence/mysql-integration/latest.txt
go version > "$output/environment.txt"
go test -mod=readonly -list . -json -p 1 ./... > "$output/inventory.jsonl" 2> "$output/inventory-stderr.log" || { cat "$output/inventory-stderr.log"; tail -n 30 "$output/inventory.jsonl"; exit 1; }
code=0
go vet -mod=readonly ./... > "$output/vet.log" 2>&1 || code=$?
printf '%s\n' "$code" > "$output/vet.exit"
if [ "$code" -ne 0 ]; then cat "$output/vet.log"; exit "$code"; fi
go test -mod=readonly -race -p 1 -count=1 -timeout=15m -json ./... > "$output/raw.jsonl" 2> "$output/stderr.log" || code=$?
printf '%s\n' "$code" > "$output/tests.exit"
printf 'Case MySQL/PG evidence: %s; exit=%s\n' "$output" "$code"
if [ "$code" -ne 0 ]; then tail -n 30 "$output/raw.jsonl"; fi
exit "$code"
