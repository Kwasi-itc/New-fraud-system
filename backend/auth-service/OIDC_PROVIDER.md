# Local provider and configurable SSO

Decision AUTH-D01, 2026-10-06: the user selected an established identity provider
in Docker, with configurable/replacement SSO later. Implementation selects
**Keycloak 26.8.0** as the local default and **standard OIDC authorization code
with S256 PKCE** as the integration boundary. No custom password database.

Implemented: Docker provider, isolated provider schema/role in the existing
database, private local configuration generation, generic Go OIDC adapter,
discovery diagnostic, protocol tests and a real-provider browser smoke test.
Platform account mapping, browser login/callback, opaque sessions, role administration
and current-state case/owner authorization are now implemented; see
[PLATFORM_AUTH.md](PLATFORM_AUTH.md) for setup and acceptance. Directory/invitation
and production release work remains open. The provider-only smoke below still
intercepts its callback; the separate platform smoke exercises real application login.

## Local startup

From the `New-fraud-system` directory:

```powershell
node backend/auth-service/scripts/setup-local-provider.mjs
docker compose up -d keycloak
docker compose ps keycloak
```

The setup script enables `local-idp` in the ignored root `.env`; subsequent
`docker compose up -d --build` includes the provider. It preserves existing
profiles and generates random secrets once under ignored `.local/auth-provider/`.
Rerunning preserves credentials. Partial/mismatched configuration fails instead
of silently breaking the persisted provider. Keep these files private, backed up
with the local provider DB, and out of logs/screenshots/source control.

| File | Purpose |
| --- | --- |
| `keycloak.env` | Bootstrap administrator, provider DB password, application OIDC client secret and exact callback registration |
| `database.env` | Only the provider DB password for the schema setup job |
| `auth.env` | Generic auth-side development issuer/client/secret/callback/backchannel configuration; never expose in frontend public variables |

