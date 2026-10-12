# Auth service implementation TODO

Created: 2026-10-06. Status: **platform accounts, real login, opaque sessions and
tenant permissions implemented and verified locally**. See [runtime/setup/evidence](PLATFORM_AUTH.md),
[preflight evidence](PREFLIGHT.md) and [provider acceptance](OIDC_PROVIDER.md).
Owner: auth implementation work, with coordinated tasks for frontend, Case
Manager, decision/screening owners and deployment. The user requested this
planning folder; recommendations below are not silently accepted business rules.

Use this as the execution tracker. Keep IDs stable. An unchecked task remains
open; a checked implementation task must link to code, tests and execution
evidence. States: Planned, Decision needed, Ready, In progress, Verified locally,
Blocked, Released, or Explicitly deferred. Record blockers and accepted deferrals
instead of deleting requirements. Documentation completion is not feature completion.

## 0. Baseline and decisions

### Implemented runtime increment, 2026-10-06

User authorized continuing implementation with permissions and the recommended
configurable provider approach. These narrower completion records do not mark
the broader tasks below complete where acceptance criteria remain outstanding.

- [x] **AUTH-V1.1** Auth-owned schema/version, exact provider-to-platform UUID mapping and explicit idempotent operator provisioning against active Data Model tenants. [Code](internal/platform/store.go), [PostgreSQL tests](internal/platform/store_test.go).
- [x] **AUTH-V1.2** Browser-bound single-use login transactions, real OIDC callback, opaque Secure/HttpOnly sessions, 8h absolute/30m idle expiry and bounded renewal; POST logout revokes server state. [Runtime](internal/platform/http.go), [Next routes](../../frontend/src/app/api/auth/[action]/route.ts).
- [x] **AUTH-V1.3** Private authenticated Next session resolution, 60-second platform credentials, mandatory `sid`/`authz_version` in Docker and current-state verification in Case/decision-review/screening-review. [Shared verifier](../investigator/jwt.go), [read/lock contract](internal/platform/schema.sql), [Case integration test](../case-manager-service/internal/store/postgres/identity_integration_test.go).
- [x] **AUTH-V1.4** Tenant-admin account listing/editing, separate case roles, expected versions, last-admin protection, tenant suspension/reactivation and atomic session revocation/audit. Race and rollback tests pass. [UI](../../frontend/src/components/identity-members.tsx), [tests](internal/platform/store_test.go).
- [x] **AUTH-V1.5** Docker auth/migration services, private configuration generation, consumer trust rollout and authenticated layout. Real Keycloak→Next→auth→Case acceptance passed using [temporary-account smoke](scripts/smoke-platform.mjs); fixtures cleaned up, audit retained. [Operator instructions](PLATFORM_AUTH.md).
- [ ] **AUTH-V1.6** Complete remaining release controls: abuse limits/cleanup, reviewed legacy mapping, tenant selection, directory/eligible named assignees, provider lifecycle/recovery and legacy owner API protection. V1.1–V1.5 are not approval to expose the development stack publicly.

Selected implementation defaults refining D02–D06: auth-controlled RS256 issuer
and 60-second private API credentials; opaque 8h/30m sessions; immutable platform
UUIDs linked by exact issuer/subject; Data Model-owned tenants; explicit operator
provisioning (no public registration); three separate roles; tenant suspension
revokes sessions after committed authorization changes. D02 key rotation operations,
D03 concurrency/fresh MFA, D04 historical mapping, D05 tenant selection/invitations
and D06 global administration remain open portions of those decisions. This
increment deterministically selects the first active membership; it does not
claim a completed multi-tenant selector.

Validation: auth PostgreSQL tests (including concurrent last-admin removal,
suspension locks and audit rollback), auth/provider unit tests, full Case suite,
focused identity-to-Case/owner integration, shared JWT tests, frontend production
build, scoped ESLint, existing 27 case browser scenarios and three new auth browser
scenarios. One ambiguous browser-test selector was corrected and all three auth
tests reran successfully. Actual Docker auth readiness and platform smoke passed.

Local setup correction: the initially empty Docker database now has a Data Model
API-created/provisioned `Local development` tenant; local `.env` uses its returned
UUID instead of the non-existent placeholder. Optional GeoIP is disabled in local
configuration until a valid MMDB is supplied. Neither creates an application user.

- [x] **AUTH-P01** Create the planning folder and source-linked current-state inventory in [README](README.md).
- [x] **AUTH-P02** Record the proposed [Case Manager handoff contract](CASE_MANAGER_INTEGRATION.md), including compatibility gaps and consumer ownership.
- [x] **AUTH-P03** Reinspect current code, local changes, existing providers and deployment constraints before implementation; update this baseline if they changed.

### Decision register

Each decision must record the selected option, rationale, date, approver, affected
task IDs and migration implications. No provider or token lifetime is decided by
the sample names in this document.

| ID / status | Decision and recommended starting point | Blocks |
| --- | --- | --- |
| AUTH-D01 — Selected 2026-10-06 | User confirmed no company SSO and requested an established Docker provider with future SSO configuration/replacement. Selected Keycloak 26.8.0 locally, generic OIDC code/S256-PKCE integration, provider-owned credentials. Same `fraud` DB with `keycloak` schema/dedicated role. [Implemented boundary, switching constraints and tests](OIDC_PROVIDER.md). Native password implementation is outside this selected approach. | Provider choice resolved; account/session integration still depends on D02-D05 |
| AUTH-D02 — Core selected; operations open | Auth-owned RS256 platform credentials after validated OIDC login, exact platform membership-derived claims, private signing key, fixed case-manager audience and 60-second TTL. Implemented in V1.3. Key rotation/discovery operations and least-privilege custody remain tracked. | Key rotation and production deployment |
| AUTH-D03 — Core selected; extensions open | Opaque Secure/HttpOnly browser sessions, digest-only persistence, 8h absolute/30m idle expiry, server-side token resolution and committed revocation. Implemented in V1.2–V1.3. Concurrent-session limits, fresh-MFA policy and production HTTPS remain open. | Session operations and production policy |
| AUTH-D04 — Platform ID selected; migration open | Immutable platform UUID linked by exact issuer/subject; no email auto-link. Implemented in V1.1. Historical actor strings require reviewed preflight mappings/aliases before existing production investigators migrate. | Historical identity migration |
| AUTH-D05 — Initial provisioning selected; tenant UX open | Data Model owns tenants. Explicit operator CLI provisions provider identities and role grants; no public registration. Tenant administrators edit roles with last-admin protection. Session currently uses the first active membership; explicit selection/switching and invitation onboarding remain open. | Multi-tenant UX and invitations |
| AUTH-D06 — Tenant suspension implemented; global policy open | Tenant suspension and role changes revoke affected sessions atomically; reactivation requires new login and preserves case history/assignments. Global suspension/admin hierarchy and automatic workload reassignment remain open. | Global account administration and workload policy |
| AUTH-D07 — Decision needed | Decide operator MFA requirements, recovery custody, break-glass access and emergency administrator procedures. Prefer provider-managed MFA/recovery with auditable privileged recovery. | Production sign-in release |
| AUTH-D08 — Decision needed | Approve directory fields visible to investigators/admins, historical-name display and profile-edit permissions. Recommend minimal tenant-scoped ID/display-name/status projections, no cross-tenant email search. | Directory and assignment UI |
| AUTH-D09 — Decision needed | Set invite TTL, resend/cancel behavior, mail provider and local mail capture; handle a user already in another tenant without information disclosure. | Invitations and recovery UX |
| AUTH-D10 — Decision needed | Choose synchronous active-principal/session validation: documented auth-owned SQL projection/function in the shared DB or authenticated introspection outside transactions. Recommend a shared-DB contract for transactional case/owner writes. Define read-cache staleness and outage behavior. | Revocation guarantee and transaction races |
| AUTH-D11 — Decision needed | Set audit/access-log retention, PII redaction, account deletion/anonymization, backup retention and evidence access. Retain investigation attribution; no cascade deletion of case history. | Retention jobs and release |
| AUTH-D12 — Decision needed | Decide broader platform access scope and rollout for legacy service-token/disabled-auth routes. Human-facing alternate routes must not bypass new account controls. Distinguish service credentials from users and decide external API/client needs. | Platform release and legacy owner endpoints |

