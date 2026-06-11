# New Workspace

This directory is now the workspace root for the extracted system.

## Local Docker stack

The root `docker-compose.yml` runs the local/dev stack with one shared
Postgres container, one-shot migration jobs, backend APIs, default workers, and
the Next frontend.

Start the default stack:

```sh
docker compose up --build
```

The default stack starts:

- Postgres on `localhost:5432`
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

Reset the local database volume:

```sh
docker compose down -v
```

Compose injects local/dev environment values directly. The checked-in service
`.env.example` files remain useful for non-Docker local runs, but they are not
the source of truth for the Docker stack.

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
