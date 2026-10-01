# New Workspace

This directory is now the workspace root for the extracted system.

## Local Docker stack

The root `docker-compose.yml` runs the local/dev stack with one shared
Postgres container, one-shot migration jobs, backend APIs, default workers, and
the Next frontend.

Before starting, configure the case integration identity in your local `.env`
(keep this file and credentials out of version control):

```dotenv
CASE_SERVICE_AUTH_TOKEN=<generated secret of at least 32 bytes>
CASE_SERVICE_TENANT_IDS=<comma-separated tenant UUIDs>
CASE_USER_JWT_KEYS_FILE=<host path to JSON mapping key IDs to trusted RSA PEM public keys>
CASE_USER_JWT_ISSUER=<trusted identity issuer>
CASE_USER_JWT_AUDIENCE=case-manager
CASE_MANAGER_FORWARDED_PORT=8086
CASE_DB_POOL_MAX_CONNS=8
```

Use keys from the issuer that signs investigator tokens. Only public keys are
mounted into case-manager; the integration token remains server-side. See the
[case-manager authentication contract](backend/case-manager-service/README.md).
Compose rejects missing required settings instead of exposing an unauthenticated
case API. This configuration is also required when rendering the database-scale
Compose override.

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
- case-manager-service on `http://localhost:8086`
- frontend on `http://localhost:3000`
- data-model, ingestion, and decision-engine workers
- screening case-callback delivery worker (two database connections)

Migrations run data-model → ingestion → decision-engine → screening → case-manager
against the same database. Case-manager exposes `/healthz` and `/readyz`, and its
dependent delivery workers wait for readiness. The case-manager worker remains
disabled because its later-phase jobs are not implemented.

Decision workers dispatch case workflows to the authenticated case intake route.
Screening reviews and file metadata atomically enqueue callbacks for durable
delivery. Workflow and review receipts prevent duplicate effects on retry;
unsupported evidence callbacks return 501 and remain visible in delivery state.
See the [intake and retry contract](backend/case-manager-service/INTAKE_CONTRACT.md)
for payloads, action behavior, retry recovery, and migration compatibility.

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
curl http://localhost:8086/readyz
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

## Local IP geolocation database

The decision engine and its worker mount a DB-IP City Lite MMDB file read-only.
By default Compose expects this file at:

```text
./dbip-city-lite-2026-08.mmdb
```

To use another host path, set `GEOIP_MMDB_HOST_PATH`. The MMDB file is local
runtime data and is ignored by Git. DB-IP Lite data is licensed under CC BY 4.0;
the frontend includes the required DB-IP attribution wherever its derived IP
geolocation accessors are available.

## Layout

```text
new/
  backend/
    data-model-service/   current Go backend service
  frontend/               Next.js application
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

It contains the Next.js application, including data-model, detection, decision,
and live case-investigation views. Cases require a signed investigator session;
identity-provider integration remains a deployment prerequisite.

## Next step

If you want to work on the current service, use:

```powershell
Set-Location "C:\Users\Kwasi Addo\Dev\Work\IT Consortium\Marble\marble\new\backend\data-model-service"
```

Case investigator workspace configuration and acceptance limits are documented in the [investigator contract](backend/case-manager-service/INVESTIGATOR_CONTRACT.md). The Cases UI uses live APIs; an identity provider or gateway must establish its signed investigator session.
