# EC2 deployment with a configurable HTTPS domain

This deployment extends the PostgreSQL stack from `main`. It runs the frontend,
APIs, workers and Keycloak on one Ubuntu amd64 EC2 instance, sharing one logical
PostgreSQL database. Only the HTTPS edge publishes application ports. The local
development stack and its data are not modified by these deployment files.

## Files

| File | Purpose |
| --- | --- |
| `instance.json` | Optional CloudFormation instance, security group, encrypted retained root disk, Elastic IP and Systems Manager role |
| `bootstrap-ubuntu.sh` | Install Docker Engine/Compose from Docker's Ubuntu repository; preserve an existing installation |
| `run.sh` | Configure, deploy, back up, verify, stop and invoke auth administration |
| `configure.mjs` | Generate private configuration once, preserve matching keys/credentials and reject accidental origin/volume changes |
| `compose.yml` | HTTPS/runtime overlay, private database/API ports and restart policies |
| `Caddyfile` | Automatic certificates, HTTPS redirects and public identity administration restrictions |
| `probe.mjs` | Verify trusted TLS, public OIDC discovery, service readiness and login initiation |

## Instance and DNS prerequisites

Use an existing Ubuntu amd64 instance, or review `instance.json` and deploy it
through CloudFormation with `CAPABILITY_IAM`. Select your VPC, public subnet,
trusted Ubuntu 24.04 LTS amd64 AMI, key pair, instance type and operator SSH CIDR.
No instance type is selected automatically and no throughput is promised. Size
the host and disk for all services, image builds, database growth and backups.

The optional template allows TCP 80/443 publicly and restricts SSH to the operator
CIDR. It requires IMDSv2 and does not publish database/backend ports. Its root
EBS disk is encrypted and retained when the instance terminates; that retained
disk requires operator lifecycle management and is not a backup.

Point the chosen hostname's DNS A record at the instance's stable public address.
Allow inbound 80 and 443 for certificate validation, redirects and HTTPS. Avoid
an AAAA record unless IPv6 routing is actually configured. This configuration
assumes Caddy receives traffic directly; an ALB/CDN needs its own reviewed proxy
and trust configuration.

Clone this repository on the instance using your normal private-repository access.
From the `New-fraud-system` repository root:

```bash
sudo bash deploy/ec2/bootstrap-ubuntu.sh
sudo bash deploy/ec2/run.sh configure \
  --domain fraud.example.com \
  --email ops@example.com \
  --case-tenants 'EXISTING_TENANT_UUID,ANOTHER_TENANT_UUID'
sudo bash deploy/ec2/run.sh check
sudo bash deploy/ec2/run.sh up
```

Replace the example hostname/email and UUIDs. The case allowlist must refer to
tenants your installation already owns or is restoring. This tooling configures
infrastructure and services; it does not create customers, provider users or
grant memberships. Initial business onboarding remains an explicit owner/API
and auth-administration operation. Never use placeholder UUIDs as real tenants.

The default data volume is `fraud-ec2_postgres_data`, separate from the local
development volume. `configure --volume CUSTOM_NAME` selects a name on first
setup. If that volume already contains data, restore its matching deployment
configuration and credentials first; newly generated passwords cannot adopt an
existing database. Subsequent origin/volume changes are rejected rather than
silently switching data or creating a replacement identity.

## Runtime and identity behavior

The browser uses `https://YOUR_DOMAIN`; login uses
`https://YOUR_DOMAIN/identity/realms/fraud`, and the callback is
`https://YOUR_DOMAIN/api/auth/callback`. Frontend workspaces come from the current
session; no public build-time tenant setting is needed.

Caddy terminates TLS and forwards to the private nginx gateway. Nginx fixes the
forwarded host/scheme/identity port to the configured HTTPS origin and discards
client-supplied `Forwarded`. Auth uses production OIDC validation, including
certificate and issuer checks. Docker DNS resolves the public hostname to the
TLS edge internally, so auth does not downgrade to an HTTP backchannel.

Keycloak runs `start`, with HTTP only on the private proxy network, an explicit
HTTPS hostname and the real `/identity` context. Its public admin/master realm
routes are blocked at the edge; use SSH/container administration for operators.
Updating a persisted realm changes the callback allowlist and SSL requirement,
while preserving users, passwords and application memberships.

First-administrator provisioning remains explicit. After verifying the actual
provider subject and existing tenant, use the auth-owned command, for example:

