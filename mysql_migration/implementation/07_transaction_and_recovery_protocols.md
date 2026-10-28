# Transaction and recovery protocols

The full protocols remain proposed specifications. The pinned-connection DDL lock prerequisite for P-01 is implemented and tested in `backend/mysqlstore/ddl_lock.go`; this does not implement schema publication or recovery. References use PG IDs from the [source-linked inventory](../02_postgresql_usage_inventory.md). POC gates in [09](09_validation_and_proof_of_concept_plan.md) must demonstrate both safety and progress. Tests must inspect committed state, not only HTTP success.

## P-01 Runtime DDL and contract publication (PG-03/05/06/07/27)

Current TableService.Create and FieldService.Create combine metadata DML with physical DDL in a PostgreSQL transaction. MySQL DDL cannot preserve that boundary by substituting BeginTx: [implicit commit behavior](https://dev.mysql.com/doc/refman/8.4/en/implicit-commit.html) and [atomic DDL](https://dev.mysql.com/doc/refman/8.4/en/atomic-ddl.html) require a recoverable protocol.

Proposed tables: model operation `(operation_id,tenant_id,table_id,request_hash,expected_version,target_version,desired_shape,state,last_error)` and registry `(tenant_id,table_id,physical_name,published_version,write_state)`. States: PREPARED -> APPLYING -> VERIFYING -> PUBLISHED; errors become FAILED for safely rejected intent or BLOCKED for ambiguous/drifted state requiring reconciliation. A failure is never represented as successful publication.

1. Authorize and validate the whole intended model change. In a short transaction lock the table registry exclusively, check version/idempotency, persist desired intent and set a table-scoped write barrier where required. Existing ingestion transactions hold a shared registry lock through commit, so this exclusive step waits for prior writers to finish. Commit the intent/barrier; release row locks.
2. Acquire a named DDL lock on a **dedicated pinned connection** that executes the DDL itself. Check lock return value, operation state and current physical shape. Execute one explicit DDL step at a time with recorded pre/postconditions. Never allow a second executor merely because a wall-clock lease expired: a still-running first DDL cannot be fenced by a row token.
3. Inspect actual columns, types, defaults, nullability, keys, collation and grants. If shape matches the intended postcondition, proceed even if acknowledgement was lost. If it matches neither precondition nor postcondition, block and surface drift. Existence alone is insufficient.
4. Publish metadata, normalized contract JSON, opaque changed-contract version and display cleanup in one short transaction, conditional on the expected operation/version. Publication includes the verified registry mapping and removes the barrier. Cache invalidation/outbox joins that transaction. A logically unchanged contract does not get a gratuitous version change.
5. Release the named lock explicitly; on uncertain cleanup destroy the connection, never return it to the pool. Recovery acquires the same lock and reconciles shape before continuing. [GET_LOCK lifecycle](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html) differs from transactional row locks.

DDL locks coordinate one server, not a split-brain topology. Failover must fence the former primary before reconciliation begins. Only the DDL role may execute physical model changes; all business writers enforce publication barriers. Long-running readers can delay metadata locks; use bounded lock acquisition and retry visibility, not unlimited request blocking.

Additive compatible changes may leave old-version reads available while new fields remain unpublished. Destructive/type/unique changes require explicit migration states, preflight data checks and scoped quiescence. Do not drop archived columns merely to complete a rename; retain mappings until readers and rollback windows no longer require them. Field/type/display/index operations each need a recovery matrix. Reconciler polling is durable even if job delivery fails; queue notification accelerates it but is not the only source of truth.

Acceptance: kill before/after every DDL and metadata step; lose connection after server commit; run two executors; race ingestion and field retirement; simulate wrong index shape and failover. No reader sees a published contract without its verified physical objects, and no successful write uses an incompatible old version. Current API synchronous success must mean PUBLISHED; a new pending response requires a separately reviewed API change.

Constraint-replacement evidence (2026-10-23): the MySQL 8.4.8 fixture in
`backend/mysqlstore/constraints_test.go` rejected dropping and adding an FK with
the same name in a single ALTER (error 1826). Separate DROP/ADD statements are
used only in that empty disposable fixture. A production replacement must model
the intermediate missing constraint, block relevant writers and reconcile after
either DDL step; a named DDL lock alone does not stop business writes. This
observation does not approve a production replacement algorithm.

## P-02 Targeted ingestion and idempotency (PG-08/09/12/28)

For single or batch requests, validate input and acquire the narrow tenant/idempotency reservation before object mutations. Persist the request hash and response kind in the same transaction as data, audit, outbox and result. A second same-key caller waits, then replays the committed result or rejects a different hash. A rolled-back caller leaves no completed receipt. Independent keys are not serialized through a tenant parent row.

Acquire per-object lookup guards in deterministic table/key order, then perform a current locked lookup by the **specified lookup field**. If absent, INSERT with a generated UUID; if present, apply only supplied patch fields and update the recorded timestamp according to the current contract. Other unique-key violations remain conflicts and roll back the operation; they must not redirect a write to another row. Database full uniqueness still enforces alternate keys. Missing-row serialization is explicit; do not depend on a gap lock under READ COMMITTED.

Guard identities use exact canonical key semantics; long keys use the dictionary design in [04](04_schema_and_data_access_design.md). The lock rows are narrow and persistent. Native key implementations may use a simpler reservation table when bounds permit. Preserve batch input behavior when the same object appears twice: lock once, then apply the existing ordered semantics. Do not reorder actual patches simply because lock acquisition is sorted.

A batch keeps its existing all-or-nothing database boundary. Optimize prepared statements/multirow inserts only after measuring parameter/packet bounds and preserving error association. No COPY-equivalent is required merely because the workload is bulk. Failure audit behavior currently differs from successful transaction audit; characterize it rather than inventing atomic failed-request records.

An unchanged patch is not missing data. Return typed outcomes based on the locked row and explicit state transition. Follow-up reads of generated values stay inside the transaction. Test alternate-unique collisions, no-op patches, concurrent identical/different hashes, response-kind mismatch, two-worker same object, batch partial error, cancellation and ambiguous commit.

## P-03 Durable work and schedules (PG-10/11/14)

Recommendation: a small shared MySQL work engine with owner-owned tables and typed enqueue methods, pending POC-02 and operational ownership approval. Retaining PostgreSQL for River fails full exit; an external broker still needs a transactional outbox and adds infrastructure. A maintained MySQL transport may be substituted if it demonstrably covers the complete lifecycle below; SQL transport alone is not proof of River schedule/uniqueness parity. [River upstream](https://github.com/riverqueue/river) is not evidence that the repository's pgx adapter supports MySQL.

Minimum work row: immutable job ID, kind, queue, payload version, owner resource/operation ID, state, available_at, attempt, lease token, lease_until, max attempts, timestamps and redacted last error. Unique-active reservations are separate rows keyed by documented scope; they reference the active job and are released only as part of its terminal transition. Persist domain mutation and enqueue in the same `sql.Tx`; missing enqueuer configuration fails startup or mutation explicitly.

Claim: short transaction selects a bounded ready batch ordered by availability/ID with FOR UPDATE SKIP LOCKED, increments attempt, assigns random token and lease deadline, then commits. Load/external work occurs without DB locks. Completion/retry/heartbeat requires job ID, token, running state and an unexpired lease; successful update increments a version or changes state. Reaper handles expired running rows through a separate indexed scan and fences the old token. Completion and domain publication use the same transaction wherever both are local. A stale worker cannot commit business effects merely because its final job acknowledgement is rejected. Publication locks and validates the work row/token before applying local domain effects and retains that lock until commit; define its position in each worker lock graph, including any source-trigger enqueue, and test deadlock recovery. Projection source-before-queue order is a separate owner protocol and must not be inverted by this helper.

[SKIP LOCKED](https://dev.mysql.com/doc/refman/8.4/en/innodb-locking-reads.html) is suitable for competing work claims, not permission/report snapshots. Pollers need jitter, bounded claims and fairness across queues. Token expiry uses database UTC time. Heartbeat cadence, lease duration and worker limits are configured from measured operation times; preserve existing limits until evidence supports changes.

Schedules: lock one schedule row; insert `(schedule_id, scheduled_instant)` occurrence with a unique key and job in the same transaction; advance the watermark. Define per-kind missed-run policy (catch up, coalesce or skip) from existing periodic configuration before enabling it. Restart/RunOnStart must not create competing active singleton jobs. Retry backoff/jitter, cancellation, dead-letter inspection, retention and operator redrive are mandatory, not future optional work.

Existing kinds to map one-for-one: data-model `index_job_execute`; ingestion `upload_log_execute`, `deferred_ingest_execute`; decision `scheduled_execution`, `async_decision_execution`, `async_decision_execution_callback`, `workflow_execution`, `screening_execution`, `scoring_request`, `outbox_event`; screening `screening_dispatch`, `dataset_update_job`, `monitored_object`; case `case_maintenance`, `case_delivery`. Source: each owner's `internal/riverjobs` and case `internal/worker/maintenance.go`. Preserve payload formats or version them with explicit translation. Case currently uses active-state uniqueness by queue, one worker per maintenance/delivery queue, 30-minute job timeout and max five attempts; these are distinct from screening outbox policy.

Queue execution is at least once. Exactly-once local **effects** require stable operation receipts and fenced writes. HTTP callbacks/email/provider operations need receiver idempotency or an explicit duplicate-effect contract. Drain or translate outstanding River work at cutover; never copy River internal tables directly into this different protocol.

## P-04 Screening leases, outbox and projection (PG-13/14/15)

Preserve dispatch's six-minute token claim, provider search outside the transaction, then locked token/status validation before candidate replacement. In one transaction replace candidates, record exclusion applications, update parent and enqueue review/case effects. A partial result with zero candidates retains the existing awaiting_review behavior. Old token publication after reclaim must affect no business rows. Provider output size and replacement batch size remain bounded.

Screening case delivery keeps the current 60-second lease, attempt fence, capped backoff and 20-attempt terminal behavior unless separately changed. MySQL splits the PostgreSQL CTE/UPDATE/RETURNING into locked selection, update and payload read within one short transaction. Finish is token/attempt conditional. Invitation delivery preserves its own generation fence; do not unify away domain-specific semantics.

Projection retains source-before-queue lock order. Find bounded due identities, acquire each source lock, then lock/recheck its queue row and due/version state. Avoid a joined lock query whose table lock order is unproven. Advance through due identities so a hot locked prefix cannot starve later work. Take a savepoint before the decision-owned reconciliation routine; failure rolls back only projection effects, records retry state and commits that retry. Success deletes the queue entry only if its source version still matches. Concurrent source updates cannot be lost.

The decision-owned routine validates tenant, decision, screening configuration, scenario and idempotency binding, then applies only a newer source version. Source version increment and enqueue must be enforced on every source mutation path (retain a proven MySQL trigger or restrict writes to a complete owner routine). Tests include invalid binding, equal/older versions, source update during projection, routine failure, retry recovery and deadlock inversion detection.

## P-05 Investigator reviews and immediate constraints (PG-16/18/35)

Current exclusion Set writes state/events before the immutable shared review; deferred PostgreSQL FKs allow that order. InnoDB immediate constraints require a planned transition under locks. Preserve the one-database transaction:

1. Validate trusted actor/session and tenant; reserve operation ID/hash; lock case, inbox permissions and resource in the reviewed order. Return exact stored response for a matching replay; reject conflicting reuse.
2. Under those locks, calculate and validate the complete transition, including before/after values, findings, final result, versions/timestamps and exclusion changes. Allocate immutable IDs once. No external call occurs here.
3. Insert the owner review record first, then its exclusion event, then current exclusion state referencing the event, then applications/other dependent rows. Apply resource/candidate/parent status mutations using the same predetermined result. If implementation cannot derive the final result before inserting the immutable review, redesign its transition planner; do not temporarily disable FKs or mutate the review afterward.
4. Invoke the case-owned review contract after resource updates so case link state and event payload reflect the committed transition. Append case event and delivery outbox; commit everything together.

Existing immutable triggers check resource existence, not the final mutated status. Verify the exact MySQL replacement permits this order while rejecting update/delete and cross-tenant references. Preserve initial-review-only uniqueness and subsequent exclusion-change semantics. Forced failure at each step must leave no review, exclusion, resource or case event committed. Review hash/canonicalization must remain replay-compatible across migration.

## P-06 Authorization, case capacity and owner contracts (PG-17/19)

Normalize membership roles but retain membership/session version checks and user -> membership -> session lock order. Current authorization reads expiry at authorization time; do not replace clock_timestamp with a transaction-start timestamp captured before a long wait. Last-admin changes use a narrow tenant/admin guard and recheck current active admins under it. External identity binding locks the exact issuer/subject scope, with tenant-independent identity ownership semantics preserved.

For assignment capacity, retain the membership-scoped lock plus bounded indexed count across open cases. Every assignment, reopen, move and automatic worker path must use the same enforcement. Prefer an owner routine with restricted direct writes if a faithful MySQL trigger cannot perform the required locking/count safely. Do not introduce workload counters without evidence; counters create an additional consistency/reconciliation burden. A read count followed by an unprotected update is insufficient. Multi-member transfers acquire locks in sorted order and revalidate permissions/capacity.

Owner routines for authorization, projection and case review must share the caller transaction and never commit. Fixed, minimally privileged definers are a proposed alternative to broad cross-owner grants. Acceptance includes restricted-account bypass attempts, accidental autocommit entry, concurrent revocation/review, last-admin races and capacity contention. If that boundary cannot be enforced, retain database triggers where feasible or revise the API; do not settle for handler-only validation.
