# PostgreSQL to MySQL Migration

Presentation draft — reviewed 2026-10-21.

- Audience: engineering, product, and operational stakeholders.
- Baseline: repository configuration for PostgreSQL 16; comparison against MySQL 8.4 with InnoDB.
- Status: assessment and proposed approach, not an implemented migration.
- Format: numbered slides separated by horizontal rules; evidence and references follow the slides.

---

**Slide 1 — What are we deciding?**

- Should the fraud system move from PostgreSQL to MySQL?
- Which current capabilities would need replacement?
- What would change in application code, data, and operations?
- What evidence would make migration worthwhile?
- Goal: preserve product behavior, tenant isolation, audit history, and reliable processing.

---

**Slide 2 — Executive summary**

- MySQL can support the product's main business capabilities.
- Our implementation depends extensively on PostgreSQL-specific behavior.
- Migration would affect database structure, queries, workers, authorization, and recovery.
- Correctness under concurrent requests is a larger concern than SQL syntax alone.
- No comparative benchmark in this review establishes a performance or cost benefit.
- Recommendation: retain PostgreSQL unless a measurable business requirement justifies the investment.

---

**Slide 3 — Current architecture**

- Application services share the logical database `fraud`.
- Namespaces include `core`, `core_ingestion`, `screening`, `case_manager`, and `identity`.
- The data-model service creates tenant schemas and tenant business tables dynamically.
- Services have independently tracked migrations.
- `core` contains objects owned by more than one service: schema name alone does not establish ownership.
- River supplies PostgreSQL-backed job processing through the configured `riverpgxv5` integration.
- Local Keycloak configuration also points to PostgreSQL.

Evidence: [deployment configuration](docker-compose.yml), [tenant schema manager](backend/data-model-service/internal/tenantdb/postgres/schema_manager.go), [data-model worker](backend/data-model-service/cmd/worker/main.go).

---

**Slide 4 — How much of the application is affected?**

- The current working tree contains 57 upward SQL migration files.
- 155 Go files reference `jackc/pgx`, including tests.
- Scope includes data-model, ingestion, decisions, screening, case management, auth, and shared investigator code.
- Include API processes, workers, migration commands, reconciliation tools, and test fixtures.
- Counts are an inventory snapshot, not an engineering estimate.
- Uncommitted files are included; checked-in history and deployed state may differ.

---

**Slide 5 — What MySQL already supports**

- InnoDB transactions and foreign keys.
- Row locks and worker claiming with `FOR UPDATE SKIP LOCKED`.
- JSON documents and JSON querying.
- Stored routines and triggers, using MySQL's own syntax and restrictions.
- Relational joins, reporting, and indexed lookups.
- Business features can remain, but their implementation and guarantees need validation.

