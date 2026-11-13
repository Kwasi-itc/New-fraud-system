# EC2 single-port HTTP test deployment with Aurora

Run `setup.py` **on the Linux EC2 Docker host**. Frontend, six fraud APIs and
Keycloak share **http://EC2_IP:8088**. Application services use Aurora's single
`fraud` database. Keycloak uses a dedicated `keycloak` database on the same
Aurora writer through its own restricted account. No PostgreSQL container or
local application MySQL server is started. Reference MySQL is disposable and
never receives application traffic or the Aurora administrator password.

This retains the existing IP-address/HTTP test profile and synthetic provider
adapters. It does not configure production TLS, DNS or production activation.
Browser traffic is HTTP; every Aurora connection verifies its CA and DNS name.

## Prerequisites

- Linux Docker Engine, Docker Compose supporting `include` and
  `config --no-env-resolution`, Python 3.10+, and Node.js 22+.
- An Aurora **MySQL 8.4 compatible writer**, using its AWS DNS endpoint.
  Aurora 3.x/MySQL 8.0 is rejected. Reference capture uses MySQL 8.4.8;
  captured owner definitions must pass the normal migration checks.
- EC2 access to the Aurora port. The migration administrator needs permission
  to create `fraud` and `keycloak`, install owner schemas/routines/triggers,
  manage restricted accounts/grants, delegate `SET_ANY_DEFINER`, and read account
  metadata including `mysql.procs_priv`, `mysql.global_grants` and `mysql.role_edges`.
  Read-only preflight does not prove every DDL privilege.
- Aurora parameters `log_bin_trust_function_creators=1` and
  `sql_generate_invisible_primary_key=OFF`. Setup checks them without changing
  AWS parameter groups.
- `max_connections` of at least **145**: 100 configured application connections,
  five for Keycloak and 40 reserved for migrations, temporary DDL and operators.
  Budget other clients separately; this is not a throughput claim.
- Registry access for builds/pulls. Supply `--db-ca-file` or allow setup to
  download the AWS RDS CA bundle. No insecure database TLS option is provided.

