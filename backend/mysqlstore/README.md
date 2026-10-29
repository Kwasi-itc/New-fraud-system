# MySQL repository foundation

DB-01 implementation, not a runtime cutover. Data-model's staged MySQL adapters
import this module. The configured application still uses PostgreSQL/River.

This module provides typed connection configuration, bounded pools, verified TLS
by default, UTC/strict session settings, READ COMMITTED mutation transactions,
read-only REPEATABLE READ snapshots, error categories, a binary UUID codec and
UTC DATETIME(6) binding/scanning that rejects implicit precision loss.
It does not implement authorization or other owners' domain repositories. The
`migrations` and `workers` packages supply shared installation/execution mechanics;
they do not enable a service or replace its domain transitions automatically.

Repositories must parameterize values and resolve authorized physical metadata
before quoting identifiers. Transaction callbacks must not execute DDL, change
session settings, commit/rollback themselves, or call external services. Snapshot
consistency starts at its first ordinary SELECT, not at function entry.

CommitError means the operation outcome needs reconciliation using its stable
operation key. Do not blindly retry it. Deadlock/timeout categories also do not
authorize automatic retries: domain idempotency and the whole transaction's
rollback must be established first. Never log raw errors or configuration with
credentials/customer values into public responses.

`RollbackError` means rollback cleanup was not confirmed. It is joined with the
original callback error, so `errors.Is`/`errors.As` retain both causes. `Classify`
returns `Other` for this combination even if the callback error was a deadlock
or lock timeout. This does not mean COMMIT was sent, and it must not be treated
as ordinary retryable failure solely from the callback's error code. This wrapper
covers rollback errors returned by the driver; cancellation cleanup already
owned by database/sql may instead return ErrTxDone without a cleanup result.

Run from the repository root:

```powershell
docker compose -p migration-foundation-test -f backend/mysqlstore/compose.test.yaml up -d --wait mysql
docker compose -p migration-foundation-test -f backend/mysqlstore/compose.test.yaml run --rm test
docker compose -p migration-foundation-test -f backend/mysqlstore/compose.test.yaml down --volumes
```

The stack pins MySQL 8.4.8 and Go 1.25.8, uses tmpfs, publishes no ports, and does
not attach to application networks or volumes. Its disposable test password is
not a production credential. Tests create unique fixture tables without dropping
preexisting tables. The Compose gate fails if integration configuration is absent;
a plain local `go test` without that configuration explicitly skips database tests.

The fixture generates a short-lived private CA/server certificate in an isolated
volume. Repeated dependency starts validate and reuse it; they do not rotate it
under a running server. Expired or incomplete certificates fail explicitly:
recreate the disposable stack and its volumes. No certificate or key enters Git.

TLS tests require encryption and verify trusted connections, incorrect hostnames
and untrusted CAs. Transaction tests also use plaintext inside the isolated network
so a test-only protocol wrapper can discard the real COMMIT success packet.
That test reconciles the persisted operation on a separate connection and proves
the callback was not retried. Real deadlock and lock-timeout tests verify rollback
of prior writes; the latter also verifies subsequent pool use.
The rollback acknowledgement fault test consumes a real server ROLLBACK OK
packet before disconnecting the client. It verifies the typed cleanup failure,
original callback cause, no automatic retry, absent rolled-back writes and a
successful later transaction on a replacement connection. The callback timeout
in that test is injected; actual lock timeouts are tested separately.
Pool tests reserve both connections in a two-connection pool, wait until sixteen
mutations are queued for acquisition, cancel them and verify no callback ran.
They check bounded connections, wait counters, recovery and no outstanding leases.
Session tests verify UTC, strict modes and default mutation settings after both
successful and failed snapshots on the same physical connection, then after idle
connection replacement. These small fixtures do not measure production capacity.

