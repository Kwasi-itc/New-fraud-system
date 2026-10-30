# Slide 31: What the second review added

[Reading guide](README.md) | [Previous: slide 30](30-recommendation-and-inputs.md) | [Next: slide 32](32-status-and-limitations.md)

Companion to slide 31 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

The earlier presentation covered the major engine differences. A second source review identified additional dependencies that deserved explicit explanation.

**Queues were a separate problem**

River's configured PostgreSQL integration and transactional enqueueing mean we must preserve job lifecycle and atomicity, not only rewrite worker SELECT statements. See slides 17 and 18.

**Integrity included deferred relationships**

Screening exclusion migrations defer selected foreign-key checks. Immediate checking in MySQL changes valid write ordering. See slide 12.

**Security included database routines**

Identity checks and locks coordinate permissions with concurrent changes. Exclusion triggers protect audit history. See slide 16.

**Data conversion included meaning**

Infinity, timestamp precision, collation, indexed text, and UUID encoding can affect meaning even when a value appears to copy successfully. See slides 14 and 15.

**Operations included recovery paths**

Repair commands, introspection, database error classification, and benchmark observation tools also depend on PostgreSQL. See slides 19 and 21.

**History needed qualification**

Existing plans report that an earlier prototype was reverted. Its described test results belong to that historical work; they do not establish current MySQL readiness.

**What this review does not establish**

Finding these gaps does not prove that no other gaps exist. An implementation project still needs a complete statement and dependency inventory plus deployed-schema inspection.

**How to explain this slide aloud**

"The second review expanded the scope from visible schema differences to queues, security, data meaning, and operational recovery. Those are essential parts of preserving the system."

---

[Reading guide](README.md) | [Previous: slide 30](30-recommendation-and-inputs.md) | [Next: slide 32](32-status-and-limitations.md)
