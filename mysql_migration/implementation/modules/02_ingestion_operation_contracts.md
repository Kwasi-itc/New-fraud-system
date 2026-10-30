# Ingestion request and operator-operation contracts

Tasks IN-01.2.032/048/050/051/052/053, 2026-10-29. This is an implementation contract
and staged cancellation/redrive adapters, not deployment approval or configured runtime.
Retention execution and public operator endpoints are not implemented.
All effects remain within the shared logical database and owner boundaries.

## Unkeyed synchronous requests (IN-01.2.032)

Source: service/ingest_service.go only performs receipt lookup/creation when the
caller supplies a key. Repeated unkeyed requests may repeat updates, success audits
and outbox effects. A record lookup ID is not a request-idempotency key, and equal
payloads do not imply that two calls represent one business operation.

Target contract for the future synchronous adapter:

1. A supplied public key binds tenant, stable table, mode, response kind and
   canonical ordered raw input. Current authorization is required on replay.
2. An unkeyed request gets a fresh private execution UUID per accepted request.
   It is not derived from payload bytes, object ID or arbitrary request-ID headers.
   Its private receipt identity is retained for that request's local recovery;
   it must never collide with public caller keys. WorkOwner already follows this
   separation for uploads and unkeyed deferred work; synchronous adoption is open.
3. Validation/definitive rollback errors are reported without a provisional result.
   No transparent retry may invent a new identity after effects could have committed.
4. Lost COMMIT acknowledgement is an unknown outcome, not proof of rollback. An
   authorized reconciliation using the SAME internal identity may recover a
   retained result. If identity/result cannot be recovered within the request
   deadline, return an explicit unknown-outcome dependency error; do not claim
   success, failure of the business operation, or safe automatic retry. HTTP
   status/body mapping remains IN-01.2.031; no wire identifier is silently added.
5. A second unkeyed caller request is a new operation, even with identical bytes.
   Callers needing safe cross-request retry must supply a key on the original
   request. Adding a key only after an unknown unkeyed outcome does not deduplicate
   the original operation. Current record contents alone cannot prove its audit
   and outbox outcome. Operational reconciliation must count all durable effects.

The current KeyedRecordOwner still requires a key. This section defines how a
future synchronous unkeyed wrapper must behave; it does not claim it is wired.

## Operator authorization (IN-01.2.048)

Permissions are separate capabilities: inspect_work, cancel_work, redrive_work
and retain_work. API submission and worker lease credentials confer none of them.
Deployment IAM must bind capabilities to verified service/operator subjects and
permitted tenant/table scopes; this document does not grant a role to any person.

- Every operation checks current tenant/table authorization and its specific
  operator capability, including repeated calls returning retained action results.
- The trusted policy returns the actor subject after credential verification.
  User JSON, tenant headers and claimed actor strings are not authority. Policy
  callbacks may perform bounded local transaction reads; no HTTP/network calls,
  independent pools or broad parent-row locks while the transaction is open.
- Inspect is scoped and paginated, excludes payloads/keys/results by default, and
  must use the organization's access-audit sink when exposed. That endpoint and
  sink binding remain unimplemented. Bulk tenant enumeration needs a separately
  authorized scope, never a client wildcard.
- Mutations require a stable action UUID and bounded redacted reason code. The
  actor, scope, original resource/job, action and committed outcome are immutable
  evidence appended in the same transaction as the mutation. Changed scope,
  actor or reason under an existing action UUID conflicts. Error messages never
  copy raw payloads, credentials or provider response bodies into audit reasons.
- Current policy revocation denies action replay. Failed authorization returns
  no retained result. Operational access-denial logging is separate from success
  audit and must not claim a transaction committed when it did not.

WorkOperator implements cancel using an explicit required authorizer and the
existing owner authorizers. CancellationGrants defines only the necessary owned
object/column access; local metadata/policy SELECT grants are deployment-specific.
Ordinary RuntimeGrants provides no work-action INSERT. The operator account has
no need for raw resource payloads, record writes or public receipt result reads.
No production credential, IAM policy binding or operator route is installed here.

