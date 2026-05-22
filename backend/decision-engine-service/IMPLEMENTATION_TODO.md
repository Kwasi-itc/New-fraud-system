# Implementation TODO

## Planning

- finalize service boundary for `decision-engine-service`
- confirm whether scoring is inside this service for V1 or remains an integration point
- confirm whether screening is inside this service for V1 or remains an integration point
- confirm whether tenant data reads are direct PostgreSQL reads in V1
- confirm whether custom lists remain external or move into the service boundary
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

- map Marble scenario domain models to new service-owned models
- map Marble decision domain models to new service-owned models
- map Marble workflow models to new service-owned models
- map Marble AST models to new service-owned models
- map Marble test-run and phantom-decision models to new service-owned models
- map Marble rule-snooze models to new service-owned models
- map workflow-to-case action semantics and dependencies

## Runtime extraction

- port AST function catalog
- port AST evaluation engine
- port dry-run validation flow
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

- define authoring endpoints
- define validation endpoints
- define publication endpoints
- define test-run endpoints
- define rule-snooze endpoints
- define synchronous evaluation endpoints
- define async execution endpoints
- define scheduled execution endpoints
- define workflow endpoints

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
