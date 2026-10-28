# Legacy data-model repair delivery recovery

Scope: DM-01.2f, PostgreSQL/River migration preparation. This is not the MySQL queue or P-01 DDL protocol. Production data has not been inspected or repaired by this implementation session.

## Problem and boundary

`internal/reconcile/service.go:scheduleRepairJobs` skips pending/running repair jobs. Older deployments could persist a repair job without a River delivery. The current reconciliation transaction prevents new split writes, but does not repair historical ones. A repeated reconciliation cannot distinguish a stranded pending job from live queued work.

`cmd/recover-delivery` and `internal/reconcile/delivery_recovery.go` provide an exact-job operator path. There is no sweep, scheduler, HTTP route or automatic retry of terminal work. It is available only to operators with database access; application callers must not receive those credentials. Tenant and job UUIDs are mandatory and the row is locked through the eligibility check and atomic queue/history insertion.

## Procedure

1. Obtain a specific repair-job UUID and tenant UUID from the reconciliation report or operator investigation. Confirm the database destination using the deployment's trusted configuration. Do not run a destructive test fixture against it.
2. Inspect using the service binary `recover-delivery -tenant <UUID> -job <UUID>`. Without `-apply`, no delivery or history is written. Inspection briefly locks the selected owner row; the command has a 30-second deadline.
3. Review the structured result and deployment logs. Only `eligible=true` with reason `missing_delivery` is eligible for this command. No retained River row of the matching kind and job argument may exist, regardless of queue, state or old uniqueness metadata. Other kinds do not represent this worker's delivery.
4. Apply with `-apply -actor <operator-reference> -reason <ticket-and-explanation>` under trusted operator credentials. The producer must use the configured worker queue and maximum attempts. Actor text is an operator assertion, not an application authentication token; use the recorded database session principal and external access logs for attribution.
5. A successful response requires `recovered=true`. The command queues the original job and appends `recover_index_job_delivery` in one transaction. It does not change job status, schedule, attempt count or physical schema. Exit code 2 means an apply request was ineligible; errors exit nonzero.
6. Confirm worker pickup and its eventual outcome separately. Recovery history records delivery restoration, not successful index creation. If acknowledgement is lost, inspect the same UUID again; an existing delivery blocks another insertion. Never delete evidence to force eligibility.

## Ineligible outcomes

| Reason | Meaning and next action |
|---|---|
| `job_not_pending` | Running, failed, applied or cancelled owner state: inspect physical/index and queue state through the existing incident process. This command does not retry or reset it. |
| `not_reconciliation_repair` | Non-repair or non-reconciliation request: use its owning lifecycle; no generic job resubmission. |
| `delivery_evidence_exists` | Any retained delivery, including completed/cancelled/discarded or a nondefault queue: investigate scheduling, queue configuration and state mismatch. Do not create concurrent work. |
| `table_identity_missing` / `table_identity_invalid` | Missing, archived, renamed or wrong-tenant table reference: investigate metadata/physical drift before any DDL. |
| No matching tenant/job | Returns an error; never falls back to an unscoped lookup. |

Retained terminal evidence can be inconsistent with a pending owner row. This command deliberately does not decide which historical outcome was correct. A repair that has run before may also have partial physical effects; the worker still inspects physical state before DDL. Complete MySQL shape verification and fencing remain independent required gates.

## Atomicity and concurrency

The owner row lock serializes concurrent recoveries and worker claims/owner updates. Normal producers create or update that owner state before enqueueing. The query additionally detects legacy River rows without a unique key. River's current argument uniqueness protects current producer insertion; it is not used as a substitute for inspecting legacy delivery evidence.

No database lock is held during physical DDL or network delivery. Queue insertion is a local database transaction. Queue/history errors roll back both effects and return no success result. Existing table identity is checked under the same transaction; subsequent changes remain the worker's responsibility, including its own tenant/table checks.

This protocol assumes supported owner APIs and queue producers, not arbitrary administrative SQL creating new legacy delivery rows concurrently. Operators must not delete running queue rows or bypass owner mutation rules. A bypass is an incident, not evidence that redelivery is safe.

## Verification and operating limits

The disposable PostgreSQL/River integration suite covers inspect/apply/replay, original schedule/attempt preservation, simultaneous recovery, all eight River states (including legacy rows with no uniqueness key), wrong tenant, invalid requests, all terminal/running owner states, wrong request type, missing/mismatched table identity, and rollback after queue/history failure. The execution log records actual test results before DM-01.2f is checked.

Delivery existence uses JSON containment with constant kind and a parameterized exact job argument. It projects only an existence bit and stops at the first match. No assumption is made that every deployed River version has a suitable index: check `EXPLAIN (ANALYZE, BUFFERS)` on the exact read-only predicate using representative retained queue volume before frequent recovery use. The command deadline bounds waiting/work, not database scan cost. This path is for rare exact-job maintenance, not a production-wide queue scanner. Broad historical inventory, if required operationally, needs an independently reviewed bounded extraction plan.

Only the existing database is used. No deployment has been switched, and no operator recovery has been run against application data.
