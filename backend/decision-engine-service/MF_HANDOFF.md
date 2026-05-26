# MF Handoff

This file is a compact handoff for the current standalone decision engine service in `new/backend/decision-engine-service`.

## What this service is

This is not just an AST evaluator.

It is the standalone extraction of Marble's decisioning domain, with an implemented baseline that already includes:

- scenario and rule authoring
- rule snooze management
- scenario iteration versioning
- publication/live version handling
- AST validation
- runtime evaluation
- decision persistence
- rule execution persistence
- phantom decisions and scenario test runs
- scheduled executions
- async decision executions
- workflow triggering, including structured workflow rule/condition/action authoring
- screening and scoring integration shells plus request/execution lifecycle control
- outbox/event creation tied to decisions and workflows

It does not yet implement the full future-state scope described in the design docs. In particular, payload parsing/enrichment, evaluation offloading, and settled workflow-to-case semantics remain open design areas.

## Current code shape

- HTTP API, repositories, and service orchestration are implemented
- AST runtime and AST validation are extracted into `internal/runtime/ast_eval`
- PostgreSQL metadata schema is implemented with follow-up migrations for workflow ordering and structured workflows
- worker processing supports both one-shot batch mode and long-running poll mode
- A maintained OpenAPI spec and Swagger UI route now exist and mirror the implemented handler and DTO contracts

## What it depends on

- `data-model-service`
  - assembled model contract
  - links, pivots, navigation, field types, revision
- tenant data reads
  - for payload lookups, database access, aggregators, pivots, scheduled scans
- custom list reads
- feature-access checks where still external
- case-management integration if workflow actions stay cross-service
- payload enrichment dependencies if evaluation starts from raw payloads
- `ingestion-service`
  - for post-ingestion evaluation triggering

## Key design rule

The decision engine should be the execution authority, while `data-model-service` remains schema authority and `ingestion-service` remains write authority.

## Immediate next implementation tasks

- finalize the exact contract from `data-model-service`
- finalize the tenant data read abstraction beyond the current baseline
- define the production shape of workflow side effects
- tighten relation-heavy evaluator semantics and coverage
- decide the provider result payload contract for screening and scoring beyond the current status/retry endpoints
- decide whether feature-access stays external and whether tenant-data reads remain HTTP-based or move to direct reads
