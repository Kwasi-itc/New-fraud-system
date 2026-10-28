# Compatibility assessment

Reference baseline: **MySQL 8.4 with InnoDB**, not MariaDB. This is a candidate baseline, not an approved target version. Official references were checked on 2026-10-22. Proposed replacements below have not been executed. Repository evidence is indexed by PG IDs in [02](02_postgresql_usage_inventory.md); verification cases are in [08](08_testing_and_validation_strategy.md).

## C-01 Driver, parameters, transactions and error contracts

PG-01/02/28: pgx-specific pools, rows, CommandTag, Batch, Identifier, PgError and transactions occur beyond the storage directories. Replace with a selected MySQL driver and narrow application-owned transaction/query contracts; retain contexts, scanning, nullability, cleanup and metrics. `$n` placeholders become driver-compatible positional parameters; repeated `$1` uses require repeated bound arguments, not a textual replacement alone. Quote registered identifiers separately from values. Port pgx Batch into bounded multi-row inserts or prepared executions only after preserving error/rollback behavior.

Map no-row, duplicate key, FK/check violation, deadlock, lock timeout, cancellation and schema drift into current domain/HTTP outcomes. PostgreSQL 23505 is not a MySQL error number. Preserve assignment-capacity constraint identity separately from generic duplicate errors. Retry whole idempotent transactions for selected deadlock/serialization cases, with bounded backoff; a connection failure at COMMIT has uncertain outcome and requires lookup/reconciliation, not blind replay. Verify cancellation releases connections and locks even after the request context is canceled. M-04 owns these contracts.

## C-02 One database, namespaces and names

