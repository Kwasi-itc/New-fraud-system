# Auth service MySQL implementation plan

The target is an independently runnable auth API on localhost:8087/docs and an
invitation delivery worker against the existing shared MySQL `fraud` database.
The existing OIDC issuer remains a configured external identity boundary; moving
Keycloak's own storage is a separate operation. Application permissions are
always assigned by auth, never inferred from provider claims or email matches.

1. Inventory the 14 current HTTP operations and invitation delivery task. Keep
   existing response contracts behind storage interfaces and retain PostgreSQL.
2. Preserve the existing identity migration prefix used by decisions. Add an
   independently tracked `identity_auth` plan for external identities, login
   attempts, audit, invitations, delivery and narrow concurrency guards.
3. Implement explicit operator provisioning, single-use login, tenant selection,
   session issuance/renewal/revocation, member management and bounded audit reads.
   Recheck expiry after lock waits and protect the last active administrator.
4. Implement encrypted invitation delivery, verified-email acceptance, replay,
   resend/cancellation, lease recovery, retries and generation fencing. Network
   calls happen after commit; SMTP delivery is at-least-once.
5. Supply restricted API/worker accounts, installer, Docker setup, readiness,
   OpenAPI/Swagger docs and private generated configuration. Do not reset data or
   silently disable authorization to make local requests succeed.
6. Run native MySQL integration tests for every route plus negative authority,
   rollback, tenant isolation, concurrency, grants and worker recovery. Preserve
   source PostgreSQL coverage and report optional/skipped checks explicitly.
7. Record endpoint/worker matrices with links to actual reports, then verify the
   live Compose runtime. Implementation, native acceptance, external-provider
   acceptance and production cutover remain separate claims.

Execution: steps 1-7 are complete for the local development profile. The
[native report](evidence/mysql-integration/2026-11-09T10-40-03Z/report.json) and [live report](evidence/mysql-endpoint-audit/2026-11-09T10-29-40.911Z/report.json) pass. The user selected
reuse of existing Keycloak, including its current storage. Production transfer,
frontend/consumer cutover and release gates are explicitly outside these checks.
Exact coverage and remaining limits are in [endpoint status](MYSQL_ENDPOINT_STATUS.md)
and [worker status](MYSQL_WORKER_STATUS.md).
