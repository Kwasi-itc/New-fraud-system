# Screening Service Extraction Design

## Goal

Extract Marble's screening-related domains from the monolithic `api` service into a standalone backend that works with:

- `new/backend/data-model-service`
- `new/backend/ingestion-service`
- `new/backend/decision-engine-service`

The target service should preserve Marble's current screening and list-screening behavior, while reframing it as a standalone bounded context with explicit contracts and dependencies.

## Problem statement

The current monolith mixes several distinct kinds of screening behavior:

- decision-time screening triggered by scenario outcomes
- manual or freeform screening
- screening review and refinement
- whitelist-driven screening suppression
- screening match enrichment
- file attachment and review evidence flows
- continuous or list screening of monitored objects
- provider dataset update processing and re-screening
- screening-linked case creation and case event generation

Some of these behaviors are adjacent to decisioning, but most are not part of rule evaluation itself.

Trying to absorb all of this into `decision-engine-service` would blur service ownership and create a service that is responsible for:

- decisions
- rules
- scenario authoring
- provider-side screening
- monitored-object state
- dataset update jobs
- review workflows
- whitelist lifecycle

That is too broad a boundary.

## Why this should be a standalone service

The legacy screening area already behaves like a service boundary because it owns:

- its own runtime flows
- its own persistence model
- its own worker workloads
- its own provider integrations
- its own review lifecycle
- its own match and whitelist semantics

It also has different scaling and operational characteristics from the decision engine:

- long-running provider calls
- enrichment follow-up jobs
- dataset update workers
- partial results and pagination
- review-heavy read patterns
- potentially large screening payloads

Those concerns justify a dedicated service.

## Scope of the future service

The service should own the full screening bounded context, including two related but distinct product slices.

### 1. Decision-linked screening

This covers:

- screening config execution requests created after decisions
- screening provider dispatch
- screening result persistence
- screening match persistence
- screening execution lifecycle updates
- match review and refinement
- whitelisting
- enrichment
- related case-facing workflows

### 2. Continuous or list screening

This covers:

- monitored objects
- monitoring configurations
- object-added and object-updated screening
- provider dataset-updated screening
- delta tracking
- dataset update jobs
- load-more result flows
- object and entity mapping against the data model
- screening-driven case creation for monitored objects

## Marble inspiration we are explicitly carrying forward

The planning is not generic.

It is intentionally based on how Marble screening already works today.

### From Marble decision-time screening

We want to preserve:

- scenario-linked screening requests triggered after a decision outcome
- screening config concepts such as:
  - datasets
  - entity type
  - field-level query mapping
  - trigger rule
  - threshold override
  - forced outcome
  - counterparty identifier expression
  - preprocessing configuration
- raw provider-result persistence
- screening statuses and match statuses
- refinement and re-run flows
- freeform or manual screening search
- match enrichment
- evidence attachment flows
- reviewer-driven whitelist creation

### From Marble continuous or list screening

We want to preserve:

- monitored-object registration per configuration
- object-added and object-updated screening
- provider dataset-updated screening
- field-to-provider-property mapping based on the tenant data model
- partial-result and load-more flows
- delta tracking and resumable dataset processing
- screening-driven case creation for monitored-object flows
- inbox-linked routing for continuous-screening review cases
- risk-tag side effects from confirmed matches

### From Marble OpenSanctions integration

We want to preserve support for the actual Marble-style provider stack, including:

- OpenSanctions-compatible matching
- catalog and dataset discovery
- dataset freshness checks
- configurable datasets per screening config
- threshold and limit handling
- organization-level screening defaults for threshold and limit
- provider-side enrichment of matched entities
- support for self-hosted and hosted provider configurations
- support for scoped index behavior
- support for provider algorithm selection

### From Marble preprocessing

We want to preserve the intent of Marble preprocessing before screening queries are sent, including:

- skip-if-under filtering
- number removal
- ignore-list filtering using a referenced custom list
- optional name-entity-recognition preprocessing
- NER classification-aware query shaping

### From Marble access and entitlement behavior

We want to preserve the fact that screening behavior is feature-gated and permission-sensitive.

That includes:

- sanctions feature access for decision-linked and freeform screening
- continuous-screening feature access for monitored-object flows
- read and write permissions for whitelist and review actions
- case-access-aware review permissions

## Marble-specific deployment inspiration to make explicit

The legacy codebase shows a concrete screening stack around OpenSanctions-compatible services.

That includes:

- `Yente`
  - legacy self-hosted OpenSanctions index and matching surface
- `Motiva`
  - newer matching surface with feature detection for:
    - body-parameter support
    - scoped-index support
- optional name-recognition provider
  - used by screening preprocessing when enabled

The future `screening-service` should not hard-code these names into the core domain model, but the planning should explicitly acknowledge them as first-class adapter targets because they are part of the real Marble inspiration we are extracting from.

## What should remain outside this service

The service should not own:

- scenario authoring and rule execution
- decision scoring and outcome selection
- tenant schema authoring
- ingestion write APIs
- authoritative tenant record storage
- general case management if that becomes its own standalone service
- general-purpose company custom list management unless we later decide to consolidate it here

## Recommended top-level boundary

The split should be:

- `data-model-service`
  - schema authority
  - table, field, link, mapping, and revision authority
