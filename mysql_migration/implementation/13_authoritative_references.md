# Authoritative reference register

Consulted 2026-10-22. Primary sources only. MySQL references are versioned to 8.4; project/provider pages can change and must be checked again for pinned releases. Repository source establishes what this system does; external documentation establishes engine mechanics. The proposed protocols/architecture are engineering recommendations derived from both, not vendor-certified implementations.

| ID | Primary source | Application to this design |
|---|---|---|
| REF-01 | [InnoDB transaction isolation](https://dev.mysql.com/doc/refman/8.4/en/innodb-transaction-isolation-levels.html) | Explicit mutation/snapshot isolation; ADR-01/06. Engine defaults are not the application contract. |
| REF-02 | [MySQL 8.4 manual](https://dev.mysql.com/doc/refman/8.4/en/) | Versioned target reference; exact patch/support policy remains G0. |
| REF-03 | [Statements causing implicit commits](https://dev.mysql.com/doc/refman/8.4/en/implicit-commit.html) | DDL cannot share the current PostgreSQL rollback contract; P-01. |
| REF-04 | [Atomic DDL](https://dev.mysql.com/doc/refman/8.4/en/atomic-ddl.html) | Atomic engine DDL is not a transaction combining application metadata and DDL. |
| REF-05 | [InnoDB locking reads](https://dev.mysql.com/doc/refman/8.4/en/innodb-locking-reads.html) | Locking and SKIP LOCKED work-claim semantics; no consistent report snapshot implied. |
| REF-06 | [INSERT ON DUPLICATE KEY UPDATE](https://dev.mysql.com/doc/refman/8.4/en/insert-on-duplicate.html) | Any-unique conflict and affected-row semantics differ from the targeted writer. |
| REF-07 | [InnoDB limits](https://dev.mysql.com/doc/refman/8.4/en/innodb-limits.html) | Index/row/column limits require actual schema/key-byte accounting. |
| REF-08 | [Locking functions](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html) | GET_LOCK is session-lived; use pinned DDL connection, not pooled business transaction lock. |
| REF-09 | [Binary vs binary collations](https://dev.mysql.com/doc/refman/8.4/en/charset-binary-collations.html) | Binary strings and text collations are distinct; verify padding/equality per domain. |
| REF-10 | [go-sql-driver/mysql](https://github.com/go-sql-driver/mysql) | Typed configuration, TLS, time parsing, context and clientFoundRows; pin dependency and test behavior. |
| REF-11 | [Stored-object security](https://dev.mysql.com/doc/refman/8.4/en/stored-objects-security.html) | Invoker/definer privileges need explicit review; not a blanket least-privilege guarantee. |
| REF-12 | [River upstream](https://github.com/riverqueue/river) | Upstream queue capabilities do not make the repository riverpgx adapter MySQL compatible. |
| REF-13 | [Foreign key constraints](https://dev.mysql.com/doc/refman/8.4/en/create-table-foreign-keys.html) | InnoDB immediate constraints require review/exclusion insertion order changes. |
| REF-14 | [Date/time types](https://dev.mysql.com/doc/refman/8.4/en/date-and-time-types.html) | Finite ranges and precision require explicit transforms and edge-value testing. |
| REF-15 | [CHECK constraints](https://dev.mysql.com/doc/refman/8.4/en/create-table-check-constraints.html) | Use enforced supported expressions; cross-row invariants need another enforcement boundary. |
| REF-16 | [Point-in-time recovery](https://dev.mysql.com/doc/refman/8.4/en/point-in-time-recovery.html) | Backup/log recovery mechanics; measured system RPO/RTO remain unverified. |
| REF-17 | [Aggregate functions](https://dev.mysql.com/doc/refman/8.4/en/aggregate-functions.html) | JSON aggregate ordering and type behavior require deliberate assembly/parity tests. |
| REF-18 | [Keycloak database configuration](https://www.keycloak.org/server/db) | Current guide lists MySQL8.4; independently verify pinned provider release and extensions. |
| REF-19 | [Keycloak import/export](https://www.keycloak.org/server/importExport) | Realm export omits some session/security state and requires consistency precautions; not a complete backup. |

## Existing repository decisions and evidence

- [Audit entry point](../README.md), [35 findings](../02_postgresql_usage_inventory.md), [119 SQL files](../12_schema_source_catalog.md), [schema object index](../14_schema_object_index.md), [1310 callsites](../16_database_callsite_index.md).
- [Historical system plan](../../POSTGRES_TO_MYSQL_MIGRATION_PLAN.md) and [historical data-model plan](../../DATA_MODEL_MYSQL_IMPLEMENTATION_PLAN.md) explicitly record the October14 implementation revert. Their former prototype results are not current implementation evidence. This design preserves the shared database and isolated-development sequencing; it does not reactivate reverted code.
- [Stress protocol](../../backend/stress-tests/STRESS_TEST_PROTOCOL.md) and [scale results analysis](../../backend/stress-tests/DATABASE_SCALE_RESULTS_ANALYSIS.md) inform measurement, with limitations recorded in 06.

No live database, installed provider internals, external deployment inventory or current benchmark was inspected. Official documentation alone cannot close data-distribution, privilege-enforcement, query-plan or recovery gates.
