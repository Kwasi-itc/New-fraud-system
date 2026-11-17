# PostgreSQL development gateway

The active branch is `main`. The base `docker-compose.yml` runs the PostgreSQL services; `docker-compose.gateway.yml` adds the retained single-origin frontend/API gateway.

## Start

From the repository root with Docker Desktop running:

```powershell
node docker/setup-postgres.mjs
```

For images already built:

```powershell
node docker/setup-postgres.mjs --skip-build
```

Use `--port 8099` to choose another local port, or `--prepare-only` to prepare and validate configuration without starting containers. The existing local identity setup and required case integration tenant/issuer settings must already be present; see [platform auth](../backend/auth-service/PLATFORM_AUTH.md). This command does not create users, grant memberships or migrate NovaPay.

The command preserves existing provider/platform keys and credentials, creates missing private integration tokens in ignored `.local/postgres-dev/compose.env`, backs up PostgreSQL before service migrations, builds images and starts the stack. It updates only the existing Keycloak `fraud-auth` client's callback allowlist for the gateway origin. Do not run concurrent setup commands.

## EC2 RDS setup and initial tenant

For an EC2 host using RDS, prepare the required `.env` and project-scoped identity files, then run:

```sh
node docker/setup-postgres-ec2.mjs
```

After service migrations, this setup detects whether RDS already has an active tenant. If it does not, it starts data-model-service and runs the separate [`setup-root-tenant-ec2.mjs`](./setup-root-tenant-ec2.mjs) script. That script creates the `Root` tenant (`external_key: root`) through the authenticated data-model API and provisions its tenant schema. It safely reuses and provisions an existing pending root tenant on retries, then EC2 setup stores its UUID for the application.

The root tenant is a data-model tenant only; this does not create an application user, grant membership, or replace platform identity setup. Keep the data-model service credential in `.local/<project>/data-model.env` private.

Backups include private application and identity data. Keep `.local/` private. Startup can take several minutes for an existing large database. Do not delete the PostgreSQL volume to resolve a configuration error.

For private EC2 deployments accessed over plain HTTP only, set both
`AUTH_COOKIE_SECURE=false` and `CASE_SESSION_COOKIE=fraud_session` in `.env`
before setup, or in the active `.local/<project>/compose.env` (which takes
precedence for an existing project). Then recreate the frontend:

```sh
docker compose --env-file .local/new-fraud-system/compose.env -p new-fraud-system \
  -f docker-compose.ec2.yml -f docker-compose.gateway.ec2.yml \
  up -d --build --force-recreate frontend
```

Replace `new-fraud-system` with the project name used during setup.
This disables transport protection for the login-binding and session cookies;
use only on a trusted private network. Do not use these settings for public
traffic. Restore `AUTH_COOKIE_SECURE=true` and `CASE_SESSION_COOKIE=__Host-fraud_session`
when using HTTPS. The separate `deploy/ec2` HTTPS deployment keeps Secure cookies
enabled.

## Addresses

| Purpose | Default address |
| --- | --- |
| Frontend | http://localhost:8088 |
| API index | http://localhost:8088/services/ |
| Data model Swagger | http://localhost:8088/services/data-model/docs |
| Ingestion Swagger | http://localhost:8088/services/ingestion/docs |
| Decision Swagger | http://localhost:8088/services/decisions/docs |
| Screening Swagger | http://localhost:8088/services/screening/docs |
| Case API | http://localhost:8088/services/cases/ |
| Auth API | http://localhost:8088/services/auth/ |
| Local identity provider | http://localhost:8088/identity/ |

PostgreSQL main currently has no case/auth Swagger routes. Their health routes are linked from the index. The four Swagger UIs use pinned assets bundled in the gateway image; Redoc retains its external asset dependency.

Only the loopback gateway port is published by the overlay. The browser calls authenticated same-origin `/api/*` routes. Service tokens remain server-side; gateway API requests still require each owning service's credentials and tenant permissions. The gateway does not inject an integration token or bypass authorization.

Dashboard and detection pages select the tenant from the authenticated session;
`NEXT_PUBLIC_DATA_MODEL_TENANT_ID` is not required by the frontend. The gateway
waits for the frontend health check during Compose startup. Local builds limit
static-generation workers to two to reduce concurrent memory pressure. The
decision worker restarts automatically after transient startup failures.

## Data and screening behavior

If a pre-existing account reaches the sign-in denial page after a gateway URL
change, check its exact provider issuer binding. Use the guarded
[auth issuer repair](../backend/auth-service/PLATFORM_AUTH.md#postgresql-gateway-issuer-changes)
only after verifying the same provider realm, subject and existing platform
user. Changing the gateway address does not automatically relink identities.

`AUTH_DEFAULT_TENANT_ID` optionally selects an already authorized login tenant.
When unset, auth selects an existing active membership; an old frontend demo
tenant setting no longer restricts sign-in.

The application keeps using `new-fraud-system_postgres_data`, with all service-owned schemas in the same logical PostgreSQL database. The retired `fraud-mysql-dev` containers/volumes are separate and retained. Existing PostgreSQL accounts and memberships determine access; MySQL-only logins and NovaPay data are not copied.

Screening authoring, rules-list entries, search/history/report pages and investigation results are retained. The PostgreSQL engine dispatches screening after saving decisions; decision-time forced outcomes remain disabled. Configure a real compatible provider separately: no synthetic catalog, external list or internal source collector is activated by this setup. See the [PostgreSQL screening status](../backend/screening-service/POSTGRESQL_PORT_STATUS.md).

Optional GeoIP lookup is disabled in this overlay because its database is machine-specific. Add a reviewed read-only MMDB mount and path if needed.

## Verification

Gateway transport checks use an isolated fixture network with no application database or credentials:

```powershell
docker compose -f docker/gateway/compose.test.yaml up -d --build --wait
node docker/gateway/contract-test.mjs
docker compose -f docker/gateway/compose.test.yaml down
```

After starting the real application, verify the four Swagger UIs and health execution with:

```powershell
node docker/gateway/verify-browser.cjs
```

The latter uses the frontend's Playwright installation. Set `POSTGRES_GATEWAY_PORT` when using a custom port. Database integration tests must use an isolated disposable PostgreSQL server, never the application database; see the screening status for test variables and evidence.

Stop application containers while retaining data:

```powershell
docker compose --env-file .local/postgres-dev/compose.env -p new-fraud-system -f docker-compose.yml -f docker-compose.gateway.yml stop
```
