# Slide 16: Business rules and authorization inside the database

[Reading guide](README.md) | [Previous: slide 15](15-text-comparison-and-keys.md) | [Next: slide 17](17-river-job-processing.md)

Companion to slide 16 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

Some business rules are implemented in database functions and triggers rather than only in HTTP handlers.

A function is called explicitly. A trigger runs automatically for specified table events. PL/pgSQL is the PostgreSQL language used by the routines discussed here.

**Examples in our system**

Assignment logic prevents an investigator from exceeding capacity. Screening routines reconcile source results into decision records. Exclusion audit guards reject modifications that would rewrite immutable history.

Identity functions evaluate users, membership, roles, tenant status, session validity, and membership versions.

**Why authorization has concurrency concerns**

Consider this simplified sequence:

1. A request checks that an investigator may modify a case.
2. An administrator revokes the investigator's access.
3. The original request performs the modification.

Whether step 3 is permitted must follow a defined contract. If authorization and mutation are disconnected, a migration can introduce a gap that was previously coordinated by locks.

Some existing identity functions acquire shared locks. The relevant operations must preserve their intended ordering with user, membership, session, and domain changes.

**Where should replacement rules live?**

MySQL routines are one option; service-owned transactional code is another. Whichever is chosen, every mutation path must enforce the rule. Moving a trigger into one API endpoint is insufficient if workers or other authorized writers can bypass it.

Do not hold locks across a remote authorization call. Prefer a design that fits the documented shared-database ownership contract.

**How to explain this slide aloud**

"Database code currently protects permissions and business invariants. We must preserve those guarantees across every writer, especially when access or records change concurrently."

Evidence: [identity functions](../backend/auth-service/internal/platform/schema.sql), [assignment trigger](../backend/case-manager-service/internal/migrations/metadata/000007_assignment_sla.up.sql), [exclusion guards](../backend/screening-service/internal/migrations/metadata/000009_object_exclusions.up.sql).

---

[Reading guide](README.md) | [Previous: slide 15](15-text-comparison-and-keys.md) | [Next: slide 17](17-river-job-processing.md)
