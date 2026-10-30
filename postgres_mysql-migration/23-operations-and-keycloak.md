# Slide 23: Operations and identity-provider migration

[Reading guide](README.md) | [Previous: slide 22](22-benefits-and-costs.md) | [Next: slide 24](24-delivery-phases.md)

Companion to slide 23 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

Application correctness also depends on how the database is configured, monitored, backed up, and restored. These responsibilities continue after the code compiles.

**Settings that need explicit choices**

InnoDB is the transaction-capable storage-engine baseline. SQL mode affects acceptance of invalid or out-of-range values. Character set and collation affect text. Time zone and isolation affect dates and concurrent behavior.

Pinning a release means choosing a specific supported target rather than relying on an unspecified "MySQL" installation.

**One connection budget**

A connection pool belongs to a process, but database capacity is shared.

For example, ten hypothetical processes each allowing twenty connections could request two hundred connections in total. Worker and API replicas multiply the budget. Actual limits need measurement and headroom for migrations and operations.

**Recovery terms**

A backup is a recoverable copy. Point-in-time recovery restores to a chosen time using the necessary backup and change history. Failover moves responsibility to another database instance. Each needs a tested procedure in the selected environment.

A read replica may lag behind the writer. Stale reads are particularly important for session revocation and mutation decisions.

**Why Keycloak needs separate attention**

Keycloak owns its provider tables and migration conventions. The application's identity tables are a different data set.

Select a supported process for the actual Keycloak version and verify what it preserves. Do not assume an export includes every credential, session, or identity-state category.

This document does not establish a version-specific Keycloak migration procedure.

**How to explain this slide aloud**

"The target must be operable and recoverable, including authentication. Database settings, connection budgets, and provider-owned data are part of the migration."

Evidence: [local provider and application configuration](../docker-compose.yml).

---

[Reading guide](README.md) | [Previous: slide 22](22-benefits-and-costs.md) | [Next: slide 24](24-delivery-phases.md)
