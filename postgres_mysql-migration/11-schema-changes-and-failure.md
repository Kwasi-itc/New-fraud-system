# Slide 11: Schema changes and partial failure

[Reading guide](README.md) | [Previous: slide 10](10-advisory-locks.md) | [Next: slide 12](12-deferred-foreign-keys.md)

Companion to slide 11 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

DDL means Data Definition Language: commands that create or alter database structures, such as CREATE TABLE and ALTER TABLE. It differs from inserting ordinary business records.

Our system uses DDL during deployments and when tenant models change.

**Atomic DDL versus transactional DDL**

Atomic DDL means one supported schema-change statement succeeds or fails as a unit. Transactional DDL can group supported schema changes with other work in a transaction that can be rolled back.

MySQL's atomic DDL does not make a multi-step migration one rollback unit. Its DDL implicitly commits.

**An illustrative interruption**

```text
1. Record that a new tenant field is being created.
2. Add its physical database column.
3. Mark the field ready for ingestion.
```

If the process disconnects after step 2, the column may exist while metadata still says the operation is unfinished.

Simply retrying ADD COLUMN may produce an error. Simply ignoring "already exists" can also be wrong: a column of that name might have an incompatible type.

**What reconciliation means**

Reconciliation compares intended state with actual database state and decides the next safe action. It should inspect the physical definition, recognize successful prior work, and detect conflicts rather than falsely report success.

An explicit state model could track requested, applying, ready, and failed operations. Those names are an illustrative design, not an implemented MySQL workflow.

**What to test**

Interrupt before, during, and after DDL. Include lost acknowledgements: the database may commit an operation even though the client never receives confirmation.

**How to explain this slide aloud**

"Schema changes can stop halfway through. We need recovery that understands what actually happened and safely completes the intended operation."

Evidence: [tenant DDL](../backend/data-model-service/internal/tenantdb/postgres/indexing.go). Source: [MySQL atomic DDL](https://dev.mysql.com/doc/refman/8.4/en/atomic-ddl.html).

---

[Reading guide](README.md) | [Previous: slide 10](10-advisory-locks.md) | [Next: slide 12](12-deferred-foreign-keys.md)
