# Data-model MySQL delivery persistence and worker boundary

Status: staged implementation, not runtime activation. This covers DM-01.2j.1/.2
within the data-model module. P-03, production grants/migrations, worker polling,
operator tooling, River drain and P-01 physical DDL readiness remain required.

## Source behavior and target representation

The pinned source is River v0.31.0, `internal/riverjobs/{enqueuer,index_job,index_job_worker}.go`.
The kind is `index_job_execute`, payload v1 contains `index_job_id`, default queue
is `index_jobs`, execution timeout is 30 seconds, and configured attempts default
to five. The enqueuer uses ByArgs without ByQueue or an explicit ByState override.
Inspection of the pinned module's `insert_opts.go` and `rivertype/river_type.go`
confirms deduplication includes available, pending, running, retryable, scheduled
and completed states. Cancelled/discarded states are excluded. Completed reservations
must survive until retention cleanup; active-only deduplication would change behavior.
Pinned default retention is 24 hours completed/cancelled and seven days discarded.

`data_model_index_deliveries` stores typed tenant/job references, queue, fixed kind/payload
version, availability, attempt limit/count, random lease token, expiry, heartbeat
version, finalization/retention times and a redacted error code. No arbitrary JSON
payload or user SQL is executed. `data_model_index_delivery_reservations` reserves one
tenant/job identity across queues, including retained completed deliveries.
The job UUID is globally unique; the composite tenant relationship prevents an
incorrect tenant from reserving or operating on another tenant's work.

Queue names are explicitly validated ASCII identifiers of 1..128 bytes. Lease
duration must be an explicit microsecond value in 1ms..1h; no deployment default
is inferred from tests. Queue configuration compatibility, retry delay/backoff and
jitter, heartbeat cadence, connection budget and worker timeouts need runtime
configuration/acceptance. Caller-selected retry delays are bounded to 24 hours.

## Transactions, locking and recovery

`IndexDeliveryStore.EnqueueTx` requires the owner's existing SQL transaction.
It locks the exact tenant/job row before checking or inserting the reservation.
Existing delivery identity and schedule are retained on a duplicate. Domain
metadata, history and enqueue can commit or roll back together. No transaction
or operation is automatically retried.

Claims select one due available row ordered by availability/UUID with FOR UPDATE
SKIP LOCKED, increment its delivery attempt and assign a fresh random token and
database-clock lease expiry, then commit before work starts. Claims and heartbeat
lock delivery rows only. Completion/cancellation/reaping/cleanup use the order
job -> delivery -> reservation; they do not hold those locks across DDL or network
work. All selected-row writes carry explicit tenant predicates.

`Finish` validates tenant/job/delivery/token/attempt and unexpired database time
before a short local SQL callback. It checks expiry again when finalizing, so
expired completion rolls back the callback's effects. Callback failure also rolls
back job/outcome and delivery changes. A completed reservation remains until cleanup;
cancelled/discarded reservations are released in the terminal transaction. Repeated
cancellation does not rerun effects. Completed/discarded cancellation is rejected.

Maintenance uses distinct ready, expiry and retention indexes. `MaintainPage`
returns a queue/stream/time/UUID cursor even when every candidate was locked.
Pollers must follow cursors before wrapping to nil to avoid a permanently hot
prefix starving later candidates. Each candidate uses a short transaction and
skips busy source/queue rows. The one-page convenience methods are useful for
bounded operator actions; repeated polling must use the cursor API.

Expired deliveries are made available with the old token cleared, or discarded
when the persisted delivery attempt limit is reached. Cleanup only removes terminal
rows whose database retention deadline passed. This is delivery state, not proof
that a physical index was applied or that the domain job is terminal. Queue and
domain attempt counters remain distinct, as in the source River/runner boundary;
dead-letter tooling must expose pending/running domain jobs whose delivery exhausted.
No success response or physical readiness is inferred from transport completion.

