# Slide 13: JSONB, arrays, and data representation

[Reading guide](README.md) | [Previous: slide 12](12-deferred-foreign-keys.md) | [Next: slide 14](14-timestamps-and-infinity.md)

Companion to slide 13 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

A relational table has defined columns. JSON lets one column hold a structured document, such as configuration with several named properties. PostgreSQL JSONB is its binary JSON representation with a particular set of functions and operators.

Our system stores configuration and response data in JSONB and uses SQL functions to build projections.

**Arrays are a separate concept**

PostgreSQL also has native SQL arrays, such as a list of allowed outcomes or identity roles. A SQL array is not the same type as a JSON array.

MySQL JSON can store an array, but existing SQL array expressions and Go scanning code do not automatically work with it.

**Choosing a replacement**

A JSON array may suit a small configuration value read as one document. A related table may suit values requiring individual queries or relational constraints. The choice depends on how the application uses the data.

For identity roles, preserve the authorization result, not just the visible list of strings.

**Three values that look similar**

- Missing property: a JSON document has no such key.
- JSON null: the key exists with a null value.
- SQL NULL: the database column or expression has no SQL value.

An update or filter can treat these differently. A migration must preserve the intended distinction.

**An example**

If a configuration omits a timeout, the application may choose a default. If null explicitly disables a value, converting omission into null could change behavior.

**How to explain this slide aloud**

"MySQL can store JSON, but data shape, query operators, null handling, and indexing must preserve the meaning of our documents and arrays."

Evidence: [decision schema](../backend/decision-engine-service/internal/migrations/metadata/000001_init.up.sql), [identity schema](../backend/auth-service/internal/platform/schema.sql). Source: [MySQL JSON](https://dev.mysql.com/doc/refman/8.4/en/json.html).

---

[Reading guide](README.md) | [Previous: slide 12](12-deferred-foreign-keys.md) | [Next: slide 14](14-timestamps-and-infinity.md)
