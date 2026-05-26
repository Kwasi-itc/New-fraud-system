# Decision Engine Service Implementation Blueprint

## Service shape

The service should be built as a standalone Go backend consistent with the `data-model-service` and `ingestion-service` layout.

## Current status note

Parts of this blueprint are now implemented. Treat this document as the intended architecture and package direction, not as an exact reflection of the current file tree.

Current notable differences from the original proposed layout:

- `internal/runtime/ast_eval` now exists and contains both evaluation and validation logic
- `runtime/evaluators` has not been split out yet
- a dedicated `domain/testrun` package was not created; test-run types currently live under `domain/scenario` and `domain/decision`
- worker behavior currently lives in `cmd/worker` rather than a dedicated `internal/worker` package
- screening, scoring, and platform helper integrations are implemented as baseline service/repository flows rather than their full planned future-state boundaries

## Proposed layout

```text
new/backend/decision-engine-service/
  cmd/
    server/
    worker/
    migrate/
  internal/
    app/
    domain/
      ast/
      decision/
      execution/
      integration/
      snooze/
      scenario/
      screening/
      scoring/
      testrun/
      workflow/
    httpapi/
      dto/
      handlers/
    ports/
    runtime/
      ast_eval/
      environment/
      evaluators/
    service/
    store/
      postgres/
    clients/
      datamodel/
      ingestion/
      customlists/
      screening/
      scoring/
    worker/
  README.md
  DECISION_ENGINE_SERVICE_EXTRACTION_DESIGN.md
  DECISION_ENGINE_SERVICE_IMPLEMENTATION_BLUEPRINT.md
  DECISION_ENGINE_SERVICE_INTEGRATION_CONTRACTS.md
  DECISION_ENGINE_SERVICE_DOMAIN_BREAKDOWN.md
  IMPLEMENTATION_TODO.md
  MF_HANDOFF.md
```

## Internal modules

### `domain/ast`

Should contain:

- AST node model
- function catalog
- evaluation DTOs
- execution errors

### `runtime/ast_eval`

Current implementation contains:

- AST execution engine
- dry-run and runtime evaluation support

Still open:

- cache support
- short-circuit and cost optimization logic

### `runtime/evaluators`

Planned split, not yet implemented as a separate package.

It should contain evaluator implementations grouped by kind:

- pure evaluators
- payload evaluators
- tenant-data evaluators
- cross-service evaluators

### `domain/scenario`

Should contain:

- scenario
- scenario iteration
- rule
- publication
- scheduled execution
- async decision execution
- publication preparation state

### `domain/testrun`

Planned domain package, not currently present as a separate package.

It should contain:

- scenario test run
- phantom decision
- test-run summary

### `domain/snooze`

Should contain:

- snooze group
- rule snooze
- snooze query result models

### `domain/decision`

Should contain:

- decision
- rule execution
- outcome mapping
- decision events
- analytics field capture metadata
- evaluation offload metadata if retained
- webhook event payload models tied to decision outcomes if retained here

### `domain/workflow`

Should contain:

- workflow definitions
- workflow conditions
- workflow actions
- workflow execution result
- workflow-to-case action models

### `domain/integration`

Should contain boundary models for:

- decision-created event payloads
- workflow-generated case event payloads
- offloaded evaluation references

### `service`

Should contain the application layer:

- scenario service
- iteration service
- publication service
- preparation service
- validation service
- evaluation service
- decision service
- test-run service
- snooze service
- payload validation/enrichment service
- scheduled execution service
- async execution service
- workflow service

### `ports`

Should define interfaces for:

- scenario repositories
- decision repositories
- workflow repositories
- test-run repositories
- snooze repositories
- `data-model-service` reader
- tenant data reader
- custom list reader
- feature access reader
- payload enricher
- inbox / case-review settings reader if workflow-created cases depend on them
- case-management writer or workflow case-action port
- webhook event creator / outbox writer
- screening provider
- scoring provider
- event publisher

### `clients`

Should hold external client adapters:

- `datamodel`
- `ingestion`
- `case_management`
- `screening`
- `scoring`

### `store/postgres`

Should hold service-owned persistence adapters.

## Planned runtime modes

The service should support:

- synchronous evaluation
- async batch evaluation
- scheduled evaluation
- publication-time validation

## Planned workers

Background workers should likely include:

- scheduled execution worker
- async batch decision execution worker
- workflow dispatch worker
- test-run summary worker
- optional evaluation offloading worker
- optional screening follow-up worker
- optional webhook dispatch/outbox relay worker if not delegated elsewhere
- optional watermark maintenance workers where summary/offloading logic requires them

Current implementation note:

- the current `cmd/worker` runs scheduled execution, async execution, workflow dispatch, screening dispatch, scoring dispatch, and outbox dispatch once per invocation

## Suggested phases

### Phase 1

- create service scaffold
- port AST model
- port AST runtime
- port scenario validation
- inventory evaluator dependencies that require external reads

### Phase 2

- port scenario/rule/iteration/publication domain
- port rule snoozes
- port test-run and phantom-decision domain
- add service-owned persistence
- expose authoring APIs

### Phase 3

- add evaluation and decision persistence
- add phantom decision persistence
- add `data-model-service` integration
- add tenant data read abstraction
- add custom list and decision-history dependency abstractions
- add case-management and webhook side-effect abstractions

### Phase 4

- add async execution
- add scheduled execution
- add test-run summary workers
- add workflow dispatch

### Phase 5

- add screening integration
- add scoring integration

## Blueprint rule

Keep pure evaluation concerns isolated from platform orchestration concerns.

The AST runtime should remain reusable inside the service, but the service boundary should stay focused on decisions, not just formulas.