## 1. Architecture and service foundation

- [ ] **AUTH-F01** Record the selected provider, ownership and trust-boundary diagram. Include browser, Next server, auth, provider, all three investigator APIs, service callbacks and shared PostgreSQL. Identify every credential crossing a boundary.
- [ ] **AUTH-F02** Establish Go module/runtime conventions, configuration validation, dependency interfaces, bounded HTTP clients, cancellation, graceful shutdown, readiness/liveness and structured logging. Fail startup for partially configured production authentication.
- [ ] **AUTH-F03** Reserve nonconflicting internal/host ports and exact public/internal URLs. Separate browser-reachable issuer/callback addresses from container service DNS; never put internal-only provider URLs into browser redirects.
- [ ] **AUTH-F04** Specify DB roles, grants, schema ownership and migration identity. Consumers may read approved contracts but must not modify identity-owned tables. Document that current shared DB credentials do not themselves enforce ownership isolation.
- [ ] **AUTH-F05** Budget API/worker/provider pools across the existing shared database; set connect, statement, lock, idle transaction and request timeouts. Choose concurrency from measured capacity, not independent per-service maxima.
- [ ] **AUTH-F06** Define errors consistently: 400 validation, 401 absent/invalid/expired identity, 403 current authorization denial, 409 version/uniqueness conflict, 429 throttling and 503 unavailable identity dependency. Login/recovery errors must not enumerate accounts.
- [ ] **AUTH-F07** Publish versioned OpenAPI/read contracts, bounded body/query sizes, cursor format, deterministic sort, idempotency rules, request IDs and safe diagnostic fields. Do not introduce a generic arbitrary token-minting endpoint.

Acceptance: starts with valid config; rejects invalid config; clean shutdown; health
does not disclose secrets; no private keys in API images, logs, public variables
or browser bundles. Auth and existing service migrations coexist in one DB.

## 2. Data model and migrations

All table names below are proposed. Confirm tenant ownership before creating
tables; reuse authoritative tenant records rather than inventing a parallel registry.

| Proposed identity entity | Required semantics / constraints |
| --- | --- |
| `users` | Stable platform ID, display name, global account state, timestamps and optimistic version. Separate operator account from customer data; email is mutable profile/login metadata, not case identity. |
| `external_identities` | Unique `(issuer, subject)` mapping to one user; verified provider metadata; explicit audited linking/unlinking. Handle provider changes without recycling IDs. |
| `tenant_memberships` | Unique tenant/user, active/suspended/removed state, authorization version and timestamps. Tenant-consistent role grants. A global active user is not automatically a tenant member. |
| `role_grants` | Fixed initial role allowlist, scoped grantor, grant/revoke history, tenant/user consistency. Keep tenant administration distinct from case investigation permissions. |
| `sessions` | Digest of opaque identifier, user, active tenant, creation/idle/absolute expiry, revoked state, version and authentication/MFA provenance. No plaintext cookie secret persisted. |
| Provider credential storage | Only if needed: encrypted recoverable provider refresh tokens, key version, refresh coordination and invalidation. Hash bearer session/invitation secrets; encryption is needed for upstream tokens that must later be presented. |
| `login_transactions` | Single-use state/nonce/PKCE binding, provider, callback/return target, expiration and consumption. No unbounded abandoned transactions. |
| `invitations` | Tenant, intended recipient/roles, token digest, inviter, expiry, acceptance/cancel state, resend generation/version and delivery reference. |
| `identity_audit_events` | Immutable actor/tenant/action/target/time/correlation, safe before/after change metadata, bootstrap/service/human attribution. No credentials or raw OIDC responses. |
| `identity_outbox` and command receipts | Durable notification/integration intent and deduplication where required; no assumption that case outbox is the same subsystem. |
| Legacy principal aliases | Only if D04 requires them: reviewed old-to-new mappings with unambiguous ownership and immutable migration provenance. |

- [ ] **AUTH-M01** Write schema, FK, uniqueness, nullability, enum/check and index design; include tenant-consistent composite relationships and explicit deletion behavior.
- [ ] **AUTH-M02** Add independently tracked forward/down migrations (`schema_migrations_auth_service` or approved equivalent). Set bounded lock/statement timeouts and document irreversible transformations.
- [ ] **AUTH-M03** Index session digest, issuer/subject, tenant/user membership, invitation digest/expiry, directory search/order and pending outbox/cleanup scans. Explain queries against representative tenant sizes.
- [ ] **AUTH-M04** Define bounded list projections and keyset pagination. Batch ID resolution to avoid one lookup per assignee, event, contributor or row.
- [ ] **AUTH-M05** Inventory existing case subjects, inbox memberships, assignees, report authors, review receipts and audit actors. Produce a read-only conflict/unresolved-identity report before any migration.
  - Partial implementation: [preflight command](PREFLIGHT.md) inventories 14 typed
    actor-reference sources with bounded tenant-scoped pages and exact strings.
    Unit/real-migration PostgreSQL tests and vet passed. Intended-environment
    execution, reviewed mapping/conflict aggregation and legacy JSON reference
    inventory remain open; AUTH-M05 is deliberately unchecked.
- [ ] **AUTH-M06** Reconcile identity aliases explicitly; preserve historical strings/attribution and receipt replay identity. Never auto-link equal emails, choose arbitrary duplicate owners, or delete unmatched history.
- [ ] **AUTH-M07** Test fresh installs, existing shared DB upgrades, migration ordering, retries, constraints, partial failures and documented rollback/preflight conditions. No destructive setup against application databases.
- [ ] **AUTH-M08** Implement bounded expiration/retention cleanup only after D11: expired login transactions, sessions and invitations; preserve required audit and case references.

## 3. Provider and login integration

