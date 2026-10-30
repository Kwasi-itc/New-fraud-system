#!/bin/sh
# Run only inside the explicitly isolated dual-database verifier.
set -eu
test "${INGESTION_REQUIRE_PARITY:-}" = 1
test "${INGESTION_REQUIRE_MYSQL_TEST:-}" = 1
test "${INGESTION_MYSQL_TEST_ADDRESS:-}" = mysql:3306
test "$#" = 1
mkdir "$1"
go version > "$1/environment.txt"
go test -json -race -mod=readonly -count=1 -timeout=240s ./internal/store/mysql \
  -run 'TestLiveParity|TestMembership|TestScalar|TestRecord' \
  > "$1/raw.jsonl" 2> "$1/stderr.log"
