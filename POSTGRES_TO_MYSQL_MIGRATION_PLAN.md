# PostgreSQL to MySQL migration plan

Status: implementation reverted at the user's request; retained as a historical planning document. Application configuration remains PostgreSQL.

Prepared: 2026-10-14.

Revert update (2026-10-14): removed `backend/tools/mysql-feasibility` implementation/test files and restored the ingestion transaction/queue preparation files to their pre-migration Git baseline, including reversal of the bundled retry/error-handling changes. Removed the three added ingestion queue test files. The implementation updates below describe historical work that is no longer present. No application database or deployed service was changed by this source revert.

Implementation update (2026-10-14): the isolated MySQL 8.4.8 prototype passes race-enabled tests for atomic writes, rollback, cancellation, tenant-scoped keys/relationships, concurrent ingestion, and delivery leases. These are fixed-schema feasibility checks; Phase 2 is not complete because dynamic DDL recovery, real case intake, and representative query comparisons remain outstanding. No production service has switched databases.

Second implementation slice (2026-10-14): deterministic tenant/table and field naming plus recoverable additive DDL now pass race-enabled tests, including a real DDL-session disconnect after commit, concurrent schema callers, collision/drift detection, and cancellation cleanup. Nullable int/float/text/timestamp fields are covered. Full dynamic-model semantics, automatic reconciliation, real case intake, and representative query comparisons remain outstanding; Phase 2 is still incomplete.

Third implementation slice (2026-10-14): ingestion's mutation-store contract now owns upload/deferred enqueue operations; its service layer and ports no longer expose PostgreSQL transaction types or depend on River. The PostgreSQL adapter retains atomic River insertion. Missing queue configuration fails explicitly, and retryable worker failures now return errors for River-owned retries instead of succeeding through no-op enqueuers. The full ingestion-service test suite passes against disposable PostgreSQL. MySQL ingestion repositories, data-model transaction extraction, deployment, and cutover remain outstanding.

Verification for the third slice: the PostgreSQL storage, service, and River adapter packages also pass race detection. Real queue insertion tests cover rollback, cancellation, scheduling, and concurrent deduplication. Full worker lifecycle/recovery tests and migration-scale benchmarks are still required.

## Objective and recommendation

Evaluate and, if justified, migrate the fraud system from PostgreSQL to MySQL while preserving tenant isolation, service ownership, business behavior, audit history, and reliable background processing.

This is a backend architecture migration, not a connection-string change. Stay on PostgreSQL unless MySQL addresses a concrete business or operational requirement. No comparative benchmark currently establishes a performance or cost benefit.

The earlier implementation authorization has been superseded by the request to revert its code. The feasibility prototype and its compatibility inventory were removed. The remaining sections preserve the design discussion, not an active implementation instruction. Preserve one shared logical application database unless a separate architecture decision explicitly changes that requirement.

## Verified baseline

The initial read-only inspection found:

- Local PostgreSQL 16.15 running in Docker, with application database `fraud`.
- Shared schemas: `core`, `core_ingestion`, `screening`, `case_manager`, and `identity`.
- `core` contains tables owned by both data-model and decision-engine; schema names do not fully define service ownership.
- Two tenant schemas, each containing `merchants`, `merchant_products`, and `transactions`.
- River queue tables in `public` and `case_queue`.
- Local Keycloak tables in `keycloak`.
- No row-level security enabled on the inspected tables. Isolation relies on application authorization, query scope, tenant-specific storage, and constraints.
- PostgreSQL-specific Go dependencies, including `pgx`, PostgreSQL migration drivers, and River's `riverpgxv5` integration.
- PostgreSQL-specific SQL, including advisory locks, `RETURNING`, JSONB expressions, casts, and `ILIKE`.
- Dynamic tenant DDL and ingestion updates that modify records in place. Validity columns do not establish complete version history.

Observed migration versions were data-model 6, ingestion 2, decision-engine 20, screening 7, and case-manager 13, with no dirty flags. These are a snapshot, not proof of full schema compatibility or deployment readiness.

Repository entry points:

- [Main deployment](docker-compose.yml)
- [Database-scale deployment override](docker-compose.database-scale.yml)
- [Tenant DDL](backend/data-model-service/internal/tenantdb/postgres/indexing.go)
- [Ingestion writes](backend/ingestion-service/internal/store/postgres/tenant_data_writer.go)
- [Case-manager transactions](backend/case-manager-service/internal/store/postgres/transactions.go)
- [Screening delivery outbox](backend/screening-service/internal/store/postgres/case_event_outbox.go)
- [Shared investigator logic](backend/investigator)

## Scope

Include data-model, ingestion, decision-engine, screening, case-manager, auth, shared investigator code, workers, migration commands, operational scripts, deployment configuration, and integration/load tests.

Account explicitly for Keycloak data and configuration. Do not translate provider-owned tables manually or assume a realm export preserves every category of identity state.

Keep HTTP and domain contracts stable where possible. Do not combine this effort with an unrelated ORM adoption, tenancy redesign, or service split. Temporary PostgreSQL and MySQL environments are needed for validation; permanent dual-engine support is not a default objective.

## Tradeoffs

| Area | Potential benefit | Cost or risk |
|---|---|---|
| Operations | Alignment with existing MySQL expertise, hosting, or organizational standards | New backup, restore, monitoring, and failover procedures if that expertise does not already exist |
| Performance | Opportunity to tune for a measured workload | Different plans, index behavior, and contention; no automatic improvement |
| Persistence | Existing domain/repository boundaries may reduce changes outside storage code | Drivers, transaction types, SQL, errors, and migrations must be ported |
| Tenant storage | One shared database can remain | PostgreSQL namespaces have no identical MySQL equivalent |
| Jobs | Existing outbox principles remain useful | River integration needs replacement for a complete MySQL migration |
| Schema changes | MySQL supports dynamic DDL | Implicit commits require explicit partial-failure recovery |
| Delivery | Possible long-term operational consolidation | Migration competes with product work and temporarily increases maintenance |

## Design decisions required

### Target deployment

- Select and pin an exact supported MySQL release and hosting platform. MySQL 8.4 documentation is the compatibility baseline for this proposal, not a final deployment choice.
- Use InnoDB for application tables requiring transactions and foreign keys.
- Record data volume, tenant/table counts, growth, downtime allowance, recovery objectives, and representative workload targets.
- Specify SQL mode, character set, collation, time zone, isolation level, and connection settings explicitly.
- Review one database-wide connection and resource budget across all API and worker replicas.

### Schema and tenant mapping

MySQL treats `SCHEMA` as a synonym for `DATABASE`; it does not provide PostgreSQL's namespace layer inside a database.

To preserve one application database, prototype service-prefixed tables and tenant-prefixed dynamic tables:

| PostgreSQL | Illustrative MySQL mapping |
|---|---|
| `core.decisions` | `fraud.decision_decisions` |
| `case_manager.cases` | `fraud.case_cases` |
| `tenant_<id>.transactions` | `fraud.t_<tenant_key>_transactions` |

These names are illustrative, not an approved naming contract. Define a deterministic identifier registry with collision detection, identifier-length handling, and safe quoting. Never derive executable identifiers directly from untrusted input.

Retain independent service migration histories and tenant-consistent composite constraints. Document cross-service read contracts and mutation ownership. Table prefixes are not authorization controls.

Shared tenant tables with `tenant_id` are an alternative, but tenants can define different models. Treat that alternative as a separate redesign requiring an explicit decision and broader testing. Do not silently create a database per service or tenant.

### Queue replacement

River currently documents PostgreSQL and SQLite backends, not MySQL. Inventory each River integration, including transactional enqueue, schedules, uniqueness, retries, cancellation, and lease recovery.

Evaluate either a maintained MySQL-compatible queue or an external queue with a transactional MySQL outbox. A custom queue requires explicit ownership and a full reliability contract; SQL polling alone is not a complete replacement.

Preserve atomic business writes plus job/outbox insertion, bounded claims, retry backoff, deduplication, lease fencing, terminal failure visibility, and recovery after worker crashes. Keep network delivery outside database transactions. MySQL supports `FOR UPDATE SKIP LOCKED`, but the existing claim statements still require adaptation.

### Transaction and type semantics