## Fenced cancellation (IN-01.2.050)

Implementation: shared mysqlstore/work.CancelTx/LockJobStateTx and ingestion
WorkOperator.Cancel, with append-only ingestion_work_actions. The new table is
an appended ingestion_v002_work_actions installation step, not a rewrite of
existing migration IDs. It links tenant/table, resource and job through FKs.

Lock order: authorize from local metadata, lock the exact job row, inspect action
replay, then change job/resource/reservation and append audit. The job lock is the
same lock required by worker FenceTx. No shared tenant lock serializes unrelated
jobs. Original jobs share the resource ID; redriven jobs have distinct IDs and
resource status/cancellation follow the current-job pointer.

| Observed state under lock | Cancellation result |
|---|---|
| available | Cancel, release active reservation, set durable finish/resource completion and append changed=true audit. |
| running (including expired lease) | Same transition; clear lease token/deadline so the old worker cannot commit. Cancellation does not wait for a separate reaper. |
| completed | Preserve all successful effects and return completed/changed=false with action evidence. Cancellation cannot undo delivery or ingestion. |
| discarded or cancelled | Preserve terminal state and return changed=false with action evidence. |
| Missing job/resource or inconsistent reservation | Error; no provisional success or partial cancellation. |

If the worker's commit wins first, cancellation records a terminal no-op. If
cancellation wins, the worker's fenced transaction cannot commit. Audit INSERT,
resource update or reservation failure rolls back the whole cancellation.
Repeating the same action UUID returns the original recorded state/changed value,
not a newly computed no-op, after reauthorization. A changed reason/actor/scope
conflicts. Concurrent same-resource calls serialize on the job; a global UUID
collision on a different resource fails INSERT and rolls back, never upserts.

No external side effect can be recalled by cancellation. The staged ingestion
worker currently keeps record/audit/outbox/completion in one local transaction;
future external processing must preserve that distinction with delivery contracts.

## Redrive identity and receipt rules (IN-01.2.051)

Redrive is an explicit operator operation, not a flag that resets terminal jobs.
The staged implementation adds a tenant-consistent current_job_id on resources
and immutable attempt lineage; status joins, worker validation and cancellation
now follow the current attempt. A NULL pointer denotes the original job, avoiding
a deployment-wide backfill. API/worker/operator runtime adoption remains open.

- Eligible by default: discarded jobs exhausted by retryable handler/dependency
  failures or expired leases, after the cause is addressed. Invalid payload/model
  failures require corrected input as a NEW resource. Completed work is never
  redriven. Cancelled work is not implicitly resurrected; any future resume policy
  needs an explicit separate reviewed capability and transition.
- Retain original resource UUID, immutable input, original requested_at and public
  key. Allocate a NEW job UUID, new attempt budget and creation/schedule time.
  Preserve old job attempts/errors/start/finish and previous_job_id lineage; do not
  overwrite old audit rows or reset attempt counters on the terminal job.
- Hold the old/current job fence and resource row in consistent job-before-resource
  order, compare expected current job, recheck terminal eligibility, then insert
  new job/reservation, advance current_job_id and append operator evidence in one
  transaction. Concurrent redrives cannot create two active reservations.
- The execution receipt namespace stays stable by RESOURCE ID (or original caller
  key), never the new job ID. A retained successful receipt returns its original
  result and completes the new delivery without repeating record/audit/outbox
  effects. Changed input or receipt identity conflicts; it cannot be a retry.
- A stable operator action UUID makes ambiguous redrive commits reconcilable.
  Replay returns the same new job ID and result after current authorization.
  Retained action receipts must survive for at least the operator retry window.

Tests required by IN-01.2.052: concurrent redrive, stale old/new worker, receipt
present/absent, conflicting command, rollback at each write, lost acknowledgement,
revoked authorization and immutable old history. Implementation and focused acceptance evidence are recorded below; these do not
complete the production API/worker operational gate.