Sources: [locking reads](https://dev.mysql.com/doc/refman/8.4/en/select.html), [JSON](https://dev.mysql.com/doc/refman/8.4/en/json.html), [transaction isolation](https://dev.mysql.com/doc/refman/8.4/en/innodb-transaction-isolation-levels.html).

---

**Slide 6 — The compatibility gaps at a glance**

| Current dependency | MySQL 8.4 / InnoDB situation | Migration consequence |
|---|---|---|
| Schemas within one database | Schema means database | Redesign namespaces |
| Partial and conditional unique indexes | No equivalent index predicate | Redesign indexes and constraints |
| Transaction-scoped advisory locks | Named locks are session-scoped | Redesign lock lifecycle |
| Transactional DDL | DDL implicitly commits | Add partial-failure recovery |
| Deferred foreign keys | Checks are immediate | Change write ordering or relationships |
| PostgreSQL arrays and JSONB interface | Different types and expressions | Convert storage and queries |
| PL/pgSQL and authorization functions | Existing code cannot execute unchanged | Port rules and permissions |
| Configured River PostgreSQL integration | Existing driver/transaction integration cannot connect to MySQL | Replace or redesign queue integration |

- Missing native capabilities and incompatible implementations are different categories.
- The following slides explain the impact of each.

---

**Slide 7 — Schemas and tenant organization**

- PostgreSQL supports database → schema → table organization.
- MySQL treats `SCHEMA` and `DATABASE` as synonyms.
- Existing names such as `case_manager.cases` cannot retain the same meaning inside one MySQL database.
- Candidate design: service-prefixed and tenant-prefixed tables in one application database.
- Define deterministic identifiers, collision detection, length handling, and safe quoting.
- Prefixes do not enforce permissions or tenant authorization.
- Do not introduce separate databases per service or tenant without an explicit architecture decision.

Source: [MySQL CREATE DATABASE](https://dev.mysql.com/doc/refman/8.4/en/create-database.html).

---

**Slide 8 — Partial indexes: indexing only relevant records**

- PostgreSQL can index only rows matching a condition.
- Current examples include open, unassigned cases and active investigator workloads.
- MySQL has no equivalent `CREATE INDEX ... WHERE condition` syntax.
- Candidate replacements include composite indexes and generated columns.
- Replacement indexes can have different storage, write cost, and execution plans.
- Test realistic queue sizes and distributions of open versus closed cases.
- Do not assume a replacement is efficient merely because the query returns the same result.

Evidence: [assignment migration](backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql). Source: [MySQL index syntax](https://dev.mysql.com/doc/refman/8.4/en/create-index.html).

---

**Slide 9 — Conditional uniqueness: protecting business rules**

- Current unique indexes can apply only to active records.
- Examples: non-archived inbox names, non-deleted tags, and active case-tag relationships.
- Business example: an active tag must be unique in its scope, but a deleted tag's name may be reused.
- An ordinary unique index would change that behavior.
- Candidate replacement: nullable generated values with tenant-scoped unique indexes.
- Validate creation, soft deletion, restoration, and simultaneous requests.
- Include text comparison and NULL behavior in the design.

Evidence: [case-manager initial migration](backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql). Source: [MySQL unique and functional indexes](https://dev.mysql.com/doc/refman/8.4/en/create-index.html).

---

**Slide 10 — Advisory locks: coordinating logical operations**

- PostgreSQL transaction-scoped advisory locks end automatically on commit or rollback.
- The system uses them in identity, investigator review, and case intake paths.
- MySQL `GET_LOCK()` survives commit and rollback; it belongs to the session.
- A pooled connection can retain an incorrectly managed session lock.
- Prefer narrowly scoped row locks where they meet the required contract.
- If using session locks, explicitly own the connection and release locks on all exits.
- Test request cancellation, timeouts, crashes, and connection reuse.

Evidence: [investigator review](backend/investigator/review.go), [identity store](backend/auth-service/internal/platform/store.go). Source: [MySQL locking functions](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html).

---

**Slide 11 — Schema changes and partial failure**

- Current PostgreSQL migrations group schema changes inside transactions.
- MySQL DDL implicitly commits; supported atomic DDL protects one statement, not an entire migration transaction.
- Failure halfway through a migration can leave earlier steps applied.
- Dynamic model changes also need coordination between metadata and physical tables.
- Record intended changes and reconcile the actual database state after interruption.
- Make retries safe; detect incompatible existing columns or indexes rather than accepting them silently.
- Test a disconnect immediately after a DDL operation succeeds.

Evidence: [dynamic table and field operations](backend/data-model-service/internal/tenantdb/postgres/indexing.go). Source: [MySQL atomic DDL](https://dev.mysql.com/doc/refman/8.4/en/atomic-ddl.html).

---

**Slide 12 — Newly identified: deferred foreign keys**

- PostgreSQL can delay selected relationship checks until transaction commit.
- Screening exclusion migrations use `DEFERRABLE INITIALLY DEFERRED`.
- These relationships connect exclusions, exclusion audit events, and investigator reviews.
- MySQL InnoDB checks foreign keys immediately.
- An equivalent final database state can still fail if records are written in the wrong order.
- Reorder writes or redesign the relationship while preserving one atomic operation.
- Do not disable foreign-key checking as an application workaround.
- Also rewrite PostgreSQL inline `REFERENCES` definitions as explicit MySQL foreign-key constraints; verify enforcement.

Evidence: [screening exclusion migration](backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql). Source: [MySQL foreign-key differences](https://dev.mysql.com/doc/refman/8.4/en/ansi-diff-foreign-keys.html).

---

**Slide 13 — JSONB, arrays, and data representation**

- JSONB stores configuration, responses, and projections; PostgreSQL functions build and transform these values.
- Arrays appear in allowed outcomes and identity roles.
- MySQL JSON can replace some storage, but PostgreSQL operators and array expressions need rewriting.
- Choose related tables or JSON arrays based on access and integrity requirements.
- Preserve the distinction between missing fields, JSON null, and SQL NULL.
- Test numeric values and round trips through Go and JSON.
- Use explicit indexes for frequently queried paths; storage compatibility alone is insufficient.

Evidence: [decision schema](backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql), [identity schema](backend/auth-service/internal/platform/schema.sql). Source: [MySQL JSON](https://dev.mysql.com/doc/refman/8.4/en/json.html).

---

**Slide 14 — Newly identified: timestamps and infinity**

- Tenant tables currently use `valid_until TIMESTAMPTZ DEFAULT 'INFINITY'`.
- MySQL temporal types have finite ranges; infinity needs an explicit replacement.
- Candidate representation: NULL means no end date, with matching query and index changes.
- Do not mechanically map every PostgreSQL timestamp to MySQL `TIMESTAMP`.
- MySQL 8.4 `TIMESTAMP` ends in 2038; `DATETIME` has a wider range but different time-zone behavior.
- Define UTC handling, fractional precision, accepted ranges, and invalid-value behavior.
- Verify expiration, scheduling, audit order, and cursor pagination.
- Existing validity columns alone do not establish complete historical versioning.

Evidence: [tenant table creation](backend/data-model-service/internal/tenantdb/postgres/indexing.go). Source: [MySQL temporal types](https://dev.mysql.com/doc/refman/8.4/en/datetime.html).

---

**Slide 15 — Newly identified: text comparison and key design**

- MySQL's default nonbinary collation is case-insensitive; the default named collation is also accent-insensitive.
- A migration can change whether two identifiers or names compare equal.
- Current queries use `ILIKE`; indexes also include PostgreSQL `text_pattern_ops`.
- Choose comparison rules separately for external IDs, human names, emails, and search fields.
- Detect collisions under the target collation before loading data.
- Review bounded lengths for indexed text and compound keys, including screening exclusion keys.
- Do not truncate identifiers or use a text-prefix unique index as a substitute for full-value uniqueness.
- Choose and test UUID encoding, binary payload handling, and numeric precision explicitly.

Evidence: [decision queries](backend/decision-engine-service/internal/store/postgres/decision_repository.go), [prefix index](backend/decision-engine-service/internal/migrations/metadata/000015_decisions_object_id_prefix_idx.up.sql). Sources: [MySQL case sensitivity](https://dev.mysql.com/doc/refman/8.4/en/case-sensitivity.html), [index rules](https://dev.mysql.com/doc/refman/8.4/en/create-index.html).

---

**Slide 16 — Business rules and authorization inside the database**

- PL/pgSQL triggers enforce assignment capacity and immutable screening exclusion history.
- Functions reconcile screening results into decision execution records.
- Identity functions check users, roles, tenant status, membership version, and session validity.
- Some authorization paths take locks to coordinate with revocation and role changes.
- MySQL has routines and triggers, but these implementations require rewriting.
- Preserve lock ordering and authorization within the mutation transaction where required.
- A separate permission check before a later write may introduce a race with revocation.
- Test revoked sessions, role changes, concurrent assignments, and attempted audit-history modification.

Evidence: [identity authorization](backend/auth-service/internal/platform/schema.sql), [assignment capacity](backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql), [screening reconciliation](backend/decision-engine-service/internal/migrations/metadata/000019_screening_projection.up.sql).

---

**Slide 17 — Newly identified: River is a migration workstream**

- Current worker wiring uses River's PostgreSQL driver and `pgx.Tx`.
- Ingestion uses `InsertTx` to enqueue jobs inside the same transaction as application writes.
- Supporting `SKIP LOCKED` in MySQL does not make this integration portable.
- A complete MySQL migration must replace or redesign the configured queue integration.
- Preserve scheduling, retries, uniqueness, worker concurrency, and recovery behavior actually used by each service.
- Include operational tooling and outstanding jobs in the migration plan.
- Do not assume that all River versions or drivers have identical capabilities; evaluate the selected replacement explicitly.

Evidence: [ingestion enqueueing](backend/ingestion-service/internal/riverjobs/enqueuer.go), [data-model worker](backend/data-model-service/cmd/worker/main.go). Sources: [River drivers](https://riverqueue.com/docs/database-drivers), [transactional enqueueing](https://riverqueue.com/docs/transactional-enqueueing).

---

**Slide 18 — Reliable processing: preserve atomic enqueueing**

- Required behavior: business data and its pending work commit together, or neither commits.
- Writing data and then calling an external queue creates a failure window between the two actions.
- Candidate design: a MySQL-backed transactional job table or transactional outbox.
- For external delivery, publish committed outbox entries and track retries.
- Expect repeated delivery; make consumers idempotent.
- Use leases or ownership tokens so a stale worker cannot complete a reassigned job.
- Release database locks before network calls.
- At cutover, drain or transfer queued work and reconcile in-flight external requests.

---

**Slide 19 — Drivers, SQL, errors, and repair tooling**

- Replace PostgreSQL drivers, pools, migration runners, and transaction types exposed through ports.
- Rewrite placeholders, quoting, casts, `ON CONFLICT`, and `RETURNING` workflows.
- Review aggregate `FILTER`, `ANY(array)`, interval arithmetic, JSON expressions, and database functions.
- Ingestion's created-versus-updated outcome must remain accurate during concurrent writes.
- Map duplicate keys, constraint errors, deadlocks, and lock timeouts to the correct domain behavior.
- Preserve cancellation and rollback behavior; retry only operations designed to be safe to retry.
- Port schema introspection such as `to_regclass`, plus repair and reconciliation commands.
- Update PostgreSQL-specific monitoring and test fixtures as part of the same workstream.

Evidence: [ingestion writes](backend/ingestion-service/internal/store/postgres/tenant_data_writer.go), [transaction port](backend/data-model-service/internal/ports/transactions.go), [index introspection](backend/data-model-service/internal/tenantdb/postgres/index_introspection.go), [bulk error handling](backend/case-manager-service/internal/service/bulk.go).

---

**Slide 20 — Concurrency and transaction isolation**

- Ingestion explicitly relies on `READ COMMITTED` visibility between statements.
- MySQL InnoDB defaults to `REPEATABLE READ`, with different range-lock behavior.
- Setting the same isolation-level name does not prove identical behavior.
- Verify duplicate ingestion, concurrent updates, queue claiming, and assignment limits.
- Keep transactions short and use consistent lock ordering.
- Avoid serializing unrelated tenants or cases through a shared parent row.
- Measure deadlocks, lock waits, retries, and tail latency under representative contention.

Source: [MySQL isolation levels](https://dev.mysql.com/doc/refman/8.4/en/innodb-transaction-isolation-levels.html).

---

**Slide 21 — Performance: what we must measure**

- Neither engine is automatically faster for this workload.
- Compare the same business behavior, data distribution, durability expectations, and resource budget.
- Include ingestion throughput, decision latency, investigator queues, reports, and worker backlog.
- Measure p95/p99 latency: the slower requests that averages can hide.
- Capture query plans, rows examined, CPU, memory, I/O, and lock contention.
- Include large tenants, many small tenants, long histories, and skewed queues.
- Retain pagination, batching, bounded queries, and indexed tenant filters.
- Adapt existing replay tooling; PostgreSQL observation queries cannot measure MySQL unchanged.

Evidence: [database-scale suite](backend/stress-tests/production_replay/database_scale_suite.py), [PostgreSQL observation](backend/stress-tests/production_replay/postgres_observation.py).

---

**Slide 22 — Potential benefits and costs**

| Potential benefit | Evidence needed | Cost or tradeoff |
|---|---|---|
| Align with existing MySQL expertise | Team capability and support coverage | Training if expertise does not already exist |
| Meet a hosting or customer requirement | Specific supported deployment requirement | Infrastructure and implementation changes |
| Reduce operating cost | Comparable capacity, resilience, and pricing | Migration, testing, and temporary duplicate environments |
| Improve workload performance | Representative benchmark and query plans | Tuning and possible regression elsewhere |

- Include engineering time, operational support, downtime, and delayed product work in total cost.
- Permanent support for both engines would add continuing maintenance; it is not a default migration goal.

---

**Slide 23 — Operations and identity-provider migration**

- Pin the target MySQL release and hosting platform; this document uses 8.4 as a comparison baseline.
- Specify InnoDB, SQL mode, charset, collation, time zone, isolation, and connection settings.
- Budget connections across every API, worker, and replica against one database-wide limit.
- Define backups, point-in-time recovery, restore tests, failover, and monitoring.
- Keep critical authorization and mutation reads sufficiently current; review any replica routing.
- Handle Keycloak with a version-supported migration process; validate that process before cutover.
- Do not hand-translate provider-owned tables or assume a realm export includes every identity-state category.
- Decide how sessions, credentials, and pending invitations are preserved or intentionally invalidated.

---

**Slide 24 — Proposed delivery phases**

1. Inventory dependencies, business invariants, and deployment requirements.
2. Prototype the highest-risk paths in an isolated MySQL environment.
3. Implement persistence, workers, routines, authorization, and schema recovery.
4. Build repeatable data conversion and validation tooling.
5. Run full system, concurrency, performance, and failure testing.
6. Rehearse cutover and rollback on representative data.
7. Cut over, stabilize, and retire old infrastructure only after exit criteria pass.

- Keep HTTP and domain behavior stable where possible.
- Preserve service migration ownership and coordinate dependencies across services.
- Each phase requires evidence before progressing to production.

---

**Slide 25 — What the feasibility prototype must prove**

- Concurrent ingestion produces correct records and created/updated outcomes.
- Assignment limits hold under simultaneous requests.
- Business writes and pending jobs roll back together.
- Worker crashes, retries, and stale leases do not lose work or corrupt state.
- Dynamic DDL recovers after interruption without metadata/schema drift.
- Deferred screening relationships can be replaced without losing atomicity or audit integrity.
- Session revocation and role changes remain effective during concurrent mutations.
- Representative queue and lookup plans meet agreed performance targets.
- A small fixed-schema test alone does not establish production readiness.

---

**Slide 26 — Data conversion and reconciliation**

- Inventory every service table, tenant table, job queue, and provider-owned data set.
- Preserve primary identifiers and tenant-consistent relationships.
- Define conversions for timestamps, infinity, UUIDs, arrays, JSON, text, and binary evidence.
- Preflight duplicates under target collations and conditional uniqueness rules.
- Load in a dependency-aware order and prove constraints are active.
- Compare row counts, critical values, relationships, business totals, and audit ordering.
- Validate queue state, retries, scheduled execution times, and outstanding external effects.
- Use canonical comparisons where storage encoding differs; raw byte equality may be inappropriate.
- Account for rows changed or deleted during transfer, not just the initial snapshot.

---

**Slide 27 — Cutover choices**

**Maintenance window**

- Pause new writes and coordinate API and worker shutdown.
- Drain or reconcile in-flight work.
- Perform final data synchronization, validation, and application switch.
- Simpler coordination, but requires a measured downtime allowance.

**Minimal downtime**

- Copy a baseline and capture subsequent changes.
- Transform and reconcile changes in order, including deletes and schema changes.
- Stop old writers, catch up fully, validate, and switch.
- More moving parts; cross-engine change capture must be proven for this schema.

- Do not allow old and new workers to process the same external effects uncontrolled.

---

**Slide 28 — Rollback is a data problem**

- Before MySQL accepts new writes, reverting is comparatively straightforward.
- After new writes, PostgreSQL is stale unless those changes are reconciled back.
- Switching connection strings alone can lose accepted work.
- Define a rollback decision window, owners, and measurable triggers.
- Reconcile business data, audit records, jobs, and external side effects together.
- Record recovery objectives: acceptable data loss and acceptable recovery time.
- Rehearse restoration and reconciliation rather than relying on an untested runbook.

---

**Slide 29 — Acceptance criteria before production**

- Tenant isolation, inbox permissions, session validity, and audit immutability tests pass.
- Constraints and conditional uniqueness preserve the intended business rules.
- Concurrency and failure tests demonstrate rollback and retry safety.
- API and worker integration contracts pass against real MySQL.
- Migration ordering and partially completed migrations recover correctly.
- Query plans and load tests meet agreed latency and throughput budgets.
- Backups restore successfully within agreed recovery objectives.
- Data reconciliation and cutover rehearsal pass with documented results.
- Remaining gaps have explicit owners and are not represented as completed features.
- Destructive test setup runs only against isolated disposable databases.

---

**Slide 30 — Recommendation and decision inputs**

- Retain PostgreSQL unless a concrete business or operational benefit outweighs migration cost.
- If MySQL is required, start with the feasibility prototype and explicit acceptance criteria.
- Estimate delivery effort only after the dependency inventory and difficult prototypes are complete.
- Required inputs: data volume, tenant/table count, growth, throughput, downtime allowance, and recovery objectives.
- Required ownership: service engineers, database operations, identity integration, testing, and release coordination.
- A successful syntax conversion is not the decision gate; demonstrated business and operational parity is.

---

**Slide 31 — What the second review added**

- River integration and atomic job enqueueing as a separate migration workstream.
- Deferred screening foreign keys and inline foreign-key syntax risks.
- Authorization routines, revocation races, and immutable audit rules.
- Timestamp infinity, temporal range, precision, and UTC conversion.
- Collation collisions, indexed text sizing, and identifier representation.
- PostgreSQL transaction types exposed through service ports.
- Schema introspection, repair tools, error classification, and benchmark observation code.
- Queue-state transfer, identity-provider handling, and rollback after new writes.
- Historical prototype status, so past results are not presented as current implementation.

---

**Slide 32 — Current status and limits of this assessment**

- Current source wiring and deployment configuration target PostgreSQL.
- Existing migration plans report that an earlier prototype and preparation work were reverted at the user's request.
- Their recorded tests are historical reports, not results rerun during this review.
- This presentation does not resume migration implementation.
- The review inspected the current working tree, including uncommitted changes; it did not inspect the live database.
- No application tests, workload benchmark, migration, or runtime configuration change was performed for this documentation update.
- Engine differences were checked against official MySQL documentation; River references describe its integration model.
- The inventory is targeted, not proof that every SQL statement and dependency has been audited.
- No claim is made that optional PostgreSQL features such as RLS, GIN, or extensions are currently enabled without deployment evidence.

Related historical documents: [system migration plan](POSTGRES_TO_MYSQL_MIGRATION_PLAN.md), [data-model implementation plan](DATA_MODEL_MYSQL_IMPLEMENTATION_PLAN.md).

---

**Appendix — Evidence and reading guide**

| Topic | Repository evidence | What it establishes |
|---|---|---|
| Shared database and local identity provider | [docker-compose.yml](docker-compose.yml) | Configured topology, not live deployment state |
| Dynamic schema management | [schema manager](backend/data-model-service/internal/tenantdb/postgres/schema_manager.go), [indexing](backend/data-model-service/internal/tenantdb/postgres/indexing.go) | Tenant schema/table creation, field changes, timestamp infinity |
| Transaction coupling | [transaction ports](backend/data-model-service/internal/ports/transactions.go) | `RawTx() pgx.Tx` crosses the persistence boundary |
| Ingestion concurrency | [tenant writer](backend/ingestion-service/internal/store/postgres/tenant_data_writer.go) | Conflict handling, returning rows, read-committed assumption |
| Queue atomicity | [ingestion enqueuer](backend/ingestion-service/internal/riverjobs/enqueuer.go) | River `InsertTx` calls |
| Partial indexes | [initial case migration](backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql), [assignment migration](backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql) | Conditional uniqueness and queue/workload indexes |
| Deferred integrity and audit guards | [screening exclusions](backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql) | Deferred FKs, immutable event trigger, composite text key |
| Authorization | [identity schema](backend/auth-service/internal/platform/schema.sql) | Role arrays, session validity, lock-aware authorization |
| Cross-service projection | [screening reconciliation](backend/decision-engine-service/internal/migrations/metadata/000019_screening_projection.up.sql) | PL/pgSQL, JSONB construction, relationship checks |
| Introspection and measurement | [index state](backend/data-model-service/internal/tenantdb/postgres/index_introspection.go), [observation tooling](backend/stress-tests/production_replay/postgres_observation.py) | PostgreSQL-specific metadata/statistics queries |

- External citations beside the relevant slides are the compatibility references.
- Internal links identify observed implementation; proposed replacements remain design options.
- Historical plans must be read with their revert notices, not as evidence of currently installed MySQL functionality.
