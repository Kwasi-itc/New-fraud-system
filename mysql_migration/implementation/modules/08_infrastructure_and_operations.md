# Deployment, operations, recovery and provider provisioning

Status: **NEEDS_REVIEW** (design); implementation **NOT_STARTED**. Primary task: OP-01; prerequisites: B-01, PX-01, SC-01, DB-01, Q-01. Findings: PG-02/04/27/29/30/31/32.

## Verified execution path

[Source entry point](../../../docker-compose.yml#L36). Root Compose runs shared postgres16; optional local Keycloak has its own provisioning SQL. Standalone/scale variants, Dockerfiles, Makefiles, scripts and stress tools contain additional PostgreSQL assumptions. Production CI/deployment outside the repository is not known. Full source occurrences are in the [audit catalog](../../11_source_evidence_catalog.md).

## Target implementation

Prepare MySQL variants in future implementation with pinned image/config, health/readiness based on migrations and permissions, TLS/secrets, owner accounts and restore tooling. Replace psql/pg_dump/pg_restore/catalog observation, PostgreSQL health and migration invocations. All replicas/read/worker URLs must preserve one dataset. Version-controlled grants and routines must survive restore. Provider provisioning is gated by AU-02. Existing forced legacy-database drop paths must never target application data.

Use the common [schema contract](../04_schema_and_data_access_design.md), [security requirements](../05_security_and_integrity_requirements.md) and [transaction protocols](../07_transaction_and_recovery_protocols.md). These are required acceptance inputs, not already implemented helpers.

## Implementation checklist

- [ ] **OP-01.1 ? NOT_STARTED:** Inventory every entrypoint/env/Compose/Make/script and external deployment owner; define secure secret and network injection without logging values.
- [ ] **OP-01.2 ? NOT_STARTED:** Provision disposable and production templates, pool budgets, grants, migration ordering, readiness and monitoring; include auth/provider omitted from scale harness.
- [ ] **OP-01.3 ? NOT_STARTED:** Implement encrypted backup/PITR/restore, failover fencing, schema/queue repair and credential rotation runbooks; measure recovery.
- [ ] **OP-01.4 ? NOT_STARTED:** Convert stress observation and CI targets to exact engine; isolate test databases and enforce target checks before destructive setup.

## Tests and acceptance

OP-01 and PERF-09: fresh install/upgrade/restart, wrong credentials/TLS, partial migration, grant restore, primary failure, source/target URL mix detection, backup restore and negative destructive-target tests. External deployment review remains a required owner artifact.

No existing Compose or secret changed in this design phase. HA choice and numeric RPO/RTO require G0. Backups are not verified until restored with routines/grants/keys and business consistency checks.

## Delivery and rollback dependency

Implement and validate in disposable environments. All shared consumers must be compatible before live activation; an individual module passing its suite does not authorize a partial cross-engine production cutover. Preserve old adapters/compatible API artifacts through rehearsal as needed, with a single chosen runtime authority. The [roadmap](../10_implementation_roadmap.md) owns task dependencies and [cutover plan](../11_data_transfer_and_cutover.md) owns data rollback. Module schema rollback cannot undo externally delivered effects.
