# Target architecture

Status: **proposed**, 2026-10-22. Binding constraints are the user's shared-database, ownership, security and atomicity requirements. Recommendations below are not accepted ADRs or configured infrastructure.

## Engine, topology and ownership

Recommend **MySQL 8.4 LTS, InnoDB for every application and work table**, strict SQL modes, UTC sessions, and a pinned, tested patch version. Community-compatible features are the baseline; select a commercial edition or managed service only for an identified support, audit, encryption or recovery requirement. MariaDB is not an interchangeable target. Official [InnoDB isolation documentation](https://dev.mysql.com/doc/refman/8.4/en/innodb-transaction-isolation-levels.html) is the engine contract; test the exact server and driver build.

Keep **one logical application database**. Replace PostgreSQL namespaces with an explicit physical-object registry and owner prefixes: `dm_`, `de_`, `ing_`, `scr_`, `cm_`, `id_`; owner work tables use the corresponding prefix. This separates the two owners currently sharing `core`. Qualify SQL with the configured, validated database identifier. Do not create a database per service or tenant. Existing reader/worker URLs designate connection roles; they must resolve to the same authoritative dataset.

Dynamic table physical names are `td_<table UUID as 32 lowercase hex digits>`; registry primary identity is `(tenant_id, table_id)` and physical name is unique. Verify imported table IDs are globally unique; allocate a separate physical UUID if they are not. Field identifiers may use `f_<field UUID hex>`, with reserved base columns unchanged. User-supplied logical names never become SQL fragments. The mapping is persistent and versioned, not recomputed inconsistently by each reader. See PG-03/05/32 and [schema design](04_schema_and_data_access_design.md).

One primary handles mutations and authorization-sensitive reads. Production must have a tested recovery/failover arrangement meeting agreed RPO/RTO, but the repository does not establish those targets. Evaluate a managed primary/standby deployment first if available; self-managed single-primary InnoDB Cluster is an alternative requiring operational expertise. Do not introduce multi-writer topology, sharding, replicas for current permissions, or partitioning by default. There is no measured justification yet. Backups plus a demonstrated restore are essential even with HA.

Keycloak is externally owned software with its own schema migrations and credentials. Its configured local deployment currently uses the shared PostgreSQL database. MySQL schema/database equivalence and provider-managed object names require a separate decision: determine whether provider objects can safely coexist in the same database with object-specific privileges; otherwise seek an explicit provider-storage exception before provisioning another database. Never silently violate the one-database requirement or prefix vendor tables without supported provider behavior. Remote OIDC providers are outside application database ownership.

## Connections, access and transaction contract

Use Go `database/sql` with a pinned `go-sql-driver/mysql` version. Keep domain services and ports independent of driver types; repository mutation contexts own `*sql.Tx` internally and expose typed operations including transactional enqueue. Do not add an ORM solely for this migration. Preserve raw, parameterized, reviewed SQL where it expresses owner contracts clearly.

Use typed driver configuration, verified TLS, UTC parsing plus UTC server session time zone, explicit timeouts and connection lifetime. Disable multi-statements and client-side parameter interpolation for runtime accounts. Dedicated migration execution parses/executes controlled statements; runtime flags are not relaxed to execute migration files. The [driver documentation](https://github.com/go-sql-driver/mysql) distinguishes matched-row configuration from changed-row reporting; repositories must return semantic outcomes rather than exporting raw affected-row counts.

Normal mutation transactions explicitly request READ COMMITTED, matching important current ingestion assumptions. Multi-query workflow snapshots, analytics and authorization preflight use a short read-only REPEATABLE READ transaction with a consistent snapshot. Locking permission checks use current primary reads inside the mutation transaction, not a previously cached snapshot. Do not imply evaluation and persistence share one snapshot: current decision AST evaluation occurs before its persistence transaction (PG-34).

Every transaction has a context deadline, bounded lock waits, rollback on all error paths and a cancellation-independent cleanup path. Deadlock or timeout handling rolls back the entire application transaction before any bounded retry. Unknown commit outcome is resolved through the stable operation key; it is not retried as a new operation. No HTTP/provider/email call occurs while database locks are held.

Database-wide connection budget: `sum(replicas * max_connections_per_process_pool) + migration + recovery + monitoring + operational_reserve <= tested_server_budget`. Include separate API, read and worker pools for all six services and Keycloak. Pool maxima do not establish useful concurrency: benchmark database CPU/I/O, lock waits and queue age before increasing workers.

## Schema and access boundaries

Use independent owner migration ledgers and immutable checksums in one coordinated dependency graph. Identity authorization contracts and parent objects precede consuming routines and FKs. Bootstrap cross-owner objects in multiple passes where dependencies cycle. Each DDL step has an inspectable precondition/postcondition and repair path; do not wrap MySQL DDL in a transaction and claim rollback. Runtime DDL is a separate publication protocol in [07](07_transaction_and_recovery_protocols.md).

Runtime accounts receive owned-object DML and named cross-owner read/execute grants. Separate deploy, runtime-DDL, grant-provisioning, backup and monitoring identities. Prefixes are naming conventions, not privilege boundaries: explicitly provision object grants and verify absence of database-wide grants. Owner mutation procedures are candidates where callers would otherwise need cross-owner writes or bypass capacity/review invariants. Such procedures require a reviewed minimal definer, fixed SQL, explicit tenant/actor validation and no internal commit. ADR-07 remains proposed until bypass tests pass.

Tenant scope comes from authenticated request context and trusted operation contracts. Retain tenant-composite FKs and add only justified missing relationships after auditing existing data. There is no assumed PostgreSQL RLS to preserve and no claim that table prefixes provide MySQL RLS. Broadly compromised application credentials remain a threat requiring least privilege, network controls and audit evidence.

## Operations and capacity

Essential before cutover: encrypted connections, secret rotation, protected backups and keys, binary-log recovery policy, restore rehearsal, schema drift checks, migration lock monitoring, job age/retry metrics, slow-query sampling with redaction, pool saturation metrics and failover tests. Log tenant-safe operation IDs, SQL fingerprints, durations, error classes and row counts; never raw credentials, login verifiers, identity tokens or entire fraud payloads.

Size storage for data, secondary indexes, redo/undo, binary logs, temporary query spill, schema-copy operations and recovery headroom. Dynamic table/index counts may stress metadata caches even with modest row counts. Measure table count and field count, not only transaction count. Optional query caches, replicas, partitioning and analytical stores need a separate measured requirement and consistency contract.

No performance, cost, availability or compatibility benefit has been demonstrated in this phase. Exact patch, service deployment, retention, RPO/RTO, pool budgets and SLOs are gate G0 decisions in [roadmap](10_implementation_roadmap.md).
