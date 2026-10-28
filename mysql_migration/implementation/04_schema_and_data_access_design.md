# Schema and data access design

Status: proposed. The authoritative input is the [schema object index](../14_schema_object_index.md), [full SQL catalog](../12_schema_source_catalog.md) and [usage inventory](../02_postgresql_usage_inventory.md). This design does not replace those column-by-column definitions with an invented simplified model. Task SC-01 must produce a reviewed source-to-target manifest for every existing column, constraint, index and routine before schema implementation is accepted.

## Object and type mapping contract

| Existing object/type | Proposed representation | Required validation |
|---|---|---|
| core metadata tables | Descriptive owner prefixes in the same DB; data-model uses `data_model_tenants`, `data_model_tables`, `data_model_fields`, etc. | Manifest assigns exactly one owner; cross-owner dependencies explicit; other service implementations remain separate work |
| tenant schemas/tables | Registry-backed `tenant_<tenant label>_<table label>_<full table UUID>`; labels fixed at registration | Tenant/table authorization before resolving names; full UUID identity and immutable labels; no fallback to guessed schema |
| UUID | BINARY(16), standard network byte order, no swap | Go codec vectors, FK byte equality, cursor order and UUID text round trip |
| Match ID/entity-derived text | Full text or exact-key reference | Never run UUID conversion on `EntityID + '-' + screeningUUID` |
| TIMESTAMPTZ | DATETIME(6) UTC with explicit UTC serialization | Range profiling, microsecond rounding policy, equal-time cursor tests |
| valid_until infinity | NULL means open-ended, with explicit predicate translation | Audit all reads, export/API representation; negative infinity requires separate treatment if found |
| PostgreSQL INTEGER/BIGINT | Signed INT/BIGINT | Preserve overflow behavior; do not widen public semantics silently |
| DOUBLE PRECISION | DOUBLE | NaN/infinity/out-of-range scan; differential threshold and aggregate tests |
| BYTEA | BLOB family sized by existing bounds; fixed BINARY for validated fixed digests | Byte-exact hashes, upload limits, packet/memory limits |
| INET | Address family + VARBINARY(16) + prefix length | Preserve host bits and mask semantics; IPv4/IPv6 formatting and equality corpus |
| JSONB | JSON | SQL NULL vs JSON null, scalar types, precision and duplicate-key policy |
| Identity role text[] | Membership-role child relation | Tenant/user FK, uniqueness of role; transactional version invalidation |
| Ordered UUID/text arrays | Child rows `(owner_id, ordinal, value)` where relational; JSON only for opaque payloads | Preserve order, duplicates and nullability where meaningful |
| TEXT names/keys | Profile and choose domain-specific collation plus full key strategy below | No arbitrary truncation or globally case-insensitive default |

Role normalization removes the source array cardinality constraint from the membership row. Preserve the nonempty allowed-role invariant through the complete restricted identity mutation contract, with atomic child replacement and tests denying direct deletion of the last role. Do not accept normalization until that enforcement is proved; a validated JSON representation is an alternative if it offers a simpler database-enforced contract.

Application SQL did not establish a DECIMAL/NUMERIC domain. Do not convert all floats to DECIMAL as an incidental change: numerical behavior affects rule outcomes. A future monetary-domain redesign would require its own contract. PostgreSQL timestamps may exceed MySQL's representable range; quarantine and resolve them explicitly rather than coercing zero dates. Historical row IDs and API UUID strings remain unchanged. UUIDv7 is not introduced during migration merely for locality.

