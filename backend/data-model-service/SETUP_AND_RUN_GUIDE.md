# Setup And Run Guide

This guide explains how to set up, run, verify, and stop the standalone data model service in `new/backend/data-model-service`, including the async index worker.

## What you need

- Go installed locally
- optional: [mise](https://mise.jdx.dev/) if you want `.env` loaded externally like `api/`
- Docker Desktop or another local Docker runtime
- PostgreSQL available through Docker using the provided `docker-compose.yml`
- PowerShell if you are following the Windows examples below

## Default local ports

- PostgreSQL: `5434`
- HTTP API: `8080`
- optional full service container: `8088`

## Important local paths

Repo root:

- `C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble`

Service root:

- `C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service`

## Environment variables

Minimum required:

- `DATABASE_URL`

Common local values:

```env
DATABASE_URL=postgres://datamodel:datamodel@localhost:5434/datamodel?sslmode=disable
PORT=8080
SERVICE_AUTH_MODE=disabled
SERVICE_AUTH_TOKEN=
GIN_MODE=debug
LOG_LEVEL=debug
```

If you want API auth enabled:

```env
SERVICE_AUTH_MODE=token
SERVICE_AUTH_TOKEN=change-me
```

## Step 1: go to the service directory

```powershell
Set-Location "C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service"
```

## Step 2: start PostgreSQL

```powershell
docker compose up -d postgres
```

What this does:

- starts a PostgreSQL 16 container
- exposes it on `localhost:5434`
- creates a persistent Docker volume for local data

If Docker is not running, start Docker Desktop first.

## Step 3: run metadata migrations

Plain `go run`:

```powershell
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
go run ./cmd/migrate up
```

With `mise`:

```powershell
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
mise exec -- go run ./cmd/migrate up
```

What this does:

- creates the `core` schema
- creates the metadata tables used by the service

## Step 4: start the API

Recommended interactive run:

```powershell
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
go run ./cmd/server
```

Equivalent `mise` run:

```powershell
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
mise exec -- go run ./cmd/server
```

If you want token auth:

```powershell
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
$env:SERVICE_AUTH_MODE='token'
$env:SERVICE_AUTH_TOKEN='change-me'
go run ./cmd/server
```

With `mise`, you can instead put those values in `.env` and run:

```powershell
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
mise exec -- go run ./cmd/server
```

Expected startup behavior:

- Gin prints route registration in debug mode
- the service logs a startup message including the selected port

## Step 5: start the async index worker

Run this in a separate terminal if you want pending `core.index_jobs` to be executed:

```powershell
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
go run ./cmd/worker
```

Equivalent `mise` run:

```powershell
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
mise exec -- go run ./cmd/worker
```

There is also a Makefile target:

```powershell
make run-worker
```

What this does:

- polls `core.index_jobs`
- creates managed secondary indexes for pending jobs
- retries failed jobs with scheduled backoff until max attempts are exhausted
- marks jobs `applied` or `failed`
- writes schema-change audit rows for retry/apply/fail transitions

## Step 6: verify health

Open a second terminal and run:

```powershell
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8080/healthz | Select-Object -ExpandProperty Content
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8080/readyz | Select-Object -ExpandProperty Content
```

Expected responses:

```json
{"status":"ok"}
```

and

```json
{"status":"ready"}
```

You can also open the API docs in a browser:

- `http://127.0.0.1:8080/docs`
- `http://127.0.0.1:8080/openapi.yaml`

## Step 7: create a tenant

When auth is disabled:

```powershell
$body = @{
  name = "Fraud Ops"
  external_key = "fraud-ops"
} | ConvertTo-Json

Invoke-WebRequest `
  -Method POST `
  -Uri http://127.0.0.1:8080/v1/tenants `
  -ContentType "application/json" `
  -Body $body |
  Select-Object -ExpandProperty Content
```

When auth is enabled:

```powershell
$body = @{
  name = "Fraud Ops"
  external_key = "fraud-ops"
} | ConvertTo-Json

Invoke-WebRequest `
  -Method POST `
  -Uri http://127.0.0.1:8080/v1/tenants `
  -Headers @{ Authorization = "Bearer change-me" } `
  -ContentType "application/json" `
  -Body $body |
  Select-Object -ExpandProperty Content
```

Save the returned `tenant.id`.

## Step 8: provision the tenant schema

Replace `<tenant-id>` with the real value:

```powershell
Invoke-WebRequest `
  -Method POST `
  -Uri http://127.0.0.1:8080/v1/tenants/<tenant-id>/provision `
  -Headers @{ Authorization = "Bearer change-me" } |
  Select-Object -ExpandProperty Content
```

If auth is disabled, omit the `Headers` block.

What this does:

- creates the physical PostgreSQL schema for that tenant
- marks the tenant as active

## Step 9: create a table

Replace `<tenant-id>`:

```powershell
$body = @{
  name = "cases"
  description = "case records"
  alias = "Cases"
  semantic_type = "case"
} | ConvertTo-Json

Invoke-WebRequest `
  -Method POST `
  -Uri http://127.0.0.1:8080/v1/tenants/<tenant-id>/tables `
  -ContentType "application/json" `
  -Body $body |
  Select-Object -ExpandProperty Content
```

What this does:

- creates metadata for the table
- creates the physical tenant table
- adds default metadata fields `object_id` and `updated_at`
- creates the unique `object_id` index

## Step 10: create a field

Replace `<table-id>`:

```powershell
$body = @{
  name = "email"
  description = "customer email"
  data_type = "string"
  nullable = $false
  is_enum = $false
  is_unique = $false
} | ConvertTo-Json

Invoke-WebRequest `
  -Method POST `
  -Uri http://127.0.0.1:8080/v1/tables/<table-id>/fields `
  -ContentType "application/json" `
  -Body $body |
  Select-Object -ExpandProperty Content
```

## Step 11: create a navigation option

This creates navigation-option metadata and enqueues a background index job for the target table.

Replace `<table-id>`, `<source-field-id>`, `<target-table-id>`, `<filter-field-id>`, and `<ordering-field-id>`:

```powershell
$body = @{
  source_field_id = "<source-field-id>"
  target_table_id = "<target-table-id>"
  filter_field_id = "<filter-field-id>"
  ordering_field_id = "<ordering-field-id>"
} | ConvertTo-Json

Invoke-WebRequest `
  -Method POST `
  -Uri http://127.0.0.1:8080/v1/tables/<table-id>/navigation-options `
  -ContentType "application/json" `
  -Body $body |
  Select-Object -ExpandProperty Content
```

List the navigation options for a table:

```powershell
Invoke-WebRequest `
  -Method GET `
  -Uri http://127.0.0.1:8080/v1/tables/<table-id>/navigation-options |
  Select-Object -ExpandProperty Content
```

List index jobs for the tenant:

```powershell
Invoke-WebRequest `
  -Method GET `
  -Uri http://127.0.0.1:8080/v1/tenants/<tenant-id>/index-jobs |
  Select-Object -ExpandProperty Content
```

## Step 12: read the assembled data model

```powershell
Invoke-WebRequest `
  -Method GET `
  -Uri http://127.0.0.1:8080/v1/tenants/<tenant-id>/data-model |
  Select-Object -ExpandProperty Content
```

## Step 13: run reconciliation

CLI:

```powershell
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
$env:DATABASE_URL='postgres://datamodel:datamodel@localhost:5434/datamodel?sslmode=disable'
go run ./cmd/reconcile
```

HTTP:

```powershell
Invoke-WebRequest `
  -Method GET `
  -Uri http://127.0.0.1:8080/v1/admin/reconcile |
  Select-Object -ExpandProperty Content
```

This report now includes missing managed-index details and the number of repair jobs scheduled by reconcile.

## Step 14: inspect tenant schema migration history

```powershell
Invoke-WebRequest `
  -Method GET `
  -Uri http://127.0.0.1:8080/v1/tenants/<tenant-id>/schema-migrations |
  Select-Object -ExpandProperty Content
```

This returns the recorded physical-schema mutation history for that tenant.

## Running tests

Normal test suite:

```powershell
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
go test ./...
```

Integration test path:

```powershell
$env:DATA_MODEL_TEST_DATABASE_URL='postgres://datamodel:datamodel@localhost:5434/datamodel?sslmode=disable'
$env:GOCACHE='C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service\.gocache'
go test -run Integration ./...
```

There is also a Makefile target:

```powershell
make test-integration
```

If `make` is not installed on Windows, use the direct `go test` command above.

## Running with Docker only

You can also start the packaged service container:

```powershell
docker compose up --build
```

This starts:

- PostgreSQL
- the data model service container on `localhost:8088`

In that mode, the service itself is exposed on port `8088`.

## Stopping everything

If running interactively, stop the API with `Ctrl+C`.

Stop Docker containers:

```powershell
docker compose down
```

Remove containers and local volume:

```powershell
docker compose down -v
```

## Common problems

PostgreSQL connection refused:

- Docker is not running
- the `postgres` container is not up
- port `5434` is already in use

Go build cache access denied:

- set `GOCACHE` to the service-local `.gocache` directory as shown above

Auth errors on `/v1` routes:

- check `SERVICE_AUTH_MODE`
- if token mode is enabled, send `Authorization: Bearer <token>`

Docker access denied:

- start Docker Desktop
- make sure your shell has permission to talk to Docker on Windows

Service starts but does not stay resident in a detached Windows shell:

- prefer running the server interactively during local development
- or run the full service via `docker compose up --build`

## Suggested local workflow

For reliable local development on this machine:

1. `docker compose up -d postgres`
2. `go run ./cmd/migrate up`
3. `go run ./cmd/server` in an interactive terminal
4. `go run ./cmd/worker` in a second interactive terminal if you are testing async indexes
5. use another terminal for API requests and tests
