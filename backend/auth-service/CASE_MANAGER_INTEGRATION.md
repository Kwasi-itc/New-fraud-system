# Auth and Case Manager integration contract

Status: partial implementation handoff, 2026-10-06. The current implemented
contract is [PLATFORM_AUTH.md](PLATFORM_AUTH.md): platform UUIDs, opaque sessions,
private Next resolution, signed `sid`/`authz_version`, and the auth-owned
`identity.authorize_case_v1` read/lock function are implemented across all three
investigator consumers. Compose requires sessions. The baseline and proposed
contracts below are retained for remaining work; descriptions of JWT cookies or
missing revocation describe the pre-auth baseline, not current runtime behavior.

## 1. Existing contract to preserve

See the [coverage review](COVERAGE_REVIEW.md) and tracker AUTH-X01-X12 for
additional required contracts: private session resolution, existing tenant
ownership, identity-versus-case roles, evidence-read revocation timing, public
service-token consumers and coordinated key deployment. These requirements apply
to the rollout and acceptance steps below.

Current sources:

- [Shared verifier](../investigator/jwt.go): trusted RSA keys, RS256, key ID,
  configured issuer/audience, required expiry and issued-at, nonempty subject,
  nonzero tenant UUID, and `case_investigator` or `case_manager_admin` roles.
- [Case principal](../case-manager-service/internal/access/principal.go) and
  [JWT adapter](../case-manager-service/internal/access/jwt.go): separate trusted
  human identity from integration credentials. Administrator status does not
  grant cross-tenant access.
- [Owner authority](../investigator/review.go): current case, inbox and resource
  authorization inside owner transactions. Receipts bind verified actors and
  command content; replay still requires current authority.
- [Inbox service](../case-manager-service/internal/service/access.go): membership,
  assignment, capacity and investigation effects remain Case Manager business
  rules. Inbox user listing is administrator-only. Membership currently accepts
  subject strings without validating against a directory.
- [Case proxy](../../frontend/src/lib/server/case-proxy.ts): reads the cookie
  named by `CASE_SESSION_COOKIE`, default `__Host-case_session`, and forwards its
  value as a Bearer JWT. It does not resolve opaque sessions.
- [Owner review proxy](../../frontend/src/app/api/case-review/[tenant]/[caseId]/[kind]/route.ts)
  forwards human identity to the resource owner. Preserve this human boundary.

The three investigator APIs currently share issuer/audience/key configuration.
The local audience is `case-manager`. A provider ID token is not automatically
an API access token. The integration service token cannot impersonate a human.
Current signature verification does not check account suspension or session
revocation; valid old JWTs therefore need additional current-state enforcement.

## 2. Identity and token contract

Proposed stable identifier: an immutable platform user ID, mapped from the exact
provider `(issuer, subject)` pair. The final identifier and legacy mapping are
AUTH-D04. Never automatically join identities because email strings match.
Operator accounts are separate from screened customers and ingested entities.

| Claim / field | Current | Proposed responsibility |
| --- | --- | --- |
| `sub` | Nonempty actor string | Stable platform subject; used by memberships, assignments, history and receipts |
| `tenant_id` | Nonzero tenant UUID | Selected active membership, derived from trusted platform state |
| `roles` | Case investigator/admin role vocabulary | Current tenant grants; provider claims alone cannot grant platform administration |
| `iss`, `aud`, `iat`, `exp`, signing `kid` | Verified against configured trust | Preserve strict verification; choose issuer/key custody and rotation in AUTH-D02 |
| Session identifier | Not enforced | Consider signed `session_id` bound to active server session; never the raw browser cookie secret |
| Authorization version | Not enforced | Consider signed `authz_version` to detect obsolete grants; comparison requires current owner state |

Additional claims are proposals, not existing protections. Update every consumer
before relying on them. Choose short token lifetimes and rotation policy through
AUTH-D02/D03; expiry alone cannot satisfy immediate revocation.

Recommended browser flow, subject to AUTH-D03:

```text
Browser -> Next login -> selected provider -> validated callback
        -> platform account + active tenant + server session
        -> Secure HttpOnly opaque cookie
Browser -> Next case proxy -> resolve active session server-side
        -> short-lived signed human API token -> Case/Decision/Screening API
        -> current identity + domain authorization -> transaction -> response
```

