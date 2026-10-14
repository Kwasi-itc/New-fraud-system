# Platform accounts, sessions and permissions

## Invitation onboarding increment - 2026-10-09

Application roles remain auth-owned; Keycloak owns login credentials, MFA and
provider identity. Tenant administrators now have `/administration/users`, linked
from My Account, for invitations, existing member access and an access audit
trail. The old My Account member editor remains available for compatibility.
This section supersedes earlier statements that all new members require CLI
provisioning. The CLI remains the first-administrator bootstrap mechanism.

Auth schema **version 3** adds invitations, encrypted mail delivery and an
invitation digest on browser-bound login transactions. Apply it before starting
the new auth binary. Existing memberships receive no new permissions.

### Invitation contract

- An active tenant administrator selects an email and fixed roles. Their UUID
  request key makes identical create retries idempotent. A tenant/email can have
  only one pending invitation, including expired ones; resend or cancel that row.
  Creation is capped at 100 per tenant per hour. Resend has a one-minute cooldown.
- Invitations expire after 48 hours. Recipient local-part case is preserved;
  domains are lowercased. Display-name addresses, header injection and malformed
  addresses are rejected. Provider-specific alias/dot rewriting is not applied.
- The database keeps a digest for acceptance, plus a separate AES-256-GCM
  encrypted delivery payload. The API never returns a token. The email link puts
  its secret in a URL fragment, which is removed from history on page hydration;
  an explicit form POST starts Keycloak login. No token goes into a query string,
  local storage, audit event or generic error log. Do not add analytics or request
  body capture to this route.
- Login completion uses the trusted provider's verified email, issuer and subject.
  Email is an invitation eligibility check, never a key for linking existing
  identities. The browser's single-use login state binds the invitation digest.
  Acceptance rechecks expiry, generation, active tenant and the inviter's current
  active administrator membership/version. Changing that membership invalidates
  its pending grants until an active administrator resends them.
- Membership insertion, consumption, session creation and audit commit together.
  Concurrent acceptance succeeds once. The session selects the invited tenant,
  even when the identity already belongs elsewhere. An existing membership in
  that tenant is never overwritten, reactivated or elevated by an invitation;
  an administrator must use the normal access editor instead.
- Cancel invalidates the link. Resend rotates it and refreshes the grantor/version.
  Delivery claims use leases and generation fencing, with no transaction held
  across SMTP. A delayed old email cannot grant access after resend/cancel.
- Delivery attempts are bounded at five with exponential retry delay. The UI
  distinguishes queued/sending/failed from mail-server acceptance; it never
  promises inbox delivery. Failed mail requires explicit resend after correcting
  configuration. SMTP has at-least-once delivery: a crash after relay acceptance
  can duplicate mail, but cannot duplicate membership or acceptance. Encryption
  payloads are cleared after success, terminal failure, cancellation or acceptance.
- The audit UI lists only the current tenant's events, with UUID cursor pagination
  (not chronological ordering), actor, target, timestamp and change details.

### Enable and configure

1. Run `node backend/auth-service/scripts/setup-platform.mjs`. It preserves existing
   secrets and creates `.local/auth-provider/invitations.env` with a random
   `AUTH_INVITATION_KEY`. Invitations are disabled by default; no email is sent by
   the setup script. The optional env file is mounted only into auth by Compose.
2. Configure `AUTH_APP_ORIGIN` to the exact browser origin. HTTPS is required except
   localhost/127.0.0.1 development. Configure `AUTH_SMTP_ADDR` as `host:port`,
   `AUTH_SMTP_FROM`, and optional `AUTH_SMTP_USERNAME`/`AUTH_SMTP_PASSWORD`.
   Real SMTP requires STARTTLS with certificate verification. Set
   `AUTH_SMTP_LOCAL_CAPTURE=false` for real delivery. The generated `mailpit:1025`
   value is only a placeholder for an independently configured local mail capture;
   this stack does not create a capture server. Explicit local capture permits
   plaintext SMTP and rejects credentials. Never use that mode for real mail.
3. Keep the encryption key private and backed up. There is no automatic multi-key
   rotation in this increment; rotating it requires resending outstanding invites
   to replace delivery ciphertext and invalidate previous links. Restore handling
   and retention procedures remain release work.
4. Apply auth migration v3, rebuild auth/frontend, set
   `AUTH_INVITATIONS_ENABLED=true`, and recreate auth with the configured env file.
   Enabled invitations fail startup if key/origin/SMTP configuration is invalid.
