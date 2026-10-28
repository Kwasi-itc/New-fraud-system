# Ingestion contract construction and version selection

`BuildIngestion` constructs the migration-000005 payload from a complete table,
field and enum snapshot. `PrepareIngestion` compares that payload with an existing
publication and selects its opaque version. Neither function accesses a database
or publishes anything. PostgreSQL triggers remain the runtime authority.

The eventual owner publication adapter must:

1. Authorize the tenant and acquire the table-scoped publication lock. Read all
   metadata and the previous publication in that same transaction. Include
   archived fields. A missing publication is distinct from a failed read.
2. Verify the expected operation/version, physical schema and grants before
   calling the planner. Keep DDL outside this metadata transaction, following
   the durable intent protocol. Do not hold database locks across network calls.
3. Call `PrepareIngestion` using a fresh random UUID generator such as
   `uuid.NewRandom`. Never derive tokens from payload hashes or reuse an earlier
   token when a payload reverts. The helper rejects the immediately previous
   token and zero; historical non-reuse relies on fresh random generation.
4. Persist changed payload/version with metadata, display cleanup and required
   delivery state atomically. Preserve the stored token when `changed` is false.
   Commit before reporting publication success. A prepared value is not proof
   of persistence; ambiguous commit needs reconciliation, not blind replay.

Comparison ignores JSON object-key order, whitespace and equivalent escaping.
Array order, string contents, JSON types, missing/extra keys and null versus empty
remain significant. Numeric enum values are strings in this contract. This
comparison is deliberately not a general-purpose PostgreSQL JSONB comparator.
Malformed stored JSON/UTF-8 and inconsistent tenant/table/version identity fail
explicitly. Input and stored payload bytes are not mutated or returned by alias.

Unit tests cover version selection and error behavior. PostgreSQL parity tests
in `internal/store/postgres/contract_parity_test.go` compare payloads and token
change decisions against real migration-000005 triggers. These tests do not
establish MySQL transaction safety: locked snapshot loading, target persistence,
concurrency, grants and recovery tests remain required before runtime adoption.