DateTime rejects non-microsecond inputs rather than silently changing equality or
expiry semantics. Callers must choose any truncation explicitly. This is a
non-null codec. NullDateTime and NullUUID bind SQL NULL when Valid is false;
when true they retain the underlying strict codec rules. An all-zero UUID with
Valid=true remains a non-null binary value. Scanning NULL or an invalid value
clears prior nullable state, preventing stale data when destinations are reused.
Always handle Scan errors; cleared state is not a successful NULL conversion.
Owner repositories must still decide which columns permit NULL and map these
storage values to their domain types.

`EncodeInet(netip.Prefix)` and `InetColumns.Prefix()` preserve address family,
host bits and prefix length using three columns: family, VARBINARY(16) address,
and prefix bits. IPv4 uses four bytes; IPv6 uses sixteen, including IPv4-mapped
IPv6. Neither direction masks host bits or unmaps an address. This is a non-null
storage codec, not an API parser or SQL ordering/containment implementation.
Repositories must explicitly handle NULL and bind all three columns. Migrations
must enforce family/byte-length/prefix consistency; the real-engine test includes
an enforced CHECK and verifies direct invalid inserts fail. Source ingestion
currently accepts bare addresses only; this codec does not broaden that API.
Persisted PostgreSQL prefixes still need preservation during data transfer.

`NullInet` handles optional IP values across the same three columns. `Values()`
returns three bind parameters: all NULL when Valid is false, otherwise a validated
family/address/prefix tuple. Scan the three SQL columns into temporary values,
check the row Scan error, then call `ScanColumns`. It clears previous state on
NULL or validation failure and rejects all six partially-null combinations.
Handle its error; cleared state does not make invalid storage a valid NULL.
Zero IPv4/IPv6 addresses with Valid=true remain present values.

Nullable owner schemas must explicitly enforce either all three columns NULL or
all three NOT NULL with valid family/length/prefix ranges. The test fixture
checks each partial-null combination through direct SQL, as well as invalid
non-null tuples. This is verified constraint guidance, not an applied application
migration. Owner repository adoption and API/query parity still remain pending.

