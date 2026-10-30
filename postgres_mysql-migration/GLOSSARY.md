# Plain-language glossary

[Reading guide](README.md) | [Presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md)

These definitions explain the terms used in the slide companions. They do not establish that a feature is enabled in a running deployment.

| Term | Meaning in this guide | Related slide |
|---|---|---|
| Database engine | Software that stores data and executes database operations, such as PostgreSQL or MySQL | [1](01-what-are-we-deciding.md) |
| Database server | The running database software accepting connections; physical deployment may involve additional infrastructure | [3](03-current-architecture.md) |
| Logical database | A named database the application connects to, here `fraud` | [3](03-current-architecture.md) |
| Schema / namespace | In PostgreSQL, a named area organizing objects inside a database; in MySQL, schema means database | [7](07-schemas-and-tenants.md) |
| Table / row / column | A structured collection, one record in it, and one defined field of each record | [3](03-current-architecture.md) |
| Tenant | A customer or organization whose data and permissions must be kept correctly scoped | [7](07-schemas-and-tenants.md) |
| Service ownership | Responsibility for a set of data and its mutation rules; not automatically determined by schema name | [3](03-current-architecture.md) |
| Migration | A versioned structural change, or the larger project of transferring engines; context distinguishes them | [11](11-schema-changes-and-failure.md) |
| Transaction | A unit of database work committed together or rolled back | [5](05-what-mysql-supports.md) |
| Commit / rollback | Make a transaction's changes durable, or discard its uncommitted changes; deployment rollback is a separate concept | [28](28-rollback-and-recovery.md) |
| Atomic | Treated as one indivisible success/failure unit within a specified boundary | [18](18-atomic-enqueueing-and-outbox.md) |
| DDL | Structural commands such as CREATE TABLE and ALTER TABLE | [11](11-schema-changes-and-failure.md) |
| Index | A lookup structure used to find and order records efficiently | [8](08-partial-indexes.md) |
| Partial index | An index containing only rows satisfying a condition | [8](08-partial-indexes.md) |
| Unique constraint | A database rule preventing duplicate keys within its defined scope | [9](09-conditional-uniqueness.md) |
| Generated column | A column whose value is calculated from an expression | [9](09-conditional-uniqueness.md) |
| Foreign key | A rule requiring a reference to match a related record | [12](12-deferred-foreign-keys.md) |
| Deferred constraint | A constraint whose check can be postponed, here until transaction commit | [12](12-deferred-foreign-keys.md) |
| Advisory lock | Application-coordinated locking around a logical key | [10](10-advisory-locks.md) |
| Connection pool | Reusable database connections managed for application requests | [10](10-advisory-locks.md) |
| Concurrency | Operations overlapping in time | [20](20-concurrency-and-isolation.md) |
| Isolation level | Rules affecting what transactions observe and how concurrent work interacts | [20](20-concurrency-and-isolation.md) |
| Deadlock | A cycle in which transactions wait on locks held by one another | [20](20-concurrency-and-isolation.md) |
| JSONB | PostgreSQL's binary JSON representation and associated query interface | [13](13-jsonb-arrays-and-types.md) |
| SQL NULL | An absent or unknown SQL value; not the same as a missing JSON key | [13](13-jsonb-arrays-and-types.md) |
| Collation | Rules for comparing and sorting text | [15](15-text-comparison-and-keys.md) |
| Trigger | Database code automatically invoked by specified table events | [16](16-business-rules-and-authorization.md) |
| PL/pgSQL | PostgreSQL's procedural language used by the routines discussed here | [16](16-business-rules-and-authorization.md) |
| Invariant | A rule that must remain true, such as tenant-consistent relationships | [24](24-delivery-phases.md) |
| Worker / job | A process executing background work, and a recorded unit of that work | [17](17-river-job-processing.md) |
| River | The job-processing library currently integrated through a PostgreSQL driver | [17](17-river-job-processing.md) |
| Outbox | Database records of work/messages awaiting delivery, committed alongside related business changes | [18](18-atomic-enqueueing-and-outbox.md) |
| Idempotency | Repeating the same logical operation does not repeat its intended business effect | [18](18-atomic-enqueueing-and-outbox.md) |
| Lease / ownership token | Time-limited work ownership and an identifier used to reject stale owners | [18](18-atomic-enqueueing-and-outbox.md) |
| Keycloak | The identity provider in the local configuration, owning its own provider tables | [23](23-operations-and-keycloak.md) |
| Query plan | The database's chosen method for executing a query | [21](21-performance-measurement.md) |
| p95 / p99 latency | Times within which about 95% / 99% of observed requests finish | [21](21-performance-measurement.md) |
| Reconciliation | Comparing intended/source state with actual/target state and resolving or reporting differences | [26](26-data-conversion-and-reconciliation.md) |
| Cutover | The controlled switch to the target system as the active application database | [27](27-cutover-choices.md) |
| RPO / RTO | Acceptable data loss and acceptable recovery time objectives | [28](28-rollback-and-recovery.md) |
| Exit gate | Evidence required before progressing to the next delivery phase | [24](24-delivery-phases.md) |
| Parity | Equivalent required behavior even when implementation details differ | [1](01-what-are-we-deciding.md) |

[Return to the reading guide](README.md)
