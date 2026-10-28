# Index worker execution boundary

`ports.IndexJobExecutionStore` separates attempt/outcome metadata transactions
from physical DDL. The runner receives a committed claim, reads the owner tenant
and table, checks both identities and the recorded table name, performs inspection
and DDL without metadata row locks, then asks the store to complete that attempt.

The outcome store must update exactly the running row identified by tenant, job
ID and attempt count, then append the supplied schema-change event in the same
transaction. A stale or wrong-tenant attempt returns `ErrStaleIndexJobAttempt`
without history. A history failure rolls back the status update. No adapter may
return success after saving only one half. The attempt counter is monotonic;
operator retry must not reset it. Claiming a terminal applied/failed/cancelled job
returns it unchanged so a repeated queue delivery does not execute DDL again.

The runner returns outcome-persistence errors to the queue. DDL, inspection and
lookup failures preserve the original error alongside any persistence error;
lookup/inspection errors now use the configured retry limit rather than being
acknowledged immediately as terminal failures. A missing table identity or a
tenant/table mismatch is terminal only once its failed outcome is durable.
Missing dependencies and invalid claims fail explicitly.

An inspection must return a nonempty index name. After successful creation, the
runner inspects again and requires both existence and the same name before
recording an applied outcome. Missing or changed identity returns
`ErrManagedIndexUnverified`; inspection failures preserve their original error.
Both use the normal retry/exhaustion and atomic outcome-history path. If DDL
committed but inspection failed, a later attempt can recognize the existing
index without repeating creation. This is an existence/identity safeguard only:
the PostgreSQL adapter uses `to_regclass`, and full MySQL definition, grant and
readiness verification still requires the physical execution protocol below.

Worker timestamps are explicitly rounded to UTC microseconds before crossing
the port. At attempt exhaustion, inspection may still recognize completed DDL
and recover its applied outcome; new DDL is not executed past the limit. This
allows recovery when the last permitted DDL succeeded but saving its outcome
failed. Terminal redelivery bypasses inspection and DDL entirely.

The PostgreSQL implementation uses the existing transaction manager, a row lock
for claim and a conditional update for completion. It does not hold locks while
running physical DDL. The worker executable selects this adapter while runtime
still uses PostgreSQL/River. A failed/uncertain completion can be redelivered:
committed terminal outcomes are observed without another attempt; a rolled-back
outcome remains running and a new attempt inspects existing physical state.

## Staged MySQL adapter

`internal/store/mysql/index_job_execution.go` implements the same metadata port
against `data_model_index_jobs` and `data_model_schema_change_log` in one selected database.
Claim holds one job row lock through its size preflight, full read and attempt
increment. Completion uses a tenant/job/status/attempt conditional update and
inserts history before committing. Concurrent claims receive increasing attempt
numbers; only the latest running attempt may complete. SQL failure or cancellation
returns an error, and an uncertain commit is not reported as a successful claim.

The disposable fixture's `columns_json` stores the ordered job argument array;
this is a candidate representation for immutable queue payload names, not a
replacement for relational model-field ownership. It rejects JSON null and null
elements before incrementing an attempt. Optional table ID/error/timestamps use
explicit nullable codecs. Timestamps must already fit UTC DATETIME(6); the codec
does not silently truncate precision. Snapshot byte limits are explicit; they
are not production capacity recommendations.

These fixture tables do not constitute production migrations: tenant FKs,
conditional exact dedupe uniqueness, complete queue indexes, grants and durable
delivery still belong to the outstanding metadata/queue gates. No application
constructor selects this MySQL adapter yet.

## Migration gates still open

This is metadata-attempt fencing, not physical DDL fencing. It neither prevents
two executors from reaching DDL nor proves that an existing index has the right
definition. MySQL adoption still requires the pinned execution connection and
named lock, durable DDL intent, full shape verification, publication/readiness
barrier, stale-operation checks and recovery specified by P-01. Never use an
expired metadata lease to assume that a previous DDL statement stopped.

The MySQL durable transport, schedule/retry/retention semantics and River drain
are separate required work. No queue acknowledgement is atomic with a database
transaction merely because this interface exists. Lost acknowledgements must be
handled by idempotent redelivery; no worker is declared migrated by this change.

Legacy terminal jobs with missing history need inspection; terminal redelivery
does not synthesize historical events. Legacy pending reconciliation repair jobs
without River rows have an exact-job operator recovery command under DM-01.2f.
It refuses all retained delivery evidence and records recovery with delivery
atomically; it is not a general terminal-job retry or a production inspection.
Production load and failover acceptance remain open as well.

## Delivery host primitives

`LeaseSupervisor` and `DeliveryPoller` provide driver-free, staged lifecycle
supervision. A claimed task supplies heartbeat and execution callbacks bound to
its exact durable lease. Fixed worker slots do not prefetch. Explicit deadlines,
renewal failure cancellation and joined shutdown prevent releasing a worker slot
while its executor is still active. Claim/report errors stop the host; execution
errors must be reported before continued polling. Outcomes remain exclusively
owned by the execution store. Neither primitive acknowledges or retries a task.

These primitives are verified with deterministic lifecycle tests and a real
MySQL metadata-only execution fixture. They are not selected in cmd/worker.
The staged DeliveryMaintenance scheduler follows independent expiry/retention
cursors across bounded paced cycles and propagates partial/ambiguous failures.
Maintenance host wiring, source-compatible retry/error counts, operational metrics,
physical DDL fencing, resource-budget settings and full host integration remain
required. See the [delivery contract](../../../../mysql_migration/implementation/modules/01_data_model_delivery_contract.md).
