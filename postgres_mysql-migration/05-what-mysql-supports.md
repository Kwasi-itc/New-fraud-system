# Slide 5: What MySQL already supports

[Reading guide](README.md) | [Previous: slide 4](04-application-scope.md) | [Next: slide 6](06-compatibility-gaps.md)

Companion to slide 5 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

This slide prevents an incorrect impression that moving to MySQL means losing transactions, queues, JSON, or relational data. Many foundations are available in both engines.

**The main capabilities**

- **InnoDB:** the MySQL storage engine used as the baseline here; it supplies transactional storage and foreign keys.
- **Transaction:** a group of data changes committed together or rolled back.
- **Foreign key:** a rule requiring a reference to identify an existing related record.
- **Row lock:** coordination that prevents conflicting changes to a record while a transaction works.
- **JSON:** structured document data stored in a column.
- **Trigger:** database code automatically run for specified table events.
- **Stored routine:** logic executed inside the database.

**What SKIP LOCKED means**

Imagine two workers selecting pending jobs. Worker A locks one job. With `SKIP LOCKED`, worker B can skip that locked row and claim another available job rather than wait for A.

MySQL supports this pattern. That does not automatically make our River integration work with MySQL: the library driver, SQL, scheduling, and transaction integration still need attention.

**Capability is different from compatibility**

Both databases can have triggers, but PostgreSQL trigger code is not valid MySQL trigger code. Both can query JSON, but expressions and index choices differ.

The question is therefore not only "Does MySQL have feature X?" It is also "Does our use of X retain the same behavior after conversion?"

**How to explain this slide aloud**

"MySQL has the basic tools this product needs. Our work is to preserve the way those tools enforce our business rules."

Sources: [locking reads](https://dev.mysql.com/doc/refman/8.4/en/select.html), [JSON](https://dev.mysql.com/doc/refman/8.4/en/json.html), [isolation](https://dev.mysql.com/doc/refman/8.4/en/innodb-transaction-isolation-levels.html).

---

[Reading guide](README.md) | [Previous: slide 4](04-application-scope.md) | [Next: slide 6](06-compatibility-gaps.md)
