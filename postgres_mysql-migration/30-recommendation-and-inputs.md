# Slide 30: Recommendation and decision inputs

[Reading guide](README.md) | [Previous: slide 29](29-production-acceptance.md) | [Next: slide 31](31-second-review-findings.md)

Companion to slide 30 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

The recommendation is to retain PostgreSQL unless there is a concrete benefit or requirement that outweighs the conversion and validation work.

This recommendation is specific to our current implementation and the evidence available. It is not a ranking of database engines for all applications.

**Inputs needed for a decision**

Data volume affects transfer duration. Tenant and table counts affect dynamic schema management. Growth and throughput affect future capacity.

Downtime allowance constrains the cutover approach. Recovery objectives constrain backup, reconciliation, and rollback design.

A supported customer environment or organizational policy may make MySQL necessary even without a speed advantage.

**Why effort is not estimated yet**

We have identified substantial dependencies, but inventory counts do not reveal how hard the replacements are. The deferred relationships, queue integration, and dynamic DDL recovery need concrete designs and prototypes.

Estimating before those risks are understood can create a misleading delivery commitment.

**Who needs ownership**

Service engineers own business behavior and storage contracts. Database operations own deployment and recovery. Identity integration needs a provider-aware migration approach. Testing and release coordination need agreed acceptance and cutover procedures.

These are responsibilities to assign, not evidence that particular people or teams have already been assigned.

**A decision example**

Proceed when the business requirement is explicit, a prototype resolves the hardest technical risks, and stakeholders accept the measured cost and operational plan.

Retain PostgreSQL when benefits remain speculative and the current implementation meets requirements.

**How to explain this slide aloud**

"We should decide from a clear requirement and demonstrated feasibility. The next investment, if migration is needed, is a focused prototype with measurable acceptance criteria."

---

[Reading guide](README.md) | [Previous: slide 29](29-production-acceptance.md) | [Next: slide 31](31-second-review-findings.md)
