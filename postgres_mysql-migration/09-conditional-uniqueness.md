# Slide 9: Conditional uniqueness: protecting business rules

[Reading guide](README.md) | [Previous: slide 8](08-partial-indexes.md) | [Next: slide 10](10-advisory-locks.md)

Companion to slide 9 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

A unique constraint prevents two records from using the same key. Conditional uniqueness applies that rule only to records satisfying a condition.

**What soft deletion means**

Soft deletion keeps a record for history but marks it deleted, for example with a `deleted_at` timestamp. The record remains in storage even though normal views hide it.

Our case-management migrations use partial unique indexes for active records, including tags.

**An illustrative tag example**

| Tenant | Name | State | Desired result |
|---|---|---|---|
| A | High Risk | Active | Allowed |
| A | High Risk | Active again | Reject duplicate |
| A | High Risk | Deleted history | May coexist |
| B | High Risk | Active | Allowed in another tenant |

Actual uniqueness must also include every scope column required by the existing rule, such as the tag target.

**How a MySQL replacement might work**

A generated column calculates a value from other columns. It could return the tag name when active and SQL NULL when deleted. A unique index would combine that value with tenant and scope columns.

MySQL unique indexes permit multiple NULL values. This can represent inactive records that should not compete for an active name. The precise expression and index need validation.

**Why application-only checks are insufficient**

Two requests could both check "does this name exist?" before either inserts. A database constraint must arbitrate the concurrent writes.

Restoring a deleted tag is also a uniqueness event. It must fail or follow an explicit product rule if another active tag now owns the name.

**How to explain this slide aloud**

"We must preserve uniqueness for active records without preventing historical records from remaining in the database."

Evidence: [case migration](../backend/case-manager-service/internal/migrations/metadata/000001_init.up.sql). Source: [MySQL unique indexes](https://dev.mysql.com/doc/refman/8.4/en/create-index.html).

---

[Reading guide](README.md) | [Previous: slide 8](08-partial-indexes.md) | [Next: slide 10](10-advisory-locks.md)
