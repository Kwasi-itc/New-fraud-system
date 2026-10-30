# Slide 2: Executive summary

[Reading guide](README.md) | [Previous: slide 1](01-what-are-we-deciding.md) | [Next: slide 3](03-current-architecture.md)

Companion to slide 2 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

This slide gives the main conclusion before the technical detail: MySQL could support the product, but this application already depends on PostgreSQL in ways that make replacement substantial.

**What 'PostgreSQL-specific' means**

Some code uses a feature PostgreSQL provides directly, such as an index containing only active records. Other code uses a PostgreSQL interface for something MySQL also supports, such as JSON querying.

These lead to different tasks. A missing native feature needs a replacement design. A different interface needs a port, followed by tests to confirm equivalent behavior.

**Why concurrency matters**

Concurrency means several operations happen at the same time.

Imagine an investigator has room for one more case. Two requests both inspect the current workload. If each sees one available slot and both assign a case, the investigator exceeds capacity. A correct implementation coordinates the check and assignment so that only the permitted assignment succeeds.

The application can pass a simple single-user demonstration while still failing this example.

**What the recommendation does and does not mean**

Keeping PostgreSQL is the current recommendation because it preserves the existing implementation without a proven benefit from switching. It is not a claim that PostgreSQL is always faster or that MySQL cannot run a fraud system.

No comparative workload benchmark was performed for this documentation. Therefore, speed and cost remain questions to measure, not advantages already established.

**How to explain this slide aloud**

"Migration is feasible, but our existing guarantees require careful replacement. We should proceed when a concrete benefit outweighs that work and we can prove the new implementation behaves correctly."

---

[Reading guide](README.md) | [Previous: slide 1](01-what-are-we-deciding.md) | [Next: slide 3](03-current-architecture.md)
