# Screening Service Integration Contracts

## Overview

This document describes the current integration surface for the implemented standalone `screening-service`, plus the major live-rollout questions that still need proof.

## External relationships

The service currently depends on:

- `decision-engine-service`
- `data-model-service`
- `ingestion-service`
- general custom-list capability
- provider integrations
- case-management integration
- blob storage integration

## Contract with decision-engine-service

The implemented V1 path is an internal HTTP intake.

Current endpoint:

- `POST /internal/v1/tenants/{tenantId}/decision-screenings`

Current request shape includes:

- `decision_id`
- `scenario_id`
- `screening_config_id`
- `object_id`
- `object_type`
- `provider`
- `queries`
- `provider_config`
- `limit_override`
- `idempotency_key`

The worker-backed lifecycle remains asynchronous after intake even though the initial command boundary is HTTP.

## Contract back to decision-engine-service

The screening service communicates result state back through an explicit HTTP callback.

Possible needs:

- screening execution accepted
- provider request sent
- provider request failed
- screening result completed

The decision engine likely does not need full match detail in its own store.

Recommended feedback contract:

- decision engine keeps high-level screening execution lifecycle if needed for audit
- screening service remains source of truth for full screening details and matches

Current callback endpoint expected on the decision engine:

- `POST /internal/screening-status-updates`

Current callback fields include:

- `tenant_id`
- `screening_id`
- `decision_id`
- `scenario_id`
- `screening_config_id`
- `status`
- `provider`
- `object_type`
- `object_id`
- `provider_reference`
- `last_error`
- `partial`
- `idempotency_key`
- `completed_at`
- `match_count`

## Contract with data-model-service

The screening service will need data-model reads primarily for continuous or list screening.

Likely needed capabilities:

- load assembled tenant model
- validate object type exists
- validate mapped fields exist
- read field data types
- read links if future monitoring flows navigate related objects

Likely endpoint:

- `GET /v1/tenants/{tenantId}/data-model`

Likely required response shape:

- `revision_id`
- `tables`
- per table:
  - `name`
  - `fields`
  - optionally links and navigation metadata
- per field:
  - `name`
  - `data_type`

The screening service should not require authoring endpoints from the data model service.

## Contract with feature-access and authorization dependencies

This is a cross-cutting but important integration boundary.

The current service boundary still needs checks for:

- sanctions screening availability
- continuous-screening availability
- freeform search availability
- review and whitelist permissions
- case-access-aware review permissions

## Contract with ingestion-service

The screening service needs tenant record reads and event notifications.

### Read needs

Likely capabilities:

- read one record by object type and object ID
- read one record by internal ID for monitored-object updates
- search or list records when dataset-updated screening matches provider entities back to tenant objects

Possible endpoints:

- `GET /v1/tenants/{tenantId}/records/{objectType}/{objectId}`
- `GET /v1/tenants/{tenantId}/records/{objectType}/search?...`

### Event needs

Continuous or list screening may need notification of:

- object added
- object updated
- object deleted if modeled explicitly

Current baseline integration:

- explicit HTTP callback from ingestion service

Possible later integration:

- event bus with canonical ingestion event envelope

## Contract with organization-level screening settings

Legacy Marble behavior includes organization-level screening defaults such as threshold and limit values used when screening configs do not override them.

The broader platform still needs to decide whether those defaults come from:

- the screening service itself
- a separate organization or settings owner
- or are passed in by calling services

## Contract with provider integrations

This service hides provider-specific behavior behind one provider port.

Capabilities likely required:

- search or match query
- enrich entity or match
- get catalog
- get dataset freshness
- download or process update files
- get algorithm metadata
- support provider scope selection
- support feature detection for backend capabilities where relevant

The internal provider port should separate:

- decision-linked screening search
- continuous screening search
- enrichment
- dataset update operations

### Marble-specific provider reality that should be captured

The legacy Marble code shows that the provider surface is not only a generic OpenSanctions API.

It includes practical support for:

- OpenSanctions SaaS
- self-hosted Yente-style deployments
- self-hosted Motiva-style deployments
- optional name-recognition sidecar integration

Important concrete behaviors to preserve in planning:

- configurable datasets per request
- query threshold and cutoff handling
- result limit handling
- exclude-entity lists for whitelist behavior
- scoped-index behavior when supported by the backend
- algorithm selection
- dataset catalog and freshness inspection

The service now models these behind adapters, but rollout still needs to validate them against the real provider deployment.

## Contract with preprocessing and name-recognition dependencies

Screening preprocessing is not only an in-memory transformation.

The legacy system supports optional name-recognition infrastructure.

Likely needed capabilities:

- detect whether name recognition is configured
- submit a text query for entity extraction
- receive typed matches such as person or organization

Likely owned by `screening-service`:

- when preprocessing is applied
- how NER output is turned into screening queries

Likely external:

- the actual name-recognition engine

## Contract with case-management integration

This is one of the highest-risk boundaries because legacy screening review flows cause case-side effects.

The current contract supports:

- create case for a continuous or list-screening result
- attach screening review event to case
- attach reviewer identity
- attach evidence or contributor information

This should be implemented as one explicit port rather than scattered repository dependencies.

Possible command types:

- `create_case_for_screening`
- `record_screening_review_event`
- `add_screening_contributor`

Likely read-side validations also needed:

- verify inbox target exists and is active for continuous-screening configs
- verify case or review target exists before attaching screening-side effects

## Contract with general custom-list capability

This is a critical distinction.

`screening-service` is not currently intended to own Marble's general company custom-list CRUD.

However, screening preprocessing currently depends on custom-list reads for ignore-list behavior.

So the likely V1 contract is read-only and may require:

- fetch custom list metadata by ID
- fetch custom list values by list ID

This keeps the boundary clear:

- general custom lists remain a separate capability
- screening whitelists remain screening-owned state

The external custom-list capability currently includes lifecycle concerns such as:

- one-by-one value insertion
- CSV-based bulk replacement
- import and export participation
- organization-level default list seeding

## Contract with blob storage

Needed for screening evidence file support.

Capabilities:

- open upload stream
- delete uploaded file on failure
- generate signed download URL

This should remain an infrastructure port rather than a service-to-service dependency.

## Internal ownership contracts

The screening service is now the source of truth for:

- screenings
- screening matches
- whitelist entries
- match comments
- screening files
- monitored objects
- continuous screening configs
- dataset update jobs

The screening service should not automatically become the source of truth for:

- all organization custom lists used elsewhere in the platform

The decision engine should not remain source of truth for match-level screening data.

## Open contract questions

The following decisions or rollout proofs still need finalization:

- whether case-management side effects remain synchronous or become command-driven
- whether tenant record reads stay HTTP-based through ingestion service or move to direct tenant-store reads behind a port
- whether provider catalog and dataset-freshness inspection are sufficient for production operations as currently exposed
- whether general custom-list reads should come through a dedicated platform service or another existing owner
- whether the current provider dataset delta handling matches the real provider behavior in production

## Guiding rule

Every cross-service dependency should be:

- explicit
- narrow
- replaceable behind a port

The screening service should not be extracted by recreating monolith-style shared database assumptions.
