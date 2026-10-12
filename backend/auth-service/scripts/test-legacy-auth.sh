#!/bin/sh
# Run from any directory in a Go environment. No application database is used.
set -eu
backend=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
for service in data-model-service ingestion-service decision-engine-service screening-service; do
  (
    cd "$backend/$service"
    go test -count=1 -p 2 ./internal/app ./internal/httpapi
  )
done
