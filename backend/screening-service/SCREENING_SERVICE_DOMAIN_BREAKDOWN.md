# Screening Service Domain Breakdown

## Core bounded context

The extracted service is best understood as a `screening` bounded context.

It combines provider-driven entity matching, result persistence, review workflows, whitelist state, and continuous or list-screening orchestration.

## Domain areas

### Screening provider execution

Owns:

- provider search requests
- provider-specific request payloads
- provider response normalization
- provider retry handling
- provider request references
- provider status transitions
- dataset freshness reads
- provider catalog reads
- provider algorithm discovery
- provider scope handling
- deployment-capability detection for OpenSanctions-compatible backends such as Motiva

### Screening execution lifecycle

Owns:

- decision-linked screening execution records
- request and response payload persistence
- sent, completed, failed, retried lifecycle state
- provider callback or polling status updates
- operational inspection of screening runs

### Screening result persistence

Owns:

- screening rows
- screening status
- number of matches
- partial-result flags
- archived or superseded screening runs
- initial query and effective query metadata
- freeform or manual screening result lifecycle when those searches are operationally exposed

### Screening match persistence

Owns:

- screening matches
- match status
- reviewed-by metadata
- comments
- enrichment payloads
- payload score and entity metadata
- skipped, pending, confirmed-hit, and no-hit state

### Screening refinement

Owns:

- refine-search flows
- manual re-runs
- replacement or archival of prior screening runs
- carry-forward of relevant review artifacts where applicable

### Screening preprocessing

Owns:

- screening query cleanup before provider submission
- skip-if-under logic
- number removal
- ignore-list-based term removal
- optional name-entity-recognition query rewriting
- preservation of preprocessing audit or debug metadata if desired

### Screening whitelist

Owns:

- whitelist entries
- counterparty-to-entity suppression
- entity-based whitelist lookups
- whitelist writes during review
- whitelist reads during provider query preparation

Important distinction:

- this is not the same domain as Marble's general-purpose custom lists

### Screening enrichment

Owns:

- match enrichment requests
- enriched payload persistence
- idempotent handling for already-enriched matches
- enrichment worker orchestration

### Screening evidence and files

Owns:

- screening file metadata
- uploaded evidence references
- download link generation handoff
- screening-related reviewer evidence flows
- contributor side effects associated with evidence upload if those stay screening-owned

### Continuous or list screening configuration

Owns:

- monitoring configuration metadata
- object-type coverage
- provider datasets used for monitoring
- threshold and candidate-limit settings
- inbox routing target for review cases
- mapping between data model fields and provider properties
- enable and disable lifecycle

### Monitored objects

Owns:

- monitored object registry
- monitoring add and remove audit trail
- object registration by configuration
- detection of objects monitored by multiple configs

### Continuous or list screening execution

Owns:

- object-added screening
- object-updated screening
- dataset-updated screening
- partial result handling
- load-more match retrieval
- deduplication against prior matches

### Dataset update processing

Owns:

- provider dataset update records
- update jobs
- update job offsets
- update job errors
- dataset file tracking
- delta application lifecycle

### Delta tracking

Owns:

- change tracks for monitored objects
- change tracks for provider entities
- add, update, and delete operations
- processing state for changes introduced by dataset updates

### Screening review side effects

Owns or coordinates:

- review events
- final screening status transitions
- risk-tag extraction from confirmed matches
- whitelist creation from no-hit reviews
- optional case-side commands or events
- case contributor creation for screening evidence uploads if not moved elsewhere

## Shared concepts across subdomains

These concepts appear in both decision-linked screening and continuous or list screening:

- provider entities
- screening status
- match status
- whitelist rules
- enrichment
- partial results
- reviewer actions

The service should define these once in a shared domain vocabulary rather than duplicating them across multiple services.

## Areas adjacent but external

### Decision engine

External owner:

- `decision-engine-service`

Role:

- decides when decision-linked screening should happen
- creates decisions and their outcomes
- may create screening execution requests or commands

### Data model

External owner:

- `data-model-service`

Role:

- owns table and field definitions
- provides metadata needed for mapping monitored object fields to provider properties

### Ingestion

External owner:

- `ingestion-service`

Role:

- owns record writes
- exposes read contracts or event notifications used by continuous screening

### Tenant data storage

External from a domain perspective.

Role:

- source of ingested objects used in monitoring and screening preparation

### General custom lists

Likely external owner in V1:

- platform or decision-support capability outside `screening-service`

Role:

- supports scenario rule functions
- supports screening preprocessing ignore lists
- supports organization import and export
- supports CSV-based bulk value replacement and default-list seeding flows

Important distinction:

- screening-service should probably consume this capability
- screening-service should not automatically own general custom-list CRUD in V1

### Name-recognition provider

Likely external infrastructure dependency.

Role:

- optional preprocessing step for screening query shaping

### OpenSanctions-compatible deployment layer

External infrastructure dependency from the service perspective.

Examples in Marble:

- OpenSanctions SaaS
- self-hosted Yente
- self-hosted Motiva

Role:

- matching
- enrichment
- catalog access
- dataset freshness access
- algorithm discovery

### Feature-access and authorization layer

Likely external cross-cutting dependency.

Role:

- gates sanctions screening
- gates continuous screening
- gates freeform search
- gates review and whitelist actions

### Case management

Potentially external.

Role:

- owns cases, inboxes, assignments, and case review state if those are extracted separately

### Blob storage

Potentially external infrastructure.

Role:

- stores screening evidence files

## Practical interpretation

The future service is not merely:

- a provider adapter
- a background worker
- a CRUD API

It is all of the following within one bounded context:

- a provider orchestration service
- a result persistence service
- a reviewer workflow service
- a monitored-object orchestration service
- a dataset-update processing service

That is the right level of scope for a dedicated `screening-service`.
