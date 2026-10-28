# Security and integrity requirements

Status: required controls, proposed enforcement designs. Existing controls are verified source behavior, not an independent penetration test. [PG-17/18/19/35](../02_postgresql_usage_inventory.md) establish application identity, shared review and relational ownership; a database replacement must not remove them.

| Control ID | Existing evidence / gap | Target requirement | Verification gate |
|---|---|---|---|
| SEC-01 Injection | pgx parameters and quoted dynamic metadata identifiers | Parameterize values; allowlisted SQL shape; registry-authorized identifiers; no runtime multi-statements | Adversarial names, identifiers, sort/filter values and query-log inspection |
| SEC-02 Tenant scope | Tenant predicates, tenant-composite FKs, some ID-only metadata references | Preserve constraints; validate metadata ownership; reject cross-tenant reference/replay/key reuse | Negative SQL and API tests for every relationship and module |
| SEC-03 Revocation | Identity user/membership/session locks and version checks | Current locked authorization in same transaction as mutation; primary only | Revoke/disable/role-remove racing review, model edit and case operation |
| SEC-04 Review atomicity | Shared investigator transaction, immutable reviews and case routine | Owner mutation, review, case event and outbox all commit or all roll back | Fault after each write and lost commit acknowledgement |
| SEC-05 Integrity | FKs/checks/unique indexes plus capacity/review triggers | Immediate FK order, exact unique keys, no direct bypass of invariant fields | Restricted-account SQL attempts, collision and concurrent capacity tests |
| SEC-06 Database access | Compose/provider credentials; production grants not inspected | Per-owner roles, narrow cross-owner contracts, separate deploy/DDL/backup credentials | Export effective grants; deny arbitrary cross-owner DML and grants |
| SEC-07 Credentials | Environment configuration; live secrets not inspected | Secret manager or equivalent deployment facility, rotation, no committed DSNs | Rotate with active pools, scan logs/artifacts, test expired credential failures |
| SEC-08 Transport/storage | Production encryption not established | Verified TLS; protected database volumes, snapshots, exports and key custody | Wrong CA/hostname refusal; restore with authorized keys only |
| SEC-09 Delivery | Outbox leases, invitation generation, screening tokens | Fenced publication, stable idempotency keys, no network under locks | Crash/timeout/retry and duplicate external acknowledgement scenarios |
| SEC-10 Audit/privacy | Immutable investigator rows, domain audit/events | Preserve evidence and actor provenance; redact SQL arguments and PII in metrics | UPDATE/DELETE rejected; payload export access logged; retention policy reviewed |
| SEC-11 Recovery | Scripts and local deployment; production process unknown | Encrypted consistent backup, PITR, restore validation, protected recovery credentials | Timed isolated restore and failover with integrity/queue reconciliation |
| SEC-12 Safe deployment | Owner migrations and PostgreSQL transactional DDL assumptions | Checksummed coordinated migrations; resumable DDL; explicit targets and drift refusal | Partial DDL failure/restart; disposable-target protection tests |

## Privilege design

Runtime API and worker identities are distinct where worker permissions differ. Read contracts receive SELECT on explicit views/columns. Owner write roles cannot grant privileges, create arbitrary routines or disable auditing. DDL executor cannot read arbitrary sensitive business payloads; grant provisioning is a separate deployment responsibility. Dynamic object publication waits until required object grants exist. Backup access is monitored and backups carry the same sensitivity as production data.

Proposed fixed SQL SECURITY DEFINER procedures are justified only for owner mutation/authorization contracts requiring privileges the caller should not possess. Minimal definers, explicit grants, parameter validation, fixed object references and controlled deployment are mandatory. Verify definer account lifecycle and backup/restore behavior against the selected server; never use a root definer. SQL SECURITY INVOKER remains appropriate when callers already have justified access. [MySQL stored-object security](https://dev.mysql.com/doc/refman/8.4/en/stored-objects-security.html) describes the distinction; it does not certify this design.

Revoke direct writes to fields protected only by owner procedures, or retain a proven equivalent trigger. Procedures do not COMMIT or perform external calls. Their caller-owned transaction requirement needs integration tests, including accidental autocommit invocation; a procedure wrapping several statements is not automatically an atomic application transaction. Do not use an unverified `@@autocommit` check as a substitute for detecting an active transaction. Until transaction-entry enforcement and bypass prevention are demonstrated, the procedure design is not accepted.

The trusted service establishes tenant and actor from authenticated context; end users do not choose a privileged tenant via request payload. A routine receiving a tenant parameter does not independently authenticate that tenant. Treat compromised service credentials separately from honest-but-buggy query protection. Database account separation limits blast radius but does not create per-request RLS.

## Locking and retry rules

Maintain a documented partial order: identity user -> membership -> session; operation/idempotency guard; case; inbox/member; source resource/candidate; subordinate ledger/event rows. Source projection always locks source before its queue record. Batch keys and multi-resource case moves use sorted stable IDs. Inspect every caller for inversions before codifying the order; a universal order must not be asserted without checking auth revocation and case reassignment paths.

Locks protect only relevant actors/resources, never a whole tenant row for unrelated activity. Last-admin serialization is narrowly tenant/admin-domain scoped. No network call inside the lock boundary. READ COMMITTED is not itself a race-prevention mechanism; unique keys, current reads and narrow locks enforce invariants.

Retry only classified transient failures with bounded jitter and remaining deadline. Roll back the entire transaction first. Do not retry validation, authorization, stale version or unique-business conflicts. Lost commit acknowledgement triggers idempotency reconciliation. External deliveries may repeat; their receiver must deduplicate by stable event ID or the contract must explicitly expose at-least-once effects.

## Data protection and rollout

Determine jurisdictional retention, backup retention, key rotation and audit export policies before deployment; do not invent compliance requirements. No raw customer/provider payloads in benchmark artifacts unless appropriately approved and protected. Use synthetic or sanitized data preserving distributions. Login nonce/verifier/binding bytes, sessions, invitations, evidence uploads and case audit histories require separate migration validation and access controls.

Do not disable FOREIGN_KEY_CHECKS as a routine bulk-load strategy: re-enabling it is not a substitute for validating previously loaded rows. Load in dependency order and run full orphan/tenant/uniqueness checks. If exceptional staging requires relaxed checks, isolate it from application credentials and validate into constrained target tables before publication. No destructive test setup may target the application database.
