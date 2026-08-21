# New Workspace

This directory is now the workspace root for the extracted system.

## Local Docker stack

The root `docker-compose.yml` runs the local/dev stack with one shared
Postgres and ClickHouse containers, a bounded Valkey feature cache, one-shot
migration jobs, backend APIs, default workers, and the Next frontend.

Start the default stack:

```sh
docker compose up --build
```

The default stack starts:

- Postgres on `localhost:5432`
- ClickHouse HTTP on `localhost:8123`
- Valkey on `localhost:6379`
- data-model-service on `http://localhost:8080`
- ingestion-service on `http://localhost:8081`
- decision-engine-service on `http://localhost:8082`
- screening-service on `http://localhost:8085`
- frontend on `http://localhost:3000`
- data-model, ingestion, and decision-engine workers

The screening worker is profile-gated because it needs provider configuration
before it can process real screening jobs safely:

```sh
docker compose --profile screening-worker up --build
```

Health checks:

```sh
curl http://localhost:8080/healthz
curl http://localhost:8081/healthz
curl http://localhost:8082/healthz
curl http://localhost:8085/healthz
```

Stop the stack:

```sh
docker compose down
```

Reset the local PostgreSQL and ClickHouse volumes:

```sh
docker compose down -v
```

Compose injects local/dev environment values directly. The checked-in service
`.env.example` files remain useful for non-Docker local runs, but they are not
the source of truth for the Docker stack.

## Data storage classes

Data-model tables explicitly choose a `storage_class`:

- `operational` (the default) keeps mutable entity/reference records in PostgreSQL.
- `event` keeps append-only, high-volume facts in ClickHouse and requires a non-null timestamp `event_time_field`.

Event ingestion is synchronous and ClickHouse is required. Successful single
event writes create no PostgreSQL row, including when callers send an
`Idempotency-Key`. A keyed event batch retains one PostgreSQL receipt for the
complete request rather than one row per event. PostgreSQL does not receive the
event payload, tenant event row, ingestion outbox row, or new event-table indexes.
Existing PostgreSQL history can be read alongside ClickHouse for a
30-day cutover window, while new event tables have no legacy window. Ingestion
and decision-engine processes import the same event repository module and own
separately bounded ClickHouse pools. Decision reads always route event tables
directly to ClickHouse; `TENANT_DATA_READ_MODE` only chooses whether operational
tables use PostgreSQL directly or the ingestion API.

Each event data-model table has its own physical ClickHouse table. Every active
field in the published model is a typed ClickHouse column; model fields are not
stored in a shared JSON payload. The first event ingestion atomically locks that
table's schema in data-model metadata. After that point, table, field, and enum
mutations are rejected with `409 Conflict`. Schema-versioned replacement tables
and historical backfill are intentionally deferred to a later evolution phase.
An existing shared `event_records` table containing legacy JSON rows requires an
explicit migration or a fresh ClickHouse volume; the shared repository refuses to start
rather than silently hiding that history.

## Layout

```text
new/
  backend/
    data-model-service/   current Go backend service
  frontend/               frontend app placeholder
```

## Current backend

All backend work completed so far lives in:

- `backend/data-model-service`

That service contains:

- the Go module
- the HTTP API
- metadata migrations
- tenant schema management
- Docker and local run files
- service docs and handoff notes

## Frontend

The frontend directory has been created as the next workspace area:

- `frontend/`

It is currently a placeholder so frontend work can be added without mixing it into the backend service directory.

## Next step

If you want to work on the current service, use:

```powershell
Set-Location "C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service"
```
