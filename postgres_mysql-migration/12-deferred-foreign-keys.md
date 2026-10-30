# Slide 12: Deferred foreign keys

[Reading guide](README.md) | [Previous: slide 11](11-schema-changes-and-failure.md) | [Next: slide 13](13-jsonb-arrays-and-types.md)

Companion to slide 12 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

A foreign key requires a referenced record to exist. For example, an audit event can reference the review operation that caused it.

PostgreSQL can defer selected checks until transaction commit. Our screening exclusion migration explicitly uses `DEFERRABLE INITIALLY DEFERRED`.

**What deferring the check allows**

Consider this simplified transaction:

```text
Begin
  Insert an audit event referencing review R.
  Insert review R.
Commit
```

If the relevant foreign key is deferred, the relationship can be valid at commit even though it was temporarily incomplete between statements.

This illustrates the feature; it is not a claim that every current review path uses this exact write order.

**What differs in MySQL InnoDB**

Foreign-key checks are immediate. An equivalent operation must write the parent before the child, or use a redesigned relationship that preserves integrity.

The final state being valid is insufficient if an intermediate statement violates a required constraint.

**Why disabling checks is not the replacement**

Turning checks off removes protection and can allow invalid relationships. The application needs a supported sequence of valid writes inside one transaction.

For our screening flow, the design must preserve tenant-consistent links among reviews, exclusion events, and exclusions, plus immutable audit history.

**A second syntax issue**

PostgreSQL migrations use inline REFERENCES clauses in some tables. MySQL needs explicit foreign-key definitions for actual enforcement in this context. A table being created successfully does not prove the intended relationship rule exists.

**How to explain this slide aloud**

"Our current database can wait until commit to validate some relationships. MySQL checks earlier, so we must make the write sequence compatible without weakening the rules."

Evidence: [exclusion migration](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql). Source: [foreign-key differences](https://dev.mysql.com/doc/refman/8.4/en/ansi-diff-foreign-keys.html).

---

[Reading guide](README.md) | [Previous: slide 11](11-schema-changes-and-failure.md) | [Next: slide 13](13-jsonb-arrays-and-types.md)