Exactly one component owns callback validation and session establishment. Tokens
and provider refresh credentials stay server-side. Changing the cookie to opaque
requires changing every proxy; forwarding it as a JWT would break current APIs.
Reject untrusted return URLs, protect login/logout and mutations against CSRF,
and never expose provider credentials or signing keys to the browser.

## 3. Proposed API surface

These are planning names under `/v1`; finalize in OpenAPI before implementation.
Reuse the selected provider's credential, MFA and recovery mechanisms rather
than exposing duplicate password endpoints. Browser-facing routes may live in
Next; do not implement a second callback/session owner in auth.

| Method and proposed path | Authority / input | Result and behavior |
| --- | --- | --- |
| `GET /auth/login` | Browser; validated relative return path | Start provider redirect with server-bound state, nonce and PKCE |
| `GET /auth/callback` | One-time login transaction; provider code/state | Validate provider response; map account; establish session or safe denial; no credentials in response body |
| `GET /auth/session` | Browser session | Minimal current user, active tenant, allowed tenant choices and permissions; no-store |
| `POST /auth/logout` | Session plus CSRF/origin checks | Revoke current session, clear cookie; idempotent; provider logout is separate policy |
| `POST /auth/session/tenant` | Session; target tenant UUID | Verify active membership, rotate session/claims; clear tenant-scoped client caches |
| `GET /auth/sessions` | Current user | Bounded safe session/device summaries; no raw tokens or digests |
| `DELETE /auth/sessions/:id` | Session owner or explicitly permitted administrator | Revoke target; audit; inaccessible IDs do not leak other users |
| `POST /auth/sessions/revoke-all` | Current user or scoped administrator | Revoke applicable sessions atomically; define whether caller survives |
| `GET /tenants/:tenant/users` | Authorized tenant directory reader | Cursor, bounded limit, search/status filters; minimal paginated directory |
| `POST /tenants/:tenant/users/lookup` | Authorized tenant directory reader; bounded distinct ID array | Batch historical labels/status for visible IDs; unknown IDs use safe fallback, not cross-tenant discovery |
| `POST /tenants/:tenant/invitations` | Tenant user administrator; email and allowed roles | Pending invitation, durable mail publication; operation idempotency; never return invite secret in normal listings |
| `GET /tenants/:tenant/invitations` | Tenant user administrator | Bounded invitation status listing |
| `POST /tenants/:tenant/invitations/:id/resend` | Tenant user administrator | Rate-limited reissue/delivery with invalidation policy |
| `DELETE /tenants/:tenant/invitations/:id` | Tenant user administrator | Cancel pending invitation; audit |
| `POST /auth/invitations/accept` | Verified identity plus one-time invite proof | Validate expiry, intended account and tenant; atomically consume and grant approved membership |
| `PATCH /tenants/:tenant/users/:id/roles` | Authorized tenant grant administrator; roles and expected version | Validate grant hierarchy and last-admin invariant; invalidate stale privileges; audit |
| `POST /tenants/:tenant/users/:id/suspend` | Scoped administrator; reason and expected version | Disable tenant membership, not necessarily global account; invalidate access |
| `POST /tenants/:tenant/users/:id/reactivate` | Scoped administrator; reason and expected version | Explicit reinstatement; old revoked sessions stay revoked |

Global suspension requires separately authorized platform administration; tenant
administrators must not disable accounts in other tenants. Define that API only
after AUTH-D05/D06. Provider recovery and administrative MFA reset require their
own approved authority; Case Manager roles do not imply identity administration.

Directory item proposal: `id`, `display_name`, `membership_status`, and a bounded
set of approved role indicators. Email visibility is AUTH-D08, not assumed.
List envelope: `items`, `next_cursor`; stable sorting and tenant/filter-bound
cursors. Lookup envelope: `users` plus safe unresolved-ID results. Set explicit
request, page, search and batch limits in OpenAPI, not arbitrary unbounded arrays.

Use consistent safe errors: 400 malformed input; 401 absent/expired/revoked
identity; 403 insufficient authority; 404 inaccessible/missing resource where
appropriate; 409 stale version or invariant conflict; 429 throttling; 503 required
identity/provider dependency unavailable. Do not turn an outage into successful
authentication or an empty successful directory. Errors include a request ID,
never cookies, invite secrets, tokens or provider response dumps.