5. Recipients must be able to authenticate with a verified email in Keycloak.
   The checked-in realm disables public registration. Existing enterprise accounts,
   federation or a separately approved provider-registration process are required;
   invitations neither create Keycloak accounts nor enable public signup.
6. Sign in as a tenant administrator, open My Account -> Manage users, invitations
   and access, and invite a controlled recipient. Verify actual mail, callback,
   permission denial, cancellation and revocation in the intended deployment before
   enabling routine onboarding. Inbox membership remains a separate Case decision.

This increment has not been deployed and no real invitations have been sent.
Older auth binaries reject schema v3; use a compatible forward fix instead of
reverting the binary alone. Distributed abuse controls beyond the stated limits,
retention, production mail monitoring, provider lifecycle synchronization and
full-stack provider/mail acceptance remain open.

2026-10-08 update: [Data Model authorization](DATA_MODEL_AUTH.md) adds auth schema
version 2, four explicit tenant-scoped platform roles, current-state Data Model
checks, transactional write revocation, a private frontend proxy and owner-client
credentials. The original version-1 acceptance below remains historical evidence;
use the linked rollout instructions for this increment. Other legacy APIs remain
open AUTH-X13 work.

Implementation increment: 2026-10-06. This is the current implemented contract;
the broader [TODO](IMPLEMENTATION_TODO.md) remains the release tracker.

## Implemented behavior

* Keycloak (or the configured OIDC provider) authenticates credentials using
  authorization code flow with S256 PKCE. Auth stores a five-minute, single-use
  login transaction bound to a separate HttpOnly browser cookie, state and nonce.
  The callback consumes it before calling the provider. Provider email/roles
  never grant platform access.
* Explicit operator provisioning maps the **exact issuer and subject** to an
  immutable platform UUID. Unknown identities cannot self-register. The tenant
  must already exist and be active in Data Model's `core.tenants`.
* Auth owns `identity` in the same logical `fraud` database. Users, external
  identities, tenant memberships, sessions, login transactions, audit events
  and migration version 1 are implemented in
  [schema.sql](internal/platform/schema.sql) and [store.go](internal/platform/store.go).
* The browser receives an opaque 256-bit session secret; only its SHA-256 digest
  is persisted. `__Host-fraud_session` is Secure, HttpOnly, SameSite=Lax, Path=/.
  Absolute expiry is eight hours; idle expiry is thirty minutes, extended at
  most once per five minutes. Local Chrome permits secure cookies on localhost;
  remote deployments require HTTPS. There is no provider refresh-token storage.
* A session is bound to one tenant. `AUTH_DEFAULT_TENANT_ID`, when set to a nonzero
  UUID, selects that tenant for ordinary logins and requires an active membership
  and active tenant. Missing access denies login; there is no fallback to another
  membership. Invalid configuration prevents startup. When unset, login preserves
  the legacy first-active-membership selection ordered by tenant UUID. Compose
  sets this value from `NEXT_PUBLIC_DATA_MODEL_TENANT_ID` so frontend and ordinary
  login use the same configured workspace. This does not grant roles or modify
  existing sessions: rebuild/recreate Auth, then sign out and back in. Invitation
  acceptance still selects the invitation's tenant. Interactive tenant switching
  is not implemented.
* Next resolves the opaque session through a private authenticated auth endpoint.
  Auth issues an RS256 credential valid for at most sixty seconds, audience
  `case-manager`, issuer `urn:fraud:identity`, with platform `sub`, `tenant_id`,
  `roles`, `sid` and `authz_version`. Only Next receives this credential.
* Case Manager and both owner investigation APIs validate signature and current
  account, membership, version and session state. Compose sets
  `USER_REQUIRE_SESSION=true`; direct API calls do not avoid these checks.
  Inactive tenants are rejected. Identity-store failures fail closed.
* Case/owner mutations acquire identity locks before case/inbox/resource locks:
  user -> tenant membership -> session. Suspension/role changes conflict with
  these locks. An already-authorized mutation may finish before suspension
  commits; subsequent commands, including receipt replays, are denied afterward.
  No provider/network operation executes under these locks. The contract is
  `identity.authorize_case_v1`, consumed by [shared investigator code](../investigator/identity.go).
* My Account shows the verified name/roles and offers POST logout. Logout commits
  session revocation before clearing the cookie. It signs out of this application;
  it does not terminate the provider's SSO session.
