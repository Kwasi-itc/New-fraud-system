# Slide 22: Potential benefits and costs

[Reading guide](README.md) | [Previous: slide 21](21-performance-measurement.md) | [Next: slide 23](23-operations-and-keycloak.md)

Companion to slide 22 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

This slide makes the business case explicit. A migration may be justified even when it does not make queries faster, for example if a required customer environment supports MySQL.

**Potential benefits are conditional**

Existing MySQL operational expertise can simplify support if that expertise is actually available. Hosting alignment can matter if the deployment requirement is real. Lower infrastructure cost needs comparable pricing, capacity, resilience, and support assumptions.

None of those benefits was measured or confirmed by this review.

**What total cost includes**

Total cost includes more than the monthly database bill:

- Engineering implementation and testing.
- Temporary environments for both engines.
- Data transfer and cutover preparation.
- Training, monitoring, and on-call support.
- Possible downtime and recovery work.
- Product features delayed while engineers migrate the platform.

**An illustrative decision**

If infrastructure savings are modest but migration requires significant engineering work and carries operational risk, savings alone may not justify it. A mandatory customer requirement could change the decision even with the same technical costs.

The point is to compare the actual benefit against the complete cost, not to assume one answer for every organization.

**Why permanent dual support is different**

Maintaining both PostgreSQL and MySQL indefinitely requires ongoing testing and implementation work for both. Every future schema feature or query change may need two paths.

Temporary coexistence during migration does not imply a commitment to permanent dual-engine support.

**How to explain this slide aloud**

"The strongest case for migration is a concrete requirement or measured benefit. We should compare it with engineering, operational, and opportunity costs."

---

[Reading guide](README.md) | [Previous: slide 21](21-performance-measurement.md) | [Next: slide 23](23-operations-and-keycloak.md)
