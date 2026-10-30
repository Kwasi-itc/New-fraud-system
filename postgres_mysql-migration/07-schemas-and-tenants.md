# Slide 7: Schemas and tenant organization

[Reading guide](README.md) | [Previous: slide 6](06-compatibility-gaps.md) | [Next: slide 8](08-partial-indexes.md)

Companion to slide 7 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

PostgreSQL has a separate schema layer inside a database. MySQL uses the words schema and database for the same thing. This is one of the architectural differences that directly affects our current layout.

**Before and an illustrative candidate**

```text
Current PostgreSQL:
fraud database
  case_manager schema -> cases table
  tenant_example schema -> transactions table

Possible MySQL naming:
fraud database
  case_cases table
  t_example_transactions table
```

The MySQL names are examples, not an approved naming contract.

**Why not create a database for every schema?**

That would change the current requirement to keep one application database. Although the engines organize databases differently, we should not let a mechanical translation silently decide a new architecture.

We must explicitly decide how service and tenant objects map into the shared target database.

**What a naming registry does**

A registry records the mapping between a logical object and its physical table name. It needs deterministic names: the same object must resolve to the same physical table.

It must also handle collisions, length limits, renames, and safe identifier quoting. A user-supplied model name should never be inserted directly into executable SQL.

**Naming is not authorization**

A table prefix describes organization. It does not prove a caller belongs to that tenant. Permission checks, trusted identity, tenant-scoped queries, and relationships must still enforce isolation.

**How to explain this slide aloud**

"PostgreSQL gives us folders inside one database. MySQL has a different naming hierarchy, so we must deliberately map our service and tenant tables without weakening isolation."

Source: [MySQL schema/database naming](https://dev.mysql.com/doc/refman/8.4/en/create-database.html).

---

[Reading guide](README.md) | [Previous: slide 6](06-compatibility-gaps.md) | [Next: slide 8](08-partial-indexes.md)
