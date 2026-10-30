# Slide 17: River is a migration workstream

[Reading guide](README.md) | [Previous: slide 16](16-business-rules-and-authorization.md) | [Next: slide 18](18-atomic-enqueueing-and-outbox.md)

Companion to slide 17 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

River is the job-processing library used by the current application. It records work to be done, lets workers claim it, and provides mechanisms for scheduling and retries.

Our configured integration uses the PostgreSQL driver riverpgxv5 and PostgreSQL transaction objects.

**A job's lifecycle**

```text
Job recorded -> eligible to run -> worker claims it
                                  ├── success
                                  └── failure -> retry according to policy
```

Not all jobs run immediately. A job may be scheduled for a future time or delayed after a failed attempt.

**Why a database driver change is insufficient**

River's integration involves more than opening a connection. It includes database statements, migration tables, transaction handling, worker coordination, and lifecycle behavior.

MySQL's SKIP LOCKED can help implement row claiming, but that is one component of a queue. It does not replace scheduling, retries, uniqueness, or operational visibility.

This is a statement about our configured PostgreSQL integration, not a timeless claim about every River release or possible driver.

**Why InsertTx matters**

The ingestion enqueuer uses InsertTx. This records the job within the same transaction as associated data changes, so both commit or neither does.

A replacement must preserve that contract. It must also account for jobs already pending when the system switches databases.

**What needs to be inventoried**

List the job types, scheduling requirements, retry limits, deduplication rules, worker concurrency, and failure recovery actually used by each service. Do not assume a new queue has identical defaults.

**How to explain this slide aloud**

"We are migrating a job-processing system as well as data tables. The replacement must preserve the lifecycle and atomic creation of background work."

Evidence: [ingestion enqueuer](../backend/ingestion-service/internal/riverjobs/enqueuer.go). Sources: [River drivers](https://riverqueue.com/docs/database-drivers), [transactional enqueueing](https://riverqueue.com/docs/transactional-enqueueing).

---

[Reading guide](README.md) | [Previous: slide 16](16-business-rules-and-authorization.md) | [Next: slide 18](18-atomic-enqueueing-and-outbox.md)
