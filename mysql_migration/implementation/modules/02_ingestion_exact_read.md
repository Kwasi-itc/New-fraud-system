# Staged exact record reads

Implemented 2026-10-29: IN-01.3.013a-e. Production adoption remains 013f.

## Contract

`internal/store/mysql/record_read.go: ReadExactRecord` accepts an already
owner-authorized tenant/model, publication UUID, field projection, object ID,
result limits and caller-owned READ COMMITTED transaction. It does not authorize,
open/commit a transaction, cache metadata, or activate a runtime adapter.

`NewRecordProjection` validates tenant/table/publication and complete mapped field
identity under the existing shared publication barrier. The conservative
active/writable/open requirement remains; read access to closed or read-only
models requires an explicitly designed policy. The transaction must remain open
through collection. The projection retains the validated physical object column.

One SELECT resolves `data_model_find_record_object_key_v1(tenant, table, bytes)`
and selects by `record_object_key_id` plus full-byte `CAST(object_column AS BINARY)`
equality, with LIMIT 1. Caller values are parameters. Logical field names never
become SQL identifiers. This uses the existing unique record-key index; no scale
or optimizer performance claim is made. Representative EXPLAIN/cost measurements
remain pending. The key routine must be deployed with its reviewed grants.

Key and row selection share a statement snapshot, avoiding an inter-statement
key/row race without mutation-only absent-key locks. The reader performs no
record, dictionary, audit or outbox mutation. Concurrent ingestion may legitimately
change subsequent reads under READ COMMITTED; cross-request snapshot stability is
not promised. Concurrent reader/writer integration acceptance remains IN-01.4.

IDs must be nonempty valid UTF-8 and at most 1 MiB. Case, trailing spaces, embedded
NUL and Unicode normalization forms remain distinct. Tenant/table scope derives
from the validated model and is supplied to the key routine.

`RecordProjection.Bounded` supplies SQL string transfer guards; `Collect` enforces
row/page/field bounds, decodes typed projected fields, closes rows, and rejects
partial results. PageBytes includes the conservative one-element array budget,
even though this function returns a map. These are bounded value/output policies,
not process memory or HTTP envelope guarantees. Limit validation occurs before the
record SELECT, after bounded metadata resolution.

No row returns `sql.ErrNoRows`. Query, publication, decoding, missing routine,
permission and cancellation failures propagate as errors, never absence/success.
PostgreSQL GetRecord currently propagates pgx.ErrNoRows; production adoption must
preserve the service/HTTP missing-record contract with explicit error mapping.
This staged API returns only requested logical fields; it is not yet a drop-in
replacement for PostgreSQL SELECT *.

## Verification

`record_read_test.go` has five race-enabled tests: invalid IDs; seven distinct
byte identities plus missing record; tenant/table/version rejection; exact/over
field and row bounds; canceled context and missing routine propagation. Each SQL
test uses generated disposable fixtures. Existing projection/bounds regression
tests also cover mapping gates, barrier lifetime and connection cleanup.

Evidence: [new tests](../evidence/2026-10-29_ingestion-exact-read.log) and
[regression tests](../evidence/2026-10-29_ingestion-exact-read-regression.log).
`go vet ./...` passed. Restricted production role execution, deployment review,
service/HTTP adoption and representative concurrent/scale plans remain open.
Runtime remains PostgreSQL/River. No application database or dependency changed.
