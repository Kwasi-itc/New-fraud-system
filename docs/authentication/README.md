# Platform authentication

This is the current authentication contract. It supersedes static bearer-token and fixed-role instructions in older service handoff documents. All services continue using one logical PostgreSQL database, with service-owned schemas and migrations.

## Permissions and identities

Each tenant account has exactly one assigned role. Changing it replaces the previous assignment. The `roles` wire field remains an array containing exactly one role ID; human JWT verification rejects missing or multiple roles. Roles are explicit capability bundles. There is no inheritance or wildcard permission. Auth-service owns tenant roles, assignments, clients, public keys, approvals, sessions and the capability catalog. APIs authorize capabilities; roles in human tokens are descriptive metadata, except that the verifier additionally protects reserved administrative grants.

`tenant_admin` receives **every tenant-scoped human capability**, including access to every inbox in that tenant. Only that predefined role can create/edit/delete roles or assign them. `roles:manage` and `roles:assign` cannot appear in custom roles or machine scopes. Built-in roles are read-only in the role editor. A custom role cannot impersonate a built-in role through its identifier or name. The final active Tenant Administrator cannot be removed or deactivated.

Investigator and Case Manager Administrator bundles use case-scoped evidence/review capabilities; they do not grant raw tenant-wide screening read/review authority. Raw screening review records the signed human as reviewer, and adding to a whitelist also requires current `screening:admin`.

The canonical [catalog and role bundles](capability-contract.json) are generated from `backend/platform-auth`. The [route inventory](route-inventory.json) comes from the actual registrations in all six backend APIs. Identity administrative routes are described below. [Service signing profiles](../../backend/platform-auth/services.json) bind each service key to its own identity, receiving audiences and maximum capabilities.

| Principal | Grant claim | Default / maximum lifetime | Authority |
| --- | --- | --- | --- |
| Human (`user`) | `capabilities` | 60 seconds / 60 seconds | One tenant, canonical user UUID, session UUID, membership version |
| External client (`client`) | `scopes` | 15 minutes / 60 minutes accepted | One tenant and client UUID, fraud-system audience, only client-eligible scopes |
| Internal service (`service`) | `capabilities` | 60 seconds / 60 seconds | One service identity, one tenant, receiving audience, originator metadata |

Every access token requires RS256, `typ=at+jwt`, a trusted `kid`/issuer/type binding, `sub`, one `aud`, `jti`, `iat` and `exp`. Five seconds of clock leeway apply. A service key cannot mint user/client identities, another service's identity, or capabilities outside its configured maximum. Tenantless service tokens are restricted to explicit bootstrap/provisioning/reconciliation policies; a Tenant Administrator is not a cross-tenant platform operator.

Ordinary reads verify the signed snapshot in memory, then enforce resource ownership and inbox access. Existing tokens retain that snapshot until expiry plus five seconds. Protected mutations recheck the session, active membership, version and current capability grants in their transaction. Shared-role edits therefore revoke protected actions immediately, even without updating every member's version. Resource checks continue after authentication; a valid signature never bypasses inbox, case-link, tenant or domain rules.

## Human requests and administration

The frontend keeps an opaque HttpOnly session cookie. Its server authenticates to auth-service using a distinct `frontend-bff` service key with only `auth:bootstrap`. Session resolution issues the short-lived human JWT, which BFF proxies forward to backend APIs. Actor headers and static service secrets grant no authority. Origin checks, body limits, no-store responses and redirect restrictions remain in place.

Account pages share the Scenarios/Lists/Decisions pill styling with account-specific labels; there is no administration heading in the tab bar. Tabs and actions depend on the current capabilities.

- `/my-account`: own profile, one role tag, provider-managed sign-in, and paginated application sessions.

- `/administration/users`: membership and invitation management, with paginated built-in/custom role selection.
- `/administration/roles`: built-in/custom role list and capability details; Tenant Administrator only.
- `/administration/roles/new` and `/administration/roles/{roleId}/edit`: full-page custom-role editor with the human capability catalog, category/search filters, bulk selection, and optional copying of an existing role’s capabilities. Copying creates an explicit snapshot; it creates no inheritance. Reserved grants are visible but disabled and rejected by the owner API.
- `/administration/api-clients`: registration, scope requests, public-key rotation/revocation, sensitive approval, and client inbox grants.

