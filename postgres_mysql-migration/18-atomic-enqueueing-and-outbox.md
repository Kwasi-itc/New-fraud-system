# Slide 18: Reliable processing: preserve atomic enqueueing

[Reading guide](README.md) | [Previous: slide 17](17-river-job-processing.md) | [Next: slide 19](19-drivers-errors-and-tools.md)

Companion to slide 18 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

Atomic enqueueing means recording business data and the work it requires as one database transaction. This closes a failure window between saving data and requesting further processing.

**The failure window**

```text
Save screening record -> commit -> send job to external queue
                            ^
                    process crashes here
```

The record exists, but no job may arrive. Reversing the order can create a different problem: a worker receives a job whose data never commits.

**What an outbox is**

An outbox is a database table containing messages or work awaiting delivery. The application writes its business change and the outbox entry in one transaction.

A separate worker reads committed entries, sends them, and records delivery progress. Network delivery remains a separate action: sharing a database does not make the network atomic.

**Why repeated delivery is possible**

A receiver may accept a message while its acknowledgement is lost. The sender then cannot be certain whether delivery happened and may retry.

An idempotent consumer recognizes the same logical operation and avoids repeating its business effect. This is stronger than merely checking whether the current request body looks familiar.

**Leases and ownership tokens**

A lease temporarily assigns a job to a worker. If it expires, another worker can take over. A token distinguishes the current owner from an old worker that resumes late, preventing stale completion updates.

A lease by itself does not prevent duplicate external effects; consumer idempotency still matters.

**How to explain this slide aloud**

"We can make data and pending work atomic inside the database. Delivery outside it still needs retries, ownership checks, and protection against repeated effects."

Related implementation: [screening outbox](../backend/screening-service/internal/store/postgres/case_event_outbox.go), [ingestion enqueueing](../backend/ingestion-service/internal/riverjobs/enqueuer.go).

---

[Reading guide](README.md) | [Previous: slide 17](17-river-job-processing.md) | [Next: slide 19](19-drivers-errors-and-tools.md)