## Retention dependency contract (IN-01.2.053)

No deletion duration is invented. Default until configured policy approval is
retain. Receipt/action retry horizons, legal holds, consumer delivery confirmation
and reconciliation requirements must be explicit inputs to future tooling.

| Data | Eligibility and dependency |
|---|---|
| Active reservations/jobs/resources | Never eligible while available/running or with unexpired/recoverable delivery. A reservation is deleted only by an atomic terminal transition, not as cleanup to unlock work. |
| Operator actions | Immutable; retain for replay/lineage/accountability. They reference jobs/resources, so those rows cannot be purged while actions remain. |
| Public/private receipts | Preserve replay protection. Deleting an old result while permitting the same key to execute again is unsafe. Default no deletion; future compact tombstones must preserve exact identity/conflict behavior or a versioned API must reject expired keys. |
| Receipt buckets and lookup guards | Do not remove independently during live traffic: absent-key recreation can split serialization. Guard cleanup needs a coordinated quiescent/retired-table protocol and owner review. |
| Legacy provenance | Retain with its receipt and source reconciliation evidence; imported_at is not source business time. |
| Audit/outbox | Separate accountability/delivery policy. Pending or unconfirmed outbox events are never eligible. Appended events are not proof of external delivery; ingestion consumer ownership remains unresolved. |
| Terminal job/resource history | Eligible only after redrive/replay horizons close, no holds/delivery obligations remain, and referencing action/lineage requirements are satisfied or migrated to approved archival contracts. Payload removal must distinguish historical terminal records from executable work and be separately authorized. |
| Data-model tenant/table/physical metadata | Ingestion evidence FKs prevent owner deletion while retained references exist. Coordinate retirement across services; no ingestion-driven cascade deletes. |

Future deletion order for an explicitly eligible set is referencing provenance/
action/attempt-lineage records before their receipt/job/resource parents, with
all replay/audit obligations resolved first. FK order alone is not eligibility.
Worker terminal reservation release is already atomic and is not a retention job.
Use bounded keyset batches, scoped indexed eligibility, dry-run counts, checkpoints,
audit and rechecks under narrow locks. Never disable FKs or delete ledger history.
Retention tools and permissions remain IN-01.2.054; this contract authorizes no
production cleanup or erasure and performs none.

## Verification evidence

### Fresh-deployment retention decision packet (2026-10-30)

IN-01.2.054 remains open. The user's confirmation of no existing data eliminates
legacy transfer, not future retention obligations. No response selecting a
retention policy has been received. Existing default is retain; no destructive
cleanup operation or retention grant has been added.

The reviewable first-release proposal is **retain all ingestion history and defer
deletion tooling**. If selected, record the task as deferred by release scope,
not implemented or tested cleanup. Track storage growth and revisit policy before
capacity is exhausted; no growth rate or storage threshold is claimed here.

If deletion is required, the policy must specify the tenant/table scope, resource
classes, replay/redrive horizons, hold authority, delivery-confirmation evidence,
archival requirements and payload-erasure rules. All are required before a row is
eligible; age or terminal state alone is insufficient. Current outbox pending-only
state has no delivery-confirmation contract, so these events cannot be purged.
Receipt deletion cannot permit the same public/private identity to execute again.

Implementation acceptance once the policy is selected:

1. Dry-run uses scoped indexed keyset pages with explicit limits, policy version,
   fixed cutoff and opaque checkpoint; reports eligible/blocked counts without
   exposing payloads. EXPLAIN must verify the intended scoped access path.
2. A separate authorized action rechecks policy/holds/references under narrow
   locks before each bounded atomic batch; dry-run is not a deletion reservation.
3. Preserve receipt replay protection, immutable operator evidence and the
   resource/current-job/redrive FK graph. No FK disabling or blanket cascading
   deletion; archival must have an approved durable verification contract first.