- [ ] **AUTH-L01** Implement one selected OIDC authorization-code flow using a maintained library. Bind state, nonce, PKCE and callback transaction to the browser; no implicit or password grant shortcut. Security baseline: [OAuth security BCP](https://www.rfc-editor.org/rfc/rfc9700.html).
- [ ] **AUTH-L02** Validate provider issuer, intended audience/authorized party, signature, algorithm allowlist, expiry and nonce; consume login state once. ID tokens identify the login and are not automatically case API access tokens. Follow [OIDC Core](https://openid.net/specs/openid-connect-core-1_0.html).
- [ ] **AUTH-L03** Restrict discovery/JWKS/endpoints to configured providers, enforce TLS, bounded fetches and key caching/rotation rules; reject token-controlled `jku`/`x5u`, arbitrary discovery URLs and stale/unknown keys according to the chosen failure policy.
- [ ] **AUTH-L04** Resolve validated external identity to the platform user and current tenant memberships. Deny unknown/uninvited/suspended users according to D05/D06; do not create an administrator from provider email/domain alone.
- [ ] **AUTH-L05** Enforce exact callback registration and local return-target allowlists. Reject open redirects, protocol-relative URLs, replayed state, changed issuer and concurrent callback confusion.
- [ ] **AUTH-L06** Implement cancellation/provider-denial/provider-outage/timeout UI with safe retries and correlation IDs. No account existence disclosure or raw provider errors in the browser.
- [ ] **AUTH-L07** Coordinate provider refresh rotation for simultaneous tabs/requests; detect reuse and revoke affected sessions according to policy. Perform provider network IO outside DB locks using claim/version/finalize semantics.
- [ ] **AUTH-L08** Integrate MFA requirements and step-up for privileged account/role/credential actions; distinguish provider session from platform session and record authentication freshness without storing authentication secrets.
- [ ] **AUTH-L09** Test provider outage, signing-key rotation, bad clocks, expired/forged tokens, wrong audience/issuer, missing claims, algorithm substitution, JWKS failure and login callback replay.

Native password signup, hashing, verification, reset, lockout and credential-change
endpoints are **conditional scope**, not hidden prerequisites of this folder.
If selected under D01, create dedicated reviewed tasks for secure hashing,
one-time hashed reset tokens, enumeration resistance, rate limits, MFA and recovery;
do not leave a development password backend enabled beside SSO unintentionally.

## 4. Sessions, access tokens and revocation

- [ ] **AUTH-S01** Implement the chosen browser/session contract. If opaque sessions are accepted, update all current JWT-cookie consumers together; do not send the opaque identifier as a downstream Bearer JWT.
- [ ] **AUTH-S02** Create cryptographically random session identifiers, store only their digests and rotate after login/privilege or tenant changes. Set Secure, HttpOnly, correct SameSite/Path/host scope and no-store responses; document local HTTPS behavior. Use the [session security guidance](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).
- [ ] **AUTH-S03** Implement idle/absolute expiry, renewal, concurrency limits and bounded last-seen writes. No sliding session that can live forever. Ensure provider expiry and platform session policy are reconciled.
- [x] **AUTH-S04** Current-session inspection, POST logout, paginated session listing and self-service revoke-one/revoke-all are implemented in [sessions.go](internal/platform/sessions.go), [HTTP handlers](internal/platform/http.go), [Next proxy](../../frontend/src/app/api/auth/[action]/route.ts) and [My Account session controls](../../frontend/src/components/identity-sessions.tsx). Scope is the caller's own current tenant; all includes the current browser. Safe creation/expiry times are shown, without device/location inference. Audit and revocation commit atomically; cookies clear only after confirmed revocation. Provider SSO logout remains separate. [PostgreSQL permission/race/rollback/API tests](internal/platform/sessions_test.go) and [browser tests](../../frontend/tests/cases/auth.spec.ts) cover the behavior. No schema change was needed.
- [ ] **AUTH-S05** If issuing platform access tokens, derive claims solely from verified identity and current membership; select active tenant explicitly. Restrict TTL, audience, role vocabulary and signing algorithm. Protect signing keys and rotate overlapping verification keys without accepting token-supplied URLs.
- [ ] **AUTH-S06** Implement the accepted current-state contract across case, decision-review and screening-review APIs. Verify global user, tenant membership, active session and authorization version as applicable; short JWT expiry alone is not immediate revocation.
- [ ] **AUTH-S07** Define and implement authorization linearization for suspension versus an in-flight mutation. Use consistent narrow lock ordering with case/inbox/resource locks; no remote introspection under those locks and no tenant-wide serialization.
- [ ] **AUTH-S08** Ensure old role/tenant/admin claims cannot retain privileges after revocation. Recheck access before acknowledging idempotent review replays; preserve valid command receipts without allowing removed users to read them.
- [ ] **AUTH-S09** Make identity-store outage behavior explicit and fail closed for protected writes. Bound any read cache staleness; test cold/warm caches, invalidation loss, restarts and direct API calls that bypass the frontend.
- [ ] **AUTH-S10** Add CSRF protections for session-backed mutations, exact origin checks, trusted proxy configuration and login/logout CSRF protection. CORS is not an authorization boundary.
- [ ] **AUTH-S11** Test stolen/revoked sessions, refresh reuse, duplicate logout, expired cookies, multi-tab refresh, tenant switching, concurrent suspension and old JWTs against all three investigator APIs.

## 5. Bootstrap, operator accounts, tenants and roles

- [ ] **AUTH-U01** Build explicit one-time admin bootstrap via an operator command/invitation bound to the intended provider identity and tenant. No default password, first-public-request administrator, or permanently enabled public bootstrap endpoint.
- [ ] **AUTH-U02** Confirm tenant source, create/reference tenant through its owner contract, and assign initial tenant/case roles explicitly. Log bootstrap attribution; make retries idempotent and prevent competing first-admin grants.
- [ ] **AUTH-U03** Implement minimal self-profile read/update and tenant membership list. Reject mass assignment of roles, tenant, subject, account state or verified-email flags.
- [ ] **AUTH-U04** Implement tenant-admin listing/detail and controlled role changes with optimistic versions and grant/revoke audit. Enforce the role-grant hierarchy and last-admin protection under concurrent requests.
- [ ] **AUTH-U05** Implement global suspension and tenant-specific suspension/removal separately. End affected access promptly; keep case history and open assignment references. Reassignment failure must not restore a suspended user's login.
- [ ] **AUTH-U06** Define reactivation: no implicit restoration of revoked sessions; require new authentication and re-evaluate roles/memberships. Reactivation must not automatically reassign old cases.
- [ ] **AUTH-U07** Resolve account/email/provider-identity changes with verified ownership and explicit linking. Preserve stable principal IDs and tenant isolation; conflicting linking must roll back.
- [ ] **AUTH-U08** Implement no privilege elevation through profile APIs, invitation acceptance, bootstrap replay, tenant selection, forged headers or cached admin roles. Audit rejected administrative operations safely.

## 6. Invitations and delivery

- [ ] **AUTH-I01** Authorize invitation creation per tenant and target role; canonicalize intended recipient safely, generate a random one-time secret and persist only its digest. Define idempotency and duplicate pending-invite behavior.
- [ ] **AUTH-I02** Enqueue mail atomically with invitation intent. Deliver with bounded attempts, leases, backoff, visible failed state and explicit retry; never return “email sent” before delivery is established.
- [ ] **AUTH-I03** Accept only unexpired, current-generation, uncanceled invitations using the required verified identity. Commit membership/roles, consumption and audit once; handle two-browser acceptance races.
- [ ] **AUTH-I04** Resend rotates or reuses secrets according to D09; cancel invalidates acceptance. If resend rotates, old URLs must fail even when mail delivery is delayed or reordered.
- [ ] **AUTH-I05** Bind return URLs to approved origins, avoid token logging/referrer leakage and make error states generic. Configure local mail capture separately from real delivery.
- [ ] **AUTH-I06** Test provider success/platform rollback, mail outage, invitation expiration, wrong recipient, cross-tenant URL modification, replay and privilege changes between invite and acceptance.

## 7. User directory and Case Manager integration

- [ ] **AUTH-C01** Publish the versioned minimal directory contract in [CASE_MANAGER_INTEGRATION](CASE_MANAGER_INTEGRATION.md). Paginate tenant listings and batch explicit-ID lookup; enforce caller tenant/purpose permissions.
- [ ] **AUTH-C02** Expose current active membership/role state separately from historical name resolution. A display label is not proof of assignment eligibility or authorization.
- [ ] **AUTH-C03** Case Manager validates an active same-tenant operator before creating/updating inbox membership. Preserve existing capacity and assignment rules; do not move inbox membership into auth.
- [ ] **AUTH-C04** Update manual assignment, automatic assignment, reassignment, case move, human auto-claim, escalation and reopen to enforce the accepted active-user policy. Cover backend paths, not only dropdown options.
- [ ] **AUTH-C05** Add an eligible-assignee endpoint/projection owned by Case Manager: authorized inbox membership joined to active directory state and case-owned workload/capacity. Current `/inboxes/:id/users` is admin-only; do not expose it wholesale to investigators.
- [ ] **AUTH-C06** Replace raw subject inputs with named selections; show loading/error/empty/unavailable states, disabled/ineligible users and capacity explanations. Preserve a stable selected ID across search pages and reject stale eligibility at submission.
- [ ] **AUTH-C07** Resolve names for assignees, contributors, comments, report authors and review actors in batches. Keep historical IDs visible/fallback-safe for unknown/deleted identities; avoid leaking users from another tenant.
- [ ] **AUTH-C08** Coordinate suspension with open workload remediation through case-owned commands/events. Existing inbox-member removal rejects open assignments; identity suspension must not be blocked by this rule. Define manager visibility and reassignment notifications separately.
- [ ] **AUTH-C09** Update shared investigator verification and decision/screening owner routes to use current identity state; test direct calls and receipt replay after access revocation.
- [ ] **AUTH-C10** Version and migrate cookie/token/subject changes; coordinate frontend, case and both owners. Preserve historical receipts and avoid a mixed deployment silently bypassing session checks.

## 8. Frontend and platform access

- [ ] **AUTH-W01** Replace the decorative login form with the selected provider flow; add pending, denied, expired, offline and retry states. Do not collect a password locally for an SSO-only flow.
- [ ] **AUTH-W02** Implement exactly one callback/session-establishment owner and a private Next server integration. No access/refresh credentials in localStorage, public environment variables, HTML or client logs.
- [ ] **AUTH-W03** Protect authenticated layouts and server routes. Preserve a validated return destination; handle direct deep links, server rendering, expiry and API calls independently of client navigation.
- [ ] **AUTH-W04** Add authenticated user menu, logout, account page, session-expiry UX and tenant picker derived from current memberships. A tenant URL or environment default never grants membership.
- [ ] **AUTH-W05** Add invitation acceptance and tenant user/role administration screens with confirmations, version-conflict recovery and accessible error/loading states.
- [ ] **AUTH-W06** Update case API, source-evidence and human-review proxies for the chosen session contract. Preserve human/service credential separation, same-origin writes, private caching and downstream request correlation.
- [ ] **AUTH-W07** Clear user/tenant caches on logout, expiry, suspension and tenant switch; cancel or isolate in-flight old-session responses. Test two users sharing a browser and two tabs with different active-tenant intent.
- [ ] **AUTH-W08** Inventory every other browser-to-service route. Legacy APIs currently accepting disabled auth/service tokens need a defined protected boundary; a protected sidebar/layout does not secure those APIs.
- [ ] **AUTH-W09** Add browser tests against real auth/provider integration for the final release, beyond mocked session cookies. Keep fast contract fixtures for failure cases, clearly labeled as fixtures.

## 9. Events, audit and observability

- [ ] **AUTH-O01** Define versioned user/membership/role/session/invitation event envelopes: ID, tenant where applicable, actor kind/ID, aggregate/version, time, correlation and minimal payload. Commit state/audit/outbox atomically.
- [ ] **AUTH-O02** Define which consumers actually need events. Case workload remediation/name-cache refresh can be asynchronous; authorization revocation must meet D10 even during delivery outage.
- [ ] **AUTH-O03** Add idempotent, version-aware consumers and receipts; test duplicate/reordered/lost acknowledgements, replay, poison events and user reactivation after delayed suspension events. Never emit secrets or invite URLs into a generic event bus.
- [ ] **AUTH-O04** Add structured login/session/admin/provider/delivery logs and request IDs, with explicit redaction of cookies, JWTs, passwords, authorization codes, nonces, reset/invite tokens and provider payloads.
- [ ] **AUTH-O05** Add bounded metrics for login failures/latency, throttles, session denial/refresh conflicts, provider availability, invite delivery age/failures and DB pool waits. Avoid tenant/email/session IDs as unbounded metric labels.
- [ ] **AUTH-O06** Add readiness semantics that distinguish local DB readiness from provider/mail degradation, alert thresholds, dashboards and runbooks for login outage, revoked keys, stuck invites, exhausted delivery and locked-out admins.
- [ ] **AUTH-O07** Define audit access/retention/tamper resistance and privileged database audit scope separately from debug logs. Do not claim stdout logs satisfy investigation or compliance audit requirements.

## 10. Docker, configuration and migration rollout

- [ ] **AUTH-R01** Add real service/migration/required worker images only when they implement the selected flow; use the existing logical database and schema-qualified identity migrations.
- [ ] **AUTH-R02** Add selected provider/local TLS/mail dependencies where approved. Document host versus container URLs, callback registration, health dependencies, volumes, ports and resource limits.
- [ ] **AUTH-R03** Document environment variables without working shared credentials: database URL, provider/issuer/audience, callback origins, server client credentials, session/key settings, mail settings and log level. Generate unique local secrets into ignored files; mount only intended secrets into each consumer.
- [ ] **AUTH-R04** Set local TLS and Secure cookie behavior deliberately. Do not weaken production auth or accept unsigned/default administrator tokens to make Docker convenient.
- [ ] **AUTH-R05** Coordinate additive identity migrations, read contract deployment, subject preflight, consumer changes, key overlap, frontend session change and old-session retirement. Add startup/version checks where an old consumer would bypass revocation.
- [ ] **AUTH-R06** Document backup/restore of identity state and signing/encryption keys, session invalidation after restore, compromised key response and provider credential rotation. Test recovery in an isolated environment.
- [ ] **AUTH-R07** Verify container builds, clean-stack migrations, readiness, bootstrap, callback URLs, real browser session, owner commands, shutdown/restart and persistent accounts. Compose validation alone does not complete this task.
- [ ] **AUTH-R08** Document upgrade/downgrade boundaries and safe rollback: no rollback that re-enables suspended accounts, reuses consumed invites or silently accepts obsolete claims. Preserve case/identity audit and receipts.

## 11. Verification matrix

All items below require executable evidence in addition to the feature-level tests.

| Area | Minimum acceptance scenarios | Related tasks |
| --- | --- | --- |
| Cryptographic/protocol boundary | Forged/expired/wrong issuer/audience/algorithm, unknown/rotated key, missing nonce/state, replayed callback, wrong provider and open redirect | L01–L09, S05 |
| Account/tenant isolation | Unknown account, suspended global account, suspended tenant membership, guessed user/invite IDs, cross-tenant admin and directory requests | U01–U08, C01–C07 |
| Session lifecycle | Fixation, expiry/idle policy, concurrent refresh, logout/revoke-all, old cookie/JWT, provider outage, backend restart and stale cache | S01–S11 |
| Role/claim consistency | Revoked admin with old JWT, tenant switching, grant hierarchy, two concurrent last-admin removals, provider claims attempting self-grant | D05, U04, S06–S09 |
| Case authorization | Inbox access independent of login, ineligible assignment, capacity, suspended user auto-claim/reopen, disable versus review/assignment races | C03–C09 |
| Data integrity | Concurrent invite acceptance/bootstrap/linking, optimistic conflicts, atomic audit/outbox rollback, orphan prevention, historical subject mapping | M01–M08, I01–I06 |
| Owner APIs | Human token required; service credential rejected on human routes; current session/role check on direct calls and replay; existing lifecycle/link rules preserved | C09, W08 |
| UX | Deep link login, return URL, session expiry with unsaved findings, logout cache clearing, two users/tabs, searchable assignee paging, accessible failure states | W01–W09 |
| Failure recovery | DB/provider/mail/receiver down, read timeout, exhausted delivery, refresh uncertainty, cancellation while locks held, key overlap failure | L07, O01–O07, R05–R08 |
| Load and deployment | Shared pools, query plans, directory batches, login bursts/throttling, bounded cleanup, build/startup/migration/restore | F05, M03–M04, R01–R08 |

- [ ] **AUTH-T01** Unit tests for policy, claims mapping, role grants, lifecycle, expiry, token digests and validation.
- [ ] **AUTH-T02** Real PostgreSQL tests for shared-schema migrations, constraints, transactions, rollback, uniqueness and concurrent revocation/assignment.
- [ ] **AUTH-T03** Signed HTTP/API tests for permissions, error contracts, pagination, idempotency, cookies, CSRF and direct owner routes.
- [ ] **AUTH-T04** Provider contract tests with a controlled OIDC test provider, plus configured-provider acceptance; do not substitute manually minted test JWTs for login acceptance.
- [ ] **AUTH-T05** Browser integration tests and manual acceptance of administrator and investigator journeys.
- [ ] **AUTH-T06** Secret-scan/redaction assertions, dependency/security review, rate-limit bypass tests and documented unresolved risks.
- [ ] **AUTH-T07** Mixed-service load and operational recovery tests in the intended deployment; record pool/concurrency budget and actual results.

## 12. Vertical implementation sequence

| Slice | Deliverable | Dependencies | Exit gate |
| --- | --- | --- | --- |
| V0 | Decisions, threat model, stable ID/claims/session contracts and migration inventory | D01–D05 and D10 | No incompatible provider/session assumptions left for V1 |
| V1 | Identity schema, explicit bootstrap, real login, secure session, permitted case queue, logout | V0, F/M/L/S core; coordinated frontend and case consumer | Real admin signs in; wrong tenant/expired session denied; logout removes access |
| V2 | Tenant roles, invite/accept, admin user list, MFA/recovery and current-state revocation | V1, D06–D09 | Investigator invitation works; suspension and role removal deny all investigator APIs within the accepted guarantee |
| V3 | Case inbox directory, eligible assignment and historical display names | V2, C01–C10 | Named investigator can receive and review a case; ineligible/suspended/capacity-blocked assignment rejected |
| V4 | Account/session administration, workload remediation, mail/events and complete audit | V2–V3, D11 | Failure/replay/reassignment and recovery paths verified |
| V5 | Broader platform protection, Docker production configuration, load and release acceptance | D12, all required slices | Real full-stack acceptance and signed release checklist |

Even if V1 uses a temporary approved session design, V2/current-state enforcement
must be complete before releasing access to real investigators. No “login works”
completion claim should hide missing logout, tenant isolation or owner API checks.

## 13. Final handoff and completion

- [ ] **AUTH-G01** Every required decision has a recorded answer; optional/native-password/enterprise features are implemented or explicitly deferred, never implicitly omitted.
- [ ] **AUTH-G02** API/OpenAPI/schema/session/claims contracts match code; consumers pass compatibility tests and use stable IDs.
- [ ] **AUTH-G03** All required tasks have code, test commands/results and migration/deployment evidence; skipped tests and limitations remain visible.
- [ ] **AUTH-G04** Complete the Case Manager handoff checklist and update R08.2, REL01 and REL04 in its roadmap. Do not mark those gates complete on auth-service unit tests alone.
- [ ] **AUTH-G05** Operator runbooks cover bootstrap, account recovery, suspension/reassignment, keys, backups, email/provider outage and incident response.
- [ ] **AUTH-G06** User reviews the configured real administrator/investigator journey and the accepted release scope; record remaining work honestly.

## Cross-check additions — 2026-10-06

The [coverage review](COVERAGE_REVIEW.md) records repository evidence for these
additions. Existing IDs remain unchanged. These are open requirements, not new
implemented functionality or accepted policy decisions.

- [ ] **AUTH-X01 — Existing tenant owner (V0/V1):** Reference Data Model's `core.tenants` and tenant create/provision service. Define readiness checks before bootstrap/login, tenant-disabled behavior and non-cascading identity retention. Do not introduce another tenant registry or provision tenant schemas directly from auth. Test existing, unprovisioned, missing and unavailable tenants. Depends on D05/U02/M01.
- [ ] **AUTH-X02 — Private session resolution (V1):** Specify the Next-to-auth trust contract: authenticated internal client, opaque session proof, current tenant binding, allowed downstream audiences and response lifetime. Never accept caller-selected arbitrary subject/roles. Return only the minimal signed downstream credential to the server; prevent raw cookies in logs and browser responses. Define service credential rotation and denial/outage behavior. Test malicious server requests, audience substitution and revoked sessions. Depends on D02/D03/S01.
- [ ] **AUTH-X03 — Existing public token consumers (V0 inventory; required before live access to affected routes):** Replace or isolate `NEXT_PUBLIC_DATA_MODEL_SERVICE_TOKEN`, `NEXT_PUBLIC_INGESTION_SERVICE_TOKEN` and `NEXT_PUBLIC_DECISION_ENGINE_SERVICE_TOKEN` code paths. These variables are supported by code; this review does not establish that deployed values exist. If credentials were published, rotate them and invalidate affected bundles/caches. Introduce an authorized server boundary or owner-validated human identity, with tenant permissions for each operation. Do not merely move a broad service token behind an unrestricted proxy. Test direct service access and browser bundle secret absence. Depends on D12/W08.
- [ ] **AUTH-X04 — Evidence read revocation (V1 contract; before investigator release):** The current evidence proxy performs a human Case access check followed by a service-authorized owner fetch. Define read authorization timing and maximum staleness separately from write revocation. Test suspension, membership removal and case transfer between the two calls. Choose an owner-authorized human read or another explicitly scoped contract where required; preserve current source/resource access restrictions. Depends on D10/C09/W06.
- [ ] **AUTH-X05 — Invitation delivery secret (V2):** Resolve the digest-only acceptance versus asynchronous mail requirement: an isolated encrypted short-lived delivery payload or approved equivalent must let the worker send/retry without recovering a digest. Keep secrets out of generic events/receipts/logs, restrict decrypt permissions, expire/delete delivery material, and define resend generation handling. Test crash after commit, delayed obsolete mail, key rotation and provider timeout after accepted delivery. Depends on I01-I04/O01/R06.
- [ ] **AUTH-X06 — Enforcement of abuse limits (V1/V2):** Implement explicit bounded limits for login initiation/callback, refresh, invites/resend, recovery and directory search. Specify trusted client-IP derivation, per-account/tenant/client dimensions, multi-replica enforcement, Retry-After and limiter outage behavior. Avoid account enumeration and attacker-induced permanent lockout. Test forwarded-header spoofing, distributed bursts and storage growth. Depends on F06/L01/I01/T06.
- [ ] **AUTH-X07 — Provider account lifecycle (V2):** Decide how provider disable/delete, password/MFA recovery and provider logout affect existing platform sessions. If events/back-channel logout are selected, verify signature, issuer, audience, replay and session correlation; reconcile missed events. Otherwise document the bounded detection mechanism and limitation. Do not claim platform suspension handles provider-side suspension automatically. Depends on D06/D07/S06.
- [ ] **AUTH-X08 — Role and route matrix (V0/V1):** Enumerate platform administrator, tenant identity administrator, case administrator, investigator and non-case operator permissions across every proposed endpoint. Do not assume these are all required roles; record the approved vocabulary and grant hierarchy. The current shared verifier requires a case role, so identity administration and ordinary login must not reuse that business-specific gate blindly. Test users with identity-admin permissions but no case access and investigators with no identity-admin authority. Depends on D05/F07/U04.
- [ ] **AUTH-X09 — Session-aware page context (V1):** Update `case-page.ts` tenant selection and cache/session key with the accepted session contract. Do not expose an opaque cookie, token or reusable credential digest as a UI identifier. Bind permitted tenant selection to current membership, partition caches using a safe nonsecret identifier, and cancel stale responses across session/tenant changes. Depends on W04/W07/S01.
- [ ] **AUTH-X10 — Scope classification (V0):** Record MVP, future and explicitly excluded scope for SCIM/group synchronization, multiple enterprise providers, delegated support/impersonation, machine/client credentials, custom roles, native passwords and self-registration. None is implicitly required by this plan; no accidental public endpoints or business rules. Record whether provider-managed MFA/recovery needs platform UI links or event integration. Depends on D01/D05/D07/D12/G01.
- [ ] **AUTH-X11 — Revocation/key deployment details (V1/V5):** Existing consumers load configured public keys; record whether rotation requires restart or a new reload/discovery implementation. Prove new-key-before-issuance deployment and compromised-key removal on every replica. Define session revocation, encryption-key rewrap, clock skew and rollback together; overlapping files alone do not prove running replicas refreshed trust. Depends on S05/R05/R06.
- [ ] **AUTH-X12 — Real-stack evidence (V1-V5):** Extend acceptance with X01-X11, including full browser -> Next -> auth -> Case -> owner -> shared DB execution, not separate fixtures mistaken for the full journey. Verify provider callback network reachability under Docker, pooled multi-replica revocation, direct alternate endpoints, role-only users, SMTP retry secrecy and restore invalidation. Preserve representative query-plan/load evidence and test command/results. Depends on T01-T07/G03.

### Sequence corrections from the cross-check

- V0 must complete X03's exposure inventory and X08's route/role matrix. Existing
  tenant ownership is established by code; D05 decides onboarding policy, not
  whether to create a competing registry.
- V1 includes X02 and X09, working logout enforcement at the API, and the X04
  read-consistency contract. V2 adds administrative suspension and all-owner
  enforcement; session logout must not wait until V2.
- Invitation delivery/audit/outbox needed for V2 invitations belongs in V2.
  V4 hardens operations and adds remaining workflows; it cannot defer the atomic
  delivery, audit and failure handling required by earlier slices.
- V5 can expand platform coverage, but exposed alternative APIs that bypass the
  investigator boundary must be protected or isolated **before** live investigator
  release. X03/D12 is not permission to leave an alternate write path unprotected.

## Marble comparison follow-up - 2026-10-08

Source review confirmed that the new platform already implements exact
issuer/subject identity mapping, opaque digest-only sessions, short-lived API
credentials, tenant-scoped session management, separate identity/case roles,
and current-state authorization in the integrated investigation APIs. Preserve
these implementations rather than copying Marble's longer-lived role-bearing
JWT behavior or its unexpectedly write-capable `VIEWER` role. Existing V1.1-V1.5
and S04 completion records remain unchanged; this review adds no runtime proof.

Evidence: [identity/session store](internal/platform/store.go),
[session management](internal/platform/sessions.go),
[token issuance](internal/platform/http.go),
[transactional authorization contract](internal/platform/schema.sql), and
[consumer identity checks](../investigator/identity.go).

### Priority 1: complete authorization coverage

2026-10-08 increment: **X13 in progress.** The four legacy APIs now default to
token authentication and reject invalid configuration at startup and middleware.
The [route inventory and rollout boundary](LEGACY_API_AUTH.md) records current
coverage and the resource ownership checks still required. This closes the
implicit-disabled/blank-token configuration paths, not X13's human authorization
or deployment acceptance. The subsequent V1.8 increment enables authentication
for Data Model in Compose; the other general legacy APIs still select disabled auth.

- [x] **AUTH-V1.7 - Legacy authentication configuration safeguard:** Omitted mode
  defaults to token; unknown modes and missing/whitespace-only configured tokens
  fail closed; explicit development disabled mode remains supported. Added
  configuration and HTTP boundary tests for Data Model, ingestion, decision and
  screening, including handler non-execution on denial and secret redaction.
  [Repeatable test command](scripts/test-legacy-auth.sh) passed all eight app/HTTP
  packages in an isolated Go 1.25 Docker container. No application database or
  running service was changed. Full X13 remains open.

- [ ] **AUTH-X13 - Endpoint authorization inventory and legacy enforcement (Planned):** Extend X03/X08 and D12 with a reviewable matrix for every general-purpose Data Model, ingestion, decision, screening and Case endpoint, including internal routes and frontend proxies. Record owner, caller kind (human/service/public), credential, tenant binding, required action permission, resource checks, current-state/revocation behavior, feature gate where applicable, and deployment exposure. The checked-in [Compose common configuration](../../docker-compose.yml) sets `SERVICE_AUTH_MODE: disabled`; general owner middleware supports disabled/shared-token authentication ([Data Model](../data-model-service/internal/httpapi/auth.go), [ingestion](../ingestion-service/internal/httpapi/auth.go), [decision](../decision-engine-service/internal/httpapi/auth.go), [screening](../screening-service/internal/httpapi/auth.go)). This is source/configuration evidence, not verification of running containers. Protect or isolate affected routes before live access; moving a shared token behind a proxy must not replace user/tenant authorization. Acceptance: direct backend calls and alternate frontend paths reject missing identity, wrong tenant, insufficient permission and revoked credentials; service identities cannot impersonate humans; deployed configuration and browser bundles are checked. Depends on D12/X03/X08; feeds T03/X12/G02-G04.

- [ ] **AUTH-X14 - Action permissions beyond case management (Decision needed):** Extend X08 with explicit capabilities for scenario/rule reading, editing and publishing, data-model administration, ingestion, screening administration and other operations found by X13. Approve the initial role-to-permission map and grant hierarchy; illustrative names are not approved business policy. Preserve `tenant_admin`, `case_manager_admin` and `case_investigator` separation; do not force ordinary non-case actions through the case-specific JWT role gate. Define whether fixed roles suffice; custom-role editing remains conditional under X10. Keep resource authorization in the owning service and expose a consistent permission projection for frontend controls. Acceptance: every matrix operation has allow/deny coverage, unknown permissions deny, role changes invalidate old authority under the documented guarantee, and hiding UI controls is never the sole enforcement. Depends on X08/X13/D12; coordinate auth, frontend and each domain owner.

2026-10-09 implementation update: **X13/X14 partially implemented; both remain
open.** Data Model now has an executable human route allowlist, owner-side tenant
and resource checks, current membership/session checks and transactional write
rechecks. The selected initial roles are `platform_viewer`, `platform_builder`,
`platform_publisher` and `platform_admin`: all four can read Data Model; only
platform admin can mutate it. No roles are automatically granted. Builder and
publisher capabilities outside Data Model are not implemented. Existing tenant
identity and case roles remain separate. This is an implementation default,
not final approval of the platform-wide permission map.

- [x] **AUTH-V1.8 - Data Model human authorization (verified locally):** Auth
  migration v2 adds the four platform roles and the current-state authorization
  function. Signed platform identity is verified without requiring case access.
  Data Model enforces explicit route, tenant, resource and role checks; mutations
  recheck authority under identity locks. The private browser proxy forwards the
  user's credential, and the browser Data Model client no longer uses a public
  service token. Ingestion/decision integrations use a private dedicated token
  and reject redirects. See [contract, code and coordinated rollout](DATA_MODEL_AUTH.md).
  General ingestion/decision/screening human authorization, machine scopes,
  feature entitlements and intended-deployment acceptance remain open.

### Priority 2: define feature access and machine-client scope

- [ ] **AUTH-X15 - Tenant feature entitlement contract (Decision needed):** No Marble-style entitlement model/evaluator was found in the backend/frontend source searched. Decide which capabilities require tenant-specific access and whether licensing is needed at all; record initial-release scope or explicit deferral under G01. Assign ownership separately from authentication and action permissions. If selected, define tenant settings, required service configuration, allowed/restricted/missing-configuration states, explicit trial semantics (Marble treats `test` as allowed), authorized updates, audit and an API/read contract. Use the existing logical database with independently tracked owner migrations. Enforce access in backend operations as well as frontend controls. Acceptance: feature denial still applies to a caller with the action permission; wrong-tenant changes, missing configuration, unknown states and unavailable entitlement storage follow a documented fail-closed policy. Depends on X13/X14/F04/F07; no new licensing policy is implied by this task.

- [ ] **AUTH-X16 - Entitlement freshness and rollout separation (Planned, conditional on X15):** Define cache keys, invalidation, maximum staleness and update visibility across API replicas and workers; distinguish commercial/tenant entitlements from deployment rollout flags. A rollout flag must not grant a missing permission or entitlement. Specify behavior for in-flight work, delayed jobs and retries after access is removed. Acceptance: entitlement updates and revocations are tested across replicas, stale caches, restarts and dependency outages; frontend availability agrees with backend decisions within the accepted freshness contract. Keep authorization-version/session revocation guarantees intact. Depends on X15/D10/T02/T03/T07.

- [ ] **AUTH-X17 - Machine credential lifecycle (Decision needed):** Resolve the external API/client portion of D12/X10. Existing shared service credentials are not a managed tenant API-key system. If external clients are required, specify named client identities, tenant/action scopes, issuance authority, secret storage, expiration, rotation overlap, revocation and audit attribution; reuse service-principal restrictions without assigning human sessions to machines. If deferred, document which internal integrations remain permitted and their credential rotation/revocation contract. Acceptance: wrong-tenant and out-of-scope requests deny, revoked credentials cannot execute or replay operations, secrets are not returned after creation or logged, and machine credentials are rejected on human-only routes. Depends on X13/X14/D12/X10; coordinate consuming services.

### Existing work still required; do not duplicate it

| Remaining work | Existing tasks / decisions | Current boundary |
| --- | --- | --- |
| Explicit tenant selection/switching | D05, X09, W04/W07 | Login currently chooses the first active membership ordered by tenant UUID. |
| Invitations, directory and eligible named assignees | D08-D09, I01-I06, C01-C10 | CLI provisioning and admin member listing exist; complete onboarding/assignment journeys remain open. |
| Reviewed historical identity mapping | D04, M05-M06 | Read-only preflight exists; historical attribution must not be auto-linked by email. |
| Provider disable/logout/recovery reconciliation and MFA | D06-D07, L08, X07 | Application-session revocation does not terminate provider SSO or automatically detect provider account changes. |
| Distributed abuse controls and bounded cleanup | X06, D11, M08 | Login/session implementation does not complete throttling or retention operations. |
| Signing-key rotation and least-privilege database grants | X11, F04, R05-R06 | Key files and logical table ownership do not prove live rotation or database-enforced ownership isolation. |
| Evidence-read races and full deployment acceptance | X04, X12, T01-T07, G03-G04 | Preserve the implemented write-lock contract; verify read consistency and alternate routes independently. |

Sequence: close X13's exposed-route gaps first, using X14's approved permission
map for affected operations. Resolve X15/X17 scope before claiming platform
feature/client coverage; implement X16 if entitlements are selected. Existing
release controls above remain prerequisites, not work deferred by this ordering.

Review verification: static source and checked-in configuration inspection only.
Existing tests were inspected, not rerun; Go was not found on PATH or the standard
installation paths checked. Earlier recorded PostgreSQL/browser/Keycloak results
remain historical evidence. No live container configuration or new runtime
acceptance was verified, and no implementation task was marked complete.

## Work log

### Invitation onboarding increment - 2026-10-09

- [x] **AUTH-V1.9 - Invitation onboarding (verified locally):** Admin invitation
  creation/list/resend/cancel, encrypted bounded SMTP delivery, verified provider
  acceptance and tenant membership/session/audit transaction, with frontend
  invitation acceptance and Users & access management. This narrow implementation
  record does not close the broader release requirements listed below.
- User selected auth-owned roles with Keycloak authentication and approved the
  administrator invite -> email -> provider sign-in -> membership -> access/audit
  workflow. Implemented auth migration v3, fixed-role invitations, encrypted SMTP
  outbox with retries/leases, create idempotency, resend/cancel, browser-bound
  verified-identity acceptance and atomic membership/session/audit writes.
- Added `/administration/users`, linked from My Account, for invitations, existing
  role/suspension controls and tenant audit events. Initial administrator bootstrap
  remains CLI-only. The provider must already support the recipient's account;
  automatic provider provisioning/public registration was not added.
- Advances I01-I06, W05, X05 and O01. Parent tasks remain open pending intended
  provider/mail acceptance, broader event contracts, retention, key rotation,
  monitoring and abuse-control work. The implementation does not deploy or send
  real email. [Contract, SMTP configuration and rollout](PLATFORM_AUTH.md).
- Validation: auth tests/vet against disposable PostgreSQL and frontend production
  build/scoped lint passed. The first browser run caught an Origin-header issue
  from the invitation page referrer policy; corrected to same-origin referrers.
  Final expanded auth tests/vet passed, including concurrent acceptance, audit
  rollback, no existing-member elevation, wrong recipient/provider, cross-tenant
  denial, expiry/cancel/resend, inviter-role changes, SMTP TLS enforcement, bounded
  failures and stale-delivery completion. All nine selected invitation/account
  browser tests passed. Node syntax and Compose structural checks passed. Tests
  used temporary databases, fake provider/browser fixtures and a loopback SMTP
  test server; no live Keycloak-plus-email acceptance was performed.

### Data Model authorization increment - 2026-10-09

- Implemented V1.8, advancing X03/X08/X13/X14 without completing their broader
  acceptance criteria. Added migration upgrade/replay, role separation,
  foreign-resource denial, stale/revoked identity, transaction-entry revocation,
  suspension locking and dependency-outage tests. Route coverage is checked
  against the registered router. No existing memberships were elevated.
- Verification: auth, shared verifier and Data Model `go test -count=1 -p 2 ./...`
  passed in isolated Go 1.25 containers against disposable PostgreSQL. New
  integration tests applied real schema SQL; older suites requiring separate
  database environment variables were not enabled. Ingestion/decision client,
  HTTP, app and worker packages passed. Frontend production build/TypeScript,
  scoped lint, Data Model Docker image build and Compose structural validation
  passed. Browser suite exited successfully with 36 passing tests after manually
  stopping its two temporary web-server process trees to finish stalled Windows
  teardown. These fixtures are not live provider acceptance.
- Rollout remains pending: generate the private Data Model env file, apply auth
  schema v2, rebuild coordinated consumers and explicitly grant platform roles.
  No live application database or deployment was changed. Follow
  [rollout and rollback limits](DATA_MODEL_AUTH.md). Other owners' legacy auth,
  full-stack acceptance and X15-X17 remain open.

### Legacy API configuration increment - 2026-10-08

- Implemented V1.7 and advanced X13 with a source-linked legacy route inventory.
  Kept existing account/session/case roles and integrated investigator checks.
- Verification: `sh backend/auth-service/scripts/test-legacy-auth.sh` passed all
  four services' configuration and HTTP packages with `go test -count=1 -p 2` in
  an isolated `golang:1.25.0` container. Database suites were not enabled. Source
  was mounted into the test container; no application database or live service
  configuration was used. Changed Go files were formatted; whitespace checks passed.
- Rollout: standalone deployments that omitted auth mode must now provide a
  private service token or explicitly choose development disabled mode. Current
  Compose already chooses disabled mode, so it remains an open exposure item;
  this increment was not deployed. Coordinate private browser proxies and service
  callers before changing that setting; never publish service tokens to browsers.
- Remaining: X13 route-specific current identity/tenant/resource enforcement,
  X14 non-case permission policy, browser credential removal, service-principal
  scope, deployment acceptance and X15-X17. Existing unrelated working-tree
  changes were preserved. See [rollout details](LEGACY_API_AUTH.md).

### Provider increment — verified locally, partial roadmap coverage

- Implemented local provider setup, generated private configuration and profile
  activation, shared-DB schema/role setup, realm/client import, readiness and pool
  limits. This advances F03/F04, R02/R03 and X10; full production deployment and
  application role isolation remain open.
- Implemented generic OIDC discovery, confidential authorization-code/S256 PKCE,
  nonce/issuer/audience/signature/expiry/issued-at/authorized-party validation,
  safe failures, response/timeout bounds and strict endpoint-origin checks.
  This advances L01-L03/L05/L09. The caller still needs durable browser-bound
  single-use state, callback handling, account mapping, sessions and UI; those
  parent tasks remain unchecked.
- Verification: Go tests and vet passed; two fake issuer configurations plus
  invalid claims/metadata exercised; actual local Keycloak discovery and browser
  code exchange passed, including code replay denial and disabled application
  password grant. Browser callback was intercepted, not a working application
  callback. No Case sign-in, current-state revocation or full REL04 claim.
- Existing case JWT trust stays unchanged. A provider ID token does not grant
  case access. Switching real operators still needs reviewed platform identity
  mapping; email auto-linking is not implemented or authorized.
- Details and commands: [OIDC_PROVIDER.md](OIDC_PROVIDER.md).

| Date | IDs | Result | Remaining |
| --- | --- | --- | --- |
| 2026-10-06 | S04 | Own-session inventory and revoke-one/revoke-all implemented across auth, Next and My Account; no schema change. Auth tests/vet passed with disposable PostgreSQL, including tenant/account isolation, pagination, audit rollback and case-write locking. Frontend build/lint and all 33 browser tests passed. Corrected a pre-existing browser-test synchronization race: the test could click the old queue navigation before the case detail route committed. Live Docker/Keycloak acceptance verified a second browser's Case access was denied after revocation, cross-account revocation was forbidden, and revoke-all cleared the secure cookie. Temporary test users removed; auth readiness 204. [Runtime contract](PLATFORM_AUTH.md). | Provider-wide logout/recovery reconciliation, invitations, tenant switching, eligible named assignees, abuse/retention controls and legacy API protection remain open. |
| 2026-10-06 | D01; partial F03/F04, L01-L03/L05/L09, R02/R03 | Local Keycloak, generic OIDC adapter and real provider smoke test implemented/verified. User requested configurable replacement SSO. | Platform mapping, browser transaction/session/callback, current-state enforcement and Case login remain open. |
| 2026-10-06 | M05 partial | Implemented read-only identity-preflight command and Go module; unit tests, real shared-migration PostgreSQL test and vet passed. [Commands, scope and evidence](PREFLIGHT.md). | Provider choice pending; no identity schema/accounts/login or intended-environment census. |
| 2026-10-06 | X01-X12 | Cross-checked current tenant, JWT, proxy, client and deployment code; added coverage review and corrected phase dependencies. Documentation only. | All X tasks and provider/policy decisions open; no runtime acceptance performed. |
| 2026-10-06 | P01–P02 | Planning workspace, source inventory, phased TODO and Case Manager handoff drafted. Documentation only. | D01–D12 open; no runtime implementation or deployment. |

For each later increment record: selected task IDs, accepted behavior, affected
owners, code/migration links, tests actually run, failures resolved, rollback and
deployment notes, newly discovered work and next dependencies.