* My Account also lists the caller's active application sessions in the current
  tenant, with creation/absolute/idle expiry times and a current-session marker.
  Pages contain at most 100 records, using a UUID cursor. No cookie secrets,
  digests, access tokens or invented device/location labels are returned.
  Users can revoke one owned session or all of their sessions **in this tenant**.
  Both actions require confirmation, a valid current session and same-origin POST.
  Even administrators cannot use this self-service endpoint against another
  account or tenant. Revocation and audit commit atomically; revoking the current
  session clears its secure cookie and redirects to login.
  Session management locks user -> own membership -> current/target sessions;
  the exclusive membership lock serializes competing self-service revocations,
  creation and permission changes for that one account/tenant. It also waits for
  case operations already holding the membership authorization lock. A new login
  after revocation may create a fresh session; this does not disable the account.
* Tenant administrators list accounts with keyset pagination and change roles or
  tenant-active state with an expected version and confirmation. Changes revoke
  all of the target's sessions in that tenant and append an audit event atomically.
  Competing administrator changes serialize per tenant; ordinary case work does
  not. The last active tenant administrator cannot be removed. Reactivation
  requires a fresh application login and does not restore old sessions.
* Strict-mode inbox membership rejects nonexistent/inactive/non-case users;
  assignment candidate queries exclude them. Full concurrent target-eligibility
  enforcement and named assignee selection remain AUTH-C04–C06 work.

## Roles

| Role | Capability |
| --- | --- |
| `tenant_admin` | List/edit accounts in the current tenant, including role grants. Does not independently grant case access. |
| `case_manager_admin` | Existing Case Manager administration and case investigation within the current tenant. Does not grant account administration. |
| `case_investigator` | Existing investigator operations subject to Case Manager inbox/resource permissions. |

Grant both administrator roles explicitly when one person needs both duties.
Case inbox membership, workload, assignment and history remain Case Manager owned.
Provider groups/roles are not implicitly trusted. Role changes are not profile edits.

## Run and provision a real operator

From `New-fraud-system`:

```powershell
node backend/auth-service/scripts/setup-local-provider.mjs
node backend/auth-service/scripts/setup-platform.mjs
docker compose up -d --build
```

The setup scripts preserve existing generated configuration. The platform script
sets the three investigator consumers to the new public key/issuer; old development
JWT cookies are no longer accepted after recreation. Private signing material is
mounted only into auth, proxy credentials only into auth/Next, and public keys into
the three consumer APIs. Keep `.local/auth-provider` and `.env` private and backed up.
Keycloak's dedicated schema/DB role remains separate from application table ownership.

The sign-in branding is **ITC Fraud System**. For an already-imported local
Keycloak realm, run `node backend/auth-service/scripts/set-local-branding.mjs`
to apply the template's display name without recreating the realm or its accounts.
Setup seeds no application email/password. It generates only the Keycloak
bootstrap administrator (`local-admin`), with its password stored as
`KC_BOOTSTRAP_ADMIN_PASSWORD` in the private `keycloak.env` file. That administrator
signs into the master-realm administration console, not the application's fraud realm.

1. In `http://localhost:8090/admin/`, use the generated bootstrap admin credentials
   from `.local/auth-provider/keycloak.env` locally. Select the **fraud** realm.
2. Create an enabled user and set credentials through Keycloak. Copy its user ID
   (the verified provider subject), not its username/email. The master-realm
   bootstrap administrator is not an application account.
3. Obtain the intended existing active tenant UUID from Data Model. Do not create
   another tenant registry or silently map historical case actors.
4. Provision the exact identity with explicit roles:

```powershell
docker compose exec auth-service /app/admin -action provision -tenant "<existing-tenant-uuid>" -issuer "http://localhost:8090/realms/fraud" -subject "<keycloak-user-id>" -name "<display-name>" -roles "tenant_admin,case_manager_admin"
```

For an investigator use `-roles "case_investigator"`, then add the returned platform
UUID to the intended Case Manager inbox. Provision replay is idempotent only when
membership roles/state agree; it cannot elevate an existing membership. Role changes
belong in My Account. Database/operator access is privileged and must be restricted.

5. Visit `http://localhost:3000/login`, select **Continue to sign in**, and sign in
   through the provider. My Account opens after callback; its Case Manager link
   opens the permitted workspace. Existing stale/manual actor IDs require the
   reviewed migration described in [PREFLIGHT](PREFLIGHT.md).

For another compatible provider change the documented [OIDC configuration](OIDC_PROVIDER.md)
and provision/review the new `(issuer, subject)` mappings. Swapping issuer URLs alone
does not migrate account ownership. API consumers continue trusting the platform issuer.

## HTTP boundaries