Commit ambiguity propagates as `mysqlstore.CommitError`, with no successful claim
returned after uncertain claim commit. Enqueue reconciliation reuses the retained
reservation. Unacknowledged claims recover through expiry/reaping. An unacknowledged
completion leaves atomic domain/delivery effects if the server committed; subsequent
stale completion cannot rerun those effects. This is not owner request-receipt
integration or a general command retry policy.

## Lease-bound worker execution

`LeasedIndexJobExecutionStore` implements the existing driver-free
`ports.IndexJobExecutionStore` for one committed delivery lease. Starting an attempt
locks the exact job, verifies its delivery lease and bounds the job read, then
increments the domain attempt and rechecks lease validity before commit. Terminal
redelivery returns the unchanged domain job and acknowledges its transport only
after a fresh terminal-state check under the same lock order.

Completion validates both the current job attempt and the delivery lease. Domain
status, immutable audit event and delivery retry/completion share one transaction.
The standalone and leased stores use the same `completeIndexAttemptTx` helper,
preventing different implementations of outcome/history atomicity.

`IndexJobOutcome.RetryDelivery` explicitly carries execution-error transport
behavior when the domain attempt limit has produced a failed job. Pending domain
outcomes always retry; a failed job requests transport retry only for an execution
error, not a direct terminal validation result. Applied outcomes cannot request
retry. The runner continues to return the original execution error. PostgreSQL's
River wrapper still observes that returned error; the MySQL leased adapter uses
the explicit flag in its atomic completion transaction.

The retained delivery's max_attempts is authoritative even if a restarted worker
has different configuration. An exhausted retry discards transport, releases its
reservation and records delivery_attempts_exhausted without rewriting the domain
outcome or its audit. A failed domain outcome with remaining delivery attempts is
retried; terminal redelivery acknowledges without incrementing the domain attempt
or appending a second history event. A pending domain job whose transport exhausted
remains pending and requires operator inspection/redrive. This intentional source
distinction also applies to uncertain outcomes recovered through expiry.

This adapter is not selected by `cmd/worker`. Physical named-lock execution,
DDL intent/shape verification, grant readiness, operator redrive and River drain
remain open. A metadata lease cannot fence a
previously issued MySQL DDL statement; P-01 is still mandatory before activation.
No worker is declared migrated by this component.

## Staged polling and lease supervision

`internal/worker/lease_supervisor.go` verifies/renews a committed lease before
execution, then renews it periodically with a separate per-call timeout. Work has
an overall deadline. Any heartbeat error cancels work and remains observable;
work returning nil after cancellation is not success. The supervisor waits for
both executor and renewer to exit, including after shutdown and executor panic.
Callbacks must honor cancellation. Context cancellation cannot prove server-side
DDL stopped, so this is not a replacement for the physical execution lock.

Lease, heartbeat interval/timeout and work timeout are explicit; heartbeat timeout
must be no longer than the interval, and the interval at most one third of the
lease. The supplied lease duration must match the queue adapter. No deployment
durations are inferred from test values. A stale heartbeat racing a committed
completion conservatively reports an error; recovery inspects persisted state.
Cancelling an in-flight heartbeat after normal completion does not itself fail
the completed task. Other heartbeat errors are retained.

`internal/worker/delivery_poller.go` uses a fixed number of slots with no claim
prefetch. Each slot claims only when ready to execute, under a claim deadline.
An empty queue waits the configured interval. Execution failures go to a required
concurrency-safe reporter before polling resumes after a delay. Claim errors,
including ambiguous commit, malformed tasks and reporter failures stop the host,
cancel sibling work and propagate after all slots exit. The poller never invents
a second completion/retry from an execution error: the leased execution store
owns those transitions. Interrupted/uncertain claims recover through expiration.

The constructor's 1..1024 worker range is a safety bound, not approved deployment
capacity. API/worker replica counts, heartbeat connections, metadata transactions,
DDL connections and maintenance still require one shared database resource budget.
No executable selects these primitives. Complete retry policy, error/metrics
integration, and operational shutdown/failover acceptance remain required.

