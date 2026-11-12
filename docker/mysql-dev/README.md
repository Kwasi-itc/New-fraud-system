# Local MySQL development database

For the complete frontend/backend platform, use `docker/setup.ps1` and
`docker/compose.yaml` as described below. The separate `compose.mysql-dev.yaml`
foundation starts **MySQL only**. For service development, use the dedicated
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

## Unified frontend and backend

From `New-fraud-system`, use one setup command:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File docker/setup.ps1
```

The canonical Compose file is [docker/compose.yaml](../compose.yaml), with gateway
and provider modules in the same `docker/` folder. Docker
Desktop shows one project, **fraud-mysql-dev**, containing the frontend, gateway,
six fraud APIs, workers, MySQL, the reference database, invitation mailbox and
Keycloak. All runtime databases use MySQL. Multiple containers are expected:
the project is the single group that starts/stops them together.

On first setup, the script runs the owner installers in dependency order. For an
already installed platform, it reuses schemas and backend images, configures the
frontend callback, and rebuilds the frontend. Use `-Install` after backend schema
or image changes to replay the owner setup sequence explicitly. Use `-SkipBuild`
when restarting an unchanged, already-built frontend. Existing credentials,
signing keys, owner plans and application data are retained. The first transition
from the old temporary reference database also runs owner installers once to
populate the separate `fraud-mysql-dev_mysql_dev_reference_data` volume. That
reference schema now survives Docker stops alongside the application database.

| Component | Address |
| --- | --- |
| Frontend | http://localhost:8088/ |
| API index | http://localhost:8088/services/ |
| Data model | http://localhost:8088/services/data-model/docs |
| Ingestion | http://localhost:8088/services/ingestion/docs |
| Decisions | http://localhost:8088/services/decisions/docs |
| Screening | http://localhost:8088/services/screening/docs |
| Cases | http://localhost:8088/services/cases/docs |
| Auth | http://localhost:8088/services/auth/docs |
| Keycloak | http://localhost:8088/identity/ |
| Keycloak administration | http://localhost:8088/identity/admin/ |
| Keycloak issuer | http://localhost:8088/identity/realms/fraud |

The frontend and all fraud APIs share port **8088**. The gateway routes ordinary
pages, `/_next` assets and `/api` backend-for-frontend routes to Next.js. Raw owner
APIs use `/services/<service>/...` so they cannot shadow frontend pages such as
`/cases`. For example, a decision request goes to
`http://localhost:8088/services/decisions/v1/tenants/<tenant-id>/decisions`.
The prefix is removed before forwarding. Swagger/Redoc references and OpenAPI
servers are adjusted so interactive requests stay on the gateway.

The consolidated profile publishes only **127.0.0.1:8088**. Individual fraud API,
frontend, Keycloak, MySQL and mailbox ports remain internal. Keycloak serves its
actual `/identity/` context through the gateway, including login, administration,
OIDC discovery, callbacks and static resources. Login returns to
`http://localhost:8088/api/auth/callback`.
Service credentials remain on the frontend server, and owner permission/session
checks still apply. The gateway does not inject private credentials into raw API
requests.

Keycloak stores its realms, users and credentials in the dedicated `keycloak`
database on the same `mysql:3306` server as the application's `fraud` database.
Its `fraud_keycloak` account has privileges only on `keycloak.*`. Keycloak owns
its schema migrations, uses `utf8mb4_unicode_ci` within its own database, and
has a maximum pool of five connections within the shared server budget of 100.
Fraud tables retain their existing database, ownership and collation.

For an existing PostgreSQL provider, setup runs a user-inclusive offline CLI
export and imports into an empty MySQL target using the same Keycloak version.
Before takeover, it compares user and realm IDs, password material, client
secrets/callbacks, roles and signing keys. A populated target is never
automatically overwritten; a resumed import must verify against its source.
Keycloak generates secrets when importing some built-in bearer-only clients
whose source exports had none. Verification accepts only that narrow generated
default on known built-ins; pre-existing secrets and application client secrets
remain exact checks. Maintenance imports disable bootstrap-admin creation
because the full master-realm export already contains the retained administrator.
Keycloak's export omits active provider sessions, events, workflow state and
revoked tokens; users may need to sign in again. Application memberships are
not rewritten by this storage migration.

When moving the public issuer from `http://localhost:8090/realms/fraud`, setup
first starts the gateway without its frontend dependency. It then stops auth
writers and runs an auth-owned local maintenance command to move external
identity keys and invitation issuer references in one transaction. User IDs,
subjects, memberships, passwords, client secrets and application sessions are
preserved. In-progress old-issuer login transactions expire; restart sign-in if
one was open during the move. Colliding identities cause the whole transaction
to roll back. The command supports at most 10,000 external identities and 10,000
pending invitations/logins per issuer, and fails explicitly beyond those limits.
Private configuration backups remain in `.local/mysql-dev/issuer-move/`.
Rerunning setup safely resumes an interrupted move. Local port changes use the
same maintenance path; do not change only the Compose port on an installed stack.

