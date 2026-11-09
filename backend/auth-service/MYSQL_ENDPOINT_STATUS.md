# Auth MySQL endpoint status

Updated 2026-11-09T10:30:07.545Z. The local MySQL API is running at **http://localhost:8087/docs**.
All **14 existing operations** are implemented and have positive native and live
HTTP acceptance. Three additional runtime routes provide docs, OpenAPI and capabilities.

The [live report](evidence/mysql-endpoint-audit/2026-11-09T10-29-40.911Z/report.json) passes **7/7 suites across 60 HTTP requests**,
including actual existing Keycloak login, signed platform sessions, two-tenant
isolation, real SMTP capture, invitation acceptance, role changes and revocation.
All 14 documented operations have successful assertions. Expected 401/403/409
responses are separate negative coverage and never count as successful endpoints.
The API/worker image identities are retained; the final image is unchanged during
the audit. All three temporary provider users were removed, and issued test
sessions were logged out/revoked. Synthetic MySQL identities, audit and mail remain.

The [full native report](evidence/mysql-integration/2026-11-09T10-40-03Z/report.json) passes **42 top-level tests / 69 test and subtest results**, with all 42 independently inventoried tests accounted for. Vet passes, with zero failed/skipped tests and zero race warnings. Six packages compile without test files; the API executable is also exercised by the process suite. The shared MySQL module separately passes vet and 47 top-level tests. Private target/reference MySQL and PostgreSQL characterization fixtures are distinct from application databases.

| Operation | MySQL | Asserted coverage |
| --- | --- | --- |
| GET /healthz | [x] Native + live | Liveness remains available when the database pool is closed |
| POST /internal/audit/list | [x] Native + live | Admin/tenant scope, JSON details and deterministic 101-row sentinel |
| POST /internal/invitations/change | [x] Native + live | Create/replay/conflict, pending-email uniqueness, resend/cancel, stale generation and rate bounds |
| POST /internal/invitations/list | [x] Native + live | Admin/tenant scope, safe projection, expired and delivery status, no secret material |
| POST /internal/invitations/start | [x] Native + live | Token/issuer/expiry binding and real provider acceptance |
| POST /internal/login/complete | [x] Native + live | Real Keycloak code exchange, single-use state/binding, explicit membership and tenant selection |
| POST /internal/login/start | [x] Native + live | Persisted digests, nonce and PKCE; proxy and origin rejection |
| POST /internal/logout | [x] Native + live | Idempotent atomic revocation/audit, including logout while inactive and later tenant reactivation |
| POST /internal/members/change | [x] Native + live | Version conflict, last-admin race, post-lock expiry, session revocation and audit rollback |
| POST /internal/members/list | [x] Native + live | Admin authorization, foreign isolation, 101-row sentinel and tenant-key query plan |
| POST /internal/session | [x] Native + live | Signed platform JWT, current role/version, tenant/expiry/revocation, idle renewal and read-only sessions |
| POST /internal/sessions/list | [x] Native + live | Current user/tenant only; safe projection and stable bounded cursor |
| POST /internal/sessions/revoke | [x] Native + live | One/all, ownership, foreign tenant denial, immediate revocation and durable audit |
| GET /readyz | [x] Native + live | Applied migration state/count and exact table grants; missing grants and dependency outage rejected |

## Authentication and scope

Every internal operation requires the private proxy bearer credential. Session
bodies supply auth-issued opaque credentials; caller-provided actor/tenant/role
assertions do not grant access. Login never provisions users or auto-links email.
Operator provisioning is explicit, and a configured login tenant cannot fall back
to another tenant. With no default, the source first-active-membership selection
is preserved. Invitations select their own authorized tenant.

The frozen 14-step identity contract used by decisions is preserved; seven new
auth tables have their own ledger. Issued sessions work with the existing MySQL
decision session verifier. Runtime readiness checks counts/state and restricted
grants; full definition/checksum drift verification belongs to the installer.

## Evidence boundaries

The initial [full-suite failure](evidence/mysql-integration/2026-11-09T09-53-39Z/raw.jsonl)
is retained: an older PostgreSQL preflight test's deliberate DROP needed CASCADE
for newer fixture foreign keys. The correction is confined to its disposable
database, and the final complete suite passes. The initial live callback-capture
timeout is retained under evidence/mysql-endpoint-audit; the final browser runner
waits for the actual redirect before consuming its code.

Keycloak storage remains PostgreSQL by the user's choice. Application auth state
uses MySQL. This is local development acceptance, not production data transfer,
historical PostgreSQL user/session import, frontend cookie/proxy cutover, provider
disaster recovery, invitation capacity or sustained throughput certification.
PostgreSQL entrypoints and preflight tools remain separate. See
[plan](MYSQL_PLAN.md), [Docker setup](MYSQL_DOCKER.md) and [worker status](MYSQL_WORKER_STATUS.md).