`internal/worker/delivery_maintenance.go` supplies the staged maintenance scheduler
through the driver-free `ports.DeliveryMaintenance` interface. Expiry and retention
have independent cursors retained across paced cycles. Pages alternate between
streams, each with an explicit timeout; each cycle permits an explicit bounded
number of pages per stream. A zero-change page still advances its cursor. An
exhausted stream waits until the next cycle before wrapping, avoiding idle hot
loops and allowing the other stream to progress. One scheduler uses at most one
database call at a time. Page size and pages per cycle are each bounded to 1..1000;
these limits are not deployment sizing recommendations.

Any page error stops the scheduler with stream and acknowledged-change context.
Earlier candidate transactions remain committed; an ambiguous commit may have
changed another candidate. No rollback or successful cycle is inferred. The host
must surface the returned failure. Cancellation is checked before each page and
after each callback, and shutdown waits for that callback to return. Restarting
begins both indexed scans anew. Executable supervision, operational metrics and
shared resource-budget configuration remain required.

Pinned River v0.31.0 `retry_policy.go` was also inspected: default retry delay is
the next error count raised to the fourth power, with +/-10 percent jitter, from
current time. It uses `len(job.Errors)+1`, not blindly the claim attempt counter.
The staged delivery schema does not yet track that error history/count. Before
activating a policy, map crashes/rescue and imported River error counts, preserve
that distinction, and resolve the adapter's current 24-hour delay bound against
the source's longer delays for large configured attempt limits. No static delay
or attempt-only backoff is claimed to provide source compatibility.

Further source inspection of pinned `internal/maintenance/job_rescuer.go` confirms
that rescue appends an AttemptError and asks the retry policy for its schedule
before appending that error. The current MySQL reaper instead makes an expired
delivery immediately available and retains only its latest redacted error code.
This is an explicit compatibility gap, not a completed rescue policy. Migration
must retain imported error counts separately from claim attempts, include rescue
errors, and cover high-count duration saturation before claiming parity.

The source runner also distinguishes direct terminal validation failures (which
can return nil after persisting failed domain state) from retry exhaustion (which
returns the original execution error). River transport outcomes therefore cannot
be reconstructed from domain status alone. The staged leased adapter now preserves
this distinction through RetryDelivery, with worker branch tests and real-MySQL
outcome, exhaustion and terminal-redelivery tests. Full River/MySQL lifecycle
differential acceptance, imported retry error counts, rescue scheduling and backoff
remain open before activation.

## Verification and remaining acceptance

### Tenant-scoped operator inspection

`DeliveryInspector` provides read-only single-delivery lookup and tenant keyset
pages across queues. This scope is explicit because deduplication reservations
span queues. Its constructor is not authorization: operator credentials and the
caller must authorize the selected tenant. No HTTP endpoint or worker activation
is introduced. The `inspect-deliveries-mysql` command requires an explicit tenant,
database, account and deadline, uses certificate-verified TLS outside the isolated
fixture, and reads its password from DATA_MODEL_MYSQL_INSPECTION_PASSWORD.

The report shows both delivery and domain status/attempt counts, the retained
delivery limit, timing, redacted error code and whether this delivery still owns
its reservation. It deliberately omits lease tokens, unbounded job names/columns,
raw error text and audit histories. Detail lookup for another tenant returns no
row. Cursor identity includes the tenant, timestamp and UUID; malformed or foreign
cursors fail. Cancellation/query/scan errors produce no partial page.

Pages contain at most 1,000 results plus one internal lookahead. Initial selection
uses only columns covered by data_model_delivery_tenant. Continuation uses two
disjoint, independently limited ranges: equal timestamp/lower UUID and older
timestamps. The bounded union is ordered and limited before joining details,
jobs and reservations. This avoids the tenant-prefix filtering observed in an
earlier OR-predicate plan. Equal-time and cross-time pagination were verified over
1,003 tenant deliveries; EXPLAIN showed covering index ranges for both branches.
One SELECT was observed at page sizes one and 1,000, including multi-megabyte job
fields omitted from projection. These are fixture query-plan/count results, not
production load or latency acceptance. Pages are live snapshots; restart from the
beginning to observe new rows and state changes.

