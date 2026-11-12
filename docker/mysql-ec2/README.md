# EC2 HTTP test deployment with Aurora

Run `setup.py` **on the EC2 Docker host**, from this checkout. It deploys the
frontend, six backend services and workers, Keycloak, a PostgreSQL container for
Keycloak, Mailpit, and a disposable migration reference server. All application
data stays in the **single Aurora `fraud` database**. The reference server holds
schema definitions used to verify owner migrations; application traffic never
uses it.

Reference MySQL uses a separate generated password; it does not receive the
Aurora administrator password.

This is the agreed IP-address/HTTP **test deployment**, not production activation.
It retains the existing test screening/decision providers, privileged data-model
DDL processes, and test email capture. HTTP browser traffic is unencrypted. Use a
restricted test environment and test identities/data. Production still needs
HTTPS and the remaining production runtime/provider work.

## Prerequisites

- Linux Docker Engine, a current Docker Compose plugin supporting `include` and
  `config --no-env-resolution`, Python 3.10+, and Node.js 22.
- An Aurora **MySQL 8.4 compatible writer**, reached through its AWS DNS endpoint.
  The script checks the actual engine; Aurora 3.x/MySQL 8.0 is rejected. Its
  reference schema uses the repository's pinned MySQL 8.4.8 image. Cross-version
  schema definitions must pass the existing migration checks.
- EC2 network/security-group access to the Aurora writer port. Use the Aurora
  migration administrator with permission to create the `fraud` database,
  install owner schemas/routines/triggers, manage the restricted application
  accounts/grants, read required MySQL account metadata, and delegate
  `SET_ANY_DEFINER`. Read-only preflight does not prove every DDL privilege.
- Aurora parameter `log_bin_trust_function_creators=1`. The script does not
  change AWS parameter groups. `max_connections` must be at least **140** for this
  single-replica stack: configured runtime pool caps total 100, with 40 reserved
  for temporary DDL sessions, migrations and operators. Budget any other clients
  separately; this is a connection budget, not a throughput claim.
- Registry access to build/pull images. The RDS CA bundle is downloaded from AWS
  unless supplied with `--db-ca-file`. Every Aurora connection verifies the CA
  and DNS hostname; there is no insecure-TLS option.

## Start

```bash
python3 docker/mysql-ec2/setup.py \
  --public-ip 203.0.113.10 \
  --db-host YOUR-WRITER.cluster-XXXX.REGION.rds.amazonaws.com \
  --db-user YOUR_MIGRATION_ADMIN \
  --port frontend=3002 \
  --port keycloak=8090 \
  --bootstrap-user test-admin-user \
  --bootstrap-email test-admin@example.com
```

Replace the example IP/endpoint/user/email. The script prompts for the database
password on first use. Alternatively pass `--db-password-file /private/path` or
`--db-password-stdin`; password text is never a command argument. Files must
contain the exact password without a trailing newline. An explicitly supplied
password must match retained state; credentials are never silently rotated.

The optional bootstrap flags create one Keycloak test account and provision its
application membership after migrations. Default roles are `tenant_admin`,
`platform_admin`, and `case_manager_admin`; override with `--bootstrap-roles`.
Existing Keycloak passwords and existing application users/roles are preserved;
the application's provision command rejects mismatching existing membership.
New login credentials are stored in
`.local/mysql-ec2/bootstrap-user-credentials.json` with mode 0600.

Use `--check-only` for verified-TLS, engine, writer, parameter and connection
budget checks without schema migrations or service startup. It prepares private
configuration and builds/runs only the preflight container. Use `--render-only`
to validate configuration without connecting to Aurora or starting containers;
also supply `--db-client-ip` to avoid routing/DNS discovery. CA download is still
needed unless `--db-ca-file` is provided.

## Ports and access

Repeat `--port NAME=NUMBER` for any of these defaults. Ports must be distinct and
in 1024–65535. The script checks availability before stopping running services.
Container ports remain fixed; browser URLs/callbacks use the selected host ports.

| Name | Default | Host binding |
| --- | ---: | --- |
| `frontend` | 3002 | All interfaces |
| `keycloak` | 8090 | All interfaces |
| `data-model` | 8080 | Loopback |
| `ingestion` | 8081 | Loopback |
| `decisions` | 8082 | Loopback |
| `screening` | 8085 | Loopback |
| `cases` | 8086 | Loopback |
| `auth` | 8087 | Loopback |
| `mailbox` | 8027 | Loopback |