Private exports, migration state and diagnostic logs remain under ignored
`.local/mysql-dev/keycloak-migration/`. Exports contain credentials and keys;
keep them private. The old `new-fraud-system_postgres_data` volume is retained
as an offline rollback source, while its PostgreSQL container is stopped and
removed after verification. It is not part of normal runtime startup. The
source-only `docker/keycloak/postgres-source.compose.yaml` is for migration or
explicit rollback. The normal root `docker-compose.yml` remains the separate
legacy PostgreSQL application profile; do not start it alongside this stack.

The migration follows Keycloak's official [database configuration](https://www.keycloak.org/server/db)
and [offline realm export/import](https://www.keycloak.org/server/importExport)
contracts. Keycloak supports MySQL 8.4 and ships its MySQL driver.

The current [single-port verification record](evidence/single-port-identity-2026-11-12.json)
records the actual runtime bindings, retained identity/membership comparisons,
browser login and proxy checks. Earlier database migration records describe the
previous port-8090 stage and remain historical evidence.

Start, inspect and stop the complete installed platform using one file:

```powershell
docker compose -f docker/compose.yaml up -d --wait
docker compose -f docker/compose.yaml ps
docker compose -f docker/compose.yaml logs -f api-gateway frontend
docker compose -f docker/compose.yaml down
```

Normal runtime startup excludes installer jobs, so a restart does not reapply
migrations or revoke/regrant database permissions underneath running services.
Run `docker/setup.ps1 -Install` when an upgrade requires owner migrations. Do not
add `--volumes` to `down` if you want to retain application data. Keycloak's
MySQL database is persisted in the same main MySQL data volume. The old
PostgreSQL rollback volume is outside this Compose model and is not removed.

To change the application port, use `docker/setup.ps1 -Port 8099` and repeat that
argument for subsequent setup runs. Set `MYSQL_GATEWAY_PORT=8099` when using
Compose directly so the published port and frontend origin agree. Existing setup
entry points `docker/mysql-dev/setup-platform.ps1` and `setup-gateway.ps1` forward
to the unified setup. Per-service Compose files remain implementation/dev/test
building blocks; starting those directly can republish their development ports.

For administrative MySQL access without a published database port:

```powershell
docker compose -f docker/compose.yaml exec mysql sh
```

Then use the socket-based MySQL command shown in the foundation section above.
For initial users, follow the auth service's
[portable user setup](../../backend/auth-service/MYSQL_DOCKER.md#portable-user-and-permission-setup).
A user manifest can be passed to `docker/setup.ps1 -UsersFile <path>`, optionally
with `-CreateMissingUsers`. When invoking the user helper separately against the
consolidated runtime, pass
`--data-model-url http://localhost:8088/services/data-model`.

## Gateway verification

```powershell
node docker/mysql-dev/verify-gateway.mjs
# With a custom port:
node docker/mysql-dev/verify-gateway.mjs --port 8099
```

The verifier checks Keycloak discovery, public endpoint URLs and signing-key
access under `/identity/`, the frontend login UI and static assets, CSRF rejection,
all six APIs' health/readiness, documentation, OpenAPI server prefixes and
missing/invalid authentication. It makes no application data changes.
`/healthz` on the gateway checks only the proxy; each
`/services/<service>/readyz` checks that owner. The proxy accepts bodies up to
32 MiB and 120 seconds of upstream read/send inactivity. Missing upstreams return
502, and timeouts return 504. Other business status codes and response bodies
are preserved. Next.js response buffering is disabled to preserve streaming.

Disposable proxy contract tests run on a separate network, with no application
database or credentials:

```powershell
docker compose -f docker/mysql-dev/gateway/compose.test.yaml up -d --wait
node docker/mysql-dev/gateway/contract-test.mjs
docker compose -f docker/mysql-dev/gateway/compose.test.yaml down
```

They check method/body, encoded paths/query strings, credentials/session
headers, compression, redirects/cookies and upstream connection failures.
The fixture defaults to `node:22-alpine`; `MYSQL_GATEWAY_FIXTURE_IMAGE` may select
another local Node image. With the frontend's Playwright dependencies and
Chromium installed, verify Swagger execution in a browser:

```powershell
node docker/mysql-dev/gateway/verify-browser.cjs
# Existing synthetic demo account: login, same-origin frontend APIs and pages
node frontend/scripts/verify-unified-platform.mjs
# Keycloak storage, retained identity manifest and owner privilege isolation
node docker/keycloak/verify-mysql.mjs
```

Internet access to the Swagger CDN is required. The implementation uses Nginx
[proxy routing](https://nginx.org/en/docs/http/ngx_http_proxy_module.html) and
[documentation substitutions](https://nginx.org/en/docs/http/ngx_http_sub_module.html),
with Docker [service extension](https://docs.docker.com/reference/compose-file/services/#extends)
and [port overrides](https://docs.docker.com/reference/compose-file/merge/).

## NovaPay fintech demonstration

With the platform running, create or resume the retained local demonstration:

```powershell
node docker/mysql-dev/seed-fintech-demo.mjs
```

This creates `NovaPay Fintech — Demo` with the external key
`local-novapay-fintech-demo-v1`. It includes three related models (customers,
wallets, transactions), three customers, three wallets, 18 transaction records,
three published scenarios, 11 fraud rules and two screening configurations.
The wallet velocity rule counts earlier persisted transactions for the same
wallet within ten minutes. Person and organization screening use the labelled
synthetic development catalog and mapped beneficiary IDs.

The dedicated `novapay.demo` account has administrator and investigator roles
only in this demonstration tenant. Setup completes its initial password change
through Keycloak; the generated login details remain in ignored
`.local/mysql-dev/novapay-demo/login.json`. Sign out of another account before
using this login. Existing users and their passwords are not reset.

Expected scores/outcomes are checked for 21 decisions, including ordinary
payments, account takeover signals, transfer bursts, card payment risk and
synthetic screening matches. Seven explicit fraud cases and three screening
cases remain available for investigation. Screenshots and a nonsecret report
are retained under `frontend/evidence/novapay-demo/`. Source state and operation
IDs under ignored `.local/mysql-dev/novapay-demo/` allow interrupted setup to
resume. Completed setup reuses saved decisions and case IDs.

This is synthetic demonstration data and an illustrative rule policy. It does
not provide live sanctions coverage, trained-model results or production risk
threshold recommendations. The unified profile persists its reference database.
If that volume is deliberately removed, rebuild the owner references with:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File docker/setup.ps1 -Install
```

## Restart and recovery behavior

Docker may choose another private subnet when a network is recreated. During
service setup, auth, case and screening processes are stopped before their
runtime account host is reconciled to the verified local Docker network. A
`config.json.before-network-<hash>.json` backup is retained. Passwords, signing
keys, callbacks, tenant selection and access policies are preserved. Database
or server-address changes still require explicit reconciliation. Obsolete account
host entries are not automatically deleted.

Decision setup replays the screening publication prerequisite even when the
retained screening artifact includes the later lifecycle. It preserves that full
artifact and journal before completing case and screening dependencies. A
journaled reference replay can resume through the strict migration runner;
checksum and definition drift still fail. Unjournaled partial reference schemas
are rejected. Setup never force-clears a dirty application migration.

The local ingestion/decision HTTP profiles allow loopback development calls
without interactive login, and tenant checks remain active. The authenticated
frontend keeps service credentials on its server. These local profiles and
provider adapters do not establish production integration readiness.

The separate MySQL test Compose projects are disposable integration harnesses.
Do not aim their reset/fixture scripts at this persistent database.

Recovery [verified on 2026-11-11](evidence/setup-recovery-2026-11-11.json):

- [x] Complete platform setup finished and all seven HTTP probes passed.
- [x] Application and reference case/screening migration journals were clean.
- [x] All 25 pre-existing credential and signing-key files retained their hashes.
- [x] Migration/reference tests passed on disposable MySQL with the race detector;
  all 13 Node validation checks passed.
- [x] The authenticated frontend acceptance passed 10 suites and 48 gateway requests.

Consolidation [verified on 2026-11-12](evidence/unified-platform-2026-11-12.json):
one running project; frontend and six APIs on 8088; retained-user browser login;
all six Swagger execution checks; repeat setup without migrations/builds; 154
registered tenants, four provider accounts and 35 credential/key file hashes
preserved. A separate empty-workspace install and injected provider rollback
failure were not exercised in this run.

Keycloak storage migration [verified on 2026-11-12](evidence/keycloak-mysql-2026-11-12.json):
two realms and four users retained with matching identity/password material,
application client secrets, roles and signing keys; existing demo login and
frontend/API checks passed; owner access to fraud tables was denied; normal
setup restarted successfully without PostgreSQL. The original PostgreSQL
volume remains offline. Rollback was exercised successfully during migration;
an independent empty-workspace first install was not exercised.

## Stop and restart

```powershell
docker compose -f docker/compose.yaml down
powershell -NoProfile -ExecutionPolicy Bypass -File docker/setup.ps1 -SkipBuild
```

`down` preserves the named data volumes. Do not add `--volumes` if you want to keep data. Keep the ignored password files with the MySQL volume: initialization credentials apply only on first initialization, so replacing those files does not rotate existing database passwords. Setup checks whether a recreated Docker network needs restricted account reconciliation. The old PostgreSQL volume is an offline rollback source and is never deleted by these commands.
