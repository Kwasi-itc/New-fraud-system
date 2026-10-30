# Slide 28: Rollback is a data problem

[Reading guide](README.md) | [Previous: slide 27](27-cutover-choices.md) | [Next: slide 29](29-production-acceptance.md)

Companion to slide 28 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

In this slide, rollback means returning the application to PostgreSQL after attempting a migration. That is different from rolling back one database transaction.

**Why the timing matters**

Before MySQL accepts new writes, the PostgreSQL source may still represent the latest accepted state, assuming old work was coordinated correctly.

After MySQL accepts writes, that assumption fails.

**An illustrative timeline**

```text
10:00  Switch application to MySQL.
10:05  Investigator closes case C.
10:10  Switch back to the old PostgreSQL copy.
```

If the closure was not reconciled back, PostgreSQL still shows the old case state. Users can lose accepted work even though both databases are healthy.

**External effects make it harder**

A worker may already have sent a request, delivered a notification, or received a provider result. Restoring database rows does not undo those external events.

The recovery plan must reconcile data, job state, and externally visible actions, using idempotency where appropriate.

**Recovery objectives**

RPO, the recovery point objective, describes acceptable data loss measured in time or an equivalent operational requirement.

RTO, the recovery time objective, describes how quickly service must be restored.

Targets must be agreed rather than invented for the migration.

**What the runbook needs**

Define decision owners, rollback triggers, the decision window, data synchronization method, and verification steps. Rehearse them. A retained backup is useful, but it does not by itself preserve writes accepted after that backup.

**How to explain this slide aloud**

"Once users write to MySQL, returning to PostgreSQL requires reconciling their new work. A connection-string reversal alone is not a safe rollback."

---

[Reading guide](README.md) | [Previous: slide 27](27-cutover-choices.md) | [Next: slide 29](29-production-acceptance.md)