Open the selected frontend and Keycloak ports in the EC2 security group for your
test clients. Both are needed for browser login. APIs and mailbox are available
on the host or through an SSH tunnel. `--api-bind 0.0.0.0` explicitly publishes
those ports externally; restrict their security-group access as needed. No local
MySQL, reference MySQL, or provider PostgreSQL port is published.

The frontend requires a real application session and retains user/tenant checks.
Data-model and ingestion direct APIs require their private service credentials;
decision APIs accept their existing verified session/service contracts. This
profile does not introduce tenant-scoped credentials for external customers.

Screening evidence URLs use the selected screening host port, so direct
capability uploads/downloads need client reachability to that port (for example,
publish APIs for restricted test clients). Leaving API binding on loopback keeps
these direct browser evidence operations unavailable remotely.

The source address for SQL grants defaults to the EC2 host's selected route
address. If NAT/routing changes the address Aurora sees, supply
`--db-client-ip ACTUAL_SOURCE_IPV4`. Temporary physical-DDL accounts use that
exact address. Other inherited local-development accounts may retain wildcard
hosts; verified TLS, private networking and credentials remain required.

## Stages and recovery

1. Validate parameters, lock setup against concurrent runs, prepare credentials
   and Compose configuration, and check ports.
2. Run read-only Aurora preflight. Failure here does not stop the running stack.
3. Build images, stop the previous test runtime, create the shared database if absent, and
   start reference MySQL, provider PostgreSQL, Mailpit and Keycloak.
4. Apply data-model migrations and start its API/DDL
   processes. Create/reuse the test tenant and published `transactions` table.
   `--tenant-id` and `--object-type` select an existing scope instead.
5. Apply ingestion, decision, auth, case and screening migrations in order;
   start each owner's API/workers and refresh dependent grants.
6. Optionally provision the test login and start the frontend.

Rerun with the **same arguments and state directory** after resolving a failure.
The existing owner migration runner verifies retained plans/applied definitions
and resumes supported work. It fails on drift or uncertain outcomes that need
reconciliation; the script does not mark failures successful or erase data.
Failures after runtime shutdown may leave the deployment partly stopped.

Public IP, auth port and Keycloak port form issuer identity and cannot silently
change in retained state. Reconcile memberships, sessions and provider settings
explicitly before moving them. Changing the frontend port updates its callback.
The script never resets existing users or passwords to accommodate a change.

## Retained credentials and operations

Default state is **`.local/mysql-ec2/`**, already covered by the repository's
`/.local/` Git ignore. Its enclosing directory is mode **0700**. Selected
file-backed secrets/configuration are mode 0644 inside that private directory so
non-root containers can read their explicit mounts. Private host-only env,
Compose, deployment and bootstrap files are mode 0600. Back up/restrict the state
directory and Keycloak volume together. Do not print private Compose environment
output into shared logs.

Credentials, signing/encryption keys, provider configuration and migration plans
remain on disk for restart/recovery. **No RAM-backed credential storage or
Secrets Manager integration is used.** Temporary atomic-write files are removed.
Deleting retained keys would invalidate sessions/invitations or lose provider
access; setup never deletes them as cleanup.

The isolated Compose project is `fraud-ec2-test`; it does not reuse the local
`fraud-mysql-dev` database, network, images or credential directory.

```bash
docker compose --project-name fraud-ec2-test --profile install \
  --env-file .local/mysql-ec2/compose.env \
  -f .local/mysql-ec2/compose.json ps
```

Use the same Compose prefix with `logs --tail 100 SERVICE` for diagnosis.
Run setup again for migration/recovery; plain `up` does not perform the script's
tenant preparation and ordered grant refresh.

## Verification

```bash
python3 -m unittest discover -s docker/mysql-ec2 -p 'test_*.py' -v
node --test frontend/scripts/http-test.test.mjs
cd backend/mysqlstore && go test ./ec2test ./localdev ./cmd/ec2-test-preflight
```

The compiled frontend cookie flow can also be checked with the loopback auth
stub in `frontend/scripts/http-test-smoke.mjs`, run inside the built frontend
image with `--network none`. It checks both the ordinary Secure cookies and the
explicit EC2 HTTP test cookies without contacting a real identity provider.

Offline tests check port/config generation, credential retention, interrupted
model preparation, preflight failure ordering, origin checks, credential
boundaries and verified-TLS settings. A live Aurora installation and browser
login must still be verified against the actual provisioned cluster. These
checks do not establish production readiness.
