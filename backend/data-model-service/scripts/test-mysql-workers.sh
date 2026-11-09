#!/bin/sh
set -eu

# The decision journey also compares behavior with a disposable PostgreSQL
# source. These defaults name only the companion fixture in the worker overlay.
export DECISION_ENGINE_TEST_DATABASE_URL="${DECISION_ENGINE_TEST_DATABASE_URL:-postgres://worker_test:isolated-worker-test-only@postgres:5432/decision_test_worker?sslmode=disable}"
export DECISION_ENGINE_REQUIRE_INTEGRATION=1

# The Compose fixture guards in the Go helpers reject application databases.
# JSON output preserves individual assertions and skips for the audit evidence.
go mod download
go vet ./internal/mysqlruntime ./internal/worker ./internal/store/mysql/... ./cmd/mysql-worker ./cmd/reconcile-physical-mysql ./cmd/inspect-deliveries-mysql ./cmd/redrive-delivery-mysql

go test -mod=readonly -p=1 -race -count=1 -timeout=300s -json \
  ./internal/mysqlruntime ./internal/worker ./internal/store/mysql ./cmd/mysql-worker \
  ./cmd/reconcile-physical-mysql ./cmd/inspect-deliveries-mysql ./cmd/redrive-delivery-mysql

go test -mod=readonly -p=1 -race -count=1 -timeout=300s -json \
  -run '^(TestIndexDeliveryStore|TestIndexDeliveryLostCommitAcknowledgement|TestLeasedIndexExecution|TestMySQLDeliveryPolling|TestMySQLDeliveryMaintenance|TestDeliveryRetryErrorHistory|TestDeliveryInspection|TestDeliveryRedrive|TestPublishedNativeIndexCompletion|TestPhysicalReconciliationPages)$' \
  ./internal/store/mysql/metadata

# Select the real native DDL worker scenarios, including the cross-owner HTTP
# journey. Unrelated schema-authoring scenarios retain their separate suite.
go test -mod=readonly -p=1 -race -count=1 -timeout=300s -json \
  -run '^TestPhysicalCreationAllocated$/^service_allocated_native_index_worker' \
  ./internal/store/mysql/metadata

(
  cd ../mysqlstore
  go test -mod=readonly -p=1 -race -count=1 -timeout=120s -json ./workers
)