| Endpoint | Authorization / result |
| --- | --- |
| Next POST `/api/auth/start` | Exact application Origin; starts provider redirect and binding cookie. |
| Next GET `/api/auth/callback` | Single-use state/binding/nonce/code/PKCE; creates opaque session or generic denial. |
| Next POST `/api/auth/logout` | Exact Origin; revokes current session before clearing cookie. |
| Next GET/POST `/api/auth/members` | Current opaque session; tenant-admin role; writes also require exact Origin and bounded JSON. |
| Next GET/POST `/api/auth/sessions` | Own active-tenant session inventory and revocation; GET accepts `after`; POST accepts either `{target: UUID, all: false}` or `{all: true}`. Requires current session, exact Origin for writes, bounded JSON. Successful writes return `signed_out`. |
| Auth POST `/internal/login/start`, `/internal/login/complete` | Private proxy credential; callback also requires valid one-time browser binding. |
| Auth POST `/internal/session`, `/internal/logout` | Private proxy credential plus opaque session proof. |
| Auth POST `/internal/members/list`, `/internal/members/change` | Same proof, current tenant-admin role; target belongs to caller's tenant; expected version. |
| Auth POST `/internal/sessions/list`, `/internal/sessions/revoke` | Private proxy credential and opaque session proof; user and tenant are derived internally. Unknown/foreign targets return the same denial; same owned-session retries do not duplicate audit. |
| Auth GET `/healthz`, `/readyz` | Liveness; bounded schema-read readiness. Auth has no published host port in Compose. |

Internal JSON bodies are limited to 16 KiB and reject unknown/trailing fields.
Invalid sessions return 401; permissions 403; stale version/last admin 409;
unavailable dependencies 503. Responses are no-store. Passwords, cookies, codes,
tokens and private keys must never appear in logs or API error messages.

## Validation and remaining release work

Real PostgreSQL tests cover exact identity mapping, provision replay, session
revocation, cross-tenant targets, stale versions, last-admin concurrency,
authorization versus suspension locks and audit rollback. Case integration tests
apply actual owner migrations and the identity schema, then reject revoked case
and owner commands. JWT tests cover mandatory session/version claims and role
separation. Browser fixtures cover authenticated layout, account administration,
origin checks, logout and the existing case workflows.

Executed locally on 2026-10-06: auth tests/vet with disposable PostgreSQL databases;
full Case suite with PostgreSQL plus focused identity/owner revocation tests;
shared investigator tests; decision and screening tests/vet (their optional
database suites were not configured in these two commands); frontend production
build, TypeScript and scoped ESLint; 27 case browser scenarios and three auth
scenarios. Real Docker application login/suspension/logout acceptance passed,
including deletion of the Secure host-prefixed cookie. Final auth readiness was
204; the active-user membership index exists; temporary smoke-user count is zero.
The isolated PostgreSQL test cluster was stopped; application containers remain up.

Session-management increment (AUTH-S04, 2026-10-06): auth tests/vet passed against
disposable PostgreSQL, including own-account/tenant isolation, 100-record keyset
pagination, atomic audit rollback, idempotent owned-session retry and revocation
waiting for authorized case writes. Frontend build/scoped ESLint passed, followed
by all 33 browser scenarios. A queue-history test race was corrected by waiting
for route navigation before clicking its return link; case application behavior
was not changed. The extended live Keycloak smoke verified two browser sessions,
cross-account denial, revoke-one blocking Case access, tenant suspension and
revoke-all cookie clearing/replay denial. Temporary users were removed; readiness
returned 204. Auth and frontend were rebuilt and restarted locally.

This increment needs no migration. For rollout, deploy the auth endpoints before
the frontend controls. Rollback the frontend before the auth binary; previously
committed revocations remain in the existing sessions table and continue to be
enforced by the earlier current-state authorization contract.

Opt-in live acceptance (two temporary users in an existing local tenant, removed
afterward; audit retained; no case data changes):

```powershell
$env:PLAYWRIGHT_BROWSER_PATH = 'C:\Program Files\Google\Chrome\Application\chrome.exe'
node backend/auth-service/scripts/smoke-platform.mjs
```

**The entire identity roadmap is not complete.** Open release items include
reviewed historical actor mapping; multi-tenant selection; invitations; global
account administration; provider disable/recovery
and back-channel logout reconciliation; MFA policy; distributed abuse limits;
expired-secret cleanup/retention; signing-key rotation operations; least-privilege
application DB grants; eligible named assignee UI; evidence-read race policy; and
the full live investigator acceptance journey. Legacy non-investigator Data Model,
ingestion and decision endpoints still require AUTH-X03/D12 protection; an
authenticated Next layout does not secure direct owner APIs. Do not expose this
development stack as a production permission boundary until those gates close.