The command defaults to a 100-row page. Continue with -before-id and
-before-created-at from the returned next cursor, or use -delivery for one record.
Both cursor components are required, and detail lookup cannot accept a cursor.
Errors exit nonzero. It is packaged for explicit operator invocation and never
started by the server or worker entrypoint. An isolated SELECT-only account on
deliveries, jobs and reservations passed inspection and was denied history reads
and mutations. Account table privileges are not database row-level isolation;
production operator authorization, credential provisioning, audit integration and
redrive remain separate unfinished requirements.

Real-engine tests cover enqueue/domain rollback, scheduled work, cross-queue and
concurrent dedupe, competing claims, tenant/token/attempt fencing, heartbeat,
callback rollback, expiry during local effects, reaping/reclaim/exhaustion,
retry scheduling, cancellation/idempotency, retention and reservation release,
locked-prefix claim/maintenance fairness, cursor rejection and read-only refusal.
Wire-fault tests consume actual server COMMIT OK packets before hiding them for
enqueue, claim and completion. Worker-boundary tests cover applied/pending/failed
outcomes, audit failure, expired claims, stale attempts and terminal redelivery.

Nineteen deterministic Go synctest scenarios additionally verify supervisor and
poller timeouts, lease loss, cancellation, panic cleanup, concurrency limits,
idle/error pacing, fatal claim/report errors and joining sibling execution.
A real MySQL test binds two poller slots to three deliveries, observes periodic
heartbeats, and verifies each job/outcome/history/delivery is committed once.
The integration executor deliberately performs metadata only, not DDL.

Six deterministic maintenance scenarios verify independent cursors across cycles,
zero-change progress, exhaustion/pacing, partial failure, deadline/cancellation
and invalid configuration. A real MySQL scheduler scenario locks each stream's
oldest job and verifies later work progresses in both streams, then restarts
after unlocking and verifies retained completed reservations are released.
Module-wide vet and the full selected race-enabled regression passed.

Production restricted-account installation, migration recovery, delivery inspection/
redrive audit, complete backoff and deployment timeout policies, maintenance host wiring,
worker/owner wiring, old River writer drain and translation, representative query
plans/load, failover and physical-DDL acceptance remain open. The definitions add
RESTRICT relationships to retained deliveries; retirement must drain/retain/delete
them deliberately before deleting a domain job or parent, not bypass constraints.

## Optional persisted error-count retry policy

`NewIndexDeliveryStoreWithRetryPolicy` opts into the additive v002 error-count
upgrade and a driver-free `ports.DeliveryRetryPolicy`. New deliveries and redrives
start with zero errors; claims alone never increase that counter. An execution
retry or lease rescue increments it atomically with its delivery transition.
Completion/cancellation does not add an error. Exhaustion still uses the stored
transport attempt budget, independently of domain attempts and retry delay.

`worker.RiverDeliveryRetryPolicy` follows pinned River v0.31.0 retry_policy.go:
the next error uses (prior errors + 1)^4 seconds with +/-10% random jitter,
saturating at the duration limit. Delays are rounded down by less than one
microsecond for MySQL. Rescue also records an error, matching the pinned
JobRescuer behavior. The former fixed-delay 24-hour limit does not truncate this
opt-in policy. Policy implementations must be concurrency-safe local computation;
they run under the narrow delivery lock and must not call external services.

