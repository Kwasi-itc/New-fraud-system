# Slide 15: Text comparison and key design

[Reading guide](README.md) | [Previous: slide 14](14-timestamps-and-infinity.md) | [Next: slide 16](16-business-rules-and-authorization.md)

Companion to slide 15 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

A collation is a set of rules for comparing and sorting text. It determines, for example, whether uppercase and lowercase letters compare as equal.

The documented MySQL default nonbinary collation is case-insensitive and accent-insensitive. Our target configuration must choose rules deliberately rather than inherit defaults accidentally.

**An illustrative identifier problem**

An external system might consider `AccountA` and `accounta` distinct identifiers. A case-insensitive unique key could treat them as duplicates.

For human name searches, case-insensitive behavior may be exactly what we want. The right rule depends on the field. We should not infer PostgreSQL's deployed collation settings without inspection either.

**What must be checked**

Before loading data, compare existing values under the proposed target rules and detect collisions. Include case, accents, whitespace, and relevant Unicode behavior.

Current queries use PostgreSQL ILIKE, and a migration uses text_pattern_ops for prefix matching. Those choices need equivalent query and index behavior.

**What is a compound key?**

A compound key combines columns. Screening exclusions identify records using several values, including tenant and text identifiers. The combined size matters when choosing target column lengths and indexes.

Indexing only the first part of a long value can help certain searches, but a unique prefix index is not equivalent to uniqueness of the entire value. Two different values can share the same prefix.

UUIDs and binary evidence also need explicit encoding and round-trip tests.

**How to explain this slide aloud**

"Changing comparison rules can change which records are considered duplicates. We must preserve the meaning of identifiers and searches, not merely copy text."

Sources: [case sensitivity](https://dev.mysql.com/doc/refman/8.4/en/case-sensitivity.html), [index rules](https://dev.mysql.com/doc/refman/8.4/en/create-index.html).

---

[Reading guide](README.md) | [Previous: slide 14](14-timestamps-and-infinity.md) | [Next: slide 16](16-business-rules-and-authorization.md)