Open the [local administration console](http://localhost:8090/admin/). Use the
bootstrap username/password from the private `keycloak.env` file. The username
defaults to `local-admin`; the password is unique, not checked into this repo.
This is a **provider administrator**, not a provisioned fraud-platform tenant
administrator. The `fraud` realm contains the confidential `fraud-auth` client,
but no permanent application users are seeded. Create test users through the
provider console when needed; platform membership will require auth bootstrap.

This Compose profile is local development only: loopback-bound HTTP port 8090,
`start-dev`, local realm TLS requirement disabled. Production needs HTTPS and a
reviewed production deployment; setting `AUTH_ENV=production` rejects HTTP in
the adapter. Do not expose this profile remotely by changing only the bind port.

## Same database, provider ownership

The DB setup job uses the existing `fraud` database, creates restricted login
role `fraud_keycloak` and schema `keycloak`, and refuses a conflicting schema
owner. Keycloak manages its own schema migrations on startup. Its pool is 1–5
connections and must count toward the shared DB budget. No second application
database or volume is introduced. The future auth-owned `identity` schema is
separate; Case Manager must never read or write Keycloak internals.

An existing role password is not reset by setup. If private files are lost,
recover or explicitly rotate the provider role/client/admin credentials; do not
delete the shared PostgreSQL volume. The shared stack's existing superuser-based
services are not newly isolated by this provider role.

The realm import initializes an absent realm. It does not overwrite an existing
realm on each restart. Editing the template or local secret file is not a live
client/credential update; use the provider administration workflow and coordinate
matching auth settings. PostgreSQL persistence retains users and client settings.

## Generic provider configuration

| Setting | Semantics |
| --- | --- |
| `AUTH_ENV` | Explicit `development` or `production`; production requires HTTPS URLs |
| `OIDC_ISSUER_URL` | Exact trusted discovery/token issuer; local `http://localhost:8090/realms/fraud` |
| `OIDC_CLIENT_ID` | Confidential application client ID; local `fraud-auth` |
| `OIDC_CLIENT_SECRET` | Private server credential; no `NEXT_PUBLIC_*` equivalent |
| `OIDC_REDIRECT_URL` | Exact registered callback; local reservation `http://localhost:3000/api/auth/callback` (not implemented yet) |
| `OIDC_BACKCHANNEL_ORIGIN` | Optional transport-only origin for container connectivity; local container value `http://keycloak:8080`; unset for host-side tools |

The browser uses the public issuer origin. Docker server-side requests may use
the configured backchannel origin while token `iss` validation remains exact.
There is no insecure issuer override, disabled TLS verification, arbitrary
discovery URL input, token-controlled key URL or Keycloak admin API dependency
in the Go adapter. Provider identity output contains issuer/subject and optional
profile fields; it deliberately grants no platform roles or tenant membership.

Current interoperability requirements: discovery, S256 PKCE, confidential client,
RS256 ID tokens, and authorization/token/JWKS URLs on the issuer origin. Providers
using separate endpoint origins or different signing algorithms require an
explicit reviewed allowlist/configuration extension; do not turn off validation
to make them work. Adapter tests exercise two distinct issuer paths with the same
code, including a different transport host.

`AuthorizationURL` and `Exchange` are protocol building blocks, **not complete
callback handlers**. The forthcoming application flow must generate random state,
nonce and verifier, bind them to a browser, expire and atomically consume a login
transaction, verify returned state, then exchange the code and resolve an active
platform membership before creating a session. Never accept arbitrary callback
parameters as trusted inputs to `Exchange`.

## Future SSO options and migration

1. **Broker company SSO through Keycloak:** keep the application issuer/client
   settings and configure the upstream provider in Keycloak. Review account
   linking so an equal email cannot take over an existing operator.
2. **Replace Keycloak directly:** register the callback on a compatible OIDC
   provider, change the generic issuer/client/secret settings, and remove
   `OIDC_BACKCHANNEL_ORIGIN` unless a deliberate equivalent is needed. Remove
   `local-idp` from `COMPOSE_PROFILES` and explicitly stop the old provider after
   migration. Profile changes alone do not stop an existing container.

Stable **platform user IDs** must remain independent of external `(issuer, sub)`.
Provider switching is not automatically an account migration: implement the
reviewed identity mapping/linking workflow (AUTH-D04/M06/U07) before moving real
operators. Preserve case actor references and review receipts; never infer links
from email or trust incoming provider roles as platform administrator grants.
Drain outstanding login attempts, retire old sessions according to policy,
verify both account mapping and authorization, and document rollback. This
increment provides provider configuration/protocol compatibility, not completed
identity migration or simultaneous multi-provider routing.

Do **not** replace existing `CASE_USER_JWT_ISSUER` or public keys with Keycloak's
issuer/keys yet. Provider ID tokens are not Case API tokens, and platform
tenant/role/session enforcement is still pending.

## Verification and diagnostics

From `backend/auth-service`, supply the generic environment settings above:

```powershell
go run ./cmd/oidc-check
go test ./...
go vet ./...
```

For host-side `oidc-check`, unset the Docker-only backchannel origin. Success
prints the verified issuer and explicitly reports that browser application login
is not checked by this command. HTTP responses are capped at 1 MiB; requests time out after
10s; discovery/token redirects and unexpected endpoint origins are rejected.
Provider errors never expose returned token bodies or client secrets.

From the workspace root, with the existing frontend Playwright dependency and
an installed Chromium/Chrome browser:

```powershell
node backend/auth-service/scripts/smoke-local-provider.mjs
```

If using an installed browser instead of Playwright's download, set
`PLAYWRIGHT_BROWSER_PATH` to its executable (the same convention as frontend tests).
The smoke test is **local only** and uses bootstrap admin credentials to create
one temporary user, perform real browser login, exchange the code, verify the
signed ID token, reject code replay and test that the application password grant
is disabled. It deletes the temporary user on completion. It intercepts the
reserved callback; it does not claim to test application sessions or Case access.

Verified 2026-10-06: auth Go unit/protocol tests and vet pass; Compose config
validates; the actual Keycloak image starts in the existing stack; Go discovery
check passes against it; browser smoke passes using installed Chrome after the
default Playwright browser executable was found missing. See the tracker for
remaining shared-database, session, mapping and live Case release gates.

Provider references: [containers](https://www.keycloak.org/server/containers),
[database schema configuration](https://www.keycloak.org/server/db),
[realm import behavior](https://www.keycloak.org/server/importExport),
[hostname configuration](https://www.keycloak.org/server/hostname).
