# PostgreSQL usage audit and MySQL migration blueprint

Audit date: 2026-10-22. Source investigation and migration blueprint delivered; runtime validation remains outstanding. Documentation only; no migration is implemented or authorized by this workspace.

Baseline: working tree based on `a2d241a53e082b1ff654634c41e83ecfb7328941`, including pre-existing modified and untracked files. Source evidence describes this working tree, not a deployed database. Existing migration plans and `postgres_mysql-migration/` are historical context and remain untouched; their reverted prototype results are not validation of current code.

Scope: all six backend services, shared investigator package, frontend, workers, schemas, migrations, deployment, development tooling, integration and stress tests. Only files in this directory may change during this investigation.

Start with [findings and recommendations](10_findings_and_recommendations.md), then use the detailed documents below. The source audit contains 35 dependency families, 16 future migration tasks and 14 validation groups. All migration implementation tasks are NOT_STARTED.

| Document | Purpose |
|---|---|
| [01 System inventory](01_system_inventory.md) | Services, libraries, direct/indirect access and infrastructure |
| [02 Usage inventory](02_postgresql_usage_inventory.md) | Stable PG finding IDs, evidence and compatibility mapping |
| [03 Architecture and data model](03_database_architecture_and_data_model.md) | Ownership, schemas, relationships, types, migration lineages |
| [04 Service analysis](04_service_by_service_analysis.md) | Entry-to-database traces and behavioral invariants |
| [05 Compatibility](05_postgresql_to_mysql_compatibility.md) | Proposed replacements, semantic differences and official references |
| [06 Schema/data migration](06_schema_and_data_migration_plan.md) | Transformations, ordering, transfer, reconciliation and rollback |
| [07 Risks and questions](07_risks_dependencies_and_open_questions.md) | Open decisions, severity, evidence and mitigation |
| [08 Validation strategy](08_testing_and_validation_strategy.md) | Existing tests, new concurrency/fault tests and performance gates |
| [09 Roadmap](09_migration_roadmap_and_checklist.md) | Task dependencies, statuses and acceptance criteria |
| [10 Recommendations](10_findings_and_recommendations.md) | Evidence-based decision summary |
| [11 Source evidence](11_source_evidence_catalog.md) | Detailed pattern occurrences with function/line context |
| [12 SQL source](12_schema_source_catalog.md) | Numbered SQL migration/provisioning snapshots |
| [13 Coverage manifest](13_file_coverage_manifest.md) | Enumerated files, classifications and source hashes |
| [14 Schema object index](14_schema_object_index.md) | Table columns and all up-migration object/constraint changes |
| [15 Coverage checks](15_coverage_and_validation.md) | Investigation method, verified coverage and limits |
| [16 Database callsites](16_database_callsite_index.md) | Independent query/transaction-call cross-check |

Repository-wide discovery covered 1,668 text files, 119 SQL files and 1,310 database/transaction callsites (including tests). Counts indicate discovery scope, not a claim that every matching line is PostgreSQL-specific. Critical paths were manually traced; deployment/schema/data/performance claims require runtime evidence.

Maintain stable PG IDs and task/validation links when updating the audit. Recheck source hashes and line references after code changes, distinguish observed behavior from proposals, and record executed/skipped tests with environment/version details. Keep all work in this directory during the documentation phase. Earlier plans remain historical; this workspace does not resume their reverted implementation.

No live database access, application tests, benchmarks, migration commands, or application configuration changes have been performed. MySQL replacement designs require targeted validation against a pinned release.
