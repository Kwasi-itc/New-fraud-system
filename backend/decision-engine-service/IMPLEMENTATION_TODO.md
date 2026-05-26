# Implementation TODO

## Current implementation progress

- done: service scaffold, module wiring, health endpoints, migrate command
- done: scenario authoring persistence, services, and APIs
- done: draft iteration persistence, services, and APIs
- done: iteration commit and publication lifecycle persistence, services, and APIs
- done: rule authoring persistence, services, and APIs
- done: AST domain model and dry-run validation against `data-model-service`
- done: real `data-model-service` client and tenant model contract loading
- done: decision persistence and core runtime evaluation skeleton
- done: ingestion-triggered evaluation contract and payload-driven evaluation path
- done: test runs and phantom decision persistence, services, and APIs
- done: workflows and post-decision execution side-effect skeleton
- done: workflow reorder support for the extracted service
- done: structured workflow rule/condition/action authoring model
- done: structured workflow runtime matching and execution creation
- done: scenario copy now clones structured workflows
- done: snoozes and runtime suppression hooks
- done: event/outbox side effects
- done: async execution and scheduled execution foundations
- done: worker processes and execution processing baseline
- done: batch-style worker command for scheduled, async, workflow, screening, scoring, and outbox processing
- done: poll-loop worker mode in addition to one-shot batch mode
- done: screening and scoring integration shells
- done: dispatch/status progression for workflow, screening, scoring, and outbox records
- done: screening execution and scoring request inspection, status update, and retry endpoints
- done: evaluator support for platform-aware helpers (`in_custom_list`, `record_has_tag`, `record_risk_level`, `has_ip_flag`, `past_decision_count`, `related_count`, `related_field`)
- done: service-level tests passing for current evaluator and dispatch baseline
- done: V1 operating decisions captured for worker mode, workflow dispatch boundary, helper-data ownership, and screening/scoring ownership

## Current gaps to close next

- tighten evaluator semantics and expand tests around related-record access and other context-heavy functions
- document and keep aligned the exact contract expected from `data-model-service` and `ingestion-service`
- define how workflow side effects are executed in production beyond the current dispatch shell
- harden observability, retries, and idempotency around the chosen worker and dispatch model
- define provider result payload ingestion beyond the current status/retry lifecycle endpoints

## Planning

- finalize service boundary for `decision-engine-service`
- confirm whether tenant data reads are direct PostgreSQL reads in V1
- confirm whether feature-access checks stay external

## Contracts

- define the exact `data-model-service` assembled model contract required by the decision engine
- define the `TenantDataReader` interface
- define the ingestion-to-decision trigger contract
- define decision-engine outbound event contracts
- define custom-list read contract
- define feature-access read contract if needed
- define case-management action contract for workflow side effects
- define webhook/outbox contract for decision and workflow events
- define payload enrichment contract if evaluation can start from raw payloads
- define watermark ownership for summaries/offloading if retained

## Domain extraction

- done: Marble scenario domain models mapped into service-owned models
- done: Marble decision domain models mapped into service-owned models
- done: Marble workflow models mapped into service-owned models, including structured rule/condition/action support
- done: Marble AST models mapped into service-owned models
- done: Marble test-run and phantom-decision models mapped into service-owned models
- done: Marble rule-snooze models mapped into service-owned models
- map workflow-to-case action semantics and dependencies

## Runtime extraction

- done: port AST function catalog subset needed by the standalone service
- done: port AST evaluation engine into `internal/runtime/ast_eval`
- done: port dry-run validation flow into `internal/runtime/ast_eval`
- port scenario execution flow
- map payload parsing and enrichment behavior used before evaluation
- identify all context-dependent evaluators and their required dependencies
- inventory evaluator dependencies:
  - custom lists
  - monitoring/tag annotations
  - past alerts
  - record risk level
  - IP enrichment

## Persistence planning

- define decision-engine metadata tables
- define decision and rule-execution persistence tables
- define phantom decision tables
- define test-run and test-run summary tables
- define rule snooze tables
- define scheduled execution tables
- define async execution tables
- define workflow tables if copied as-is into the service
- define evaluation offload strategy and storage if preserved
- define event/outbox tables if webhook creation stays in service

## API planning

- done: authoring endpoints baseline
- done: validation endpoint baseline
- done: publication endpoints baseline
- done: test-run endpoints baseline
- done: rule-snooze endpoints baseline
- done: synchronous evaluation endpoint baseline
- done: async execution endpoints baseline
- done: scheduled execution endpoints baseline
- done: workflow endpoints baseline
- done: screening/scoring execution/request lifecycle endpoints
- done: OpenAPI baseline now mirrors the implemented handler and DTO contracts

## Migration planning

- decide which Marble packages move first
- define compatibility period between monolith and new service
- define event mirroring or dual-write strategy if needed

## Operational planning

- define worker processes
- define observability requirements
- define retry and dead-letter behavior
- define idempotency expectations for execution requests
- define publication preparation/index lifecycle ownership
- decide whether workflow case actions execute in-process or through a downstream case service
- decide where security, feature-access, and entitlement checks live in the new boundary
