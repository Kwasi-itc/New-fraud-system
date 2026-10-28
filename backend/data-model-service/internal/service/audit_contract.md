# Mutation history and schema revision integrity

Every existing service history write goes through `recordSchemaChange`. JSON
encoding and repository failures return errors; no empty JSON fallback is used.
Every existing `recordTenantSchemaMigration` caller checks its error inside the
owner transaction. Missing repositories/ID generators fail explicitly.

This preserves the existing choice of which mutations create assembled-model
schema revisions. Table updates, navigation changes and index requests are not
silently assigned new revision events as part of this fix. Ingestion publication
tokens and display versions remain separate contracts.

The PostgreSQL rollback matrix injects an error **after** the underlying history
or revision INSERT succeeds. This detects ignored callback errors which a failed
SQL statement alone could mask by poisoning a PostgreSQL transaction. It checks
all 20 service history sites and each of the 16 existing revision paths. Coverage
includes inline enum creation, unique-index changes, table/field deletion,
caption/navigation queue work and tenant provisioning. Committed metadata,
publication JSON/version, display options, physical columns/indexes/schemas and
River rows must match the pre-call snapshot after each injected failure.

Successful lifecycle, import/export, authorization and existing queue tests still
run in the full module suite. Unit stubs explicitly supply a revision repository
where a successful mutation requires one; production no-op repositories were not
introduced.

MySQL DDL does not share PostgreSQL's rollback boundary. This change prepares
owner error handling; it does not authorize putting MySQL DDL in a metadata
transaction. P-01 must persist durable intent, verify physical effects and publish
metadata/history/readiness atomically. Until that implementation and all runtime
consumers pass their gates, services continue using PostgreSQL.
