# Screening Service Implementation Blueprint

## Purpose of this blueprint

This document translates the screening-service boundary into a staged implementation plan.

It is intentionally detailed enough to guide execution later, while still remaining planning-only.

## Recommended rollout strategy

Do not extract everything in one cut.

Use a staged rollout that minimizes simultaneous movement across:

- scenario authoring
- decision evaluation
- provider execution
- review APIs
- monitored-object workers
- dataset update jobs

## Phase model

### Phase 0: Planning and inventory

Objectives:

- finalize service boundary
- inventory all legacy screening tables, APIs, workers, and dependencies
- classify what is V1-critical versus deferred
- inventory the exact Marble OpenSanctions stack details:
  - self-hosted versus SaaS
  - Yente usage
  - Motiva usage
  - name-recognition usage
  - dataset freshness and catalog flows
- inventory the exact usage of general custom lists inside screening preprocessing
- inventory feature-access and authorization checks across screening and continuous-screening flows
- inventory freeform or manual screening endpoints and behaviors
- inventory inbox-routing behavior for continuous-screening configs

Outputs:

- final planning documents
- source inventory
- migration dependency map

### Phase 1: Service shell and persistence model design

Objectives:

- define the future service package layout
- define persistence ownership
- define primary database schemas and table groups
- define initial operational commands

Recommended internal packages:

- `cmd/server`
- `cmd/worker`
- `cmd/migrate`
- `internal/domain`
- `internal/service`
- `internal/httpapi`
- `internal/store`
- `internal/ports`
- `internal/clients`
- `internal/provider`
- `internal/worker`

Recommended domain groups:

- `screening`
- `review`
- `whitelist`
- `continuousscreening`
- `datasetupdate`
- `integration`
- `preprocessing`

Outputs:

- schema plan
- package plan
- migration plan

### Phase 2: Decision-linked screening extraction

Objectives:

- support decision-engine-created screening requests
- send provider requests
- persist screening results and matches
- support execution inspection and status updates

Capabilities:

- load pending screening requests
- dispatch to provider
- store screening result
- store matches
- track provider reference
- track lifecycle status
- preserve raw provider payloads
- preserve effective threshold and dataset context where needed

Integration options:

- option A: decision engine emits outbox events that screening service consumes
- option B: decision engine calls screening service synchronously or asynchronously over HTTP
- option C: shared request table is used only temporarily during extraction

Recommended V1 choice:

- prefer explicit HTTP or outbox-driven integration
- avoid long-term shared-table coupling

Outputs:

- provider execution runtime
- persisted screening records
- minimal screening execution APIs

### Phase 3: Review and refinement API extraction

Objectives:

- move reviewer-facing screening APIs out of the monolith
- preserve review semantics for match status changes
- preserve refinement and whitelist behavior

Capabilities:

- list screenings by decision or case reference
- get screening details
- update match status
- add comments
- refine a screening
- manual search
- list and manage files
- enrich matches
- whitelist and unwhitelist entities
- support the practical distinction between screening whitelists and general custom lists
- support freeform or manual screening APIs if they remain part of the screening-service scope

Important migration note:

- review lifecycle has side effects into case events and possibly case contributors
- that contract must be made explicit before this phase is complete

Outputs:

- review API surface
- explicit case-event or case-command integration

### Phase 4: Continuous or list screening extraction

Objectives:

- move monitored-object lifecycle
- move configuration and mapping management
- move object-triggered screening workers

Capabilities:

- create and delete monitored objects
- list monitored objects
- create and update continuous screening configs
- map data model fields to provider properties
- perform screening on object add or object update
- preserve provider dataset selection, threshold, limit, and scope behavior
- preserve preprocessing-related configuration where still relevant
- preserve inbox-routing and case-creation target behavior for monitored-object flows

Important dependency:

- this phase depends heavily on data-model and ingestion read contracts

Outputs:

- monitored-object APIs
- config APIs
- object-triggered screening workers

### Phase 5: Dataset update and delta-processing extraction

Objectives:

- move provider dataset update handling
- move delta application logic
- move rescreening and backfill workers

Capabilities:

- persist dataset update records
- track update jobs and offsets
- process full and delta provider files
- re-screen impacted objects
- persist delta tracks
- preserve provider catalog and dataset-freshness awareness

Operational concerns:

- job resumability
- idempotency
- partial progress
- fault tolerance

Outputs:

- dataset-update runtime
- resumable worker flows

### Phase 6: Hardening and simplification

Objectives:

- remove residual monolith coupling
- tighten contracts
- unify duplicated screening concepts
- improve metrics, auditability, and operations

Capabilities:

- full metrics and tracing
- stable retry semantics
- formal provider adapter boundary
- better pagination and operational tooling

## Persistence blueprint

The service should own its own metadata database for screening-related state.

Likely table groups:

- screening execution requests
- screenings
- screening matches
- screening comments
- screening files
- screening whitelists
- continuous screening configs
- continuous screening results
- continuous screening matches
- monitored object registry
- dataset update records
- dataset files
- update jobs
- update job offsets
- update job errors
- delta tracks
- outbox or integration command tables

Guiding rule:

- if the concept is about screening state rather than scenario evaluation, it belongs in the screening service store

