# Decision Engine Service Integration Contracts

## Overview

This document defines the intended integration surface between:

- `decision-engine-service`
- `data-model-service`
- `ingestion-service`

## Contract with `data-model-service`

The decision engine requires a tenant-scoped assembled model contract.

### Required capabilities

- read tenant existence and status
- read assembled model for one tenant
- read fields and data types
- read links
- read pivots
- read navigation options
- read model revision identifier

### Minimum contract shape

Likely endpoint:

- `GET /v1/tenants/:tenantId/data-model`

Required response concepts:

- `revision_id`
- tenant status / writability indicators
- tables
- fields
- field types
- enum values
- links
- pivots
- navigation options
- managed system fields

### Why the decision engine needs this

It is required for:

- AST dry-run validation
- trigger object validation
- type-aware AST evaluation
- pivot resolution
- workflow AST validation
- scheduled execution filtering
- test-run preparation checks

## Contract with tenant data reads

The decision engine needs a tenant data read abstraction.

### Required capabilities

- get one record by tenant, type, object id
- read a field through navigation/path traversal
- run aggregate reads
- list object ids for scheduled execution
- read decision-related historic context required by evaluator functions

### Suggested abstraction

`TenantDataReader`

Suggested methods:

- `GetRecord`
- `GetField`
- `Aggregate`
- `ListObjectIDs`
- `QueryRelated`
- `HasPastAlerts` or equivalent historical-risk lookup

### V1 implementation options

- direct PostgreSQL tenant-schema reads
- or an internal read API exposed elsewhere

V1 may use direct reads if this is the fastest route, but the service should still hide this behind an interface.

## Contract with `ingestion-service`

The decision engine should be triggered after successful ingestion.

### Preferred pattern

Event-driven integration.

Suggested event:

- `record.ingested`

Suggested payload concepts:

- tenant id
- object type
- object id
- operation
- ingestion timestamp
- model revision id

### Alternate pattern

Explicit callback from ingestion to decision engine.

Possible endpoint:

- `POST /v1/tenants/:tenantId/evaluate/:objectType/:objectId`

### Recommendation

Prefer events for looser coupling and lower ingestion latency risk.

### Additional ingestion-to-decision considerations

The integration contract should also clarify:

- whether the decision engine receives only object references or also payload snapshots
- whether replay or re-evaluation can be requested for backfills
- whether ingestion-triggered evaluation is best-effort or guaranteed-delivery

## Contract with payload parsing and enrichment

Marble raw decision creation currently validates and enriches payloads before evaluation.

The extracted service should make explicit whether:

- raw payload parsing lives locally in the decision engine
- enrichment data is fetched through a dedicated enrichment port
- ingestion always sends already-normalized payloads for evaluation

## Contract with custom list access

Marble AST evaluation currently depends on custom list reads.

The extracted service should define a `CustomListReader` port or equivalent local ownership decision.

### Required capabilities

- read list metadata
- read list values by list id

## Contract with feature access

Some Marble behaviors are guarded by feature-access checks, especially screening-sensitive flows.

The extracted service should define a `FeatureAccessReader` if feature gating remains external.

## Contract with inbox and case-review settings

Workflow-driven case creation currently depends on inbox settings and may enqueue AI case-review work.

If case management remains external, the boundary must preserve:

- inbox selection and lookup behavior
- case-review-on-create behavior
- any downstream task-enqueue contract needed after case creation

## Contract with case-management actions

Marble decision workflows currently do more than emit generic automation signals.

They can:

- create a new case from a decision
- add a decision to an existing case
- add tags to the case
- create case events
- trigger AI case-review tasks depending on inbox settings

The extracted service should either:

- own those workflow-side case effects directly through a case-management port

or:

- emit a richer command/event contract that a separate case service can execute losslessly

This must be explicit in V1 planning because workflow semantics depend on it.

## Contract with decision-history reads

Some AST functions and runtime behaviors depend on historical decision state.

Examples include:

- past alert checks
- test-run summarization
- phantom/live comparison reporting

These reads may be internal to the service once decision persistence is extracted, but they should still be treated as explicit domain capabilities.

## Optional screening integration

If screening remains in scope, define a `ScreeningProvider` port.

Responsibilities:

- accept screening query requests
- return screening results
- support refinement or enrichment paths as needed

## Optional scoring integration

If scoring is externalized later, define a `ScoringProvider` port.

Responsibilities:

- trigger score recomputation
- optionally query referenced risk levels if AST functions depend on them

## Event outputs from decision engine

The decision engine should likely emit events for:

- decision created
- async decision execution failed
- scheduled execution completed
- test run status changed
- phantom or test-run summary updated
- workflow action triggered
- case created from workflow
- case decisions updated from workflow
- webhook dispatch requests if webhooks remain event-driven

## Contract with webhook or outbox delivery

Marble currently creates webhook events as part of decision creation and workflow side effects.

The extracted service should define whether it:

- writes directly to a service-owned outbox
- calls a dedicated webhook/event service
- or publishes domain events that another delivery layer translates into webhooks

At minimum, the contract must support:

- `decision.created`
- async-decision failure notifications
- workflow-generated case events if that responsibility stays with this service

## Contract with watermark-based maintenance

Some Marble background behaviors are watermark-driven rather than simple queue jobs.

Examples include:

- test-run summary progress
- offloaded evaluation read/write cutovers

If retained, the extracted service should define who owns watermark state and how maintenance workers advance it safely.

## Guiding rule

The decision engine should depend on explicit contracts, not shared database knowledge from the monolith.
