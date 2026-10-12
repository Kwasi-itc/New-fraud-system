# Data Model platform authorization

Local rollout update, 2026-10-09: Auth schema v3 and the updated Auth, Data Model,
ingestion/decision APIs and workers, and frontend are running locally. The private
Data Model integration credential is configured consistently. Runtime checks
confirmed anonymous denial (401), authenticated integration reads (200), and
matching credentials across clients. Existing memberships received no new grants;
administrators must explicitly assign platform roles. Signed-in provider/user
acceptance and broader platform authorization remain outstanding. See the
[workflow rollout record](../decision-engine-service/documentation/workflows.md).

Implementation increment, 2026-10-08: AUTH-X13/X14 partial coverage.

## Contract

Auth schema version 2 adds `platform_viewer`, `platform_builder`,
`platform_publisher`, and `platform_admin` to the fixed membership role allowlist.
Existing memberships receive no new grants. All four can read Data Model resources
in their active tenant; only `platform_admin` can modify them. Rules/scenarios,
ingestion and screening permissions for these roles are not implemented yet.
`tenant_admin` retains account administration, including explicit role grants;
case roles retain case access. Neither automatically grants Data Model access.

Auth still issues a short-lived platform credential with the configured issuer
and audience (currently `case-manager`). The shared verifier has an identity-only
`VerifyPlatformSession` method requiring UUID actor/tenant/session and a positive
authorization version. The existing case verifier retains its case-role gate.
Data Model ignores token role claims when deciding permission: the auth-owned
`identity.authorize_data_model_v1` function checks current database roles, user,
tenant, membership/version and unexpired, unrevoked session state.

The explicit [route policy](../data-model-service/internal/httpapi/platform_policy.go)
allows tenant-scoped Data Model reads/writes. Global tenant create/list/provision
and reconcile are service-only. New routes deny human requests until assigned a
policy. Table, field, enum value, link, pivot, navigation option and index-job
ownership is resolved by bounded primary-key lookups in owner tables. Foreign
and missing IDs both deny access. Input link fields and direct pivot fields must
belong to their selected tables and tenant.

Reads authorize at request entry; a read already authorized may finish after
revocation. Mutations recheck identity and ownership inside the existing owner
transaction and lock user -> membership -> session before domain work. Membership
changes and session revocation wait for already-authorized writes; new writes
after committed revocation deny. Identity lookup errors return 503. Read-only
human contexts cannot enter a mutation transaction. Existing asynchronous jobs
remain service-owned; revoking an operator does not cancel work already enqueued.

## Browser and service callers

The Data Model browser client uses `/api/data-model/...` on Next. The server
resolves the opaque session and forwards only its platform credential; browser
Authorization headers and cookies are not forwarded. Writes require the exact
application Origin, paths reject encoded traversal/separators, JSON request bodies
are bounded to 4 MiB, responses are no-store and upstream redirects are rejected.
The frontend no longer reads `NEXT_PUBLIC_DATA_MODEL_SERVICE_TOKEN` or connects
directly to the Data Model API. `DATA_MODEL_SERVICE_URL` is private server config.

Ingestion and decision APIs/workers use a separate `DATA_MODEL_SERVICE_TOKEN`
through their Data Model HTTP clients. Credential-bearing redirects are not
followed. These are trusted internal integrations, not tenant-scoped external
API keys; AUTH-X17 still owns that broader lifecycle/scope work. Existing SQL
ingestion-contract reads keep their owner-defined contract.

## Coordinated rollout

1. Run `node backend/auth-service/scripts/setup-platform.mjs` from the project
   root. It preserves existing keys and creates the missing private
   `.local/auth-provider/data-model.env` with a random integration token. The
   frontend does not mount that file. Do not print or put its contents in public
   environment variables. If older public Data Model credentials existed, rotate
   those credentials and retire old browser bundles separately.
2. Apply auth migration version 2 before starting the new consumers. Rebuild auth,
   Data Model, ingestion/decision APIs and workers, and frontend together. The
   checked-in Compose configuration enables token + platform authentication for
   Data Model and mounts only public JWT keys there. Its Docker build context is
   now `backend`, using `data-model-service/Dockerfile`.
3. An account administrator explicitly grants the needed platform role in My
   Account, or an operator provisions a new identity with explicit roles. Existing
   CLI provision remains idempotent and cannot silently elevate memberships.
   Role changes revoke affected sessions; sign in again afterward.
4. Verify real provider login, allowed/denied Data Model reads/writes, worker
   integration and old-token denial in the intended deployment.

These changes were not deployed during the original implementation; the later
local rollout is recorded at the top of this document.
The new env file must be generated before rebuilding the checked-in Compose stack.
Old auth binaries expect schema version 1: do not roll back to them after adding
platform-role memberships. Keep schema version 2 and committed revocations;
use a compatible forward fix or a reviewed rollback plan. Re-enabling disabled
Data Model authentication is not a safe rollback.

## Remaining boundaries

General ingestion, decision and screening routes still use their legacy boundary
and the Compose common configuration still disables it. Their alternate data and
mutation paths mean this increment is not a complete platform release gate.
Service credential scoping, separate downstream audiences, feature entitlements,
tenant selection and the other AUTH-X13-X17 work remain open. The original
[legacy route inventory](LEGACY_API_AUTH.md) is a dated source snapshot; Data
Model's executable allowlist above is now authoritative for its human routes.

## Verification

Auth, shared-verifier and Data Model tests ran in Go 1.25 with
`AUTH_PREFLIGHT_TEST_DATABASE_URL` pointing only to an isolated temporary
PostgreSQL container. The new tests create/drop their own databases and apply
real owner and auth schema SQL. Existing database suites requiring other test
variables were not enabled by that run.

Coverage includes v1-to-v2 upgrade/replay with no automatic grants, platform/case
role separation, signed-token versus current database-role disagreement, reads
and writes, foreign tenant/table/field denial, service-only route denial, stale
versions, suspension/logout, revocation before transaction entry, lock ordering
against suspension and identity dependency failure. Ingestion/decision client
tests cover credential forwarding and rejection of redirects. Frontend build
includes TypeScript checking; browser fixtures exercise the private proxy and
existing account/case flows, not a live Keycloak deployment.

The browser suite exited successfully with 36 passing tests after its two
temporary web-server process trees were manually stopped to finish stalled
Windows teardown. The Data Model Docker image build, Compose structural check,
frontend production build and scoped lint also passed.
