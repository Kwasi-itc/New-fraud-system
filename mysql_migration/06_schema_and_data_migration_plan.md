# Future schema and persisted-data migration plan

Status: design only. No database was connected to or changed. No row counts, actual installed schema versions, storage sizes, tenant counts, backlog sizes or downtime estimates were measured. Historical values in earlier plans are not substituted for current observations.

## Prerequisites and data contract

M-01 must capture a dated read-only catalog of all schemas/tables/columns/defaults/indexes/checks/FKs/functions/triggers/views/sequences/roles and applied service/River/provider migrations, including dirty flags. Compare it with [12](12_schema_source_catalog.md) and [14](14_schema_object_index.md). Record manually created objects and runtime tenant tables; migration files alone cannot enumerate them. Measure row/byte growth per tenant/table, largest JSON/blob/text keys, timestamp ranges/infinity counts, orphan/cross-tenant references, uniqueness under candidate collations, and active/scheduled/leased jobs. Use bounded samples for distributions and complete partitioned checks for invariants; samples cannot prove absence of violations.

Freeze a versioned logical-to-physical mapping, field transformation specification and comparison policy before transfer. Every table/column in the source catalog needs one disposition: copied unchanged logically, transformed, recomputed with evidence, retained for rollback, or explicitly retired by a decision. Never silently omit outbox, audit, archived fields, pending invitations or queue state because it is not visible in the main UI.

| Source representation | Candidate target / transfer rule | Required reconciliation |
|---|---|---|
| UUID keys/references | One BINARY(16) encoding or explicitly collated CHAR(36); preserve value | Round-trip PK/FK equality and cursor ordering; never regenerate existing identities |
| Text IDs and compound TEXT keys | Approved exact-length/key design; no truncation/prefix uniqueness | Full-value collision checks including Unicode/case/trailing space and long candidate IDs |
| JSONB | Logical JSON; transform operators separately | Parse/compare logical trees, numbers, nulls and ordered arrays; preserve stored hashes independently |
| UUID/text arrays | Ordered child rows or JSON arrays | Count, order, duplicate/null semantics; role membership equality; parallel-array association |
| TIMESTAMPTZ | UTC DATETIME(6) candidate | Instant equality at defined precision, boundary/order checks, unsupported range rejection |
| Infinite valid_until | Explicit open-ended flag/nullable endpoint | Same logical validity and API output; no fabricated finite date |
| INTEGER/BIGINT/DOUBLE | Range-preserving INT/BIGINT/DOUBLE candidate | Range/overflow, non-finite policy, aggregate return type and decision outcome parity |
| INET | Agreed canonical family/address/prefix representation | IPv4/IPv6 round trip and accepted prefix semantics |
| BYTEA | Sized binary/blob representation | Byte length and cryptographic digest equality, finalized evidence retrieval |
| Partial/expression indexes | Approved generated-key/composite designs | Same uniqueness outcomes and representative query plans |
| Sequences/identity | Application IDs mostly UUID; handle River/provider separately | Enumerate actual sequence state before deciding applicability; no blanket reset |

## Target build and load ordering

1. Build empty isolated MySQL database with explicit engine, charset/collations, SQL mode, UTC, isolation and independent migration ledgers. Install schema and chosen owner contracts; do not execute PostgreSQL down/up scripts against it.
2. Load tenant and model definitions plus physical-name registry. Recreate dynamic physical tables from validated metadata/physical inventory, including archived columns that remain needed for history. Reconcile physical types/indexes before publishing ingestion readiness. Copy or regenerate contract versions under an explicit cache invalidation policy.
3. Load identity users/external mappings/memberships before sessions/invitations/delivery. Application identity depends on core tenants. Decide provider transfer independently through supported tooling.
4. Load decision configuration with reference cycles handled in two passes (scenario/iteration live pointer). Load decisions, rule evidence, tests/phantom results, execution state and authoring history in dependency order.
5. Load screening parents/configs/monitored objects, matches/comments/files, continuous-run origins and review records. Load exclusion events after review parents, then current exclusion state and applications. Verify tenant/operation/source relationships without disabling normal integrity guarantees.
6. Load case inboxes/memberships and parent cases, then links/tags/files/events/reports/receipts/routes/uploads. Resolve inbox self-references and any nullable cyclic relationship in a documented second pass. Cross-owner source rows must exist before validating links.
7. Reconcile projections against source versions. Copy durable business work/outboxes and translate queue intent through the selected queue contract, never reinterpret River private rows blindly. Respect terminal states, idempotency identities and lease ownership. Do not deliver external notifications during comparison loads.
8. Create remaining performance indexes where justified by load strategy; install/verify all constraints and owner contracts before allowing writes. Recompute statistics and capture target plans. Mark ledger states only after schema/data verification; do not forge PostgreSQL version numbers as MySQL migration success.

## Transfer mechanics and cutover choices

Prefer a rehearsed maintenance-window transfer if measured size/downtime permits: fence new writes, quiesce workers and external deliveries, wait for active transactions, capture a consistent source snapshot plus queue checkpoints, load with bounded resumable chunks ordered by stable keys, reconcile, switch all dependent services together, then resume workers with known ownership. Keep manifests/checkpoints outside business tables with source snapshot identity, chunk boundaries, row counts and checksums. Record failed rows explicitly and stop on incompatible data.

If downtime is insufficient, evaluate snapshot plus CDC with ordering and schema-change handling. CDC must account for cross-table transactions, deletes, updates-in-place, runtime DDL, provider state, queue leases and outbox duplication. Independently writing both engines from requests is not atomic; it is not an approved shortcut. Shadow reads must suppress external side effects and compare normalized responses. Migration of only data-model's live database while consumers still read PostgreSQL would violate current contracts.

## Acceptance and rollback

Compare per-table/per-tenant counts and stable-key chunk digests, all PK/FK/check/unique invariants, reference mappings, JSON/array values, blob hashes, timestamps, decision/rule outputs, session permissions and pending/terminal work. Check source and target from the same logical watermark. Explain every discrepancy; an aggregate row count match is insufficient.

Before MySQL accepts writes, rollback can route back to the fenced, retained PostgreSQL snapshot after proving no target-only side effects occurred. After target writes, rollback needs a rehearsed reverse delta/replay or a forward-repair decision; switching the URL back would lose data. Preserve source backups, binaries/configuration, mapping versions and delivery checkpoints for the agreed retention period. Test restore and commit-uncertainty recovery. PostgreSQL down migrations and the existing consolidation tool are not cross-engine rollback strategies. RPO/RTO, abort thresholds and who makes the cutover decision must be fixed before M-15.
