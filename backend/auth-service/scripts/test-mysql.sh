#!/bin/sh
set -eu
test "${AUTH_MYSQL_TEST_ADDRESS:-}" = mysql:3306
test "${AUTH_REQUIRE_MYSQL_TEST:-}" = 1
output="evidence/mysql-integration/$(date -u +%Y-%m-%dT%H-%M-%SZ)"
mkdir -p "$output"
printf '%s\n' "$output" > evidence/mysql-integration/latest.txt
go version > "$output/environment.txt"
go build -mod=readonly -race -o /tmp/auth-mysql-api ./cmd/mysql-api
export AUTH_MYSQL_PROCESS_BINARY=/tmp/auth-mysql-api
go test -mod=readonly -list . -json -p 1 ./... > "$output/inventory.jsonl" 2> "$output/inventory-stderr.log"
code=0
go vet -mod=readonly ./... > "$output/vet.log" 2>&1 || code=$?
printf '%s\n' "$code" > "$output/vet.exit"
if [ "$code" -ne 0 ]; then cat "$output/vet.log"; exit "$code"; fi
go test -mod=readonly -race -p 1 -count=1 -timeout=15m -json ./... > "$output/raw.jsonl" 2> "$output/stderr.log" || code=$?
printf '%s\n' "$code" > "$output/tests.exit"
cd ../mysqlstore
shared=0
go vet -mod=readonly ./... > "/backend/auth-service/$output/mysqlstore-vet.log" 2>&1 || shared=$?
printf '%s\n' "$shared" > "/backend/auth-service/$output/mysqlstore-vet.exit"
go test -mod=readonly -race -p 1 -count=1 -json ./... > "/backend/auth-service/$output/mysqlstore-tests.jsonl" 2> "/backend/auth-service/$output/mysqlstore-stderr.log" || shared=$?
printf '%s\n' "$shared" > "/backend/auth-service/$output/mysqlstore-tests.exit"
cd ../auth-service
printf 'Auth native evidence: %s; exit=%s\n' "$output" "$code"
if [ "$code" -ne 0 ]; then tail -n 35 "$output/raw.jsonl"; fi
test "$shared" -eq 0
exit "$code"
