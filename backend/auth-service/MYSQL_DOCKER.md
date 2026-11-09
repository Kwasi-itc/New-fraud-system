# Local auth service with MySQL

The API is at **http://localhost:8087/docs**. Auth application data uses the same
MySQL 8.4.8 `fraud` database as data-model, ingestion and decisions. The separate
invitation worker publishes through the local SMTP mailbox at
**http://localhost:8027**.

The user-selected existing Keycloak remains the OIDC issuer; its existing
PostgreSQL storage is unchanged. Auth's identity, membership, session, invitation,
audit and delivery state use MySQL. Provider claims and email addresses do not
automatically grant application permissions.

From this service directory, with Docker Desktop running Linux containers:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/setup-mysql.ps1
```

This prepares private configuration, starts the existing Keycloak dependencies,
verifies the identity migration prefix and installs auth's additional owner plan.
It refreshes auth account grants with the API/worker stopped, then starts the two
restricted roles and local mailbox. Repeating setup preserves keys, credentials
and business data. `-SkipProviderStartup` keeps provider lifecycle external when
the configured existing provider is already available. The setup does not start
the PostgreSQL-backed auth API or migrate its application tables.

| Address | Purpose |
| --- | --- |
| http://localhost:8087/docs | Swagger UI |
| http://localhost:8087/openapi.json | OpenAPI 3.0.3, 14 existing operations |
| http://localhost:8087/capabilities | Development profile and authentication scope |
| http://localhost:8087/readyz | Applied migration counts and required restricted table grants |
| http://localhost:8027 | Local invitation email capture |

Every `/internal/` request needs the proxy bearer credential in the ignored file
`../../.local/mysql-dev/auth-proxy-token.txt`. Swagger's Authorize button accepts
that credential. Most request bodies also need an opaque session issued after a
real login; the proxy credential is not a session. Wrong-origin browser requests
are rejected. The runtime never supplies a development login bypass.

The frontend callback remains the configured OIDC redirect. Point its server-side
auth integration at this API and trust the generated public signing key only as
part of an explicit frontend/consumer rollout. The backend acceptance runner
captures a real browser callback to exercise the API directly; it does not certify
the existing frontend cookie/proxy deployment or production SSO cutover.

## Explicit operator provisioning

Login requires an already provisioned membership. Obtain the real provider issuer
and immutable subject (Keycloak user ID), and prepare this private JSON file as
`../../.local/mysql-dev/auth-runtime/provision.json`:

```json
{
  "TenantID": "existing-active-data-model-tenant-uuid",
  "Issuer": "http://localhost:8090/realms/fraud",
  "Subject": "existing-keycloak-user-id",
  "Name": "Local administrator",
  "Roles": ["tenant_admin", "platform_admin"]
}
```

Then run:

```powershell
docker compose --profile install --env-file ../../.local/mysql-dev/compose.env -f docker-compose-mysql.yml run --rm --no-deps migrate-auth /app/install-auth --provision /runtime/provision.json
```

Provisioning checks the tenant's active data-model registration, creates the auth
activation entry only when missing, and preserves existing roles, versions and
revocation. A mismatching replay conflicts. Provisioning does not refresh grants
under running traffic. It never links an account merely because emails match.
External subjects retain exact byte identity and the source API's 1 KiB byte cap;
issuer values are bounded to 2 KiB. Hash-key collisions are checked against the
complete stored issuer/subject pair.

Sessions remain scoped to their selected active membership. With no configured
default tenant, login preserves the source's first-active-membership selection;
it does not create a global tenant allowlist. Invitations select their own tenant.
Tenant activation belongs to auth's identity registry; coordinated platform-wide
activation/cutover remains an operational contract.

## Ownership, workers and configuration

The 14-step `identity` prefix used by decisions is preserved. Seven additional
tables use the independent `identity_auth` migration ledger. Plans come from the
separate reference MySQL server and the standard migration runner rejects target
drift. Runtime readiness checks the applied counts/state and exact table grants;
full schema-definition verification belongs to the installer.

API and worker pools are capped at five and three connections respectively. Their
connection/session settings, lock waits and request/worker deadlines are bounded.
The worker can update delivery rows and delete expired login attempts in batches
of at most 100. It cannot read session digests or change memberships/invitations.
Neither runtime gets DDL, schema-wide/global grants, or audit UPDATE/DELETE.

Private files live under ignored `../../.local/mysql-dev/`. The API alone receives
the proxy credential, signing key and OIDC client secret. Both roles need the
invitation encryption key. Configuration and tokens are not returned in docs or
generic logs. The normal PostgreSQL binaries and preflight tools remain separate.

SMTP capture uses pinned Mailpit; its [official Docker documentation](https://mailpit.axllent.org/docs/install/docker/)
describes the image and mailbox/SMTP interfaces. SMTP is available only inside the
Docker network, while the mailbox UI is bound to loopback. Captured invitation
links are private development credentials; they are not test-report contents.

## Verification

```powershell
docker compose -p auth-mysql-integration -f compose.mysql-test.yaml run --rm test
node scripts/summarize-mysql-tests.mjs
docker compose -p auth-mysql-integration -f compose.mysql-test.yaml down
node scripts/mysql-endpoint-audit.mjs
```

The private fixtures include separate MySQL target/reference servers and
PostgreSQL characterization. They publish no application ports and never reset
the application database. The full inventory, raw test events, vet and race
results are retained under `evidence/mysql-integration/`.

The live audit needs the running auth roles, mailbox, existing local Keycloak,
installed Playwright Chromium, and at least two active registered data-model
tenants. It creates temporary provider users, provisions synthetic MySQL
memberships, verifies all 14 operations, and removes only those provider users.
Synthetic MySQL identities, audit and captured mail are retained. Request traces
record paths/statuses, not tokens, passwords or HTTP bodies. No external emails
are sent. See [endpoint status](MYSQL_ENDPOINT_STATUS.md),
[worker status](MYSQL_WORKER_STATUS.md) and [plan](MYSQL_PLAN.md) for exact evidence
and the distinction between native, process, live-provider and release checks.

The final [installation proof](evidence/mysql-endpoint-audit/2026-11-09T10-29-40.911Z/installation.json)
matches all ordered IDs/checksums for the unchanged 14-step identity prefix and
seven auth steps, with no dirty steps and both roles healthy. The browser
[Swagger check](evidence/mysql-endpoint-audit/2026-11-09T10-29-40.911Z/swagger-ui.json)
renders all 14 operations without JavaScript errors.
