# Decision Engine Service Domain Breakdown

## Core bounded context

The extracted service is best understood as a `decision engine` bounded context.

## Domain areas

### Scenario authoring

Owns:

- scenario metadata
- trigger object type
- scenario lifecycle flags

### Scenario iteration authoring

Owns:

- draft iterations
- committed iterations
- version numbers
- thresholds
- schedule expressions

### Rule authoring

Owns:

- rule AST
- score modifiers
- rule grouping
- stable rule identifiers
- snooze grouping references

### Rule snoozes

Owns:

- pivot-scoped snoozes for rule groups
- active snooze lookup at evaluation time
- snooze-aware rule execution semantics

### Screening config authoring

Owns:

- screening queries
- screening trigger AST
- forced outcomes
- preprocessing config

### AST runtime

Owns:

- AST node model
- evaluator registry
- evaluation cache
- runtime and dry-run execution modes

### Publication

Owns:

- live version pointer
- publish/unpublish history
- pre-publication validation
- preparation requirements before activation

### Test runs

Owns:

- scenario test-run lifecycle
- phantom iteration comparison
- phantom decisions
- test-run summaries

### Decision execution

Owns:

- trigger evaluation
- concurrent rule evaluation
- threshold mapping
- decision outcome selection
- rule execution details
- test-run execution path
- raw payload parsing/enrichment before evaluation when requests are not object-reference based

### Decision persistence

Owns:

- decisions
- rule execution rows
- related execution metadata
- phantom decision rows
- test-run summaries
- analytics fields stored with decisions
- optional offloaded evaluation payloads
- rehydration of offloaded rule-evaluation details on reads

### Batch orchestration

Owns:

- scheduled execution lifecycle
- async execution lifecycle
- retryable execution state
- test-run summary background processing
- watermark-driven maintenance where summary/offloading behavior depends on progress tracking

### Workflow automation

Owns:

- workflow rules
- workflow conditions
- workflow actions
- post-decision dispatch logic
- case creation and add-to-case workflow behavior if retained here
- workflow-generated case tags and case-event side effects

### Decision events and webhooks

Owns if retained in the service:

- decision-created event generation
- async-execution failure event generation
- workflow-related webhook/event generation
- outbox or dispatch handoff for delivery

### Security and entitlement checks

Cross-cutting concern that must be preserved explicitly.

Includes:

- organization and scenario access checks
- feature-access gating for screening/scoring-sensitive paths
- possible entitlement or license checks if some capabilities remain gated outside the service

## Areas that are adjacent but external

### Data model

External owner:

- `data-model-service`

Role:

- schema and navigation authority
- source of model revision and preparation-related metadata

### Ingestion

External owner:

- `ingestion-service`

Role:

- write path and record intake

### Case management

External owner if not embedded in the decision engine.

Role:

- receives workflow-driven case actions
- owns case state, inbox routing, case events, and case-review automation

### Tenant data storage

External from a domain perspective, even if directly readable in V1.

Role:

- source of records used during evaluation
- source of aggregate and navigation reads used by AST functions

### Custom lists

External owner unless pulled into the service explicitly.

Role:

- supports AST functions that reference configured list values

### Feature access

External owner unless pulled into the service explicitly.

Role:

- gates screening- or scoring-sensitive behavior

### Screening provider

External dependency unless intentionally embedded in the service.

### Continuous screening

Separate subsystem unless intentionally merged later.

Role:

- handles monitored-object lifecycle
- handles dataset-update workers and continuous rescreening
- may consume decision-engine or screening outputs, but is not the same thing as scenario-time screening

### Scoring provider

Optional sibling domain or internal subdomain depending on final scope.

## Practical interpretation

If extracted fully, the service is not merely a formula engine.

It is:

- an authoring service
- an execution service
- a persistence service
- an orchestration service
- a test/comparison service
- a snooze-aware suppression service

all within the single bounded context of decisioning.
