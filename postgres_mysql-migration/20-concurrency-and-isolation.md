# Slide 20: Concurrency and transaction isolation

[Reading guide](README.md) | [Previous: slide 19](19-drivers-errors-and-tools.md) | [Next: slide 21](21-performance-measurement.md)

Companion to slide 20 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

Concurrency means operations overlap in time. Isolation controls what one transaction observes while other transactions are changing data.

**Two isolation concepts**

READ COMMITTED gives each ordinary consistent read a view of data committed by the time that statement begins. In MySQL InnoDB REPEATABLE READ, ordinary consistent reads generally reuse the transaction's established snapshot.

This simplified explanation concerns consistent reads. Locking reads and updates have additional rules, so it must not be applied indiscriminately to every statement.

The ingestion code explicitly relies on READ COMMITTED behavior. MySQL InnoDB defaults to REPEATABLE READ.

**An example to reason about**

Transaction A attempts to create a record while transaction B is creating the same identifier. If A discovers a conflict and then needs to read or update the winning record, visibility and lock behavior matter.

An implementation must handle the actual sequence correctly rather than assume that a test with one request proves it safe.

**Locks and deadlocks**

A lock can make another operation wait. A deadlock occurs when transactions wait on one another in a cycle.

Consistent ordering helps: if operations must lock both a user and membership, acquire them in the same order throughout the relevant flows.

MySQL range and gap-lock behavior also depends on isolation level, indexes, and query shape.

**Why broad locks are costly**

Locking one shared parent row can make unrelated work wait unnecessarily. Use the narrowest scope that enforces the rule and keep transactions short.

**How to explain this slide aloud**

"We must preserve behavior when requests overlap. That requires explicit isolation choices, careful lock ordering, and contention tests."

Source: [MySQL transaction isolation](https://dev.mysql.com/doc/refman/8.4/en/innodb-transaction-isolation-levels.html).

---

[Reading guide](README.md) | [Previous: slide 19](19-drivers-errors-and-tools.md) | [Next: slide 21](21-performance-measurement.md)