| Existing dependency | Required treatment |
|---|---|
| `pgx` pools and transaction types | MySQL driver and transaction adapters; preserve context cancellation, cleanup, and error contracts |
| `$1` placeholders and PostgreSQL casts | Port parameterization and expressions without interpolating user values |
| `RETURNING` and conflict handling | Design equivalent statements with explicit concurrency and affected-row semantics |
| PostgreSQL advisory locks | Prefer narrowly keyed transactional rows or uniqueness constraints where appropriate; design maintenance leases separately |
| JSONB operators and aggregation | Rewrite and verify JSON values, null handling, ordering, and indexing |
| UUIDs | Choose a consistent storage and encoding contract across primary and foreign keys |
| Timestamps and infinity | Define UTC handling, precision, range, and an explicit open-ended validity representation |
| Arrays and specialized indexes | Inventory actual usage and design query-specific replacements |
| Text comparison | Preserve intended case sensitivity, uniqueness, and search behavior through explicit collations |
| Database errors | Map duplicate keys, deadlocks, timeouts, and missing rows into existing application contracts |

MySQL `GET_LOCK()` is session-scoped, not a direct substitute for transaction-scoped PostgreSQL locks. InnoDB defaults to `REPEATABLE READ`; ingestion currently includes assumptions about `READ COMMITTED` visibility. Select isolation deliberately and test anomalies rather than relying on defaults.

## Implementation phases and exit gates

### Phase 1: Inventory and decision record

- [ ] Record the reason for migration and measurable acceptance criteria.
- [ ] Inventory tables, views, functions, indexes, constraints, migration dependencies, SQL readers, and all queue integrations.
- [ ] Inventory effective deployment settings without exposing credentials.
- [ ] Resolve tenant layout, provider storage, queue strategy, target release, and downtime approach.
- [ ] Identify service owners and reviewers for cross-service contracts.
- [ ] Document the architecture change and update standing requirements when implementation is authorized.

Exit gate: reviewed compatibility matrix and architecture decision; remaining unknowns have named owners.

### Phase 2: Feasibility prototype

Use disposable databases and representative synthetic or appropriately sanitized data. Never run destructive test setup against the application database.

- [ ] Provision a tenant and apply model changes, including interrupted DDL recovery.
- [ ] Exercise concurrent ingestion of the same object and verify returned outcomes.
- [ ] Commit business changes and enqueue work atomically; verify rollback removes both.
- [ ] Claim work concurrently and test crashed workers, expired leases, and duplicate delivery.
- [ ] Exercise duplicate case intake and tenant/inbox authorization.
- [ ] Compare one representative rule aggregate and case queue query with PostgreSQL.

Exit gate: correctness demonstrated for the highest-risk paths and measured feasibility against agreed performance targets. Reassess migration if these results undermine its expected benefit.

### Phase 3: Persistence and worker implementation

- [ ] Add MySQL storage adapters behind existing contracts where practical.
- [ ] Remove PostgreSQL-specific types from shared contracts where necessary.
- [ ] Port cross-service SQL/read views together with their consumers.
- [ ] Replace queue integrations and implement queue-state transition tooling.
- [ ] Preserve short transactions, narrow locks, consistent lock order, and bounded retries.
- [ ] Configure pool limits, timeouts, TLS, credentials, and service ownership controls.
- [ ] Port metadata introspection, dynamic DDL, operational scripts, and auth storage.

Exit gate: each service passes meaningful MySQL integration tests and its API/domain behavior remains compatible.

### Phase 4: Migrations and data transfer

- [ ] Create independently tracked MySQL migrations in dependency order.
- [ ] Retain PostgreSQL migration history for audit and rollback support; do not rewrite applied history.
- [ ] Make multi-step schema changes resumable with explicit progress and reconciliation. MySQL atomic DDL does not make a sequence of DDL statements transactionally reversible.
- [ ] Build bounded, restartable data extraction and loading with checkpoints.
- [ ] Preserve IDs, relationships, timestamps, tenant mappings, audit records, and required evidence references.
- [ ] Detect collation-related duplicate values before loading uniqueness constraints.
- [ ] Validate foreign keys and constraints explicitly; re-enabling checks is not itself evidence that imported rows are valid.
- [ ] Reconcile queue/outbox state, idempotency receipts, scheduled executions, and leases.
- [ ] Use a provider-supported Keycloak transition and document any session reset or reauthentication requirements.

