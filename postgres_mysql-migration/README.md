# PostgreSQL to MySQL: slide-by-slide explanations

This folder explains all 32 slides in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md), using plain language, examples, and connections to this system.

Prepared: 2026-10-21. Comparison baseline: the repository's PostgreSQL 16 configuration and MySQL 8.4 with InnoDB. This is learning and assessment material, not a migration implementation.

**How to read it**

- Start with [slide 3: current architecture](03-current-architecture.md) if schemas, namespaces, services, River, and Keycloak are unfamiliar.
- Read slides in number order for the complete explanation.
- Each file defines relevant terms, develops an example, and includes wording you can use when presenting.
- Use the previous/next links to move between files.
- Keep the [glossary](GLOSSARY.md) available when reading technical sections.
- Examples, counts, and proposed alternatives are qualified; no illustrative number is a measured production result.

**Reading routes**

| Topic | Slides |
|---|---|
| Decision and current architecture | 1-6 |
| Database structures and guarantees | 7-16 |
| Jobs, application integration, and concurrency | 17-20 |
| Performance, cost, and operations | 21-23 |
| Implementation approach, transfer, and recovery | 24-29 |
| Recommendation, additions, and assessment limits | 30-32 |

**All explanations**

1. [What are we deciding?](01-what-are-we-deciding.md)
2. [Executive summary](02-executive-summary.md)
3. [Current architecture](03-current-architecture.md)
4. [How much of the application is affected?](04-application-scope.md)
5. [What MySQL already supports](05-what-mysql-supports.md)
6. [The compatibility gaps at a glance](06-compatibility-gaps.md)
7. [Schemas and tenant organization](07-schemas-and-tenants.md)
8. [Partial indexes: indexing only relevant records](08-partial-indexes.md)
9. [Conditional uniqueness: protecting business rules](09-conditional-uniqueness.md)
10. [Advisory locks: coordinating logical operations](10-advisory-locks.md)
11. [Schema changes and partial failure](11-schema-changes-and-failure.md)
12. [Deferred foreign keys](12-deferred-foreign-keys.md)
13. [JSONB, arrays, and data representation](13-jsonb-arrays-and-types.md)
14. [Timestamps and infinity](14-timestamps-and-infinity.md)
15. [Text comparison and key design](15-text-comparison-and-keys.md)
16. [Business rules and authorization inside the database](16-business-rules-and-authorization.md)
17. [River is a migration workstream](17-river-job-processing.md)
18. [Reliable processing: preserve atomic enqueueing](18-atomic-enqueueing-and-outbox.md)
19. [Drivers, SQL, errors, and repair tooling](19-drivers-errors-and-tools.md)
20. [Concurrency and transaction isolation](20-concurrency-and-isolation.md)
21. [Performance: what we must measure](21-performance-measurement.md)
22. [Potential benefits and costs](22-benefits-and-costs.md)
23. [Operations and identity-provider migration](23-operations-and-keycloak.md)
24. [Proposed delivery phases](24-delivery-phases.md)
25. [What the feasibility prototype must prove](25-feasibility-prototype.md)
26. [Data conversion and reconciliation](26-data-conversion-and-reconciliation.md)
27. [Cutover choices](27-cutover-choices.md)
28. [Rollback is a data problem](28-rollback-and-recovery.md)
29. [Acceptance criteria before production](29-production-acceptance.md)
30. [Recommendation and decision inputs](30-recommendation-and-inputs.md)
31. [What the second review added](31-second-review-findings.md)
32. [Current status and limits of this assessment](32-status-and-limitations.md)

**Important distinctions**

- A PostgreSQL server, database, schema, and table are different layers.
- The five named application schemas are not necessarily all schemas: tenant, queue, and provider schemas also matter.
- Configured behavior in the repository is not proof of live deployment state.
- Historical prototype results in the existing plans describe reverted work.
- MySQL alternatives in this guide need implementation and validation before they can be claimed as equivalent.
- This folder adds explanations only. It does not change application code, database configuration, or data.

**Evidence and further reading**

Source links appear in the relevant explanations. The [presentation appendix](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md) contains the consolidated repository evidence. Its slide numbers are the reference for this folder.

The [system migration plan](../POSTGRES_TO_MYSQL_MIGRATION_PLAN.md) and [data-model plan](../DATA_MODEL_MYSQL_IMPLEMENTATION_PLAN.md) contain historical context; read their revert notices before interpreting implementation or test statements.
