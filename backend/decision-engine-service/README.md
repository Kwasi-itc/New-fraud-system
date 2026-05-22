# Decision Engine Service

Standalone Go service for Marble's decision engine domain, extracted from the monolithic `api` service and designed to work alongside `data-model-service` and `ingestion-service`.

Current location in the workspace:

- `new/backend/decision-engine-service`

This folder is currently for planning and service design only. No implementation code has been added yet.

## Purpose

The service is intended to own the full Marble decision-engine behavior:

- scenario authoring
- scenario iteration authoring
- rule authoring
- rule snoozes
- screening config authoring
- scenario test runs
- phantom decisions for test runs
- AST validation
- publication and live-version management
- publication preparation checks
- runtime scenario evaluation
- decision creation and persistence
- rule execution persistence
- analytics field capture for decisions
- optional offloading and rehydration of large rule-evaluation payloads
- test-run summary generation
- scheduled execution orchestration
- async batch decision execution
- workflow triggering after decisions
- case-creation and case-attachment workflow actions
- decision-created and workflow-related webhook/event creation
- payload parsing and enrichment on decision-ingest evaluation paths
- optional screening integration
- optional scoring integration

Notably separate from this scope unless explicitly pulled in later:

- Marble's broader `continuous_screening` subsystem and dataset-update workers

## Intended service boundary

The decision engine service should own:

- executable scenario definitions and versions
- runtime evaluation logic
- decisions and execution history
- scheduling and async execution state
- workflow definitions and dispatch triggers

The decision engine service should not own:

- tenant schema management
- physical table and field lifecycle
- raw ingestion writes
- being the source of truth for tenant data storage layout

## Expected dependencies

The service is expected to depend on:

- `data-model-service`
  - assembled tenant model contract
  - links, pivots, navigation options, field typing, tenant/model revision
- tenant data read access
  - direct PostgreSQL tenant-schema reads or an equivalent read abstraction
- `ingestion-service`
  - post-ingestion trigger integration, likely through events or explicit callbacks
- optional external services
  - screening providers
  - scoring services
  - webhook/event delivery infrastructure
  - case management / case review infrastructure if kept external

## Planned documents

- [DECISION_ENGINE_SERVICE_EXTRACTION_DESIGN.md](./DECISION_ENGINE_SERVICE_EXTRACTION_DESIGN.md)
- [DECISION_ENGINE_SERVICE_IMPLEMENTATION_BLUEPRINT.md](./DECISION_ENGINE_SERVICE_IMPLEMENTATION_BLUEPRINT.md)
- [DECISION_ENGINE_SERVICE_INTEGRATION_CONTRACTS.md](./DECISION_ENGINE_SERVICE_INTEGRATION_CONTRACTS.md)
- [DECISION_ENGINE_SERVICE_DOMAIN_BREAKDOWN.md](./DECISION_ENGINE_SERVICE_DOMAIN_BREAKDOWN.md)
- [IMPLEMENTATION_TODO.md](./IMPLEMENTATION_TODO.md)
- [MF_HANDOFF.md](./MF_HANDOFF.md)

## Current status

Planning only.