Exit gate: a repeatable rehearsal produces matching per-tenant counts, normalized content checksums, valid relationships, and reconciled pending work.

### Phase 5: System validation

- [ ] Test tenant isolation and trusted identity across HTTP and direct SQL paths.
- [ ] Test rollback, concurrent mutation, deadlocks, bounded retries, and cancellation.
- [ ] Test crashes before and after commits, delivery acknowledgments, and lease expiration.
- [ ] Compare fraud decisions on identical inputs, including aggregate, null, numeric, and time-boundary cases.
- [ ] Test schema compatibility and migration ordering across all services.
- [ ] Benchmark ingestion, decision evaluation, case queues, and background processing at representative scale.
- [ ] Record query plans, latency distributions, throughput, lock waits, CPU/I/O, and pool saturation.
- [ ] Test backup restoration, recovery procedures, and deployment rollback boundaries.

Exit gate: agreed correctness and operational targets pass with recorded evidence. Document gaps explicitly; a limited passing suite does not establish full readiness.

### Phase 6: Cutover rehearsal and production transition

Prefer a maintenance window when the business permits it:

1. Verify backups and the tested restore procedure.
2. Pause producers, writes, scheduled execution, and workers; finish or explicitly reconcile in-flight work.
3. Capture a consistent final source state and complete the transfer.
4. Reconcile records, constraints, tenant mappings, and pending jobs/outboxes.
5. Deploy all dependent services against MySQL together; verify read-only checks before opening writes.
6. Resume writes and workers in a controlled order, monitoring errors, duplicate processing, backlog, and latency.
7. Preserve PostgreSQL as a protected fallback for the agreed retention period.

If downtime requirements prohibit this approach, design and test change-data capture separately. Account for deletes, transaction ordering, dynamic DDL, replay checkpoints, and final convergence. Avoid ad hoc dual writes across engines.

Exit gate: cutover and failure rehearsal completed, reconciliation passes, and an operator runbook defines stop conditions and responsibilities.

### Phase 7: Stabilization and retirement

- [ ] Observe an agreed operating period with representative traffic and job schedules.
- [ ] Confirm MySQL backups and restoration remain functional after cutover.
- [ ] Remove obsolete PostgreSQL dependencies and deployment paths only after fallback obligations end.
- [ ] Reconcile service-local Compose files and the database-scale override with the approved architecture.
- [ ] Archive migration evidence, benchmarks, and operational decisions.

## Rollback contract

Before MySQL accepts writes, rollback can restore the original application deployment against the unchanged PostgreSQL source, subject to reconciled external effects.

After MySQL accepts writes, switching the connection string back would lose new state. A lossless rollback requires a tested reverse-transfer/replay mechanism, another write pause, reconciliation of new records and jobs, and handling of external deliveries that already occurred.

If reverse transfer is not implemented, state that limitation explicitly and define a forward-recovery strategy before opening MySQL writes. A PostgreSQL backup alone is not a lossless rollback plan.

## Effort and outstanding questions

No reliable schedule is committed. Estimate after the compatibility inventory and feasibility prototype. Include engineering, testing, operational rehearsal, and temporary dual-environment maintenance.

Questions to resolve before implementation:

- What business requirement makes MySQL preferable?
- Is this development-only data or a production migration?
- What data volume, tenant count, and peak concurrency must be supported?
- What downtime and recovery objectives are acceptable?
- Must local Keycloak move as part of the same cutover?
- Which queue replacement meets our transactional and recovery contracts?
- Is post-cutover reverse migration required, or is forward recovery acceptable?

## Documentation references

These references support the compatibility analysis; verify them against the exact selected release during implementation.

- [MySQL schema/database semantics](https://dev.mysql.com/doc/refman/8.4/en/create-database.html)
- [MySQL implicit commits](https://dev.mysql.com/doc/refman/8.4/en/implicit-commit.html)
- [InnoDB transaction isolation](https://dev.mysql.com/doc/refman/8.4/en/innodb-transaction-isolation-levels.html)
- [MySQL session locking functions](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html)
- [MySQL SELECT and locking clauses](https://dev.mysql.com/doc/refman/8.4/en/select.html)
- [MySQL JSON type and indexing considerations](https://dev.mysql.com/doc/refman/8.4/en/json.html)
- [River supported backends](https://github.com/riverqueue/river)