The [InnoDB limits](https://dev.mysql.com/doc/refman/8.4/en/innodb-limits.html) make key-byte and row-size accounting mandatory. Default 16 KiB pages with DYNAMIC row format are the initial candidate. Include charset bytes, generated columns, number of key parts, and secondary-index overhead in every manifest entry. Increasing VARCHAR lengths arbitrarily is not a key strategy.

## Exact uniqueness, including long text

Use ordinary full unique keys when an existing validated domain bound fits the byte budget. Preserve nullable uniqueness intentionally. Conditional uniqueness uses nullable generated keys, such as `active_name_key = CASE WHEN deleted_at IS NULL THEN normalized_name ELSE NULL END`; all participating scope columns must be non-null or deliberately handled. For case screening links, preserve the two current domains: one uniqueness rule for a concrete match, another for a null match. Do not map NULL to a magic identifier that may be valid data.

For currently unbounded identifiers or exclusion tuples that cannot fit, recommend a **collision-safe exact-key dictionary**, selectively, subject to POC-03. Do not use a prefix unique key or UNIQUE(SHA256(value)) as if it were exact equality. Dictionary identity consists of tenant, namespace and canonical length-delimited bytes; raw bytes are retained. A narrow hash-bucket row is locked, candidates within that bucket are compared by full bytes, and an existing key UUID is reused or a new one allocated. Different bytes with the same hash get different UUIDs. Equal values cannot get different UUIDs because allocation is serialized in the bucket. Consumer UNIQUE/FK constraints reference that key UUID with tenant scope.

Dictionary insertion must be owned by a fixed SQL routine or another database-enforced restricted mutation boundary; runtime callers cannot insert arbitrary key IDs or inconsistent raw/key pairs. For dynamic unique fields, design generated owner mutation routines/triggers or an enforced sidecar mapping before enabling that field type; a Go helper alone is not a complete integrity solution. Native bounded keys are preferable when the existing contract permits them. Tightening a public length limit is an explicit business compatibility decision, not a silent fallback.

Globally scoped external identities must use an explicitly global identity-key namespace/table, not a tenant key or a fabricated tenant sentinel; issuer/subject uniqueness remains global. Tenant-scoped domains retain tenant-composite references.

Canonicalization is domain-specific: opaque external subjects and object IDs are byte-exact unless current behavior proves otherwise; case-insensitive names require equivalent normalization. Encode tuple components with length prefixes and explicit NULL tags, never separator concatenation. Dictionary bucket hashes are lookup accelerators, not authorization identities. Test forced collisions, trailing spaces, Unicode, embedded separators, empty strings and same value across tenants. Bucket/guard cleanup is deferred until a safe reference/locking protocol exists; unbounded growth is a capacity cost to measure.

## Mutation and query APIs

Repository interfaces return domain outcomes: inserted, updated, unchanged, replay, conflict, not-found and stale-version. New UUIDs are allocated before INSERT; read generated defaults by primary key inside the same transaction when necessary. Replacing RETURNING with a later unprotected read is forbidden. For a conditional update, compare expected version and increment an explicit version counter so a matched transition changes the row. Keep existing public timestamp-based version tokens compatible until an API change is separately reviewed.

Ingestion uses the [keyed mutation protocol](07_transaction_and_recovery_protocols.md), preserving lookup-column targeting and alternate-unique failure. Safe single-key ON DUPLICATE KEY UPDATE may remain for isolated guard/queue records only after documenting every unique key. Do not use INSERT IGNORE, REPLACE or global foreign-key disabling to hide errors.

All values use placeholders. SQL operators, sort directions, tables and columns come from typed allowlists and authorized metadata; identifiers use one tested MySQL quoting implementation. In-list helpers handle empty lists explicitly and enforce bounded parameter counts. No generic regex replacement of `$1` or PostgreSQL casts across SQL text.

Keep tenant predicates in every shared-table read and mutation, including replay lookups. Parent authorization cannot justify an unscoped child query unless the transaction holds and validates the parent relationship and tests prove it cannot cross tenants. Prefer explicit composite predicates even then. Runtime dynamic reads must verify registry identity/version and only select allowed fields.

## Query patterns and indexes

| Operation | Candidate leading index keys / access strategy | Acceptance evidence |
|---|---|---|
| Decision/manual history | tenant, actor where relevant, filters, created_at DESC, id DESC | Full cursor predicate, equal timestamp coverage, no cross-actor history |
| Case queue | tenant, inbox, lifecycle/assignment filters, stable cursor | Index/covering projection selected by representative plans; no history hydration |
| Work ready/expired lease | queue,state,available_at,id and queue,state,lease_until,id | Separate queries, bounded batches, starvation and contention tests |
| Source projection | next_attempt_at,source identity; source lock then queue lock | Oldest due work progresses under hot-source contention |
| Object lookup | tenant/table scope + exact lookup key | Point lookup; alternate uniqueness enforced |
| Historical aggregate | object/customer/filter keys + time bound, based on actual AST query | Rows examined and plan measured for selective and hot keys |
| Case workload | tenant,inbox,assigned user,status | Count under narrow membership lock; no unrelated inbox serialization |

Do not translate every partial performance index into a boolean prefix mechanically. Choose filter/order columns from actual query distribution. Indexes and plans are hypotheses until measured. Avoid full JSON extraction for explorer: explicit metadata-approved columns, bounded row count, existing 128 KiB projection limit and 25+1 relationship pagination.

Ordered workflow JSON becomes bounded parent/child SQL reads within one snapshot, ordered by existing ordinal/id rules and assembled in Go. Fetch children in batches, not N+1. Retain 501-parent sentinel and reject beyond the existing 500 cap. JSON aggregate order is not assumed; [MySQL aggregate documentation](https://dev.mysql.com/doc/refman/8.4/en/aggregate-functions.html) motivates explicit assembly. Analytics uses conditional aggregates and separate grouped result queries within one snapshot, preserving permission filters, UTC boundaries and the existing 1001 sentinel.

## Migration manifest and integrity

SC-01 manifest columns: source owner/schema/table/column, target object/type/collation/default/nullability, transform/reverse transform, PK/FK/check/index mapping, current query consumers, data preflight result and verification ID. Copy every existing constraint from the catalog; mark any changed enforcement site with ADR and bypass tests. Parent key types, lengths and collations must match child references. FKs target explicit unique keys; repair existing orphan/cross-tenant data before loading, never silently discard it.

Use a new MySQL migration lineage per owner, coordinated by object dependencies. DDL phases record checksums and dirty/recovery state. Expand, backfill, validate and publish before contract; retain rollback-compatible columns until the recovery window closes. Migration down files are not a credible cross-engine data rollback. Exact rollback requirements are in [cutover](11_data_transfer_and_cutover.md).