Keycloak uses its shipped MySQL driver with `sslMode=VERIFY_IDENTITY` and the
AWS CA bundle via `KC_TRUSTSTORE_PATHS`. See the official
[Keycloak database](https://www.keycloak.org/server/db),
[system truststore](https://www.keycloak.org/server/keycloak-truststore) and
[Connector/J verification](https://dev.mysql.com/doc/connector-j/en/connector-j-server-authentication.html)
contracts. Aurora is used as a MySQL-compatible test target; acceptance against
an actual Aurora cluster remains necessary.

## Start

```bash
python3 docker/mysql-ec2/setup.py \
  --public-ip 203.0.113.10 \
  --db-host YOUR-WRITER.cluster-XXXX.REGION.rds.amazonaws.com \
  --db-user YOUR_MIGRATION_ADMIN \
  --bootstrap-user test-admin-user \
  --bootstrap-email test-admin@example.com
```

Replace the example values. Setup prompts for the migration password on first
use. Alternatively pass `--db-password-file /private/path` or
`--db-password-stdin`; passwords never go into command arguments. Password files
contain the exact password without a trailing newline. A supplied password must
match retained credentials; no password is silently rotated.

Optional bootstrap flags create/reuse one test Keycloak account and provision
its application membership after migrations. Default roles are
`tenant_admin,platform_admin,case_manager_admin`; override with
`--bootstrap-roles`. Existing passwords and memberships are not overwritten.
New test credentials, if generated, are stored privately at
`.local/mysql-ec2/bootstrap-user-credentials.json`.

Use `--port gateway=18088` for a different public port. Individual frontend, API,
Keycloak and mailbox port overrides are no longer accepted.

`--check-only` performs verified-TLS, engine, writer, parameter and connection
budget checks without migrations or service startup. `--render-only` validates
configuration without contacting Aurora or starting containers. Supply
`--db-client-ip` to avoid route/DNS discovery and `--db-ca-file` to avoid a CA
download during offline rendering.

## Public routes

Only the gateway publishes **0.0.0.0:8088** by default. EC2 security-group ingress
for test clients needs just that application port, alongside existing SSH access.

| Component | Path |
| --- | --- |
| Frontend, assets and session-bound frontend APIs | `/` |
| API reference index | `/services/` |
| Data model | `/services/data-model/` |
| Ingestion | `/services/ingestion/` |
| Decisions | `/services/decisions/` |
| Screening and evidence capabilities | `/services/screening/` |
| Cases | `/services/cases/` |
| Auth | `/services/auth/` |
| Keycloak login, resources and administration | `/identity/` |

Each fraud API has `docs` and `readyz` under its service prefix. Keycloak's issuer
is `http://EC2_IP:8088/identity/realms/fraud`; the callback is
`http://EC2_IP:8088/api/auth/callback`.

The frontend retains session/tenant authorization. Raw APIs retain existing
bearer/service credentials; the proxy never injects operator credentials.
Screening capability URLs include the service prefix so evidence transfers use
the same gateway. Mailpit, reference MySQL and Keycloak management health remain
internal. No individual development port is published.

## Stages and recovery

1. Validate parameters, lock setup, retain credentials, render configuration and
   check the single public port.
2. Run read-only Aurora preflight. A failure leaves the running stack untouched.
3. Build images, stop runtime writers, create databases if absent and verify
   Keycloak's restricted database account and retained password.
4. Start reference MySQL, Mailpit, the gateway and Keycloak.
5. Install data-model and create/reuse the test tenant and transactions table
   through its gateway route. `--tenant-id` and `--object-type` select a scope.
6. Install ingestion, decision, auth, case and screening owners in order;
   refresh dependent grants, optionally provision the login and start frontend.
7. Verify runtime port bindings, frontend login, six API readiness/docs routes,
   Keycloak discovery URLs and signing keys.

`fraud_ec2_keycloak` has privileges only on `keycloak.*`, requires SSL, and uses
the exact EC2 database-client address. Provider collation is `utf8mb4_unicode_ci`;
fraud tables retain their existing ownership and collation. Unexpected global,
cross-database, grant-option or role privileges are rejected rather than
silently revoked. Existing databases and provider passwords are never replaced.

SQL grant source defaults to the EC2 host's selected route address. If NAT or
routing changes what Aurora sees, use `--db-client-ip ACTUAL_SOURCE_IPV4`.
Temporary DDL and Keycloak accounts use that exact address; inherited application
accounts can still have their existing wildcard hosts.

Repeat setup with the **same arguments and state directory** after resolving a
failure. The owner migration runner verifies lineage/drift and resumes supported
work. Failures after runtime shutdown can leave services partly stopped. Setup
does not erase application data or mark incomplete work successful.

Deployment identity includes public IP/port, provider storage kind, database
endpoint, migration account and database-client address. It cannot silently
change on a restart.

**Existing separate-port/PostgreSQL EC2 state is rejected before configuration
is rewritten or containers are stopped.** This update does not automatically
migrate old realms or rewrite application identity keys. An operator must
perform and verify that migration explicitly, retaining old state and the
PostgreSQL volume as rollback material. Do not delete state or create replacement
realms to bypass this check.

## Retained state and operations

State defaults to **`.local/mysql-ec2/`**, covered by the existing `/.local/` Git
ignore. Its parent is mode 0700; explicitly mounted secrets/configuration are
0644 inside that private parent for non-root access. Host-only environment,
Compose, deployment and bootstrap files remain 0600. Signing/encryption keys,
credentials and plans persist for recovery; Secrets Manager and RAM-only
credential storage are not introduced.

The project **`fraud-ec2-test`** is separate from `fraud-mysql-dev` state, images,
network and database.

```bash
docker compose --project-name fraud-ec2-test --profile install \
  --env-file .local/mysql-ec2/compose.env \
  -f .local/mysql-ec2/compose.json ps
```

Use the same prefix with `logs --tail 100 api-gateway keycloak` for diagnosis.
Repeat setup for upgrades/recovery; plain Compose `up` does not perform tenant
preparation and ordered grant refresh.

## Verification without AWS

```bash
# Offline configuration, retention and actual Compose validation
docker compose -f docker/mysql-ec2/compose.test.yaml run --rm --build --no-deps offline
# Disposable verified-TLS MySQL and owner provisioning checks
docker compose -f docker/mysql-ec2/compose.test.yaml up -d --wait mysql
docker compose -f docker/mysql-ec2/compose.test.yaml run --rm --no-deps owner-test
# Actual Keycloak/MySQL JDBC truststore and identity path through Nginx
docker compose -f docker/mysql-ec2/compose.test.yaml up -d --wait api-gateway
node docker/mysql-ec2/verify-fixture.mjs
# Optional real browser sign-in and PKCE exchange (frontend Playwright required)
node docker/mysql-ec2/verify-fixture.mjs --browser
# Remove test containers/network, retaining Go caches
docker compose -f docker/mysql-ec2/compose.test.yaml down
```

The fixture's credentials and port 18089 are isolated from application state and
Aurora. Offline render checks mount no Docker socket and contact no database.
A live EC2/Aurora installation and application browser login still need
acceptance against the provisioned cluster.

See the [single-port verification record](evidence/single-port-2026-11-12.json)
for the checks performed and their limits.
