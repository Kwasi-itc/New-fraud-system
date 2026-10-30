# Staged typed record decoding

Tasks IN-01.3.012a-e, completed 2026-10-29 at decoder component scope.
Source: [record_decode.go](../../../backend/ingestion-service/internal/store/mysql/record_decode.go),
[tests](../../../backend/ingestion-service/internal/store/mysql/record_decode_test.go).
This is not the MySQL record-read endpoint or a claim of PostgreSQL JSON parity.

## Input and ownership

DecodeRecordValue accepts an ingestion.FieldSchema, ordered database/sql scan
values and a per-string byte limit of 1..16 MiB. Callers must obtain the schema
and physical projection from the authorized publication mapping. The function
performs no SQL, tenant authorization, field lookup or transaction management;
passing a FieldSchema does not authorize access. Archived and unsupported types
are rejected even when the supplied value is null.

Scalar component order is [value]; IP order is [address, family, prefix], matching
encodeWriteValue. Wrong component counts and partially null IP storage fail.
All-null storage becomes nil only for nullable fields. Required SQL nulls fail.
Errors return nil and ErrRecordDecode without echoing retained contents. No
coercion from arbitrary maps, JSON objects or fmt.Sprint is allowed. Enum metadata
is not revalidated: this decoder translates retained typed storage, not a fresh
write against potentially changed enum policy.

## Five implemented contracts

| Subtask | Input and typed output | Rejection/boundary rules |
|---|---|---|
| 012a: bool / int | Integer driver values or bounded numeric bytes; bool returns bool, int returns int64. | Boolean permits only 0/1. int follows the existing signed MySQL INT writer contract, -2147483648..2147483647. Floating-point inputs, overflow and malformed numeric bytes fail. Unsigned input must fit signed int64 before type-specific validation. |
| 012b: float | float64 or bounded numeric bytes returns float64. | Finite binary64 only; no NaN/infinity or out-of-range parsing. Numeric byte input capped at 64 bytes. Does not claim decimal precision or normalize aggregate result types. |
| 012c: string / null | Valid UTF-8 string or byte slice becomes string; allowed SQL NULL becomes nil. | Byte limit checked before byte-to-string copying; invalid UTF-8, wrong type, invalid cap and excess bytes fail. Empty strings, embedded NUL, case, spaces and Unicode bytes are preserved. Byte-slice results do not alias driver buffers. |
| 012d: timestamp | Parsed time.Time becomes UTC time.Time through mysqlstore.DateTime.Scan. | Years 1000..9999 and microsecond precision; no rounding/truncation. Zero, out-of-range, submicrosecond, unparsed text and infinity values fail. parseTime-enabled driver/session configuration is required. |
| 012e: ip_address | Binary address plus integer family/prefix becomes netip.Prefix through mysqlstore.InetColumns.Prefix. | Family 4/6 and exact 4/16 byte lengths; prefix 0..32 or 0..128 as appropriate. Partial nulls, wrong lengths/families/ranges fail. Host bits, prefix length and IPv4-mapped IPv6 identity survive. Returned value does not alias address buffers. |

IP output deliberately remains a typed prefix. Formatting an address-only public
string versus a CIDR string is an endpoint compatibility decision, not performed
by this codec. The current writer accepts full host addresses; decoding preserves
valid historical network prefixes as well. Integer output is explicitly int64;
source pgx runtime representations are not assumed to match this internal type.

## Allocation and query limits

This function is a decoding boundary, not a transfer budget. Scan has already
received driver values. The future SELECT must use bounded projections/length
checks to prevent oversized values being transferred or allocated in the first
place, and the row/page layer must enforce a combined result budget. A small
per-field limit does not cap total row bytes across many fields. Those remain
IN-01.3.011/020/021. No SELECT *, lookup query, caching or physical identifier
construction was added with the decoder.

Numeric byte parsing is bounded to 20 bytes for integers and 64 for floats;
IP input is at most 16 bytes. Timestamp and IP validation reuse shared mysqlstore
codecs rather than duplicating range/address logic. No dependency was added.

## Verification and remaining gates

[Evidence](../evidence/2026-10-29_ingestion-record-decoder.log): module-wide go vet
and five race-enabled decoder tests passed, with no skipped cases.

Unit checks cover invalid types/arity, nullability for all six types, archived or
unsupported fields, integer bounds/overflow, finite float extremes, invalid UTF-8,
exact string cap and buffer ownership, timezone normalization and timestamp
precision/range, IP host bits/prefixes/mapped identity and inconsistent components.

The isolated MySQL round-trip test uses recordFixture's generated disposable
schema and the existing encodeWriteValue output. It writes actual TINYINT, INT,
DOUBLE, utf8mb4 LONGTEXT, DATETIME(6), VARBINARY and unsigned IP components, scans
through the real driver and decodes all six values. Cases include minimum/maximum
signed INT, smallest nonzero and largest finite float64, Unicode/NUL text,
1000/9999 timestamp boundaries, IPv4/IPv6/mapped IPv6 and all-null columns.
The fixture's physical types match existing writer tests; this is not full
production data-model installation or published-projection acceptance.

Mapped projection construction, current tenant authorization, publication locks
through query execution, exact record lookup, result budgets, restricted reader
grants and public DTO serialization remain separate unfinished tasks. PostgreSQL
codec/collation parity was not run or inferred. Runtime remains PostgreSQL/River;
no application database was changed and no read route invokes this decoder yet.