Auth-service administrative APIs use `/v1/tenants/{tenantId}` and require human JWTs. Lists return at most 101 entries: display 100 and use the hundredth identifier as `after` when another entry exists.

| Routes | Required capability |
| --- | --- |
| `GET /roles`, `GET /roles/{roleId}`, `GET /capabilities` | `roles:read` |
| `POST /roles`, `PUT/DELETE /roles/{roleId}` | `roles:manage`, rechecked transactionally |
| `GET /clients`, `GET /clients/{clientId}`, `GET /client-scopes`, `GET /clients/{clientId}/scope-approvals` | `clients:read` |
| `POST /clients`, `PUT /clients/{clientId}`, `GET/POST /client-keys`, `DELETE /client-keys/{kid}` | `clients:manage`; mutations rechecked |
| `POST /clients/{clientId}/scope-approvals/{scope}` | `clients:approve-sensitive`, rechecked |

Member and client searches use escaped name prefixes, tenant-scoped indexed queries and keyset pagination; role/client details use exact tenant-qualified lookups.

After login, the BFF resolves the opaque session through the bootstrap endpoint and forwards the resulting human JWT to auth-service. Profile, session, member, invitation, and audit operations use that human principal; auth-service validates it against current database state and derives the user and tenant from the token rather than caller-supplied identity fields. Self-service profile operations require only authentication; administrative operations also require their documented capability and current authority. Only the display name is editable, with an optimistic profile version and atomic audit. Provider account name/email are display-only metadata captured from a verified identity and bound to that session’s exact issuer/subject. Sessions predating this binding display unavailable provider details until the next login. `POST /api/auth/manage-sign-in` validates the origin and active session before redirecting to the trusted `AUTH_MANAGE_SIGN_IN_URL`; deployment must configure the provider’s account console. No caller-supplied redirect is accepted.

Roles and clients use optimistic `version` fields. A role referenced by members or pending invitations cannot be deleted until those references are removed. Invitation creation/acceptance retains tenant-consistent role references and inviter-authority checks. Only Tenant Administrators can assign roles, including through invitations. BFF service authentication is limited to login/bootstrap operations, including session resolution and invitation start; possession of the BFF credential alone does not authorize post-login administration.

Case Manager owns `GET /v1/tenants/{tenantId}/clients/{clientId}/inboxes` and `PUT/DELETE .../inboxes/{inboxId}`. These require human `inboxes:manage`. An administrator without `cases:access-all` can see/change grants only for their own inboxes. Each client can have at most 256 grants; concurrent changes serialize only that client's grant set, and actual changes produce an atomic inbox audit event. Client case lists expose bounded summaries and keyset pagination, without histories or evidence.

## External clients

The customer retains their private key. Register an RSA public key (at least 2048 bits) with a client-specific `kid`; at most two keys may be active concurrently. Register the replacement before revoking the old key. Client/key/scope revocation prevents future token issuance; already-issued ordinary access tokens expire normally.

Normal scopes are `ingest:write`, `decisions:evaluate`, and `decisions:read`. Sensitive scopes `cases:read`, `records:read`, and `records:export` need approval by a **different human** with `clients:approve-sensitive`. Tenant Administrators and the predefined tenant-scoped Platform Administrator receive that capability. Case access additionally requires the Case Manager inbox grants above. Human-only and internal-only scopes are rejected.

The dependency-free Node.js [client proof helper](../../backend/auth-service/scripts/client-proof.mjs) generates customer keys and signs assertions. Run these commands from the repository root:

```sh
node backend/auth-service/scripts/client-proof.mjs keygen --dir .local/client-auth --kid customer-key-1
```

This creates RSA-3072 `private.pem`, `public.pem` and `key.json`. The directory is private (0700), and the private key is mode 0600; an existing directory is never overwritten. In `/administration/api-clients`, register the contents of `public.pem` under key ID `customer-key-1` for the selected client. Keep `private.pem` on the customer's system. Use the **client UUID**, not the tenant UUID, below:

```sh
node backend/auth-service/scripts/client-proof.mjs proof \
  --dir .local/client-auth \
  --client-id CLIENT_UUID \
  --token-url http://localhost:8088/oauth/token
```

