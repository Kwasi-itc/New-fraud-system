# Slide 27: Cutover choices

[Reading guide](README.md) | [Previous: slide 26](26-data-conversion-and-reconciliation.md) | [Next: slide 28](28-rollback-and-recovery.md)

Companion to slide 27 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

Cutover is the controlled transition in which MySQL becomes the application's active database. It requires agreement about which system may accept writes and which workers may produce effects.

**Option 1: Maintenance window**

Pause new writes, stop or coordinate workers, resolve in-flight work, synchronize the remaining data, validate, and switch the application.

This can simplify coordination. Its suitability depends on how much downtime is acceptable and how long copying and validation actually take.

A small local copy does not establish the duration for production data.

**Option 2: Minimal downtime**

Copy an initial snapshot while the source continues operating. Capture subsequent changes and apply them to the target. Then briefly stop old writers, catch up, validate, and switch.

Change capture must include updates and deletes. It must also preserve ordering where related changes depend on one another and handle the system's dynamic schema operations.

"Minimal" does not mean no coordination or guaranteed zero downtime.

**Why workers need their own plan**

An API can be paused while a worker continues sending screening requests. Copying the queue and starting target workers could then repeat work that the old worker already performed.

Drain, transfer, or reconcile work under explicit ownership. Track external requests whose results are not yet known.

**An illustrative sequence**

```text
Restrict old writers -> finish reconciliation -> validate
-> activate target writers and workers -> monitor
```

The exact runbook depends on the selected transfer approach and recovery objectives.

**How to explain this slide aloud**

"The switch must control application writes, queued work, and external effects together. We choose the downtime approach based on measured duration and proven coordination."

---

[Reading guide](README.md) | [Previous: slide 26](26-data-conversion-and-reconciliation.md) | [Next: slide 28](28-rollback-and-recovery.md)
