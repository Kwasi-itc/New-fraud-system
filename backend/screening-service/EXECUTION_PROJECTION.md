# Durable decision execution projection

R03.4 reconciles decision-engine screening executions from authoritative screening state. This is a secondary projection; it never changes machine decision outcome, human adjudication history, case ownership or case lifecycle.

## Write and recovery contract

Screening migration `000007` adds a monotonic `projection_version` and a coalescing `execution_projection_queue`. Every screening insert/update increments the version; decision-linked sources enqueue the latest version in the **same transaction**. Continuous/freeform sources without a decision do not enqueue. Existing decision-linked sources are queued for reconciliation during migration. Review, source aggregate, case history and queue insertion therefore cannot partially commit.

`ExecutionProjector` drains bounded batches (1–1000; existing worker batch setting), selecting ready work with an index and `FOR UPDATE ... SKIP LOCKED` on the source. It acquires source, queue, then target execution locks, without a network call. The decision-owned `core.reconcile_screening_source` SQL function reads current source state and updates only decision-owned tables. The projector acknowledges that exact queue version in the same transaction. Process death or acknowledgement failure rolls back both target change and acknowledgement. Multiple consumers are safe. Intermediate revisions may be coalesced; this queue is not an audit log, and immutable investigator history remains separate.

The function verifies tenant/decision/config/scenario consistency. Standard producers identify the execution by their execution-ID idempotency key. Legacy sources with an empty key require exactly one matching execution; missing/ambiguous targets and conflicting source bindings fail closed. Historical nonstandard nonempty keys require operator reconciliation, not automatic reassignment. An execution binds to one screening source, and already projected versions are no-ops.

Failures retain the row, attempt count and truncated error. Retry delays grow exponentially from two seconds and cap at 256 seconds; attempts never cause automatic deletion. New source changes reset backoff. Each database attempt has a five-second context budget. Database/cancellation errors preserve unacknowledged work even if retry metadata cannot be committed.

The existing `screening-case-delivery-worker` runs projection recovery in an independent loop so slow case HTTP callbacks do not delay it. The general screening worker also drains projections in its batch/poll cycle. Either may recover work; the normally enabled Compose delivery worker polls every two seconds with batch size 50 and a two-connection pool. No new process, credential or external notification destination is required. Operators must keep a worker running; deploying only the API queues work without delivering it.

## API compatibility

Initial human match review returns success once its source/case transaction and queue entry commit. Decision API availability is not part of that request. Browser retry remains appropriate for an uncertain review acknowledgement, not for projection recovery.

The legacy authenticated `/internal/screening-status-updates` route remains available. Its identifiers must match the current source; payload status/provider/count/timestamp fields are accepted for wire compatibility but are ignored for projection. It invokes the same source-reading SQL function. Delayed callbacks cannot overwrite newer source state, and callback delivery is no longer the reliability mechanism.

Late dispatch acknowledgements cannot overwrite a source-bound execution. Generic decision-engine execution status/retry writes also reject source-bound executions: retries of downstream screening must use screening-service's existing retry operation so source state, version and queue remain consistent. Unbound execution dispatch/retry behavior remains available. Source retries may legitimately move a projection back to pending; ordering follows source versions, not a fabricated monotonic status ranking.

## Deployment and rollback

Apply decision-engine `000019_screening_projection`, then screening `000007_execution_projection`, after their existing prerequisites. The core function is resolved against screening tables when invoked, so enable callbacks/workers only after both migrations. Deploy the decision API, screening workers and screening API together in a controlled rollout; old decision binaries still trust callback snapshots and lack the dispatch fence. No container image build or intended-environment rollout is implied by local tests.

Both migrations are transactional and lock bounded. Screening migration backfills queue entries and advances existing source versions; schedule it for a maintenance window on large tables and check database-wide pool/load limits. Core and screening retain separate migration histories and logical table ownership. The shared credentials do not provide database-role isolation.

Drain writes/workers before rollback. Screening down refuses pending queue work and retains source version counters; core down retains projection provenance/version columns. Re-upgrading advances historical versions and queues current state, including changes made while triggers were absent. Apply down migrations in reverse dependency order: remove screening `000007` before its `000005` tenant-key prerequisite. Do not delete pending rows or audit data merely to force rollback.

## Operations

Inspect failed/delayed work using a bounded query:

```sql
SELECT tenant_id, screening_id, version, attempts, available_at, last_error
FROM screening.execution_projection_queue
ORDER BY available_at, tenant_id, screening_id
LIMIT 100;
```

Worker logs report cycle failures and the count retained for retry. Monitor queue age/size and repeated failures alongside worker health. After repairing a verified missing relationship/configuration, expedite one existing item if needed:

```sql
UPDATE screening.execution_projection_queue
SET available_at=now()
WHERE tenant_id=:tenant_id AND screening_id=:screening_id;
```

Never repair a mismatch by blindly rewriting tenant or source identity. The queue retries automatically after repair; no browser session or original investigator token is required to project already committed source state.

## Local verification

`execution_projection_integration_test.go` uses disposable PostgreSQL databases and real migrations. It covers missing-target repair, worker restart, coalescing, concurrent consumers, projection failure, acknowledgement rollback, tenant/relationship denial, source-lock contention, real stale callback HTTP handling, generic status rejection, and down/re-upgrade preservation. The decision repository integration test covers late dispatch acknowledgement and generic mutation fences. The investigator HTTP journey verifies that unavailable callback HTTP does not block review. Production throughput and identity/deployment gates remain separate.
