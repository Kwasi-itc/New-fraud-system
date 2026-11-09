#!/bin/sh
# Entire decision module against private, disposable Compose database fixtures.
set -eu
test "${DECISION_MYSQL_TEST_ADDRESS:-}" = mysql:3306
test "${DECISION_MYSQL_REQUIRE_INTEGRATION:-}" = 1
test "${DECISION_ENGINE_REQUIRE_INTEGRATION:-}" = 1
case "${DECISION_ENGINE_TEST_DATABASE_URL:-}" in
  postgres://test_owner:*@postgres:5432/decision_test_admin?sslmode=disable) ;;
  *) echo 'Private differential Compose fixture required' >&2; exit 1 ;;
esac
export DECISION_TEST_DATABASE_URL="$DECISION_ENGINE_TEST_DATABASE_URL"
export GIN_MODE=release
pattern='.'
if [ "${1:-}" = --run-file ]; then
  pattern=$(cat "$2")
  shift 2
fi
if [ "$#" -eq 0 ]; then set -- ./...; fi
for package in "$@"; do
  case "$package" in ./...|./internal/*|./cmd/*) ;; *) echo 'Local module packages required' >&2; exit 1 ;; esac
done
output="evidence/mysql-integration/$(date -u +%Y-%m-%dT%H-%M-%SZ)"
mkdir -p "$output"
printf '%s\n' "$output" > evidence/mysql-integration/latest.txt
printf '%s\n' "${DECISION_MYSQL_TEST_INTERPOLATE:-0}" > "$output/parameter-interpolation.txt"
printf 'go test -mod=readonly -race -p 1 -count=1 -timeout=45m -json -run %s %s\n' "$pattern" "$*" > "$output/selection.txt"
go version > "$output/toolchain.txt"
go test -mod=readonly -race -p 1 -json -list '^Test' ./... > "$output/inventory.jsonl"
set +e
go vet -p 2 ./... > "$output/vet.log" 2>&1
vet_exit=$?
printf '%s\n' "$vet_exit" > "$output/vet.exit"
go test -mod=readonly -race -p 1 -count=1 -timeout=45m -json -run "$pattern" "$@" > "$output/raw.jsonl" 2> "$output/stderr.log"
test_exit=$?
printf '%s\n' "$test_exit" > "$output/tests.exit"
printf 'Evidence: %s; tests=%s; vet=%s\n' "$output" "$test_exit" "$vet_exit"
test "$vet_exit" -eq 0 && test "$test_exit" -eq 0
