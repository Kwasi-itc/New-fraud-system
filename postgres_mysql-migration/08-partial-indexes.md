# Slide 8: Partial indexes: indexing only relevant records

[Reading guide](README.md) | [Previous: slide 7](07-schemas-and-tenants.md) | [Next: slide 9](09-conditional-uniqueness.md)

Companion to slide 8 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

An index is a lookup structure that helps the database find records without inspecting every row. A partial index includes only records satisfying a condition.

**An example**

Suppose, purely for illustration, a cases table has one million records and ten thousand are open and unassigned. The assignment worker only needs those ten thousand candidates.

A PostgreSQL partial index can contain just the relevant rows. The actual assignment migration uses conditions involving case status and assignment.

**Why this can help**

A smaller index can reduce the amount of index data examined for the intended query. However, usefulness depends on the query, data distribution, ordering, and execution plan. This review has not measured that performance.

Updating a case can add or remove its entry as it enters or leaves the indexed condition.

**What changes in MySQL**

MySQL 8.4 does not have PostgreSQL's index predicate syntax. Candidate alternatives include a composite index, which combines several columns, or a generated column representing a useful condition.

A composite index may contain all rows, even if the query only reads active ones. It must also support the query's ordering and limit efficiently.

**What to verify**

Compare the execution plan, rows examined, index size, and update cost. Test mostly closed histories, busy queues, and multiple workers. A small demonstration with ten rows tells us very little about production queue behavior.

**How to explain this slide aloud**

"We currently maintain lookup structures focused on the cases workers need. MySQL needs a different index design, and we must measure whether it handles the same workload efficiently."

Evidence: [assignment indexes](../backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql). Source: [MySQL indexes](https://dev.mysql.com/doc/refman/8.4/en/create-index.html).

---

[Reading guide](README.md) | [Previous: slide 7](07-schemas-and-tenants.md) | [Next: slide 9](09-conditional-uniqueness.md)
