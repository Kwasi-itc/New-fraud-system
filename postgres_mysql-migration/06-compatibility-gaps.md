# Slide 6: The compatibility gaps at a glance

[Reading guide](README.md) | [Previous: slide 5](05-what-mysql-supports.md) | [Next: slide 7](07-schemas-and-tenants.md)

Companion to slide 6 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

This slide is a map of the migration challenges. It groups dependencies that should not all be treated as simple syntax changes.

**Three kinds of gap**

| Kind | Example | Work needed |
|---|---|---|
| A native capability is absent | PostgreSQL partial indexes | Design a replacement |
| A similar capability has different semantics | Named locks and transaction-scoped locks | Preserve lifecycle and failure behavior |
| Our implementation uses a specific interface | River's configured PostgreSQL driver | Port or replace the integration |

**What 'semantics' means**

Semantics means what an operation actually does. Two lock functions can both be called "locks" but release at different times. Two schema-change systems can both execute ALTER TABLE but recover differently after a multi-step failure.

Syntax is how we write a command. Semantics is its observable effect.

**An example**

A PostgreSQL migration adds a column and an index inside a transaction. If the second operation fails, the first can be rolled back with that transaction.

A superficially similar MySQL migration can leave the first change committed. The replacement needs to recognize that state and continue or repair safely.

**How to use the table**

For each dependency, record the business requirement, target implementation, failure cases, and evidence of correctness. "The query runs" is only the first check.

Slides 7 through 20 explain the listed gaps in more detail. The table is a navigation aid, not a claim that every database dependency has already been audited.

**How to explain this slide aloud**

"Some changes need new syntax, some need new implementations, and some need a different design. We must identify which category applies before planning the work."

Sources: [DDL](https://dev.mysql.com/doc/refman/8.4/en/atomic-ddl.html), [named locks](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html).

---

[Reading guide](README.md) | [Previous: slide 5](05-what-mysql-supports.md) | [Next: slide 7](07-schemas-and-tenants.md)