4. Failure/restart, revoked authorization, changed holds, concurrent redrive,
   duplicate action and ambiguous commit tests prove no partial cleanup or
   reopened execution identity. Audit/checkpoint publication follows commit.
5. Execute only on generated disposable fixtures until a separate deployment
   decision supplies actual scope and credentials. Default runtime is unchanged.

The cancellation and restricted-grant suite passed under the race detector on
isolated MySQL 8.4.8: eight state/failure cases, six worker races and restricted
principal success/negative-access/rollback checks. TestWorkOperatorLostCommit
reuses the existing COMMIT-response fault transport: unknown outcome returns no
result, then same-action replay recovers one durable action without another append.
Module-wide go vet passed. The appended schema passed lineage and 57 durable-state
recovery/drift cases. Logs: ../evidence/2026-10-29_ingestion-operator-cancel.log,
../evidence/2026-10-29_ingestion-operator-commit-loss.log and
../evidence/2026-10-29_ingestion-operator-lineage.log. These tests do not establish
production IAM, complete queue operational acceptance or deployment approval.


## Bounded queue inspection (IN-01.2.049a-e)

Staged entry point: NewWorkInspector / WorkInspector.List in ingestion
internal/store/mysql/work_inspection.go. This is an internal adapter, not a public
endpoint. Its dedicated QueueInspectionAuthorizer receives the trusted configured
tenant and exact queue/state in a read-only READ COMMITTED transaction. It must
verify the authenticated subject, current tenant status and tenant-wide inspect_work
capability. A table-scoped create/patch/cancel policy is insufficient. No wildcard
tenant scope, client-selected tenant, HTTP fallback or inferred worker authority is
accepted. Production policy binding and access-audit sink remain required before
exposure. Authorization is rechecked for every page, including a reused cursor;
this does not claim linearizable concurrent IAM revocation.

The request requires one queue (shared queue-name grammar), one of available,
running, completed, discarded or cancelled, and a page size of 1..100. Cursor fields
bind tenant, queue, state, creation timestamp and nonzero job UUID. Cursor data is
only a position, never authority. Invalid scopes, zero/unrepresentable timestamps
and invalid limits fail before querying jobs. SQL values are parameters; the
schema identifier uses the shared identifier validator.

One query returns at most limit+1 rows: job/resource IDs, bounded kind, attempts,
creation/availability/latest-start/finish times. It excludes raw payload, resource
content, keys, lease token, free-text last_error, operator identity and result JSON.
There are no resource joins, per-job lookups or total-count scan. Latest start time
is operational history even for a queued retry; this is deliberately not the public
upload DTO, which hides prior attempts' start time while queued. No provisional
page is returned on authorization, query, cancellation, scan or commit failure.

Ordering is created_at DESC, id DESC with strict keyset continuation. Fixed-state
history is visited once despite timestamp ties. Pages are live reads: concurrent
inserts above a cursor require refreshing page one, and state transitions can add
or remove jobs from subsequent pages. There is no cross-page snapshot guarantee.
Eight concurrent readers are tested; test output is not a load/capacity benchmark.

InspectionGrants gives a separate existing account column-only SELECT on the
queue table. Policy metadata reads must be granted separately. Tests deny payload,
lease token, free-text errors, resource/receipt access and UPDATE/DELETE. The
read-only transaction also rejects accidental policy writes. Credentials, network
restrictions, production roles and access-log ownership are deployment work.

The appended ingestion_v003_operator_history step adds
operator_history(tenant_id,queue,state,created_at DESC,id DESC). Existing migration
identities are retained. On the 2,000-row mixed kind/queue/state fixture, the
unhinted first-page plan chose ready and filesorted 800 matching rows. That failed
plan is retained in ../evidence/2026-10-29_ingestion-inspection-unhinted.log. The
production builder therefore requires operator_history with FORCE INDEX. Verified
first-page ref and continuation range access use this index without filesort;
missing index fails explicitly. EXPLAIN row estimates are not measured scanned
row counts or throughput claims. The extra index adds storage and write maintenance;
deployment-scale behavior and independent schema/grant review remain open.

