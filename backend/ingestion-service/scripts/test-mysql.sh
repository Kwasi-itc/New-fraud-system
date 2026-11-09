#!/bin/sh
set -eu
test "${INGESTION_REQUIRE_MYSQL_TEST:-}" = 1
test "${INGESTION_MYSQL_TEST_ADDRESS:-}" = mysql:3306
test "${INGESTION_REQUIRE_PARITY:-}" = 1
output="/backend/ingestion-service/evidence/mysql-integration/$(date -u +%Y-%m-%dT%H-%M-%SZ)"
mkdir -p "$output"
printf '%s\n' "$output" > /tmp/ingestion-test-output
go version > "$output/environment.txt"
echo "Preparing required fixture exporter and API/worker/importer binaries"
(cd ../data-model-service && go build -mod=readonly -o /tmp/export-mysql-fixture ./cmd/export-mysql-fixture)
go build -race -mod=readonly -o /tmp/ingestion-mysql-api ./cmd/mysql-api
go build -race -mod=readonly -o /tmp/ingestion-mysql-worker ./cmd/mysql-worker
go build -race -mod=readonly -o /tmp/import-mysql-receipts ./cmd/import-mysql-receipts
echo "Running ingestion vet and complete MySQL/domain/HTTP/runtime/CLI tests"
if go vet -mod=readonly ./... > "$output/vet.log" 2>&1; then
    echo 0 > "$output/vet.exit"
else
    code=$?
    echo "$code" > "$output/vet.exit"
    cat "$output/vet.log"
    exit "$code"
fi
code=0
if [ -n "${INGESTION_TEST_PATTERN:-}" ]; then
    printf '%s\n' "$INGESTION_TEST_PATTERN" > "$output/selection.txt"
    set -- -run "$INGESTION_TEST_PATTERN"
else
    echo all > "$output/selection.txt"
    set --
fi
go test -mod=readonly -json -p=1 -race -count=1 -timeout=1200s \
    ./internal/domain/ingestion ./internal/httpapi/... ./internal/mysqlruntime \
    ./internal/store/mysql ./cmd/... "$@" > "$output/raw.jsonl" 2> "$output/stderr.log" || code=$?
if [ "$code" -eq 0 ]; then
    # Package-level skip events for commands without tests are not skipped tests.
    if grep '"Action":"skip"' "$output/raw.jsonl" | grep -q '"Test":'; then code=1; fi
    if grep -q 'WARNING: DATA RACE' "$output/raw.jsonl"; then code=1; fi
    if ! grep -q '"Action":"pass".*"Test":"Test' "$output/raw.jsonl"; then code=1; fi
fi
echo "$code" > "$output/tests.exit"
echo "Ingestion tests exit=$code; evidence: $output"
if [ "$code" -ne 0 ]; then tail -n 60 "$output/raw.jsonl"; fi
exit "$code"
