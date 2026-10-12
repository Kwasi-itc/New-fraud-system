# Auth service workspace

2026-10-09 invitation increment: tenant administrators can invite users and manage
invitations, member access and audit at `/administration/users`. Auth schema v3,
SMTP configuration and an approved provider account path are required. Invitations
remain disabled until configured. See [invitation contract and rollout](PLATFORM_AUTH.md).
Backend tests/vet, frontend build/lint and nine invitation/account browser tests
passed locally; live provider/mail acceptance and deployment remain pending.

Status: **platform login, sessions and tenant permissions implemented locally**,
2026-10-06. Start with [the current runtime contract and operator setup](PLATFORM_AUTH.md).
Real Keycloak-to-application login, opaque cookies, Case API access, role denial,
tenant suspension and logout replay rejection have passed live Docker acceptance.
The [read-only preflight](PREFLIGHT.md) and [configurable provider](OIDC_PROVIDER.md)
remain part of the workflow. The full identity release gate remains open.

2026-10-09: [Data Model human authorization](DATA_MODEL_AUTH.md) adds platform
roles, auth schema v2, owner-side checks and a private browser proxy. It passed
isolated backend/build checks; coordinated rollout and full-stack acceptance
remain pending. General ingestion, decision and screening coverage is still open.

The sections below retain the original architecture inventory and future scope;
where they describe a missing session/runtime, the implemented contract in
[PLATFORM_AUTH.md](PLATFORM_AUTH.md) supersedes that baseline.

Start here:

Cross-check: [coverage review](COVERAGE_REVIEW.md) records source-backed additions
AUTH-X01-X12 and corrections to implementation dependencies.

1. [Implementation TODO](IMPLEMENTATION_TODO.md): task IDs, decisions, phases,
   dependencies, acceptance tests, operational work, and completion tracking.
2. [Case Manager integration contract](CASE_MANAGER_INTEGRATION.md): current
   behavior, proposed identity/session/directory contracts, consumer changes,
   migration compatibility, and the handoff checklist.
3. [Case Management roadmap](../../CASE_MANAGEMENT_IMPLEMENTATION_PLAN.md):
   R08 directory/workload dependencies and REL01/REL04 release gates.

## Objective and ownership

Provide real operator accounts, tenant membership, login/session integration,
account administration, and a trusted user directory for the fraud platform.
An investigator is an operator account with tenant roles and case inbox access;
it is not a screened customer, ingested `user` object, or transaction party.

Provider selection is recorded in AUTH-D01; remaining design decisions are
tracked in AUTH-D02–AUTH-D12:

| Owner | Responsibility |
| --- | --- |
| Identity provider | Keycloak is the local Docker default. A generic OIDC adapter supports future compatible SSO configuration. Provider-side credentials/MFA/recovery remain here; account linking and application sessions are separate. |
| Auth service | Platform identity mapping, active accounts, tenant memberships, roles, invitations, session lifecycle, directory/read contracts, and identity audit/events. Platform token issuance is conditional on the selected issuer design. |
| Frontend server | Login redirects/callback integration, secure browser session handling, authenticated layouts, logout, tenant selection, and server-side token forwarding. Exact callback ownership must be chosen once, not duplicated. |
| Case Manager | Inbox membership, assignee eligibility/capacity, case authorization, lifecycle, investigation history, assignment and reassignment. |
| Decision and screening owners | Their investigation commands and evidence; consume the same trusted human identity and current authorization state. |

Do not build a custom password/cryptography system just because this directory
is called `auth-service`. Native credentials are a separate, explicit scope
decision with their own security and recovery obligations.

## Shared database requirement

All application services use the **same logical PostgreSQL database** as Data
Model and Case Manager (`fraud` in local Compose). Proposed ownership:

```text
fraud
  keycloak       provider-managed schema, dedicated fraud_keycloak DB role
  identity       auth-owned accounts, memberships, sessions, invitations, audit
  case_manager   case-owned inboxes, cases, assignments and investigation history
  core           existing Data Model tenant/metadata and decision-owned tables
  screening      screening-owned data
  ...            existing data-model/ingestion-owned schemas
```

