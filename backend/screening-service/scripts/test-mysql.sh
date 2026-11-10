#!/bin/sh
set -eu
test "${SCREENING_MYSQL_TEST_ADDRESS:-}" = mysql:3306
test "${SCREENING_MYSQL_REFERENCE_ADDRESS:-}" = mysql-reference:3306
test "${SCREENING_REQUIRE_MYSQL_TEST:-}" = 1
test -n "${SCREENING_TEST_DATABASE_URL:-}"
output="evidence/mysql-integration/$(date -u +%Y-%m-%dT%H-%M-%SZ)"
mkdir -p "$output"
printf '%s\n' "$output" > evidence/mysql-integration/latest.txt
go version > "$output/environment.txt"
mkdir -p /tmp/screening-regression-binaries
(cd ../case-manager-service && go build -mod=readonly -o /tmp/screening-regression-binaries/case-api ./cmd/server) > "$output/binaries.log" 2>&1 || { cat "$output/binaries.log"; exit 1; }
(cd ../decision-engine-service && go build -mod=readonly -o /tmp/screening-regression-binaries/decision-api ./cmd/server) >> "$output/binaries.log" 2>&1 || { cat "$output/binaries.log"; exit 1; }
(cd ../decision-engine-service && go build -mod=readonly -o /tmp/screening-regression-binaries/decision-worker ./cmd/worker) >> "$output/binaries.log" 2>&1 || { cat "$output/binaries.log"; exit 1; }
go build -mod=readonly -o /tmp/screening-regression-binaries/screening-api ./cmd/server >> "$output/binaries.log" 2>&1 || { cat "$output/binaries.log"; exit 1; }
go build -mod=readonly -o /tmp/screening-regression-binaries/screening-worker ./cmd/worker >> "$output/binaries.log" 2>&1 || { cat "$output/binaries.log"; exit 1; }
export CASE_MANAGER_INTEGRATION_BINARY=/tmp/screening-regression-binaries/case-api
export DECISION_INTEGRATION_BINARY=/tmp/screening-regression-binaries/decision-api
export DECISION_WORKER_INTEGRATION_BINARY=/tmp/screening-regression-binaries/decision-worker
export SCREENING_INTEGRATION_BINARY=/tmp/screening-regression-binaries/screening-api
export SCREENING_WORKER_INTEGRATION_BINARY=/tmp/screening-regression-binaries/screening-worker
go test -mod=readonly -list . -json -p 1 ./... > "$output/inventory.jsonl" 2> "$output/inventory-stderr.log" || { cat "$output/inventory-stderr.log"; exit 1; }
code=0
go vet -mod=readonly ./... > "$output/vet.log" 2>&1 || code=$?
printf '%s\n' "$code" > "$output/vet.exit"
if [ "$code" -ne 0 ]; then cat "$output/vet.log"; exit "$code"; fi
go test -mod=readonly -race -p 1 -count=1 -timeout=20m -json ./... > "$output/raw.jsonl" 2> "$output/stderr.log" || code=$?
printf '%s\n' "$code" > "$output/tests.exit"
printf 'Screening native/PG evidence: %s; exit=%s\n' "$output" "$code"
if [ "$code" -ne 0 ]; then tail -n 30 "$output/raw.jsonl"; fi
exit "$code"
