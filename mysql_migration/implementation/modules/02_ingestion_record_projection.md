# Staged mapped record projections

Tasks IN-01.3.011a-e, 2026-10-29. Component implementation, not a public read API.
Source: [record_projection.go](../../../backend/ingestion-service/internal/store/mysql/record_projection.go)
and [tests](../../../backend/ingestion-service/internal/store/mysql/record_projection_test.go).

## Contract and ownership

NewRecordProjection takes an existing transaction, trusted tenant/model/table,
expected publication UUID and 1..100 exact logical field names. The caller must
already authorize the operation, use READ COMMITTED, and retain the transaction
through the physical query. A projection is neither authorization nor a reusable
cache token. Do not execute its SQL after releasing the acquiring transaction.

Mapping reuses NewRecordWriter and LockWriteSnapshot rather than a second registry
resolver. This deliberately inherits the conservative writable model, unarchived
table, open publication gate and expected-version checks. It validates the complete
field mapping, including unselected fields, before compiling the requested subset.
Stable field IDs, types, archive flags, required object_id/updated_at metadata,
component shapes and generated physical names must agree. The existing registry
shared lock prevents DDL preparation from closing the gate until transaction end.
No registry gate, grant or application data is changed by projection construction.

This reuse is an internal validation dependency, not a claim that all read access
should require write permission. A production read-specific authorization/gate
contract remains IN-01.3.002/003/008/009. Do not weaken authorization simply because
the builder accepts a model, and do not treat a writable flag as trusted identity.

## Five granular outcomes

| Item | Implemented behavior |
|---|---|
| 011a: request validation | Reject empty/oversized/duplicate requests, unknown names, archived fields and schema-name mismatches before registry access. No automatic physical/system-column projection. |
| 011b: mapping checks | Reuse complete tenant/table/publication and field mapping verification under the existing exact-table shared registry lock. Foreign tenant, stale version, closed gate, type mismatch and missing components fail. |
| 011c: compilation | Emit only quoted registry-generated physical columns and the qualified mapped table. Preserve requested order. Scalars select value; IP selects address, family, prefix. Logical names are never SQL aliases or expressions, even when they contain SQL-looking text. |
| 011d: decoding | Copy requested logical schema into the plan; require exact component count; call DecodeRecordValue for each field. Return one logical-name map only on complete success. Empty/uninitialized plans and malformed later fields return no partial map. |
| 011e: integration | Physical write/readback uses all six types and the real decoder. A concurrent publication-close attempt cannot cross the held transaction and succeeds after rollback releases the barrier. |

SQL() returns the column list and qualified table separately. It adds no WHERE,
ORDER BY, LIMIT or caller-selected SQL fragment. All future predicate values must
be bound parameters. Decode delegates to the existing strict scalar/IP decoder;
it is not enum revalidation or public DTO formatting. It preserves copied logical
names after the caller mutates its original name slice or model map.

## Limits and remaining integration

This selects explicit columns, but it does not bound LONGTEXT transfer. The future
query layer must supply before-Scan length guards, total row/page byte budgets,
limits/pagination and a parameter budget (IN-01.3.020/021). The decoder's string cap
alone cannot prevent prior driver allocation. No endpoint uses an unbounded
projection from this change because no new endpoint is wired.

No physical id, validity time, exact-key dictionary columns, or other unmodeled
system columns are exposed automatically. Source PostgreSQL SELECT * behavior is
therefore not silently claimed to be API-compatible. Public projection/serialization
contracts must decide system-field exposure explicitly. Source query filters,
aggregates, exact lookup, schema-transition handling and restricted reader grants
remain unfinished. The builder performs no network request and adds no dependency.

## Evidence

[Projection test log](../evidence/2026-10-29_ingestion-record-projection.log):
module-wide go vet and five race-enabled tests, including six mapping-failure
subcases, in disposable MySQL fixtures. Coverage includes request bounds, tenant
and version mismatch, closed publication, archived/mismatched/missing fields,
all-type readback, component order, detached names, partial-result rejection,
SQL-looking logical names and transaction lock lifetime. These fixtures install
actual registry DDL with minimal metadata and privileged test publication; they
are not independent production installation or authorization acceptance.

No performance improvement or full read-path readiness is claimed. Runtime stays
PostgreSQL/River; no application database migration or production switch occurred.