Policy-aware Finish and leased execution require zero fixed retry delay, avoiding
an ambiguous configuration in which a caller's explicit delay is ignored. Unknown
history fails before claiming or rescuing a delivery. No historical counter is
guessed from attempts. Owner errors, stale leases, SQL failures and uncertain
commits retain the existing atomicity/error contract; replaying a completed lease
cannot add a second error. Schema installation, River translation/drain, concrete
policy selection, per-kind timeout/rescue parity, operational supervision and load
acceptance remain separate parent requirements. Runtime still uses PostgreSQL/River.

## Audited discarded-delivery redrive

`IndexDeliveryStore.RedriveDiscarded` and `cmd/redrive-delivery-mysql` provide an
explicit operator recovery path for a retained discarded delivery whose domain
job remains pending. They reject running/terminal jobs, non-discarded sources,
queue changes and any existing reservation, including another queue's reservation.
Running jobs require physical reconciliation; terminal jobs require their owner's
retry protocol. This operation neither proves physical readiness nor resets domain
attempts. A newly created delivery starts with zero transport attempts and the
operator's explicit new transport budget and availability time.

The exact tenant/job lock serializes redrive with enqueue, completion and
maintenance. Source delivery and reservation locks follow it. New delivery,
reservation and immutable history are committed together. Errors roll everything
back; uncertain COMMIT returns no successful delivery identity. No remote calls or
DDL run inside this transaction. All lookups are exact primary/unique-key reads;
job definitions and histories are not loaded as aggregates.

The history primary key is the source delivery UUID, enforcing one successful
redrive per source without a new schema object or an unindexed history search.
Its versioned details record the request UUID, tenant/job/source, actor, reason,
schedule, queue, new attempt limit, new delivery UUID and MySQL CURRENT_USER().
The same source and exact request/configuration/principal replay the recorded
result, even after source retention cleanup or subsequent job-state changes.
Changed inputs conflict. Another exhausted replacement must be named as the new
source; changing the request UUID cannot redrive an old source twice. Request IDs
are scoped to their source, not a global owner-operation receipt namespace.
The job must still exist. Replay does not claim the returned delivery is still
available or retained. Audit projection is capped at 16 KiB and incompatible or
oversized history fails closed. The immutable history must be retained with the
job; production deletion policies must preserve this replay evidence.

Authorization remains outside this low-level adapter and is mandatory on every
call, including replay. The operator command is privileged tooling, not an API
endpoint. Its actor flag is an operator attestation, recorded alongside the actual
database principal; it is not evidence of end-user authentication. Database table
privileges do not enforce tenant row isolation. Restrict access to the executable
and credentials to operators authorized for the selected tenant.

The command requires `-apply`, `-tenant`, `-job`, `-source`, `-request`, `-actor`,
`-reason`, `-scheduled-at` (RFC3339 microseconds), `-queue`, `-max-attempts`,
`-address`, `-database` and `-user`. It reads the password from
`DATA_MODEL_MYSQL_REDRIVE_PASSWORD` (override with `-password-env`), verifies TLS
using system trust or `-ca`/`-server-name`, and bounds the operation with `-timeout`.
Plaintext is allowed only for the explicitly enabled disposable mysql:3306 fixture.
Inspect first with `inspect-deliveries-mysql`; this command has no implicit apply
or automatic retry. After an ambiguous result, reuse the same source and all inputs.
JSON output acknowledges redrive persistence, not successful index execution.

The restricted-account command fixture uses SELECT on jobs, deliveries,
reservations and history; INSERT on the latter three; and UPDATE limited to the
immutable identity columns (jobs.id, deliveries.id, reservations.delivery_id,
history.id). MySQL 8.4.8 requires the extra privilege for FOR UPDATE. The identity
guards reject actual changes, and job-status updates/audit deletion are denied.
AccountPolicy now represents these column grants and verifies the complete account
allowlist. The restricted redrive command fixture uses generated typed grants and
VerifyAccountPolicy without out-of-policy manual grants. The migration command can
verify reviewed deployment account files with its schema/definer checks under the
migration lock. Complete production principal policies and credential provisioning
remain outstanding. No production grant policy or runtime wiring was activated.
