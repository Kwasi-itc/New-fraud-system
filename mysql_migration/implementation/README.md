# MySQL migration implementation design

Prepared 2026-10-22. **Implementation has started following user authorization.** DB-01/PX-01 are IN_PROGRESS for the isolated connection/transaction foundation; IN-01 is IN_PROGRESS for portable ingestion queue boundaries; DM-01 is IN_PROGRESS for transaction and queue boundary preparation; all ADRs remain proposed. See the [execution log](16_execution_log.md) for implemented code, test evidence and remaining gaps. Existing services still use PostgreSQL.

This workspace builds on the preserved [PostgreSQL audit](../README.md). The audit records existing behavior and exact evidence; this design specifies recommended replacements, alternatives, invariants, tests and execution dependencies. Future execution must satisfy the gates here before any coordinated runtime transition. The original design phase changed only documentation; the newly authorized execution phase adds the backend/mysqlstore foundation and prepares ingestion transaction ports while retaining PostgreSQL runtime.

## Recommended architecture

Preserve one logical application database and clear owner contracts. Target a pinned MySQL8.4 LTS/InnoDB deployment, with explicit transaction isolation, tenant-consistent constraints and narrowly privileged accounts. Replace namespaces through a physical-object registry; use recoverable intent/verify/publish for runtime DDL; preserve transactional work delivery and fenced workers. Reorder immutable review/exclusion writes for immediate FKs, preserve exact uniqueness rather than prefix/hash-only approximations, and keep cross-owner review effects in one transaction.

The queue implementation, long-key dictionary, privileged owner routines and runtime DDL protocol require focused proof before acceptance. Their complexity is explicit, not assumed solved. Provider storage/state transfer, deployment/RPO/RTO, numeric SLOs, data ranges and exact server/driver versions remain decisions. No comparative performance benefit is claimed.

## Documents and status

All design documents below are **NEEDS_REVIEW**; the validation report records completed documentation checks only. File names are navigation, not execution order.

| Document | Purpose |
|---|---|
| [01_target_architecture.md](01_target_architecture.md) | Target engine, topology, ownership, pooling and operations |
| [02_postgresql_feature_replacements.md](02_postgresql_feature_replacements.md) | 17 replacement designs covering PG-01 through PG-35 with source evidence |
| [03_architecture_decision_records.md](03_architecture_decision_records.md) | 12 proposed ADRs with alternatives and validation gates |
| [04_schema_and_data_access_design.md](04_schema_and_data_access_design.md) | Types, exact keys, queries, constraints and migration contracts |
| [05_security_and_integrity_requirements.md](05_security_and_integrity_requirements.md) | Security, authorization, integrity and least-privilege controls |
| [06_performance_and_scalability_design.md](06_performance_and_scalability_design.md) | Historical baseline limits and nine measured workload specifications |
| [07_transaction_and_recovery_protocols.md](07_transaction_and_recovery_protocols.md) | Six concrete transaction, DDL, queue and recovery protocols |
| [08_risks_and_decision_gates.md](08_risks_and_decision_gates.md) | Risk register and blocking decisions |
| [09_validation_and_proof_of_concept_plan.md](09_validation_and_proof_of_concept_plan.md) | 11 POCs and mandatory validation layers |
| [10_implementation_roadmap.md](10_implementation_roadmap.md) | 17 ordered implementation tasks with dependencies and acceptance |
| [11_data_transfer_and_cutover.md](11_data_transfer_and_cutover.md) | Data transformation, coordinated cutover and rollback boundaries |
| [12_traceability_and_coverage.md](12_traceability_and_coverage.md) | Finding-to-design/task/test coverage and reusable test links |
| [13_authoritative_references.md](13_authoritative_references.md) | 19 authoritative references and historical evidence boundaries |
| [14_documentation_validation.md](14_documentation_validation.md) | Documentation validation and preserved audit hashes |
| [15_target_object_map.md](15_target_object_map.md) | 88 source table declarations mapped to proposed physical objects |

## Module plans

All module designs are NEEDS_REVIEW. Shared-foundation, ingestion and data-model preparation are in progress; MySQL service adapters and other module conversions remain NOT_STARTED.

- [Data model, metadata and dynamic schema](modules/01_data_model_service.md)
- [Ingestion, upload/deferred work and tenant reads](modules/02_ingestion_service.md)
- [Decision evaluation, workflows and historical reads](modules/03_decision_engine_service.md)
- [Screening, continuous runs, history and exclusion ledger](modules/04_screening_service.md)
- [Cases and shared investigator transactions](modules/05_case_management_and_investigation.md)
- [Application identity and external provider](modules/06_authentication_and_authorization.md)
- [Shared driver, transactions, jobs and migration contracts](modules/07_shared_database_and_workers.md)
- [Deployment, operations, recovery and provider provisioning](modules/08_infrastructure_and_operations.md)
- [Frontend, HTTP integrations and validation tooling](modules/09_frontend_integrations_and_tests.md)

## Phases and maintenance

Baseline/live inventory -> isolated POCs/ADR review -> target schema/shared foundation/identity -> data-model -> ingestion -> decision -> screening -> cases -> full-system validation -> paired performance/recovery/data rehearsal -> separately approved coordinated cutover. Provider and deployment work have explicit prerequisites in the roadmap. Developing one module first does not permit moving its live tables while shared PostgreSQL consumers remain active.

For future changes, keep PG finding IDs stable; update the replacement, ADR, module task and validation case together. Add evidence with source revision, path/line, exact engine versions and run results. Never convert a proposed ADR to accepted because a document exists. Record any change to business behavior or security as a distinct reviewed decision. Preserve audit evidence and historical plans; verified corrections should be explicit and dated.

