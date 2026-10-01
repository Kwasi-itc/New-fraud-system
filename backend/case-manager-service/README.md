# Case Manager Service

Phase 1 implements the correctness and authorization foundation in the shared application PostgreSQL database. This service owns the case_manager schema. This document covers Phase 1 contracts; see the [implementation plan](../../CASE_MANAGEMENT_IMPLEMENTATION_PLAN.md) for current progress on subsequent phases and the [intake contract](INTAKE_CONTRACT.md) for reliable integration behavior.

## Implemented behavior

- Commands atomically commit case changes, links, contributors, audit events, and pending outbox records. Failed writes roll back; relationship read failures are returned.
- Mutations explicitly lock cases. Inbox validation uses shared locks; archive/membership changes use exclusive locks. Independent case creation remains concurrent. Detail/event reads hold shared case/inbox locks through authorization and relationship reads.
- Tenant/inbox authorization applies in services and SQL list filters. Queue tags load in one batch. Indexed decision/screening lookup includes snoozed cases.
- Names, UUIDs, enums, actions, assignees, active destinations, and snooze dates are validated. Errors expose sanitized error/code fields. PATCH null clears escalation inbox, review level, or boost; omission preserves them. Empty review/boost strings also clear for compatibility.
- Human investigation actions record contributors, advance pending cases, self-assign eligible members, and clear attention boosts atomically. Explicit assignment/unassignment preserves the requested assignee and records the contributor. Integrations do not impersonate humans.
- The legacy source attachment endpoint requires source_file_id referencing screening evidence connected to the case by screening or decision. Metadata comes from the owning schema; arbitrary storage keys are rejected. Native case evidence now uses the Phase 4 upload/finalize/download endpoints described below.
- Unimplemented background actions return 501. Pending outbox rows do not imply delivery. See the intake contract for callback behavior added after Phase 1.

## Authentication

The API fails startup unless both identity contracts are configured:

| Variable | Required value |
| --- | --- |
| USER_JWT_ISSUER | Exact trusted issuer |
| USER_JWT_AUDIENCE | Audience intended for this API |
| USER_JWT_KEYS_FILE | Mounted JSON file mapping key IDs to PEM RSA public keys |
| SERVICE_AUTH_MODE | token |
| SERVICE_AUTH_TOKEN | Secret of at least 32 bytes, supplied through deployment secrets |
| SERVICE_AUTH_TENANT_IDS | Comma-separated explicit tenant UUID allowlist; no wildcard |

Human /v1/tenants/... requests require a Bearer JWT: RS256, allowlisted kid, RSA key of at least 2048 bits, iss, aud, sub, iat, exp, tenant_id, and roles containing case_investigator or case_manager_admin. nbf is enforced when present; clock tolerance is 30 seconds. Tenant claims must match the requested tenant. Key files support current/next keys; restart replicas to reload rotated keys. Configure the identity provider or trusted identity gateway to issue these claims. This service neither supplies login nor mints tokens; private signing keys never belong here.

Tenant admins manage inboxes, tags, and membership and can inspect their tenant's cases. Investigators see only member inboxes. Assignees, including admins, must be destination inbox members. Admins are not automatically assigned unless members. Membership subjects must match stable identity-provider sub values; there is no shared user directory to verify arbitrary subjects against.

Membership endpoints use /v1/tenants/:tenantId/inboxes/:inboxId/users: GET lists users; PUT /:userId accepts {"auto_assign_enabled":false}; DELETE /:userId removes membership. Removal conflicts while the member is assigned an open case; reassign/unassign first. Membership changes have atomic inbox audit/outbox records. X-Actor-ID never establishes identity.

Service Bearer tokens work only on /internal/... and /v1/screening-events/..., with tenant allowlists enforced by the service. The producer inbox read route is GET /internal/v1/tenants/:tenantId/inboxes/:inboxId. Screening API/worker clients send the configured token and report missing URL/token as errors. Never expose service tokens to browsers. Callback reviewer IDs do not establish human identity.

## Shared database and migrations

All services use the same logical database. Apply real owning-service migrations in this tested order: data-model, ingestion, decision-engine, screening, then case-manager. Each service retains its own migration tracking. Run go run ./cmd/migrate up from each service using the shared DATABASE_URL.

