# Decision Engine Service Extraction Design

## Goal

Extract Marble's full decision-engine domain from the monolithic `api` service into a standalone backend that works with:

- `new/backend/data-model-service`
- `new/backend/ingestion-service`

The target service should preserve Marble's current rule-engine behavior, but reframe it as a standalone bounded context with explicit contracts and dependencies.

## Why this is a service and not just a library

In Marble, the rule engine is not only an AST evaluator.

It currently includes:

- scenario and rule authoring
- rule snooze management
- versioning and publication
- publication preparation checks
- AST validation
- runtime execution
- decision persistence
- analytics field persistence
- optional offloading and rehydration of rule evaluations
- phantom decision persistence for test runs
- test run orchestration and summarization
- scheduling
- async execution orchestration
- workflows
- workflow-driven case creation and case attachment
- decision and workflow webhook/event creation
- payload parsing and enrichment for raw evaluation requests
- screening coordination
- scoring coordination

That is operationally and logically a service boundary, not a utility package.

## Current Marble shape

Today this behavior is distributed across the monolithic `api` service.

Major runtime slices include:

- scenario authoring and versioning
- scenario test runs and phantom decisions
- AST runtime and validation
- evaluation of triggers and rules
- screening execution
- decision creation and persistence
- analytics field persistence and evaluation offloading
- rule snoozes
- publication preparation/index requirements
- scheduled execution orchestration
- async decision execution
- workflow triggering
- workflow-driven case creation / add-to-case behavior
- decision and workflow webhook creation
- scoring triggers

## Core domain concepts

The extracted service should preserve these core concepts:

- `Scenario`
  - top-level executable unit
  - tied to a trigger object type
- `ScenarioIteration`
  - versioned executable snapshot
  - contains trigger condition, rules, thresholds, schedule, screenings
- `Rule`
  - boolean AST that may modify decision score
- `RuleSnooze`
  - pivot-scoped suppression of rule hits through snooze groups
- `PublishedScenarioIteration`
  - live executable version
- `ScenarioTestRun`
  - comparison run between live and phantom iterations
- `PhantomDecision`
  - test-run-only execution result
- `Decision`
  - runtime result for one scenario on one object
- `RuleExecution`
  - per-rule execution output
- `WorkflowExecution`
  - records post-decision actions such as case creation and webhook generation
- `ScheduledExecution`
  - batch execution state for scheduled scenarios
- `AsyncDecisionExecution`
  - async batch execution state for on-demand batches
- `Workflow`
  - post-decision automation rules

## Target boundary

The service should own:

- scenarios
- scenario iterations
- rules
- rule snoozes
- screening configs
- scenario test runs
- phantom decisions
- test-run summaries
- publications
- preparation-state handling for publication and test-run activation
- decisions
- rule executions
- scheduled executions
- async decision executions
- workflow definitions
- workflow execution triggers
- evaluation runtime
- AST validation logic

The service should not own:

- data model authoring
- tenant provisioning
- ingestion write APIs
- tenant physical schema lifecycle

## Architectural principle

The split should be:

- `data-model-service`
  - schema authority
  - model metadata authority
- `ingestion-service`
  - raw write authority
  - intake and upsert authority
- `decision-engine-service`
  - execution authority
  - decision history authority

## Major internal capability areas

### 1. Authoring

Responsible for:

- scenario CRUD
- scenario iteration CRUD
- rule CRUD
- rule snooze CRUD and evaluation-time snooze lookups
- screening config CRUD
- workflow CRUD

### 2. Versioning and publication

Responsible for:

- draft iteration creation
- iteration commit/versioning
- publish/unpublish live version management
- preparation checks before activation
- test-run activation checks against required indexes

### 3. Validation

Responsible for:

- AST dry-run validation
- rule return-type checks
- trigger return-type checks
- screening query validation
- workflow AST validation
- validation against custom-list, monitoring-list, risk-level, and tenant-data access expectations
- payload parsing/enrichment behavior used before evaluation on raw-ingest paths

### 4. Runtime execution

Responsible for:

- trigger evaluation
- concurrent rule evaluation
- score accumulation
- threshold-to-outcome mapping
- screening coordination
- pivot-aware execution
- snooze-aware rule suppression
- test-run execution against phantom iterations

### 5. Decision lifecycle

Responsible for:

- synchronous decision creation
- multi-scenario decision creation
- persistence of decisions and rule executions
- persistence of phantom decisions and test-run summaries
- workflow dispatch
- event emission
- decision webhook/event emission
- workflow webhook/event emission
- analytics field capture
- optional evaluation offloading strategy
- watermark-driven summary/offload maintenance where retained

### 6. Batch orchestration

Responsible for:

- scheduled executions
- async batch decision executions
- retries and execution status management
- test-run summary workers

## Runtime dependencies

The service runtime will need:

- tenant model contract from `data-model-service`
- tenant record reads from the tenant data store
- custom list reads
- decision/history reads for functions like past alerts
- optional feature-access checks for screening/scoring-sensitive behavior
- payload enrichment dependencies used during raw payload parsing
- case-management integration for workflow actions if cases stay outside this service
- inbox and AI case-review dependencies for workflow-created cases if those remain external
- webhook delivery or event-outbox integration for decision and case-related events
- optional screening provider
- optional screening match-enrichment follow-up worker/provider
- optional scoring provider or local scoring subsystem
- durable persistence for decisions and execution state

## Key extraction constraints

### Keep Marble semantics

The extracted service should preserve:

- draft vs committed vs published iteration lifecycle
- AST evaluator behavior
- threshold behavior
- rule-hit persistence behavior
- rule snooze behavior
- phantom decision/test-run behavior
- publication preparation behavior
- scheduled execution behavior
- async decision execution behavior
- workflow-to-case behavior
- webhook event creation behavior
- offloaded decision-evaluation behavior where retained

### Remove direct coupling to Marble's internal tables where possible

The extracted service should replace:

- direct data-model table reads

with:

- service contracts from `data-model-service`

It may still directly read tenant data tables in V1 if that is the most practical path, but this should be behind a port interface.

## Recommended service name

Use `decision-engine-service`.

This is more accurate than `rule-engine-service` because the extracted behavior includes much more than rule evaluation.

## V1 service objective

V1 should be a standalone service that:

- can author and publish executable scenarios
- can activate and summarize scenario test runs
- can validate scenario ASTs using the tenant model contract
- can evaluate live scenarios against tenant records
- can evaluate phantom/test-run iterations
- can persist decisions and rule executions
- can persist phantom decisions and test-run summaries
- can be triggered by ingestion outcomes
- can run scheduled and async batches

## Non-goals for the planning phase

- implementing code
- deciding every endpoint payload field
- deciding every database index

Those belong in the blueprint and later implementation planning.
