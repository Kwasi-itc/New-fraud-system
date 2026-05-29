# Screening Service V1 Operating Decisions

## Purpose

This document records the intended operating decisions for V1 of the future `screening-service`.

These are planning decisions, not implemented behavior.

## Decision 1: The service boundary is broad enough to include both screening and continuous or list screening

V1 should include:

- decision-linked screening
- review lifecycle
- whitelist lifecycle
- enrichment
- monitored-object lifecycle
- dataset update processing

Rationale:

- the legacy system reuses the same provider, status, whitelist, and review concepts across these flows
- splitting them immediately would create more cross-service complexity than value

## Decision 1a: Marble's OpenSanctions-compatible deployment model is part of the real V1 planning scope

V1 planning should explicitly account for:

- OpenSanctions SaaS
- self-hosted Yente-like deployments
- self-hosted Motiva-like deployments
- dataset catalog and freshness checks
- provider algorithm selection
- scoped-index behavior when supported
- optional name-recognition preprocessing

Rationale:

- these are not hypothetical future additions
- they are part of the existing Marble screening behavior we are extracting from

## Decision 2: The decision engine remains the authority for decisions

The screening service must not decide:

- whether a scenario triggers
- what a decision outcome is
- which rules hit

That remains in `decision-engine-service`.

The screening service executes and manages screening after that point.

## Decision 3: Scenario-linked screening configs can remain transitional in V1

V1 does not need to move all scenario-attached screening config authoring out of the decision engine immediately.

Transitional V1 is acceptable if:

- decision engine owns scenario-level authoring
- screening service owns provider execution and screening lifecycle

Rationale:

- lower migration risk
- less disruption to scenario authoring

## Decision 4: Screening service should own full screening result state

V1 should make the screening service the source of truth for:

- screening records
- matches
- review status
- comments
- files
- whitelist state

Rationale:

- reviewer flows should not depend on mirrored or duplicated state

## Decision 5: Prefer asynchronous provider execution

Pending screening requests should be processed asynchronously by workers.

Rationale:

- provider calls may be slow
- retries and failures need operational control
- enrichment and dataset updates are already worker-oriented problem shapes

## Decision 6: Continuous or list screening should be worker-first

Object-added, object-updated, and dataset-updated screening flows should run through dedicated workers rather than request-response execution where possible.

Rationale:

- these flows are operationally stateful
- they need retry and offset tracking
- they may involve large numbers of records

## Decision 7: Data model is read-only external dependency

The screening service should consume data-model metadata but never own table or field lifecycle.

Rationale:

- keeps service ownership clear
- avoids boundary drift

## Decision 8: Ingestion remains write authority

The screening service may read tenant records and react to ingestion events, but it should not become an alternate write path for object data.

Rationale:

- preserves clean write authority
- avoids duplicate write semantics

## Decision 8a: General company custom lists do not automatically move into screening-service V1

V1 should distinguish between:

- screening-owned whitelist state
- general-purpose company custom lists

Planning assumption:

- screening-service owns whitelist semantics
- screening-service may consume general custom-list reads for preprocessing
- screening-service does not own general custom-list CRUD by default

Rationale:

- custom lists are also used by decision-engine rule functions and organization import or export flows
- moving them into screening-service by default would over-expand the boundary

## Decision 8b: Freeform or manual screening belongs in the screening-service product boundary unless we later split it deliberately

Rationale:

- it uses the same provider integrations, datasets, thresholds, and match presentation semantics
- separating it from screening-service in V1 would create an artificial boundary

## Decision 8c: Feature gating must be treated as first-class in V1 planning

Rationale:

- Marble already gates sanctions screening and continuous screening separately
- ignoring this would make the extraction plan incomplete even if the runtime architecture looks clean

## Decision 9: Case-management coupling must be isolated behind one port

Screening review and continuous screening currently drive case-facing effects.

V1 should hide that behind one explicit integration port.

Rationale:

- this is one of the least stable boundaries
- it should be easy to redirect later if case management is extracted independently

## Decision 10: V1 should optimize for extraction safety, not ideal final architecture

Where there is a tradeoff between:

- fewer moving parts during extraction
- or more elegant long-term architecture

V1 should prefer the safer extraction path, provided service ownership remains clear.

Examples:

- HTTP integration may be acceptable before event-driven integration
- transitional config ownership may be acceptable before policy reuse

## Decision 11: Raw provider payloads should be preserved

V1 should persist raw provider result payloads even if normalized summaries are added later.

Rationale:

- important for review fidelity
- useful for debugging provider behavior
- reduces data loss during early extraction

## Decision 12: Partial-result handling is a first-class concern

V1 should model:

- partial results
- load-more flows
- final versus incomplete screening state

Rationale:

- this is already a real behavior in legacy continuous or list screening
- trying to ignore it would change product semantics

## Decision 13: Whitelist semantics must remain canonical in one place

V1 should centralize whitelist reads and writes inside the screening service.

Rationale:

- whitelist behavior affects both decision-linked and continuous screening
- duplicating it in multiple services would create drift

## Decision 14: Dataset update processing must be resumable

V1 should explicitly model:

- update jobs
- offsets
- processing errors
- retryable state

Rationale:

- dataset processing is a long-running operational flow
- resumability is necessary for reliability

## Decision 15: No code scaffolding should be created until the planning pack is accepted

Before implementation begins, we should first agree on:

- service boundary
- migration order
- integration contracts
- persistence ownership

Rationale:

- this extraction is broad
- incorrect early scaffolding would lock in the wrong assumptions