Runtime ownership validation uses tenant-qualified core.decisions and screening tables. Case-manager never writes another service's tables. Decision scenario/object metadata is authoritative. See the intake contract for grouping and delivery semantics added in Phase 2.

Migrations 000002/000003 reject orphan/cross-tenant relationships without deleting data; repair reported data before retrying. Local relationships have composite tenant foreign keys. Source references are validated on attachment without cascading foreign keys into another service's retention lifecycle. Historical metadata is preserved. Legacy files have no verified source provenance and are not retroactively certified.

Migration DDL is transactional and lock-bounded. Schedule indexes and constraint validation during a maintenance window on large existing datasets. If a version becomes dirty, inspect and repair its state before changing the recorded version. Down migrations preserve case data, membership audit history, and source provenance; re-upgrade is tested.

## Shared resource budget

| Setting | Default |
| --- | --- |
| DB_POOL_MAX_CONNS | 8 per process, allowed 1–100 |
| DB_STATEMENT_TIMEOUT | 10s |
| DB_LOCK_TIMEOUT | 3s |
| DB_IDLE_TRANSACTION_TIMEOUT | 15s |
| Connect / request deadlines | 5s / 15s |
| Minimum pool size | 0 |

Pool lifetime is 30 minutes with jitter; idle expiry is 5 minutes. SQL timeout overrides accept 1ms–1m; request deadlines still bound API work.

Budget all service API/worker replicas together: their maximum connections plus migration/admin connections and operational reserve must fit PostgreSQL's available slots. Eight is a case-manager cap, not an allocation guaranteed for every deployment. Keep worker concurrency within its pool and CPU/I/O budget. Before enabling traffic, measure mixed service workloads and observe pool acquire duration/canceled acquisitions, pg_stat_activity lock waits, CPU/I/O, and request latency. Isolated tests do not certify production capacity.

## Verification

Run go test ./... and go vet ./... in this module and screening-service.

PostgreSQL tests are skipped unless CASE_MANAGER_TEST_DATABASE_URL points to a dedicated test instance with database-creation privileges. Each test creates and drops only a unique test database, applying real owning-service and case-manager migrations. No application database is migrated.

Coverage includes HTTP/JWT identity, tenant/inbox denial, actor spoofing, assignment eligibility, investigation effects, authoritative source ownership, injected link/contributor/audit/outbox failures, concurrent edits, shared inbox validation versus archive, pool/lock limits, and migration preflight/down/up preservation. Screening client tests check authentication and missing configuration errors.

On disposable PostgreSQL 18, a 1,201-case / 1,201-decision-link fixture used exactly two queue queries for 10, 100, and 500 rows. One sample local run took 50.8ms (including initial connection), 4.5ms, and 7.1ms respectively. Decision lookup used case_decisions_lookup_idx with an index-only scan and approximately 0.31ms execution. These establish bounded query count and index use, not production throughput guarantees.

Reproduce measurement tests with the test database variable set:
go test ./internal/store/postgres -run 'TestPostgres(BoundedQueue|SharedDatabase)' -v

## Investigator workspace

Phase 3 adds the live Cases UI and authorized paginated queue/history APIs, transactional close-with-findings, and configured escalation. Apply migration `000005_workspace_indexes` before rollout. See the [investigator contract](INVESTIGATOR_CONTRACT.md) for identity integration, runtime settings, supported operations, query costs, and verification limits. Live sign-in/provider integration remains pending.

Phase 4 adds automatic assignment with per-member capacity, transactional snooze expiry, inbox SLAs, bounded evidence upload/finalize/download, bulk operations, tag administration and leased outbound delivery. Apply case migrations 000006–000008 and the case-owned River schema using `/app/migrate up` before starting the new API/worker. Compose enables `case-manager-worker` with four pooled connections; outbound delivery remains disabled until its publisher URL is configured. `/app/outbox` provides tenant-scoped failure inspection and replay. The [investigator contract](INVESTIGATOR_CONTRACT.md#phase-4-queue-operations) specifies capacity, SLA, file retention, partial-failure and retry semantics. Identity-provider deployment and mixed-service load verification remain open.
