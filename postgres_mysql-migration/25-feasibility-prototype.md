# Slide 25: What the feasibility prototype must prove

[Reading guide](README.md) | [Previous: slide 24](24-delivery-phases.md) | [Next: slide 26](26-data-conversion-and-reconciliation.md)

Companion to slide 25 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

A feasibility prototype is a deliberately limited implementation used to answer difficult design questions. Its value comes from the risks it tests, not from how quickly a demonstration runs.

**The highest-value scenarios**

Concurrent ingestion should preserve records and accurate created/updated outcomes. Simultaneous assignments must respect investigator capacity. Business changes and pending work must roll back together.

Dynamic DDL needs interruption recovery, while screening changes need valid relationships and immutable audit records.

Authorization tests must include permissions changing during an operation.

**An illustrative test**

Start two requests attempting the same logical ingestion. Coordinate their execution so they overlap at the important point. Assert the resulting records, API outcomes, and pending jobs.

This is stronger than launching two requests and hoping they overlap.

**Failure injection**

Failure injection deliberately interrupts a process or connection at a chosen point. For example, disconnect after the database commits a column creation but before the client sees success. The next attempt must discover the actual schema and recover safely.

**What a prototype cannot prove alone**

A few fixed tables do not establish support for every dynamic tenant model. A passing queue insertion test does not establish worker crash recovery. A small dataset does not establish useful query plans at scale.

The existing plans describe historical prototype work that was reverted. Those reports must not be presented as an active MySQL implementation or fresh test results.

**How to explain this slide aloud**

"The prototype should attack the hardest correctness and recovery questions. A successful demonstration is useful evidence, but only within the scope it actually tested."

Context: [historical migration plan](../POSTGRES_TO_MYSQL_MIGRATION_PLAN.md), [historical data-model plan](../DATA_MODEL_MYSQL_IMPLEMENTATION_PLAN.md).

---

[Reading guide](README.md) | [Previous: slide 24](24-delivery-phases.md) | [Next: slide 26](26-data-conversion-and-reconciliation.md)