`identity` is a proposed schema, not a second application database. Confirm its
availability before migration. Auth has independently tracked migrations and
service-owned mutation rules. Consumers use documented projections/functions or
APIs; they do not write directly into identity tables. Preserve tenant consistency
and budget every connection pool against the shared database.

If a self-hosted identity provider is selected, verify its supported storage and
schema-isolation requirements before adding it to Compose. A provider requiring
a separate application database needs an explicit architecture decision; do not
silently introduce one. An existing external SSO is an integration dependency,
not a replacement for the platform's shared application database.

## Current code to reuse

| Existing source | What it supplies / what it does not supply |
| --- | --- |
| [Shared JWT verifier](../investigator/jwt.go) | Signature, issuer, audience, expiry, tenant and case-role validation. No directory, session lookup or account-suspension enforcement. |
| [Owner review authority](../investigator/review.go) | Current case/inbox/resource authorization in owner transactions. Preserve this boundary. |
| [Case identity adapter](../case-manager-service/internal/access/jwt.go) and [principal](../case-manager-service/internal/access/principal.go) | Trusted human/service distinction; tenant and administrator checks. |
| [Case inbox access](../case-manager-service/internal/service/access.go) | Membership administration and human-action effects. Currently accepts arbitrary subject strings without a directory. |
| [Case auth contract](../case-manager-service/README.md#authentication) | Current claims, keys, service-token restrictions and shared database conventions. |
| [Frontend case proxy](../../frontend/src/lib/server/case-proxy.ts) | Reads a signed JWT cookie and forwards it privately; does not establish login or support an opaque session yet. |
| [Login form](../../frontend/src/app/(auth)/login/page.tsx) and [layout](../../frontend/src/app/(authenticated)/layout.tsx) | Presentation only; the named authenticated layout is not an authentication guard. |
| [Owner command proxy](../../frontend/src/app/api/case-review/[tenant]/[caseId]/[kind]/route.ts) | Original human identity forwarding and same-origin writes; update with session design. |
| [Compose](../../docker-compose.yml) | Shared database, case trust configuration, local-idp profile with Keycloak/schema setup. No platform auth-service runtime entry yet. |

The ignored root `.env` and `.local/case-auth/` keys are development configuration.
They do not create accounts, prove account ownership, establish browser sessions,
or constitute a production identity provider.

## Proposed auth runtime structure

Create the auth runtime after architecture decisions are recorded. The existing
`cmd/identity-preflight` and `internal/preflight` implement the independent
migration inventory prerequisite; they do not choose account or session policy.

```text
cmd/server/                 HTTP API
cmd/migrate/                independently tracked identity migrations
cmd/bootstrap-admin/        explicit one-time operator bootstrap
cmd/worker/                 only if durable invitation/events jobs require it
internal/app/               configuration, dependency wiring, lifecycle
internal/domain/            accounts, tenant membership, sessions, invitations
internal/service/           owner business rules and transactional commands
internal/ports/             provider, repositories, mail and event contracts
internal/httpapi/           routes, validation, authorization, error contracts
internal/store/postgres/    identity-owned SQL, indexes and migrations
internal/clients/           selected provider/mail integrations
```

Follow repository Go service conventions, but reuse narrowly shared verification
code rather than making other services import auth's internal implementation.
No empty scaffold or health-only container should be reported as working auth.

## Immediate next work

The first runtime slice is implemented and tested; see [PLATFORM_AUTH.md](PLATFORM_AUTH.md)
for exact coverage. Next: reviewed legacy actor mapping, named eligible assignees,
explicit tenant selection, abuse/retention controls, and protection of legacy
non-investigator owner APIs. Invitations, provider lifecycle reconciliation and
production operations remain tracked separately. Do not mark the full roadmap done.
