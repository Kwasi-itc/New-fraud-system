# Local MySQL setup

`docker-compose-mysql.yml` starts MySQL 8.4.8 and installs the data-model
owner migrations, including record/contract lookups and v007 worker indexes.
It starts the dedicated `mysql-api` entrypoint at **http://localhost:8080/docs**.
Login is disabled for this loopback-only development administrator API.
Swagger lists the complete existing API contract. Both `/openapi.yaml` and
`/openapi.json` use the shared specification without removing endpoints or
schemas. All 43 listed operations are connected in the local administrator runtime. The
normal PostgreSQL entrypoint remains unchanged. See [endpoint status](MYSQL_ENDPOINT_STATUS.md)
and [worker status](MYSQL_WORKER_STATUS.md) for current evidence and limits.

`POST /v1/tenants/{tenantId}/provision` activates a pending tenant in the installed
shared `fraud` database. It atomically records activation, audit history and a
schema revision, and repeated calls return the same active tenant without extra
history. The installer verifies shared migrations/grants before API startup.
The returned `schema_name` is a legacy logical identifier, not a separate MySQL
database. Actual business tables still require the physical creation protocol.

Table creation is now available through `POST /v1/tenants/{tenantId}/tables`.
It creates the physical table and its exact-key guards and publishes the
ingestion contract before returning 201. The private `data-model-author`
container has administrative socket access for reference capture and account
lifecycle; the public API retains its restricted account. The author has no
published port and checks a server-owned credential on every call. It runs at
most two DDL workflows concurrently with an eight-connection administration pool.
Local MySQL enables `log_bin_trust_function_creators` for restricted trigger
installation; this is a development setting, not production acceptance.

Supply a UUID `Idempotency-Key` to identify a creation operation. If omitted,
local mode derives a stable key from the tenant and normalized table name.
Retry the identical body/key after an uncertain response. Reusing the key with
changed input conflicts; no partially prepared table is reported as created.
Dedicated per-table writer/DDL principals are localhost-only and have generated
credentials; their existence does not mean ingestion or decision services have
been wired to use them. Table/field authoring recovery is request-driven. Index
jobs have their own automatic queue execution and expired-lease recovery host.

Table/field listings, enum-value listings, links, pivots, options, navigation,
audit/revision history and index-job reads are also mounted. See the full
[endpoint checklist](MYSQL_ENDPOINT_STATUS.md) for remaining runtime gaps.

**Additional mutations:** table PATCH supports alias, description and semantic
type; field PATCH supports alias and description. POST table fields supports
native bool/int/float/string/timestamp/IP fields, including native numeric unique
indexes. Native fields create actual MySQL columns and publish the new contract.
Non-nullable additions require an empty table; populated tables require an
explicit backfill protocol. Creation retries use a stable default operation key
derived from table/name, or a caller-supplied UUID `Idempotency-Key`.
PATCH requests return a generated key when omitted; supply your own UUID before
sending a PATCH to recover safely after losing the response. Reuse that key only
for the identical command. Unknown PATCH properties are rejected.
Concurrent schema changes may return 503 with `Retry-After`; retry the identical
body with its original `Idempotency-Key`. Recovery of these authoring requests
uses the original request; it is separate from background index jobs.

Table/field deletion, enums, unique text, constraint and caption changes,
relationships and import/export are available. The separate `data-model-worker`
service executes MySQL search/navigation/repair deliveries with lease supervision,
retry and maintenance. It has no published port; check it with
`docker compose -f docker-compose-mysql.yml exec data-model-worker /app/mysql-worker --healthcheck`.
Registering a tenant leaves it pending until provisioning. Swagger assets require
internet access. Production authorization and rollout remain separate work.

From `backend/data-model-service` in PowerShell:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File ../../docker/mysql-dev/setup.ps1
docker compose --env-file ../../.local/mysql-dev/compose.env -f docker-compose-mysql.yml up -d --build
docker compose --env-file ../../.local/mysql-dev/compose.env -f docker-compose-mysql.yml wait migrate-data-model
docker compose --env-file ../../.local/mysql-dev/compose.env -f docker-compose-mysql.yml logs migrate-data-model
docker compose --env-file ../../.local/mysql-dev/compose.env -f docker-compose-mysql.yml exec mysql sh /opt/mysql-dev/verify.sh
```

Check that `wait` exits with code zero and the migration log reports success;
`up -d` alone does not establish migration completion. The setup script's
printed root-level command is an alternative; use the commands above here.

Open http://localhost:8080/docs, use `POST /v1/tenants` to create a pending tenant,
then use its returned ID in `GET /v1/tenants/{tenantId}/data-model`.
`GET /readyz` checks the actual MySQL connection and tenant table.
The OpenAPI documents are at `/openapi.json` and `/openapi.yaml`; they include
all documented routes, now mounted in this runtime.
This local mode deliberately has platform-wide tenant access and is not a
production authorization configuration. Foreign browser origins are rejected.

Run the repeatable smoke check from this directory:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/mysql-local-smoke.ps1
```

It retains one pending test tenant identified by `local-compose-smoke` and
reuses it on subsequent runs. It does not delete or reset shared data.

Run `node scripts/mysql-mutation-smoke.mjs` for table/field authoring, concurrent
presentation edits, six native field types and replay/conflict checks. Each run
creates and retains a dedicated smoke tenant; it never resets the shared DB.
These checks do not establish crash-injection or production acceptance.

MySQL listens on `127.0.0.1:3307` by default. The generated compose environment
file retains any previously configured port. Credentials and retained migration
plans live in the ignored repository directory `.local/mysql-dev`.

The project name, network and persistent volumes intentionally match the root
`compose.mysql-dev.yaml`: this is the same shared `fraud` database, not another
application database for data-model. Existing ingestion and decision tables are
preserved. Do not use `--remove-orphans` when switching between these files.
The reference server is disposable and has no published port; it captures exact
DDL definitions before the journaled migration runner applies them to MySQL.
Only the installer and private authoring worker receive administrative socket access. The public API uses a
separate generated database credential with data-model table reads and the
three inserts required for tenant registration, plus column-scoped tenant status
and timestamp updates for provisioning; no DDL or cross-service writes.
Root is not exposed over TCP. The API pool uses at most eight connections.

To recheck an existing installation:

```powershell
docker compose --env-file ../../.local/mysql-dev/compose.env -f docker-compose-mysql.yml run --rm migrate-data-model
```

To stop just the database processes without deleting data:

```powershell
docker compose --env-file ../../.local/mysql-dev/compose.env -f docker-compose-mysql.yml stop data-model-service data-model-author mysql mysql-reference
```

These are shared development services, so stopping them also affects other
local MySQL consumers. Keep the volume, password file and retained plans
together. Do not delete volumes to resolve a migration error. The original
PostgreSQL Compose remains available separately.
