# Slide 29: Acceptance criteria before production

[Reading guide](README.md) | [Previous: slide 28](28-rollback-and-recovery.md) | [Next: slide 30](30-recommendation-and-inputs.md)

Companion to slide 29 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

Acceptance criteria are observable conditions that the migration must satisfy before the target is allowed to carry production responsibility.

They convert a broad goal such as "migration complete" into evidence that reviewers can assess.

**Correctness evidence**

Verify tenant boundaries, inbox permissions, session checks, conditional uniqueness, and audit immutability. Exercise real API and worker flows against MySQL.

Tests should prove outcomes, not merely reproduce the implementation's SQL.

**Concurrency and recovery evidence**

Show that simultaneous ingestion and assignment preserve the rules. Interrupt workers and DDL operations. Confirm retries, rollback, and stale-owner handling.

Tests must also cover shared-schema compatibility and migration ordering. Individually passing service tests may miss cross-service assumptions.

**Performance evidence**

Use agreed latency and throughput budgets with representative data and resource limits. Include query plans and contention metrics.

No numerical production targets are supplied by these documents; those need to be established from workload and business requirements.

**Operational evidence**

Restore backups, exercise recovery procedures, rehearse the cutover, and reconcile data. Document the observed duration and remaining gaps.

All destructive test setup belongs in disposable isolated databases. Sharing one production database does not prohibit isolated test databases.

**An example of precise reporting**

"Concurrent duplicate ingestion passed under scenario X; failover during job delivery remains untested" is useful. "All tests passed, therefore production ready" hides the scope of the evidence.

**How to explain this slide aloud**

"Production readiness requires proof of behavior, recovery, and performance. We should state exactly what passed and what remains unverified."

---

[Reading guide](README.md) | [Previous: slide 28](28-rollback-and-recovery.md) | [Next: slide 30](30-recommendation-and-inputs.md)