## API blueprint

The future API surface will likely break down into:

### Operational APIs

- health and readiness
- provider execution inspection
- retry and replay endpoints

### Screening execution APIs

- create screening request or receive screening command
- get screening by ID
- list screenings by decision
- list screenings by external aggregate reference
- inspect provider payload and execution metadata where operationally required

### Freeform or manual screening APIs

- freeform search against provider datasets
- optional persistence or transient-return behavior for manual searches
- dataset and threshold override support within policy limits

### Review APIs

- list matches
- update match status
- add comments
- refine screening
- enrich match
- upload files
- get file metadata and download links
- expose screening-specific whitelist operations

### Whitelist APIs

- create whitelist entry
- delete whitelist entry
- search whitelist entries

These APIs are for screening whitelist semantics, not for company-wide custom-list CRUD.

### Continuous or list screening APIs

- create config
- update config
- enable and disable config
- add monitored object
- remove monitored object
- list monitored objects
- list continuous screenings
- dismiss or load more matches
- validate and persist inbox target used for review-case routing

### Dataset update APIs

- create dataset update record
- inspect update jobs
- retry jobs
- inspect offsets and processing errors
- inspect provider dataset freshness or catalog state if kept in-service

## Worker blueprint

The service will likely need multiple worker loops.

Recommended worker categories:

- pending screening request dispatcher
- match enrichment worker
- monitored object screening worker
- dataset update application worker
- rescreening worker
- cleanup or archival worker
- optional provider capability or catalog refresh worker if not done on demand

Each worker should support:

- bounded batch size
- retry policy
- visibility into failed items
- idempotent reprocessing

## Integration blueprint

### With decision engine

V1 recommended flow:

- decision engine creates screening request command
- screening service executes provider call
- screening service persists result
- screening service sends status/result callback or event back if needed

The decision engine should not need to know match-level review details.

It may still need enough status to show that:

- screening was requested
- screening failed operationally
- screening completed and can be inspected in screening-service

### With data model service

Needed for:

- object field mapping for continuous screening
- object type validation
- future relationship-aware monitored-object logic if needed

### With feature-access and authorization dependencies

Needed for:

- sanctions feature gating
- continuous-screening feature gating
- freeform search entitlement checks
- review and whitelist permission checks

### With general custom-list capability

Needed for:

- screening preprocessing ignore lists

Planning assumption:

- `screening-service` consumes general custom-list reads
- `screening-service` does not own general custom-list CRUD in V1

The company custom-list lifecycle that remains external includes:

- create list
- update list metadata
- add one value
- replace values from CSV
- delete values
- import and export participation

### With ingestion service

Needed for:

- object reads for monitored objects
- ingestion-triggered monitoring events
- search and lookup against tenant records if screening provider dataset updates target Marble-side objects

### With case management

Needed for:

- case creation
- review events
- evidence contributor flows
- attachment of screenings to case-like review entities

## Migration strategy for legacy assets

Migration should separate:

- API cutover
- worker cutover
- persistence ownership cutover

Recommended order:

1. provider execution
2. screening result persistence
3. review APIs
4. whitelist APIs
5. preprocessing and optional name-recognition integration
6. continuous screening configs and monitored objects
7. dataset update jobs

This order keeps the highest-value and most isolated screening functionality moving first.

## Major risks to manage

### Risk 1: Hidden case-management coupling

Legacy review flows create case events and other side effects.

Mitigation:

- document each case-facing dependency before implementation
- isolate case interaction behind one explicit port

Include:

- inbox-target validation for continuous-screening configs
- case contributor side effects during evidence uploads

### Risk 2: Shared concepts split across services

Statuses, whitelist semantics, and screening config meaning must not drift between services.

Mitigation:

- define canonical service-owned models
- avoid duplicating reviewer semantics in the decision engine

Also keep the distinction clear between:

- screening whitelist state
- general custom-list state

### Risk 3: Data-model mapping complexity

Continuous screening depends on field-to-provider mapping rules.

Mitigation:

- keep mapping validation close to the screening service
- read schema metadata from `data-model-service`

### Risk 4: Operational complexity of dataset updates

Dataset update processing is stateful and failure-prone.

Mitigation:

- model offsets, retries, and errors explicitly
- keep job state persistent and inspectable

### Risk 5: Provider-specific behavior leaking into the wrong layer

Marble already contains provider-surface details such as:

- Motiva capability detection
- scoped-index behavior
- catalog and algorithm discovery
- optional NER sidecar integration

Mitigation:

- keep these in provider and preprocessing adapters
- do not let them pollute the core decision domain or unrelated platform services

### Risk 6: Boundary confusion between screening lists and general custom lists

Mitigation:

- keep screening whitelist state owned by `screening-service`
- keep general custom-list CRUD external unless we explicitly decide otherwise
- document every remaining read dependency on custom lists

## Success criteria

The extraction should be considered successful when:

- decision-linked screening no longer depends on monolith screening runtime
- review and whitelist flows are served by the screening service
- monitored-object screening and dataset update workers run outside the monolith
- service ownership is clear between decisioning and screening
- no core screening lifecycle depends on monolith-internal tables as the primary runtime authority