- `ingestion-service`
  - tenant write authority
  - ingestion lifecycle and record read APIs
- `decision-engine-service`
  - scenario execution authority
  - decision persistence and rule execution authority
  - screening request orchestration for decision outcomes
- `screening-service`
  - screening provider authority
  - screening review authority
  - whitelist authority
  - continuous or list screening authority

## Relationship with the decision engine

The decision engine already has a reasonable initial boundary for screening:

- it stores scenario-linked screening config metadata
- it creates screening execution records as side effects of decisions
- it dispatches pending screening executions through a provider interface

That is a workable transitional design.

The screening service should therefore be introduced in phases.

### Transitional phase

In the transitional phase:

- `decision-engine-service` continues to own scenario-attached screening configs
- `decision-engine-service` continues to create screening execution requests
- `screening-service` becomes the downstream processor of those requests

This avoids moving scenario authoring and runtime evaluation at the same time as provider execution and review lifecycle.

### Mature phase

In the mature phase, one of two models can be chosen:

- keep screening configs scenario-owned in the decision engine, with the screening service consuming them operationally
- move screening policy ownership to the screening service and let scenarios reference reusable screening policies

The first option is lower risk for V1.

## Core design principles

### Explicit service ownership

Every persistent concept should have one owning service.

For screening, likely owned concepts include:

- screening executions
- screening results
- screening matches
- screening comments
- screening files
- screening whitelists
- continuous screening configs
- monitored objects
- dataset update jobs
- delta tracks
- enrichment state

### Decision engine remains execution authority

The decision engine should continue to decide:

- whether a scenario triggered
- what outcome a decision has
- whether screening should be requested based on scenario configuration

The screening service should not re-evaluate scenario logic.

### Data model remains schema authority

Mappings used by list screening may depend on the data model, but the data model itself must remain external.

The screening service should read:

- object tables
- field metadata
- configured mapping targets
- link metadata if needed

It should not own table or field lifecycle.

### Ingestion remains write authority

The screening service may read tenant records and react to ingestion events, but it should not become another write path for tenant object data.

## Major domain slices to extract

### Screening provider integration

Responsibilities:

- execute provider search requests
- manage provider-specific request payloads
- normalize provider responses
- handle retries and transient failures
- support enrichment requests
- maintain dataset freshness information
- support OpenSanctions-compatible catalog and algorithm discovery
- support both SaaS and self-hosted deployment modes
- support Yente-like and Motiva-like provider surfaces through adapters
- support provider scope configuration and scoped-index behavior where available

### Screening review lifecycle

Responsibilities:

- persist screening results and matches
- update match review state
- update overall screening status
- support refinement and re-run
- persist comments and evidence attachments
- create screening review events where needed
- support freeform or manual screening entry points where they remain product-facing

### Whitelist lifecycle

Responsibilities:

- persist whitelist entries
- apply whitelist suppression before screening requests
- support entity-based and counterparty-based whitelist logic
- support continuous screening review-driven whitelist creation

This whitelist capability is distinct from Marble's general custom-list product capability.

The planning assumption is:

- screening whitelists belong to `screening-service`
- general custom lists do not automatically move here

### Continuous or list screening orchestration

Responsibilities:

- register and deregister monitored objects
- maintain monitoring audit state
- screen objects on add or update
- process provider dataset updates
- rescreen impacted monitored objects
- handle partial results and load-more flows
- validate review inbox routing targets for continuous-screening configurations

### Case-facing screening flows

Responsibilities:

- create or attach case-like review artifacts if this remains in service scope
- emit case events or commands when matches are reviewed
- handle case contributor or evidence attachment side effects if kept inside this boundary
- route continuous-screening review cases through configured inbox targets

## Existing legacy domains that map here

Legacy screening-related modules strongly suggest the future service boundary should include:

- `screening`
- `screening_config`
- `continuous_screening`
- screening whitelist flows
- OpenSanctions provider integration
- screening enrichment workers
- provider catalog, algorithm, and dataset-freshness flows
- screening preprocessing and optional NER flows

The legacy code also shows that decision-time screening and continuous screening reuse common concepts:

- screening statuses
- screening match statuses
- whitelist logic
- provider matching
- enrichment
- review workflows

It also shows that screening depends on adjacent but non-owned Marble capabilities that must be modeled explicitly in the extraction:

- general custom lists for ignore-list preprocessing
- organization-level screening configuration
- optional name-recognition infrastructure
- provider deployment flavor detection such as Motiva feature support
- feature-access gating and authorization logic

That commonality is a strong reason to extract them into one screening service rather than two separate services immediately.

## V1 service objective

V1 should provide a standalone service that can:

- receive or load pending decision-linked screening execution requests
- call the provider and persist screening results
- expose review and refinement APIs
- expose whitelist APIs
- expose enrichment APIs
- manage monitored objects for continuous or list screening
- process dataset updates and rescreen impacted objects
- persist enough lifecycle data for operations and auditability
- support the practical OpenSanctions-compatible deployment modes already used by Marble

## V1 non-goals

The planning phase should not assume:

- that all case-management behavior is settled
- that all provider contracts are finalized
- that reusable cross-scenario screening policies must be implemented in V1
- that event-driven integration is required everywhere on day one

Those decisions belong in the operating decisions and blueprint documents.