The command prints only the signed proof JWT. Each invocation creates a new `jti` and a 60-second expiry, using the supplied token URL exactly as its audience. An optional `--out .local/client-auth/proof.jwt` writes a new mode-0600 file instead; existing output files are rejected. The helper checks that the local public and private keys match and makes no network calls. Use `--help` for its command summary.

Create a proof JWT with RS256, header `kid` and `typ=JWT`, and these claims:

```json
{
  "iss": "CLIENT_UUID",
  "sub": "CLIENT_UUID",
  "aud": "https://your-origin.example/oauth/token",
  "jti": "a-fresh-unique-value-for-each-exchange",
  "iat": 1800000000,
  "exp": 1800000060
}
```

The proof audience remains the exact token endpoint URL; the returned access token uses the fraud-system audience. Set `iat` to the current Unix time and `exp` to at most `iat + 60`. Use the **configured public token endpoint URL exactly**, not the internal container URL. POST form-encoded fields to that endpoint:

```text
grant_type=client_credentials
client_id=CLIENT_UUID
client_assertion_type=urn:ietf:params:oauth:client-assertion-type:jwt-bearer
client_assertion=SIGNED_PROOF
audience=urn:fraud:system
scope=ingest:write decisions:evaluate decisions:read
```

Use the same returned token as `Authorization: Bearer ...` across APIs whose required scopes it carries. The access-token audience is **`urn:fraud:system`**, representing the whole fraud system; it can also be omitted from the exchange request to use that default. API-specific client audiences are rejected. One client registration and customer key pair serves all its granted scopes. Each API still enforces its required capability, tenant, principal type and resource/inbox permissions. Omitting `scope` issues all registered normal scopes and approved sensitive scopes. Request a fresh proof/token before expiry; do not retry an exchange with the same proof. The gateway bounds token requests to 32 KiB and rate-limits exchanges; auth-service also validates body-only, single-valued parameters and returns no-store responses.

Proof replay protection is an atomic, bounded, process-local cache for the approved single auth-service instance. It fails closed when full and retains entries through proof expiration plus leeway. Restart loses replay state. A shared replay store is a future prerequisite for multiple auth-service instances; it has deliberately not been introduced here.

## Internal calls and protected actions

APIs and workers load only their own private key and the public trust ring at startup. Outgoing tenant context comes from a verified request or trusted persisted work. Sign a fresh, audience-specific token for each request/retry. `orig_type`/`orig_id` preserve provenance, but never authorize a service as a human. River arguments and Screening's case-event outbox retain originator metadata rather than expiring tokens. Historical outbox rows with `{}` have unknown origin; autonomous delivery identifies its service and never invents a human actor.

The shared `ExecuteHardenedAction` pipeline performs current-capability verification, operation locking, owner resource authorization, payload-bound receipt replay, mutation, receipt and audit in one short transaction. Case review uses the Case Manager bouncer to enforce tenant, inbox and resource linkage. Existing immutable receipt digests can replay exactly; no legacy credentials are accepted. Delivery happens after claim transactions commit, outside database locks. Internal credentials are not sent to customer webhook destinations or redirects. Provider credentials, OIDC secrets, invitation encryption keys and external callback signatures keep their separate contracts.

To add an endpoint:

1. Register a precise capability in `capabilities.json` and choose eligible principal types. Human capability additions require explicit non-admin bundle decisions; the Tenant Administrator bundle includes the human catalog. Regenerate the identity seed SQL and capability documentation.
2. Declare an `auth.Policy` on every route, including explicit `Public: true` for public routes. Add `PrincipalType`, `BodyTenant`, or `ControlPlane` where required. Router coverage validation rejects undeclared routes at startup.
3. Preserve owner-controlled resource-ID/tenant and domain checks. Recheck current capabilities inside the same mutation transaction. Callbacks bind their JSON tenant to the signed tenant before mutation.
4. Run the route contract tests and regenerate the inventory through their `PolicyObserver` snapshots. Include isolation, concurrency, rollback and integration tests for the changed behavior.

## Setup, migrations and operations

See [migration and key operations](operations.md) before rebuilding existing installations. See [verification evidence and limits](verification.md) for what was actually exercised. Authentication configuration is mandatory even in development; the retired opt-out modes have no effect.
