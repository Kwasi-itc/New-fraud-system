# Findings and recommendations

PostgreSQL is a shared transactional platform for six Go services and the investigator library, not just a storage backend behind an ORM. The application uses raw pgx SQL, runtime tenant DDL, published SQL read contracts, database-enforced business/authorization rules, transactional River enqueue and durable outbox/projection state. Frontend and HTTP-only integrations depend indirectly on those guarantees. Optional local Keycloak adds provider-owned PostgreSQL state. See PG-01–35 in [the inventory](02_postgresql_usage_inventory.md).

The largest conversion work is behavioral:

1. **Recoverable schema changes:** tenant physical DDL and metadata currently share a transaction. Future MySQL behavior needs an explicit intent/reconcile/publication lifecycle, including archive/drop and schema drift (PG-05–07).
2. **Durable background work:** River jobs are inserted inside business transactions. A queue/outbox design must precede conversion of ingestion, decision and screening writes; replacing workers later would leave incomplete atomicity (PG-09/10/14).
3. **Shared authorization and investigation:** identity locks/version checks, owner review, case events/outbox and projection functions are coordinated SQL contracts. Preserve ownership and one database; do not scatter them across unrelated HTTP commits (PG-15/17–19).
4. **Exclusion audit integrity:** newer working-tree code relies on deferred FKs and immutable audit/source checks. Immediate-FK write ordering is a concrete redesign, not an optional cleanup (PG-16).
5. **Exact keys, filters and results:** conditional unique indexes, unbounded text keys, collations, arrays, JSON, infinity, timestamps and query ordering influence both correctness and throughput (PG-20–26/35).

Build foundations and proofs before service conversion: target/naming/types, queue semantics, transaction/error contracts, schema lineage, then full data-model lifecycle and dependent services. Identity contract work can proceed alongside service work. Implement in isolated environments; coordinate the live switch only when all shared-schema consumers, workers and tools are ready. [Roadmap](09_migration_roadmap_and_checklist.md) gives prerequisites and acceptance criteria.

Retain existing service boundaries and HTTP/domain contracts where possible. A MySQL migration does not justify separate per-service databases, a new ORM, or a wholesale tenancy redesign without a separate decision. Partial MySQL deployment cannot leave direct SQL consumers reading stale PostgreSQL owner data. Avoid treating legacy standalone Compose files or the incomplete scale override as the desired architecture.

No evidence here establishes a MySQL performance or cost improvement. Require a business reason, exact target version, measurable SLOs, real data profile, queue decision, provider scope and rollback policy before implementation. Existing PostgreSQL plan tests/workloads are valuable starting points, but target query plans and representative contention tests are mandatory. [Risks/questions](07_risks_dependencies_and_open_questions.md) and [validation strategy](08_testing_and_validation_strategy.md) define the unresolved work.

The audit created documentation only. Repository-wide discovery, critical-path source review and source-link/coverage checks are recorded in [15](15_coverage_and_validation.md). No application/database/configuration/dependency changes, live-schema inspection, application test execution, MySQL prototype or benchmark were performed. The earlier reverted prototype remains historical context; it does not close this roadmap's acceptance gates.