PG-03/32: MySQL SCHEMA is a DATABASE synonym, so mirroring every PostgreSQL namespace as a MySQL database would change the shared-database architecture. [MySQL CREATE DATABASE](https://dev.mysql.com/doc/refman/8.4/en/create-database.html).

Recommend a registry mapping logical owner/tenant/table/field IDs to bounded physical names in one database. Service prefixes and opaque tenant/table suffixes are proposals; shared tenant tables are a larger alternative because tenant models differ. Store collision-resistant names with explicit collision detection; do not trust truncated hashes alone. All schema manager, ingestion, decision, explorer, display and test code must consume the same mapping. Retain SQL qualification as `database.physical_table`. Backticks, name length and identifier case behavior require explicit handling. [Identifier rules](https://dev.mysql.com/doc/refman/8.4/en/identifiers.html). Prototype case-only names, reserved names, rename/archive and maximum-length names before freezing M-02.

## C-03 DDL, catalogs and migration recovery

PG-04/05/27: application metadata and physical DDL currently share a PostgreSQL transaction. MySQL DDL can implicitly commit; atomic DDL does not restore multi-statement application rollback. [Implicit commits](https://dev.mysql.com/doc/refman/8.4/en/implicit-commit.html).

Proposed lifecycle: reserve owner/table intent in a short transaction; execute DDL with explicit operation identity; introspect exact physical definition; publish metadata/contract version and job state only after verification; resume/reconcile interrupted intents. Define supported failure states for create, field add/archive/drop, unique/index changes and concurrent schema callers. Do not hold business row locks across lengthy DDL. Plan metadata-lock deadlines, online-DDL capability per operation and progress visibility. `to_regclass`/pg catalogs become target catalog queries; mere object existence is insufficient. Maintain new independent MySQL migration ledgers and forward repair steps for partially committed migrations. M-05/M-06 require disconnect/crash tests at every boundary.

## C-04 Upserts, RETURNING and affected rows

PG-08/09/16/28: MySQL ON DUPLICATE KEY UPDATE can react to any unique key; affected counts differ for inserted, changed and unchanged rows and client flags. It is not a target-specific ON CONFLICT translation. [MySQL upsert semantics](https://dev.mysql.com/doc/refman/8.4/en/insert-on-duplicate.html).

For ingestion, preserve the distinction between object_id collision (patch that record) and another unique field collision (reject). Prefer explicit insert and duplicate classification followed by a scoped locking/current read and conditional update; prove behavior under concurrent creation/deletion and same-value updates. Do not use REPLACE or broad IGNORE. For pre-generated UUID inserts, return the application ID; if defaults/triggers affect returned fields, read them on the same transaction/connection. MySQL 8.4 INSERT/UPDATE syntax does not provide the PostgreSQL RETURNING clause used here; avoid MariaDB-specific alternatives.

For exclusion toggles, `RowsAffected()!=1` currently means conflict. A MySQL port must return an explicit domain mutation outcome, including unchanged state, rather than reuse that integer test. Similar tests are needed for claim tokens, transfers and optimistic versions. Deduplication PKs remain necessary even with application lookup. M-07/M-09 cover these proofs.

## C-05 Isolation, row/advisory locks and savepoints

PG-11–14/17–19: choose isolation explicitly; InnoDB's default REPEATABLE READ differs from PostgreSQL's usual READ COMMITTED. Current pgx defaults do not prove deployed session settings. Prototype READ COMMITTED first for writer visibility, then verify snapshot requirements for analytics/permissions. [InnoDB isolation](https://dev.mysql.com/doc/refman/8.4/en/innodb-transaction-isolation-levels.html).

MySQL supports locking reads and SKIP LOCKED, but joined/subquery locking and scanned ranges need target plans and contention tests. Split CTE UPDATE RETURNING claim into select-lock + conditional update + read inside one short transaction; commit before HTTP. Preserve source→queue, identity→membership→session→domain and sorted multi-case lock order. Queue/assignment progress must remain possible for unrelated tenants. [Locking reads](https://dev.mysql.com/doc/refman/8.4/en/innodb-locking-reads.html).

Replace transaction advisory locks with narrow indexed lock/reservation rows or existing uniqueness when equivalent; bound lock-row growth and lifecycle. MySQL GET_LOCK is session-owned and not released by transaction commit, so it is not a direct replacement for pg_advisory_xact_lock. If used for maintenance, dedicate/pin a connection and prove cleanup. [Named locks](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html). Use savepoints for projection failures; prove error handling does not unintentionally commit partial work or discard retry state. Every lock conversion needs a two-session schedule test, not only a happy-path test.

## C-06 River and outbox

PG-10/14: installed code explicitly uses riverpgxv5. River's current upstream describes PostgreSQL/SQLite backing, not a drop-in MySQL backend. [River upstream](https://github.com/riverqueue/river). Pin the chosen replacement and assess its actual supported driver before implementation.

Options: maintained MySQL transactional queue; external broker plus MySQL outbox; custom durable queue with an explicitly owned reliability contract. Recommend an isolated queue decision/prototype before repository conversion. Retaining River on PostgreSQL would retain a PostgreSQL dependency and break current cross-engine EnqueueTx atomicity unless bridged by an outbox. Preserve scheduled availability, recurring uniqueness, retries/cancellation, queue separation, terminal failures, recovery, leases and stable delivery IDs. Map business work rather than copying River internal rows blindly. Document each job family and drain/replay policy. Network delivery is at-least-once and requires consumer dedupe; no exactly-once network guarantee is claimed.

## C-07 Indexes, uniqueness and unbounded text keys

PG-05/16/19/25/35: MySQL's index syntax offers functional/generated-column techniques, but not PostgreSQL partial index predicates. Conditional generated columns or a separately maintained active-key table are candidate replacements. MySQL TEXT indexes require length treatment; prefix uniqueness is not full-value uniqueness. [Index syntax and restrictions](https://dev.mysql.com/doc/refman/8.4/en/create-index.html).

For active inbox/tag names, candidate design is a nullable normalized active-name key with explicit collation plus tenant/target scope. For screening links, preserve BOTH uniqueness rules: one legacy null-match link and one per concrete match. For one pending invitation, exclude accepted/canceled rows from the active key. Ordinary non-unique partial indexes may become full composite indexes with state predicates, at greater storage/write cost; benchmark rather than claim equivalence.

Unbounded object IDs, issuer+subject, idempotency keys, candidate IDs and the exclusion compound text PK require an admissible-length decision based on real data. Choices: approved bounded exact strings; surrogate identity plus exact comparison/reservation; digest acceleration with collision verification. Never silently truncate or use a unique prefix/hash as an unchecked substitute. Test NULL, empty, case/accent, trailing spaces, long multibyte keys, soft-delete/reactivation and concurrency. This blocks final DDL until M-02/M-05.

## C-08 JSON and ordered projection

PG-06/07/20/34: JSONB maps to MySQL JSON for stored structure, but operators, nulls, whole-row conversion and index strategies need translation. Use JSON_EXTRACT/UNQUOTE/JSON_OBJECT or bounded application serialization, with explicit absent-vs-JSON-null-vs-SQL-null tests. Index known scalar paths with generated columns only where a measured query needs them. [MySQL JSON](https://dev.mysql.com/doc/refman/8.4/en/json.html).

Workflow conditions/actions and enum lists require stable ordering; MySQL JSON_ARRAYAGG order must not be assumed to follow an inner ORDER BY. Use an explicitly verified window/ordering design or batched ordered relational results assembled in application code, preserving the snapshot. [Aggregate functions](https://dev.mysql.com/doc/refman/8.4/en/aggregate-functions.html). Avoid N+1 loads. Preserve JSON numeric semantics and request hashes: PostgreSQL JSONB bytes are not a canonical cross-engine serialization. Compare logical documents and separately preserve stored request hashes/audit bytes where required.

## C-09 Functions, triggers, authorization and deferred integrity

PG-06/07/15–19/35: PL/pgSQL is not portable stored-routine code. For each routine in 14, choose an owner-managed application transaction contract or MySQL routine/trigger and retain tests for every write entry point, including CLI/maintenance. Moving enforcement to handlers alone misses other writers. Shared SQL contracts can remain same-database contracts after namespace mapping; avoid introducing HTTP solely because owners differ.

InnoDB checks FKs immediately; PostgreSQL deferred exclusion references need parent-first writes in the same transaction. Disabling foreign_key_checks is not a replacement. [Foreign key behavior](https://dev.mysql.com/doc/refman/8.4/en/create-table-foreign-keys.html). The proposed exclusion reorder must preserve immutable source validation and expected version before changing any state. Port assignment capacity with the same per-member locking authority across manual, worker, transfer and reopen paths. Preserve authorization rechecks and read/write revocation races, publication version rules, projection version monotonicity, review idempotency and audit/outbox atomicity. Routine syntax alone is not acceptance.

## C-10 Arrays, UUIDs, binary data and ordinary constraints

PG-21/22/24/35: normalize roles into tenant/user/role rows with uniqueness/allowed-role constraints; preserve lock/version semantics. Ordered UUID/text arrays can use child rows with ordinal or JSON arrays. Child tables improve constrained queries but add joins/migration volume; JSON simplifies round-trip storage but needs explicit validation/indexing. ANY-bound lists become bounded IN parameters or staged joins; parallel unnest arrays become validated multi-row inserts with correspondence retained.

Use one UUID byte-order contract for true UUID PK/FKs/cursors; BINARY(16) is compact, CHAR(36) eases inspection but widens indexes. Keep API UUID strings unchanged. Do not swap time fields opportunistically for random UUIDs. [UUID conversion functions](https://dev.mysql.com/doc/refman/8.4/en/miscellaneous-functions.html). BYTEA maps to sized VARBINARY/BLOB classes after measuring evidence/upload limits; compare hashes and exact bytes. Ordinary CHECK/FK/UNIQUE syntax is partly portable but expression functions and supporting index lengths require validation. Preserve non-UUID textual user/resource/provider IDs as text.

## C-11 Time, numeric and IP behavior

PG-23/24: recommend UTC DATETIME(6) for application instants after range/precision profiling; initialize sessions explicitly and round-trip Go timestamps consistently. MySQL TIMESTAMP has a narrower range and session time-zone conversion behavior; neither representation accepts PostgreSQL infinity. [Temporal types](https://dev.mysql.com/doc/refman/8.4/en/datetime.html). Use an explicit open-ended flag or nullable endpoint for valid_until and rewrite every predicate/serializer. Preserve statement-vs-wall-clock semantics for expiration and never use a stale transaction timestamp for long-running session checks.

Dynamic INTEGER is signed 32-bit; DOUBLE PRECISION and screening scores are approximate. No DECIMAL/NUMERIC declaration was found in the inspected application schema files: introducing decimal financial semantics is a separate decision, not a claimed current dependency. Test integer overflow, large SUM/COUNT, empty AVG/SUM, driver result types, rounding and non-finite data. INET needs canonical address storage (e.g. family plus binary address, retaining prefix if supported input uses it), not arbitrary text if ordering/equality differ. Enumerate actual accepted IP formats before selecting representation.

## C-12 Collation and ordering

PG-25/33: specify identity/opaque key comparisons independently from user-facing search. Default case-insensitive MySQL collation could merge distinct object IDs or issuer subjects; default binary comparison could change intended lower(name) uniqueness. Validate Unicode folding, accents, normalization, trailing spaces and wildcard escaping against current outputs. ILIKE becomes LIKE with deliberate collation or a normalized searchable field. `split_part` actor extraction can become a generated creator key or explicit persisted actor column; backfill only verified manual origins. UUID/timestamp cursor order and NULL ordering must remain stable across conversion. Query plans must cover prefix and substring cases separately.

## C-13 Analytical/query rewrites

PG-26/34: FILTER becomes conditional aggregate, GROUPING SETS becomes verified grouping/UNION or equivalent rollup, PostgreSQL MATERIALIZED hints need target plan treatment, UPDATE FROM/DELETE USING require MySQL grammar. CTEs/lateral or correlated reads may have equivalents but not identical optimizer behavior. Replace `to_jsonb(row)` with explicitly bounded projections. Preserve one consistent snapshot when splitting analytics/workflow reads, access predicates, group-limit errors, time bucketing, total counts and deterministic pagination. Benchmark many-tenant, high-history and selective/nonselective workloads; do not replace SQL aggregation with loading all history into memory.

## C-14 Provider identity

PG-29: current Keycloak docs support MySQL, but compatibility must be checked for the repository's pinned 26.8.0 image and chosen target. [Keycloak database guide](https://www.keycloak.org/server/db). Realm export alone is not proof of complete credential/session migration. Decide whether local-idp is in production scope, use a provider-supported transfer strategy, retain issuer/subject stability and test login/logout/invitation/session behavior. Do not hand-translate provider tables. Any separate provider database exception requires an architecture decision rather than an incidental namespace conversion.

## C-15 Infrastructure, testing and observability

PG-02/04/27/30/31: change all API/worker/migrator/read URLs, TLS settings, probes, database images/volumes, local setup, SQL fixtures and operational commands. Port PostgreSQL catalog/EXPLAIN/queue observers to equivalent MySQL metrics without pretending names map one-to-one. Replace pg_dump/restore with validated cross-engine extract/transform/load plus native MySQL backup/restore. Audit root/scale overrides together, including auth and local provider. Require real-engine suites in isolated targets; report skipped tests. The exact hosting/version, resource budget, baseline latency and restore objectives remain M-01 decisions.