Evidence: ../evidence/2026-10-29_ingestion-inspection.log (five race-enabled test
functions plus module-wide go vet), and
../evidence/2026-10-29_ingestion-inspection-lineage.log (current installation
lineage and durable recovery boundaries). Only generated isolated MySQL test
schemas were altered; PostgreSQL/River runtime configuration remains unchanged.


## Shared runner telemetry (IN-01.2.055a-e)

RunnerConfig.Metrics optionally attaches a zero-value work.RunnerMetrics to the
existing shared Queue.Run. Keep one instance per runner lifetime. Snapshot copies
fixed counters and three eight-bucket duration histograms under a short mutex;
there are no maps, sample lists, tenant/job IDs, payloads, queue-name labels or
raw error strings. Passing nil preserves uninstrumented caller compatibility.
Existing mandatory error-observer behavior remains separate; its logging policy
must still redact sensitive error details before production export.

| Measurement | Meaning and scope |
|---|---|
| ClaimCalls / ClaimFailures / Claims | Completed runner claim calls, calls returning errors, and leases returned after acknowledged commit. Empty successful polls add no Claims. |
| HeartbeatCalls / HeartbeatFailures | Completed heartbeat calls and errors, including rejected stale fences. |
| RetryCalls / RetriesConfirmed / ExhaustionsConfirmed | Retry calls, acknowledged reschedules, and acknowledged retry-exhaustion discards. These are mutually exclusive successful outcomes per call. |
| RetryFailures / RetryLeaseRejections | Failed retry calls and stale-lease rejections, separate from confirmed outcomes. |
| ReapCalls / ReapFailures / ExpiredConfirmed | Completed reaper calls/errors and expired leases recovered after acknowledged commit. The existing reaper scans the owner's work table across queues, so these are NOT queue-exclusive counts. |
| HandlerCalls / HandlerFailures / ActiveHandlers | Entered handlers, non-nil returned errors, and currently executing handlers. ActiveHandlers is a gauge. |
| ProcessesFinished / ProcessFailures | Returned processing attempts and processing errors, including terminal-state verification failures after a nil handler return. Neither counter is a business-success count. |
| ClaimDuration | Elapsed application time for claim calls, including SQL/pool/commit wait. |
| QueueWait | Database time from available_at to the successful claim, including delay beyond the schedule or retry availability. It is not original resource age, queue depth or oldest backlog age. |
| HandlerDuration | Elapsed application time inside the handler; excludes later terminal-state checks/retry persistence. |
| Pool | database/sql DBStats: configured maximum, open/in-use/idle connections, wait count/duration and closure counters, covering every user of this pool. |

Histograms contain disjoint buckets <=10ms, <=100ms, <=1s, <=10s, <=1m, <=1h,
<=24h and >24h. They are coarse operational distributions, not raw latency samples
or exact percentile measurements. Counters are process-local, reset on restart,
and unsigned counters may eventually wrap; exporters must handle counter resets.
Snapshot counters are coherent at the mutex boundary; pool stats are read separately
and are not an atomic snapshot with the counters or database state. Parallel work
may be between metric updates. Error counters include context cancellation and
uncertain commit responses, so they are not a pure service-failure rate.

Unknown commit acknowledgement can undercount confirmed effects; metrics do not
reconcile business outcomes or substitute for durable job/receipt/audit accounting.
Only operations performed by this Run instance are counted; direct Claim/Retry/
Reap calls and work performed by other processes are not included. The expiry
counter includes recovered retries and exhausted discards together.

Queue wait adds a database timestamp difference to the existing exact-primary-key
lease readback, with no additional SQL round trip, scan, schema or grant change.
Extremely old available_at values saturate at Go's maximum time.Duration instead
of overflowing. The >24h histogram bucket still classifies those values correctly.
Scheduled future work is not claimed, so there is no negative wait. Tests exercise
both a recent database timestamp and a year-1000 retained timestamp.