## 4. Current-state authorization in the shared database

All application schemas remain in the same logical `fraud` PostgreSQL database.
Auth owns proposed `identity` tables and migrations. Case, decision and screening
services do not update those tables directly.

AUTH-D10 chooses a versioned SQL read/function contract or authenticated
introspection. Recommended transactional contract returns/checks only active
user, active tenant membership, applicable roles, active session and authorization
version for the verified principal. Document schema objects, column types,
nullability, tenant rules, grants, indexes and compatibility before consumers use
them. Reuse shared verification code without importing auth internals.

For a protected mutation, current identity and case/inbox/resource authority must
hold at the defined authorization point. Establish a common narrow lock ordering
for suspension and owner writes. Acceptance rule: once suspension commits, no new
mutation may authorize using the revoked membership/session. A concurrent write
may finish before suspension according to the documented ordering; do not promise
retroactive rollback of committed investigations. Test both orderings. Avoid
tenant-wide locks and network calls while database locks are held.

HTTP middleware alone can leave a check/use race. Recheck within the authorized
transaction when the contract requires it, including receipt replay. Use indexed
principal/session lookups, bounded caches only where approved, and fail closed on
protected writes if current state is unavailable. Async identity events are not
the source of immediate revocation guarantees.

## 5. Assignment and directory ownership

Auth answers: who is this operator, what tenant membership/roles are active, and
what minimal label may this caller see? Case Manager answers: may this operator
join this inbox or receive this case, at what workload/capacity, with what audit?

Proposed Case-owned `GET /v1/inboxes/:inboxId/eligible-assignees` must authorize
the requesting investigator against that inbox, join current active identity to
case-owned membership/workload, and return bounded named choices plus eligibility
and capacity information. Final path/envelope belongs in Case OpenAPI. Do not
expose the existing administrator-only inbox-user endpoint wholesale.

Enforce eligibility again on submission and on all automatic assignment paths.
Directory display is never authorization. Reuse existing capacity, reassignment,
inbox history and lifecycle logic. Administrative capacity overrides, if any,
remain Case Manager policy rather than a new auth-service role shortcut.

Global suspension must succeed even when a user owns open cases. Existing inbox
membership removal can reject open assignments; that rule must not prevent
security suspension. Keep case attribution and assignment visible, prevent new
claims, and let an authorized manager remediate workload through case-owned
audited reassignment. Reactivation does not recreate removed inbox memberships or
silently revive sessions. Handle concurrent assignment/suspension explicitly.

Batch-resolve labels for assignees, contributors, comments, report authors and
review history. Preserve stable IDs and safe unknown/deactivated labels. A name
change must not rewrite evidence or attribution. No cross-tenant email lookup or
N+1 requests for every timeline row.

## 6. Events, operations and frontend behavior

Proposed events: account suspended/reactivated, tenant membership changed, roles
changed, session revoked, and profile changed. Publish committed changes through
an auth-owned outbox when a consumer needs delivery. Envelope includes event ID,
schema version, event type, subject ID, tenant ID where applicable, timestamp,
aggregate version and correlation ID. Payloads contain minimal approved metadata;
no tokens, passwords, invitation proofs or provider credentials.

Consumers deduplicate by event ID, handle delayed/out-of-order versions, retry
with bounded backoff, expose exhausted delivery and support operator replay.
Case workload notifications are Case-owned. Email delivery failure must retain
retryable invitation state rather than falsely claiming delivery.

Frontend implementation must cover:

- Real login/callback/logout and server-enforced authenticated layouts.
- Tenant selection and cache clearing; deep links return only to allowed local
  routes after successful login.
- Named member/assignee search with loading, empty, unavailable, ineligible and
  capacity states; preserve selected stable IDs across result pages.
- 401 session expiry and 403 permission loss without an infinite redirect loop.
  Preserve unsent investigation findings safely where feasible; never replay a
  write automatically under another tenant or identity.
- Administration and invitation pages limited by identity permissions, with
  confirmation and explanations for role removal, suspension and revocation.
- Original human authority for owner review APIs; no service-token fallback on
  login, directory or authorization errors.

## 7. Consumer change map

