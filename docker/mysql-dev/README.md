# Local MySQL development database

This Compose file starts **MySQL only**. For local APIs, use the dedicated
[data-model setup](../../backend/data-model-service/MYSQL_DOCKER.md) or
[ingestion setup](../../backend/ingestion-service/MYSQL_DOCKER.md), or
[decision engine setup](../../backend/decision-engine-service/MYSQL_DOCKER.md). Ingestion's
setup includes data-model and shares this project's persistent `fraud` database.
The normal application Compose file still starts PostgreSQL-backed servers.
Do not combine the PostgreSQL and MySQL files as overlays or substitute database
URLs without selecting a matching runtime entrypoint.

## Start from the repository root

Docker Desktop must be running with Linux containers.

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File docker/mysql-dev/setup.ps1
docker compose --env-file .local/mysql-dev/compose.env -f compose.mysql-dev.yaml up -d --wait
docker compose --env-file .local/mysql-dev/compose.env -f compose.mysql-dev.yaml exec -T mysql sh /opt/mysql-dev/verify.sh
```

The setup script creates a random local root password and port settings under ignored `.local/mysql-dev/`. Repeating setup preserves both files. To choose a different port on first setup, append `-Port 3308`; for an existing setup edit `MYSQL_DEV_PORT` in `.local/mysql-dev/compose.env` and run `up` again.

Configuration:

| Setting | Value |
| --- | --- |
| MySQL | 8.4.8 |
| Shared application database | `fraud` |
| Published address | `127.0.0.1:3307` by default |
| Docker service address | `mysql:3306` within this project's network |
| Character set / collation | `utf8mb4` / `utf8mb4_0900_bin` |
| Persistent volume | `fraud-mysql-dev_mysql_dev_data` |
| Local resource limits | 2 CPUs, 2 GiB, 100 connections |

These are development limits, not a verified 1,000 evaluations/second sizing recommendation. This setup does not provision application tables, runtime users, service grants or migration artifacts. Root is an operator account; applications must receive their own restricted accounts when their runtime integration is completed. Root access is through the container's local socket, not a provisioned host-facing root account. The published port is available for future explicitly provisioned application accounts.

Open an administrative SQL session without putting the password in command history:

```powershell
docker compose --env-file .local/mysql-dev/compose.env -f compose.mysql-dev.yaml exec mysql sh
```

Then inside the container:

```sh
MYSQL_PWD="$(cat /run/secrets/mysql_root_password)" mysql --protocol=socket -uroot fraud
```

## What still prevents frontend/API testing

| Component | Current runtime gap |
| --- | --- |
| Data-model | Dedicated local MySQL authoring API and worker are available; see the data-model setup above. Normal server/router still uses PostgreSQL. |
| Ingestion | Dedicated local MySQL Compose starts the API at `localhost:8081/docs` and its CSV/deferred worker. Setup installs migrations and restricted accounts and prepares a published tenant/table contract. The current runtime serves one configured tenant/object type. Default application entrypoints remain PostgreSQL. |
| Decisions | Dedicated local core Compose starts the API at `localhost:8082/docs` and real async/scheduled workers. It supports authoring, publication and history for one configured tenant; external workflows, screening, additional scoring, callbacks and event delivery are disabled explicitly. See the decision engine setup above. |
| Frontend login | A separate local-only session adapter is implemented. The normal auth flow remains enabled by default. Local mode does not need the auth service, but the MySQL API composition described above remains necessary. |

The existing MySQL test Compose projects are disposable integration harnesses. Do not aim their reset/fixture scripts at this persistent database. Passing those tests does not establish that the browser-facing application has switched databases.

## Local mode without login

The user approved disabling authentication for this local three-service stack.
The frontend and ingestion adapters require `LOCAL_MYSQL_NO_AUTH=1` and a loopback
HTTP `APP_ORIGIN` such as `http://localhost:3001`. Frontend additionally requires
`NEXT_PUBLIC_DATA_MODEL_TENANT_ID`, a configured nonzero tenant UUID. It uses a fixed
development identity and displays a visible banner. Ingestion requires
`Authority: "local-mysql-development"`, `Database: "fraud"` and its normal tenant,
bounded policies, migration artifacts and restricted account configuration.

The ingestion HTTP adapter accepts requests without a login and binds the existing
internal service credential on the server. It does not take a tenant or principal
from caller credentials. Foreign browser origins are rejected; owner tenant
checks and readiness checks remain active. Keep published ports on loopback.
The database-only Compose file does not yet enable these application adapters.

The full decision runtime still requires case-management, screening and delivery
provider bindings at startup. Its separate local core host checks actual stored
configuration and rejects unsupported effects; it does not supply successful no-op
providers. `/capabilities` reports the active local scope.

## Stop and restart

```powershell
docker compose --env-file .local/mysql-dev/compose.env -f compose.mysql-dev.yaml down
docker compose --env-file .local/mysql-dev/compose.env -f compose.mysql-dev.yaml up -d --wait
```

`down` preserves the named data volume. Do not add `--volumes` if you want to keep data. Keep the ignored password file with the volume: MySQL reads initialization credentials only on first initialization, so deleting or replacing that file does not rotate the password of an existing database. This project does not modify or remove PostgreSQL data.