```bash
sudo bash deploy/ec2/run.sh auth-admin -action provision \
  -tenant EXISTING_TENANT_UUID \
  -issuer https://fraud.example.com/identity/realms/fraud \
  -subject VERIFIED_PROVIDER_SUBJECT -name 'Platform operator' \
  -roles tenant_admin,platform_admin,case_manager_admin,case_investigator
```

This command does not create a Keycloak account. Follow the
[platform identity contract](../../backend/auth-service/PLATFORM_AUTH.md).
Moving an existing realm to another hostname also requires an explicit issuer
binding repair; matching email addresses are not identity proof.

## Secrets, data and redeployment

Private files are generated under ignored `.local/auth-provider/` and
`.local/ec2/`. Configuration is not sourced as shell code, credentials are not
printed, and an incomplete/mismatched bundle is rejected. Do not run the EC2
configuration command over a development checkout's existing identity files.

For an existing database migration, carry the matching identity keys, OIDC client
secret, provider/database credentials and membership data through a separate
reviewed migration. Restoring only the PostgreSQL dump into a new random-secret
installation is insufficient. The configure command deliberately refuses to
invent replacements for missing keys or automatically adopt another environment.

Deployment builds sequentially, starts PostgreSQL, creates a private database and
identity backup, then starts the router/TLS/Keycloak before auth discovery. It
checks the real HTTPS issuer before starting the full stack, then checks health
and login initiation. Changing code/migrations requires a new deployment:

```bash
sudo bash deploy/ec2/run.sh up
sudo bash deploy/ec2/run.sh up --skip-build  # Only for already built images
sudo bash deploy/ec2/run.sh verify
sudo bash deploy/ec2/run.sh status
sudo bash deploy/ec2/run.sh backup
sudo bash deploy/ec2/run.sh stop
```

Deploy/configure/backup/stop operations share a lock. Backups are written under
`.local/ec2/backups/`; incomplete outputs keep a `.partial` suffix. Keep this
directory private, monitor free disk space and establish encrypted off-instance
copies/retention and restore tests. Neither `stop` nor deployment removes volumes.
Do not use `down -v` or delete Docker's storage to repair a deployment.

Default pgx pools are bounded to four connections per process where the service
respects the shared connection options; auth and Keycloak retain their explicit
five-connection limits. Review the combined pool/concurrency budget against the
actual workload. This is one host with local PostgreSQL, not a highly available
release: service-specific database roles, off-instance recovery, live-domain
acceptance and workload measurements remain operational work. Screening source
providers and decision-time forced outcomes remain subject to the
[existing PostgreSQL limitations](../../backend/screening-service/POSTGRESQL_PORT_STATUS.md).

## Verification and current status

- [x] Nine configuration/template/Compose tests passed, including secret continuity,
  origin/volume rejection, port isolation and dependency-cycle checks.
- [x] Caddy configuration validated in the pinned official image.
- [x] Bash syntax and ShellCheck passed; CloudFormation passed `cfn-lint`.
- [x] Isolated HTTPS fixture passed certificate verification, redirects, all six
  API prefixes, identity forwarding, anti-spoofing, admin restrictions and
  callback-query log checks.
- [x] Disposable PostgreSQL accepted its file-based credential and an authenticated
  TCP connection; no application database was used for this check.
- [ ] Launch/reconfigure an actual EC2 instance using the chosen domain.
- [ ] Validate live public certificate issuance and real user sign-in.
- [ ] Complete deployment-specific tenant/user onboarding, restore and load tests.

Reproduce configuration checks from a host with Node and Docker:

```bash
EC2_COMPOSE_TESTS=true node --test deploy/ec2/configure.test.mjs
```

Transport fixtures have their own network, temporary CA and no application data:

```bash
node deploy/ec2/prepare-test.mjs
docker compose -f deploy/ec2/compose.test.yml up -d --wait
node deploy/ec2/gateway-contract.mjs
docker compose -f deploy/ec2/compose.test.yml down
```

The fixture trusts only its generated local CA; production probes use normal
certificate verification. Gateway access logs omit queries, cookies and bodies.
Caddy filters request URIs/headers from diagnostics; generated nginx uses critical
diagnostic logging because ordinary nginx error logs can include OAuth callback
queries. Use sanitized access status/timing and health probes for routine checks.

Reference contracts: [Docker Ubuntu installation](https://docs.docker.com/engine/install/ubuntu/),
[Caddy automatic HTTPS](https://caddyserver.com/docs/automatic-https),
[Caddy forwarding](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy),
[Keycloak reverse proxy](https://www.keycloak.org/server/reverseproxy),
[AWS security groups](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/security-group-rules-reference.html)
and [IMDSv2](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/configuring-instance-metadata-service.html).