| Area | Required implementation work | Tracker |
| --- | --- | --- |
| Shared investigator verifier and owner authority | Versioned claims/current-state checks, transaction ordering, revoked replay denial | AUTH-S05-S09, AUTH-C09 |
| Case identity adapter and access service | Current identity, membership validation, assignment eligibility across every path | AUTH-C02-C05 |
| Case HTTP API and repository | Bounded eligible-assignee projection, directory batching, tenant-safe errors | AUTH-C01, AUTH-C05-C07 |
| Next case and owner proxies | Resolve selected session contract, forward signed human identity, no-store | AUTH-S01, AUTH-W01-W09 |
| Login, authenticated layout and admin UI | Real provider login, session guard, accounts/invites, named operators | AUTH-U01-U08, AUTH-I01-I06, AUTH-W01-W09 |
| Compose and migrations | Real auth runtime only when implemented; identity schema before dependent consumers | AUTH-M01-M08, AUTH-R01-R08 |

Existing `USER_JWT_ISSUER`, `USER_JWT_AUDIENCE` and `USER_JWT_KEYS_FILE` consumer
settings remain until a documented coordinated replacement. Root Compose uses
their `CASE_USER_JWT_*` inputs. `CASE_SERVICE_AUTH_TOKEN` remains a separate
integration secret. Proposed provider/client/session/key/mail settings must be
specified and validated during implementation; this planning folder supplies no
default administrator password or usable credentials.

## 8. Migration and rollout

1. Inventory all existing actor strings, inbox memberships, assignments, history,
   report authors and command receipts. Obtain reviewed provider-to-platform
   mappings; unresolved identities remain explicit. Do not infer ownership from
   display names or guess account links.
2. Add independently tracked identity schema/indexes and versioned consumer
   contracts. Test shared-schema migration order and rollback on disposable DBs.
3. Bootstrap the intended tenant/admin through an explicit audited one-time
   procedure. Local sample tenant UUIDs do not prove a tenant exists.
4. Coordinate token subject/cookie/issuer changes across Next and all three
   investigator APIs. Preserve receipt actor matching and historical attribution;
   document aliases if required, without granting old subjects broad authority.
5. Deploy compatible verification keys before switching issuance; bound key
   overlap and old-session retirement. No silent fallback to unchecked old JWTs.
6. Validate real login and revocation, then remove development trust paths under
   the approved rollout. Rollback must not restore suspended access or remove
   schema objects still used by deployed consumers.

Broader legacy APIs accepting service tokens or disabled authentication require
AUTH-D12 inventory and controls. Protecting Case UI alone does not establish
platform-wide account authorization.

## 9. Handoff and acceptance checklist

- [ ] AUTH-D01-D05 decisions recorded; provider, stable subject, issuer, session
  and tenant/admin ownership unambiguous.
- [ ] First vertical slice: bootstrap real admin -> real login -> permitted case
  queue -> logout; direct APIs reject invalid identities. No handcrafted cookie
  or fixture-only provider is counted as live acceptance.
- [ ] Active operator directory and named eligible assignments work with bounded
  queries and tenant isolation; unknown historical actors remain readable safely.
- [ ] Role removal, global/tenant suspension and session revocation deny access
  through Case and both owner APIs, including previously issued tokens and replay.
- [ ] Concurrent assignment/suspension, review/revocation, last-admin removal,
  refresh/logout and invite acceptance have real PostgreSQL/API coverage.
- [ ] Real investigator can read evidence, review with findings, assign/escalate,
  close/reopen as permitted, reload/restart and see durable attributable history.
- [ ] Provider, mail and identity-store outages produce safe visible failures;
  event retries/replay, key rotation and backup restore are demonstrated.
- [ ] Browser cookie/origin/CSRF and cache-isolation checks pass; no secret/PII
  leakage in logs. Local verification and deployment evidence are separate.
- [ ] Update [Case roadmap](../../CASE_MANAGEMENT_IMPLEMENTATION_PLAN.md)
  R08.2 for actual directory work, REL01 for real identity/session integration,
  and REL04 only after full live investigator acceptance. Inbox lifecycle work
  in R08.2 is not completed merely by providing an auth directory.

Use AUTH-G01-G06 for final handoff evidence. Keep every unimplemented or deferred
item visible; this contract is a roadmap, not a completion claim.
