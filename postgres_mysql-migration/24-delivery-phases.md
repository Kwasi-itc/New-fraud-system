# Slide 24: Proposed delivery phases

[Reading guide](README.md) | [Previous: slide 23](23-operations-and-keycloak.md) | [Next: slide 25](25-feasibility-prototype.md)

Companion to slide 24 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

The phases organize work so that difficult assumptions are tested before production data depends on them. Each phase produces evidence for the next.

**1. Inventory and design**

List tables, queries, routines, jobs, tenant structures, and business invariants. An invariant is a rule that must remain true, such as a case relationship belonging to the correct tenant.

Decide the target naming, types, constraints, and ownership contracts.

**2. Feasibility prototype**

Implement a small but realistic version of the hardest paths. This answers questions such as whether queue atomicity and dynamic DDL recovery can be preserved.

**3. Production implementation**

Build the actual repositories, workers, authorization logic, migration runner, and repair tools. A prototype is not automatically suitable for production wiring.

**4. Data conversion**

Create repeatable tools that copy and transform records while preserving identities and relationships. Add reconciliation checks.

**5. System validation**

Exercise whole service flows, concurrency, failure recovery, and performance against representative data.

**6. Rehearsal and cutover**

Practice the transition, measure duration, validate data, and rehearse rollback. Cutover is the moment the target becomes responsible for production reads and writes.

**7. Stabilization and retirement**

Observe the new system, resolve issues, and retire the old environment only when recovery and acceptance requirements are satisfied.

**What an exit gate means**

An exit gate is a defined condition for proceeding, such as a successful recovery drill. It prevents "we finished the code" from substituting for evidence.

**How to explain this slide aloud**

"We move from inventory to proof, implementation, rehearsal, and operation. Each phase must answer its risks before the next depends on it."

---

[Reading guide](README.md) | [Previous: slide 23](23-operations-and-keycloak.md) | [Next: slide 25](25-feasibility-prototype.md)