This representation follows [PostgreSQL inet semantics](https://www.postgresql.org/docs/current/datatype-net-types.html).
MySQL's [INET6_ATON](https://dev.mysql.com/doc/refman/8.4/en/miscellaneous-functions.html#function_inet6-aton)
supports address bytes but rejects trailing masks, so it alone cannot retain a
prefix. Full PostgreSQL differential, query and API-format parity remains pending.

Shared scalar/text codecs and optional local observations are implemented below;
owner-specific mappings, remaining service-port conversion, deployment metric
wiring and representative load acceptance remain outstanding. Aurora/failover
testing is excluded from the current work. A passed foundation suite is not
production readiness or proof of migration parity.

Driver configuration follows the pinned
[go-sql-driver documentation](https://github.com/go-sql-driver/mysql/tree/v1.9.3).

## Pinned DDL lock lifecycle

`WithDDLLock` acquires `GET_LOCK(name, 0)` on the same pinned connection
passed to its callback. Contention returns `ErrDDLLockBusy` without invoking
the callback; there is no automatic retry. Use trusted, stable, application-scoped
lock names matching `[a-z][a-z0-9_]{0,63}` and a caller deadline. Use a separately
budgeted DDL pool against the same logical database. All cooperating executors
must derive the same name for the same physical resource.

The callback must use that connection synchronously, close result sets, and must
not close/retain the connection or manipulate named locks. Cleanup attempts an
explicit release with an independent five-second deadline, then discards the
physical connection on every exit, including success, error and panic. This
conservative isolation costs a new connection per operation; production cost has
not been benchmarked. `DDLLockCleanupError` preserves the cause and classifies
as `Other`, preventing an ambiguous cleanup from appearing as a plain deadlock.

This helper is not a transaction or schema publication protocol. DDL may already
have persisted when the callback fails. Reconcile physical shape and operation
intent before any retry. Server-wide named locks do not fence an old primary
after failover. Operation ledgers, publication barriers, recovery and service
adoption remain pending.

Real-engine tests cover same-name contention across implicit DDL commits,
independent lock names, session replacement, callback errors, panic, cancellation,
interrupted queries and unconfirmed release. They verify persisted DDL and eventual
lock availability rather than assuming cancellation undoes DDL.
The lifecycle follows [MySQL named-lock semantics](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html)
and [database/sql connection disposal](https://pkg.go.dev/database/sql#Conn.Raw).

## JSON storage values

`JSON` validates document syntax and UTF-8, binds as text, and scans a copied
buffer without decoding numbers through float64. SQL NULL is rejected by this
non-null codec. `NullJSON` uses Valid=false for SQL NULL; Valid=true with the
literal `null` is a present JSON document. Scans clear prior state on NULL or
error. Always handle Scan errors; clearing is not successful conversion.

This is a storage boundary, not domain validation or a byte-exact payload
archive. Duplicate keys are not rewritten by Go. MySQL normalizes JSON, including
last-key-wins duplicate handling; whitespace and object order are not stable
archive representations. See the [MySQL JSON reference](https://dev.mysql.com/doc/refman/8.4/en/json.html).
Numeric range/precision, Unicode escape handling and document-depth limits still
need owner-specific source/target profiling; valid JSON syntax does not guarantee
server acceptance or lossless representation of every PostgreSQL JSONB value.
The server's errors must propagate. Payload limits, JSON-path SQL, canonical
hashing, authorization and service adoption remain owner responsibilities.

## Session integrity defaults

Every new physical connection explicitly enables autocommit, foreign_key_checks
and unique_checks, and selects completion_type=0 (NO_CHAIN). Explicit Mutation
and Snapshot transactions still own their commit/rollback boundaries. The session
settings avoid inheriting relaxed integrity checks or transaction-chaining defaults
from the server; see [MySQL system variables](https://dev.mysql.com/doc/refman/8.4/en/server-system-variables.html).

Initialization is not a per-checkout reset or a security boundary against arbitrary
SQL. Repository callbacks must not alter session variables. These settings do not
retroactively validate previously loaded data or replace tenant authorization,
composite constraints and least-privilege grants. They do not modify server globals.
The isolated relationship fixture verifies composite tenant FKs, secondary unique
keys and rollback after FK failure; production owner schemas remain unconverted.

### Metadata-lock recovery fixture

`ddl_metadata_lock_test.go` holds a reader transaction open while ALTER TABLE
waits for its metadata lock. It observes the wait through PROCESSLIST, checks
that the named DDL lock still excludes a second executor, and exercises server
lock timeout and client cancellation separately. The fixture sets a two-second
session lock_wait_timeout on its disposable DDL connection. This is a test
setting, not a production timeout selected by the helper.

Recovery retains the blocking reader until named-lock release confirms the old
executor no longer owns the resource, then checks physical shape before an
explicit new ALTER. Cancellation alone is not proof that server DDL has stopped.
Choosing production metadata-lock limits and implementing durable operation/
shape reconciliation remain part of P-01 and POC-04.

### Lost DDL acknowledgement fixture

`ddl_ack_fault_test.go` consumes the actual server success packet for ALTER TABLE
and disconnects before the driver receives it. Recovery reacquires the same named
lock on a healthy session and verifies column type, nullability and default,
then uses the already-applied change without repeating ADD COLUMN. A client error
is not proof that a DDL statement failed. This fixture covers one concrete column
change; it does not implement a reusable schema inspector, operation ledger,
metadata publication or recovery from all DDL shapes.

## Column metadata inspection

`InspectVisibleColumns` queries one validated schema/table with bound parameters
on a caller-supplied pinned connection. Results are ordered by ordinal position
and contain column type, nullability, default, charset/collation, EXTRA and the
generation expression. Failed/canceled reads return no partial metadata. Empty
results return ErrNoVisibleColumns, not a claim that a table is safe to create.

Authorize physical mappings first and retain the DDL lock through inspection and
any decision. Visibility depends on account grants and server settings; a partial
column grant may expose an incomplete list. Deployment must establish full owner
metadata visibility before publication. Indexes, foreign/check constraints,
engine/table settings and grants require separate inspection. Views and temporary
objects are not certified as base tables by this column-only API.

MySQL [COLUMNS metadata](https://dev.mysql.com/doc/refman/8.4/en/information-schema-columns-table.html)
does not distinguish DEFAULT NULL from no DEFAULT clause. Expression/default
formatting is server metadata, not a canonical source SQL representation. The
helper supplies evidence; owner recovery must compare complete approved
postconditions and reject drift before publishing.

## Index metadata inspection

`InspectVisibleIndexParts` reads one validated schema/table on the supplied pinned
connection, returning key parts ordered by index name and position. Each part
includes uniqueness, index method, optimizer visibility, column or expression,
ascending/descending direction and optional prefix length. Visibility in the API
name means metadata accessible to the database account; optimizer-invisible
indexes are included and marked Visible=false.

Empty output is valid metadata output, not proof of table existence, absence or
complete grants. Resolve table identity and metadata visibility separately. The
caller retains the DDL lock and performs owner-specific postcondition comparisons.
No automatic repair, schema publication or performance conclusion is produced.
Index cardinality estimates are deliberately excluded from structural evidence.
Prefix length must be interpreted with the column type (characters for nonbinary
strings, bytes for binary strings), and functional expression text is server
metadata rather than canonical source SQL. See the official
[STATISTICS reference](https://dev.mysql.com/doc/refman/8.4/en/information-schema-statistics-table.html).

## Foreign-key and check inspection

`InspectVisibleForeignKeyParts` returns ordered child/parent column mappings,
referenced schema/table/key position and update/delete/match rules.
`InspectVisibleChecks` returns the server's check expression and enforcement
state. Both use exact schema/table parameters on the supplied pinned connection,
return no partial result on error, and share the other inspectors' authorization,
DDL-lock and metadata-visibility requirements. Empty output is not proof of table
existence or sufficient privileges. Session foreign_key_checks and other runtime
settings are separate from constraint definitions.

These are inspection primitives, not migration acceptance or automated repair.
Check expression strings are not canonical application SQL. Restricted-account
visibility, table options, grants and complete expected-schema comparisons remain
pending. The fields follow MySQL's
[referential constraint metadata](https://dev.mysql.com/doc/refman/8.4/en/information-schema-referential-constraints-table.html)
and [check enforcement metadata](https://dev.mysql.com/doc/refman/8.4/en/information-schema-table-constraints-table.html).

## Scalar codecs and observations

`Bool`, `Int32`, `Float64` and `Text` preserve Boolean, signed 32-bit integer,
finite DOUBLE and UTF-8 text domains, with `Null*` variants for SQL NULL. Invalid
scans clear reused destinations; no invalid value becomes a successful no-match.
Boolean owner columns still need enforced 0/1 constraints. Text preserves case,
whitespace and Unicode representation, rejects NUL (as PostgreSQL text does),
and does not implement owner length limits or exact-key collation by itself.
JSON null remains distinct from SQL NULL through the existing JSON codecs.

`ObservePool` reports local database/sql gauges and cumulative wait/connection
closure counters without SQL, credentials or high-cardinality tenant labels.
Compare snapshots of the same pool/process, not unrelated service replicas.
`TransactionMetrics` optionally wraps Mutation/ReadSnapshot without changing
their cleanup, panic or retry behavior. Elapsed time includes acquisition and
cleanup; uncertain commit/rollback outcomes have a separate counter. Concurrent
multi-counter snapshots are approximate; totals reconcile after quiescence.
These helpers are not yet wired into every service's runtime metrics endpoint.

`ValidateConnectionBudget` sums explicitly listed replicas times finite pool
limits and reserves database headroom. It rejects missing/unbounded/duplicate
entries, over-budget settings and overflow. The caller must inventory every
API, read, worker, DDL/installer and monitoring pool. It cannot discover omitted
processes, reserve server connections or establish a performance SLO.

## Shared migration mechanics

`migrations.New(db, owner, plan)` binds a trusted reviewed plan to the selected
logical database and `<owner>_schema_migrations`. Its dedicated owner lock,
dirty/applied journal and before/after definitions recover ambiguous DDL without
blind replay. Status/Verify do not install absent schema. Fixed routines and
triggers retain their full SHOW CREATE context; installers supply independently
reviewed reference observations and owner-specific least-privilege verification.
There is no force-clean/down repair or arbitrary production-schema adoption.

`migrations.Bootstrap` validates a bounded dependency graph before writes, then
serializes coordinators for that database while applying and verifying passes
in dependency order. Dependencies are reverified before consumers. Split owner
tables, cross-owner routines and grants into explicit passes as needed. Passes
must share the logical database and supply both Apply and Verify. A dedicated
deployment lock uses one pool connection; callbacks need additional capacity.
Independent installers must obey the same deployment coordination protocol.
This is deployment infrastructure, not permission to run migrations at API boot.

Data-model's compatibility wrapper adopts this runner while preserving its
existing lock name, ledger definition, serialized steps and checksums. Existing
installation SQL/artifacts were not regenerated. Other owners still need their
own installation/upgrade plans, grants and compatibility acceptance tests.

## Shared worker mechanics

`workers.LeaseSupervisor`, `DeliveryPoller` and `RiverDeliveryRetryPolicy` are
the implementations previously private to data-model, now reused through its
stable owner wrapper. They own heartbeat/timeout supervision, fixed concurrency,
joined shutdown and transport error-count backoff, not business mutation or
acknowledgement. Durable claims, fence validation and local effects remain in
the owner store's transaction. Cancellation is not a physical DDL fence.

The executable fifteen-kind legacy catalog and strict identity decoder document
existing payloads and detect source-contract drift. The decoder accepts only
version-zero legacy identities; it does not infer tenants, error history,
schedules, operation receipts or migration eligibility. Jobs with only resource
IDs require an authoritative owner tenant lookup. Global case jobs are explicitly
global, never assigned a fake tenant. A catalog entry does not enable a handler.
Other owners' durable MySQL transports, all-kind fencing/effects, scheduled
occurrences, cancellation, operator recovery and runtime wiring remain open.

### Dependency-free worker preparation

`MaintenanceScheduler[Cursor]` is the data-model scheduler extracted without
changing its independent expiry/retention cursors. It bounds pages per cycle,
advances through locked prefixes and stops on partial/ambiguous errors.
Data-model adopts it through its existing owner API. `FairClaimer` rotates queue
priority without claim-ahead buffering or skipping failed/ambiguous claims.
`WithPollJitter` configures an immutable poller copy with explicit bounded jitter;
zero retains existing polling timing. No deployment concurrency is selected here.

`DeliveryMetrics` optionally instruments poll/heartbeat/supervised execution
without callbacks or retries. Its counters describe **host runs**, not durable
job completion/discard state; heartbeat shutdown cancellations are separate from
failures. The owner still persists its authoritative outcomes. Data-model's
staged metadata transaction manager also supports an immutable `WithMetrics`
configuration without weakening its in-transaction authorization.

Shared lifecycle/error-history policies are used by data-model finish/cancel/
rescue paths. They retain completed/cancelled work for 24 hours and discarded
work for seven days, and never infer error history from claim attempts.

`PlanOccurrences` performs bounded UTC-microsecond interval arithmetic with
explicit catch-up/coalesce/skip-late policy and a stable scheduled instant.
It is not a durable scheduler: an owner must lock its schedule and atomically
persist the unique occurrence, job and watermark. RunOnStart, singleton ownership
and each owner's chosen missed-run policy are not enabled by this helper.

`DecodeIdentity` additionally understands ingestion's explicit v1 retry payload;
the strict legacy decoder remains v0-only. `CheckLegacyExport` validates identity,
authoritative scope binding, supported timestamps/state and independently known
error history for all fifteen kinds. It rejects running work that needs drain
and returns copies of source buffers. It does not insert jobs, infer tenants,
translate domain states, synthesize receipts or certify external effects. Those
steps depend on each owner's approved target schema and recovery contract.