Queue.Telemetry reads only in-memory counters and pool statistics. The exporter
must enforce the service's admin authorization; no HTTP route, metrics server,
Prometheus dependency or production worker switch is introduced. Production
export/scrape binding, dashboard/alert ownership, deployment-level queue acceptance
and instrumentation overhead measurements remain open. No overhead or throughput
improvement is claimed. Deferred backlog metrics remain IN-01.2.037.

Evidence: ../evidence/2026-10-29_ingestion-telemetry.log includes module vet,
race-enabled fixed-snapshot/histogram tests, isolated MySQL worker telemetry tests,
and existing ingestion worker lifecycle, recovery, timestamp and restricted-grant
regressions. Isolated trigger failures verify rolled-back claims/retries/expiry do
not increase confirmed counters; stale heartbeat/retry fences remain rejected.
A deliberately occupied single-connection pool verifies wait/utilization snapshots.

Final timing boundaries are captured before acquiring the metrics mutex, so
observer-lock wait is excluded from claim/handler elapsed samples. Final targeted
verification and private execution receipt accounting are retained separately in
../evidence/2026-10-29_ingestion-telemetry-final.log.


## Resource-preserving redrive implementation (IN-01.2.052)

`internal/store/mysql/work_redrive.go` implements `WorkOperator.Redrive` and
`RedriveGrants`. `ingestion_v005_current_job` and `ingestion_v005_work_redrives`
append two source-bound installation steps (23 total). Existing step IDs/SQL
remain unchanged. The current-job FK creates a resource/job reference cycle;
the isolated reset fixture removes that FK before dropping its own tables.
Deployment rollback/retention must respect that dependency.

The command pins resource ID, expected previous job ID, stable action/new-job ID,
reason code and schedule. It authorizes the scope/capability and actor on every
call, locks the previous job before the resource, checks the current pointer and
absence of a live reservation, and atomically appends the new job/reservation,
updates the pointer and appends immutable operator evidence. A unique previous
job ID prevents branching. Changed commands conflict; no generic unique upsert
is used. The accepted terminal error codes are `handler_failed`, `handler_timeout`
and `lease_expired`; unknown codes, cancellation, payload/model rejection and
completed work fail closed. A trusted operator must first resolve the cause.

The resource ID, payload, requested timestamp and public/private receipt identity
are preserved. The action UUID becomes the new job UUID. Original job attempt,
error and timestamps remain unchanged. Creation replay and detail/upload-list
reads retain the resource identity while following the current job. Cancellation
acts on the current job, retaining old action replay results. Workers no longer
assume job ID equals resource ID; queue fencing still rejects stale old leases.

Redrive permissions exclude payload reads/writes, physical effects, receipts,
cancellation state updates and DDL. MySQL 8.4.8 requires an update privilege for
the queue locking read: the grant is limited to `tenant_id`, constrained by the
existing tenant/resource FK and globally unique resource ID. Actual queue-state
updates remain denied. Metadata policy grants are composed separately. This is
not independent deployment grants approval.

Acceptance evidence: [full suite](../evidence/2026-10-29_ingestion-redrive/accepted/summary.json),
[raw events](../evidence/2026-10-29_ingestion-redrive/accepted/raw.jsonl),
[source hashes and commands](../evidence/2026-10-29_ingestion-redrive/provenance.json).
The full suite passed 13 tests with no failures/skips. Focused race-enabled tests
cover concurrent identical requests, conflicts, stale leases, revoked authority,
restricted grants, rollback on each required grant denial, stable public/private
receipt replay, ineligible states, current-job cancellation and dropped actual
COMMIT acknowledgements. Queue/operator regressions, 23-step installation with
69 recovery boundaries, and scoped vet passed. Lost-ack recovery passed twice.
Production IAM binding, operator endpoints, deployed worker acceptance and
independent schema/grants review remain open. This completes one existing leaf
item, not the requested batch of twenty.
