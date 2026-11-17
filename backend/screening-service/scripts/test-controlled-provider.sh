#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
: "${SCREENING_TEST_DATABASE_URL:?Use an isolated disposable PostgreSQL server; never the application database}"
bin_dir=$(mktemp -d /tmp/provider-phase1-bin.XXXXXX)
case "$bin_dir" in /tmp/provider-phase1-bin.*) ;; *) exit 1 ;; esac
trap 'rm -rf -- "$bin_dir"' EXIT
export CGO_ENABLED=0
go build -mod=readonly -o "$bin_dir/screening" ./cmd/server
go build -mod=readonly -o "$bin_dir/screening-worker" ./cmd/worker
go build -mod=readonly -o "$bin_dir/source-import" ./cmd/source-import
go build -mod=readonly -o "$bin_dir/source-worker" ./cmd/source-worker
go build -mod=readonly -o "$bin_dir/source-admin" ./cmd/source-admin
export SCREENING_INTEGRATION_BINARY="$bin_dir/screening"
export SCREENING_WORKER_INTEGRATION_BINARY="$bin_dir/screening-worker"
export SOURCE_WORKER_INTEGRATION_BINARY="$bin_dir/source-worker"
export SOURCE_ADMIN_INTEGRATION_BINARY="$bin_dir/source-admin"
if [[ ${1:-focused} == full ]]; then
  (cd ../decision-engine-service && go build -mod=readonly -o "$bin_dir/decision" ./cmd/server && go build -mod=readonly -o "$bin_dir/decision-worker" ./cmd/worker)
  (cd ../case-manager-service && go build -mod=readonly -o "$bin_dir/case" ./cmd/server)
  export DECISION_INTEGRATION_BINARY="$bin_dir/decision"
  export DECISION_WORKER_INTEGRATION_BINARY="$bin_dir/decision-worker"
  export CASE_MANAGER_INTEGRATION_BINARY="$bin_dir/case"
  test_args=()
  if [[ -n ${CONTROLLED_TEST_RUN:-} ]]; then
    test_args+=(-run "$CONTROLLED_TEST_RUN")
  fi
  go test -mod=readonly -count=1 -json "${test_args[@]}" ./...
else
  go test -mod=readonly -count=1 -json ./internal/provideraccess ./internal/sourcedata ./internal/matching ./internal/clients/provider ./internal/store/postgres -run 'TestScope|TestControlled|TestStrictProvider|TestRegistryConfiguration|TestExplainableNameScoring|TestInternalProvider(FiveOperationsAndIsolation|ThroughAuthenticatedAPIWorkerAndReview|FuzzyEvidenceAndLifecycle|FuzzyOverflowIsPartial|MaintenanceDoesNotInventPrivateJobAuthority)$'
fi
