# Slide 21: Performance: what we must measure

[Reading guide](README.md) | [Previous: slide 20](20-concurrency-and-isolation.md) | [Next: slide 22](22-benefits-and-costs.md)

Companion to slide 21 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

Performance is specific to a workload: which operations run, how much data exists, and how requests overlap. A generic comparison cannot establish which engine is faster for this application.

**Terms to understand**

- Throughput: completed operations per unit of time.
- Latency: time taken by an operation.
- p95 latency: about 95% of observed requests finish within that time.
- p99 latency: about 99% finish within that time.
- Query plan: the database's chosen method for finding and joining records.
- Rows examined: records inspected while producing a result.
- Contention: work waiting for resources or locks held by other work.

**Why averages can mislead**

Suppose, illustratively, most requests complete in 20 milliseconds but some wait several seconds for a lock. An average may hide a poor experience for the users hitting those waits.

Measure tail latency as well as throughput.

**What a fair comparison requires**

Use representative tenant sizes, history lengths, active/closed record ratios, and request patterns. Keep resource budgets and durability expectations comparable.

Do not compare a tuned target query to an accidentally unindexed baseline and attribute the entire difference to the engine.

**Our benchmark scope**

Include ingestion, decisions, investigator queues, reports, and job backlog. Test many small tenants and large or busy tenants, not just uniform sample data.

Existing replay tooling can help exercise scenarios, but PostgreSQL statistics queries need a MySQL implementation.

No comparative benchmark was run for these documents. Proposed measurements are acceptance work, not reported results.

**How to explain this slide aloud**

"We need to measure the system's real workload, especially slow requests and contention, before making a performance claim."

Evidence: [scale suite](../backend/stress-tests/production_replay/database_scale_suite.py), [PostgreSQL observations](../backend/stress-tests/production_replay/postgres_observation.py).

---

[Reading guide](README.md) | [Previous: slide 20](20-concurrency-and-isolation.md) | [Next: slide 22](22-benefits-and-costs.md)
