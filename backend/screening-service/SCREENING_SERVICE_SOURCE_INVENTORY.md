# Screening Service Source Inventory

## Purpose

This document maps the relevant screening-related parts of the current Marble monolith to the intended standalone architecture.

It answers a practical extraction question:

- what moves into `screening-service`
- what stays in `decision-engine-service`
- what stays outside as a shared or external capability
- what should be deferred until after V1

This is not a line-by-line migration script.

It is a source-oriented extraction map that helps us avoid accidental scope drift.

## Status legend

- `move`
  - this belongs in `screening-service`
- `stay`
  - this should remain in its current or already-extracted owner
- `shared`
  - this is not owned by `screening-service`, but `screening-service` depends on it
- `defer`
  - this is related, but should not be part of the first extraction cut

## Top-level boundary summary

### Move into `screening-service`

- provider-facing screening runtime
- screening result and match lifecycle
- screening review and refinement
- screening whitelist lifecycle
- screening enrichment
- screening files and evidence handling
- freeform or manual screening
- continuous or list-screening config and monitored-object flows
- dataset update and delta-processing flows

### Stay in `decision-engine-service`

- scenario and iteration authoring
- decision evaluation
- rule execution
- decision persistence
- scenario-linked screening request orchestration in the transitional V1 model
- general custom-list rule-function support

### Stay external or shared

- tenant schema and mapping authority
- ingestion write authority
- general custom-list CRUD
- case-management domain
- feature-access and authorization policy sources
- blob storage infrastructure

## Monolith source map

### 1. Decision-time screening authoring and orchestration

#### `api/usecases/screening_config_usecase.go`

Status:

- `stay` in `decision-engine-service` for transitional V1

Reason:

- this is scenario-iteration-owned authoring
- it belongs closer to scenario authoring than to provider execution

Future possibility:

- later move or split if screening policies become reusable and independently managed

#### `api/usecases/evaluate_scenario/evaluate_screening.go`

Status:

- split

Recommended ownership:

- triggering decision that screening should happen: `stay` in `decision-engine-service`
- provider execution and result persistence semantics: `move` into `screening-service`

Reason:

- today the monolith mixes orchestration and provider execution in one runtime path
- the future architecture should separate decision outcome handling from screening execution handling

#### `new/backend/decision-engine-service/internal/service/decision_service.go`

Status:

- `stay` in `decision-engine-service`

Reason:

- this is the current standalone decision-side orchestration point
- it should keep creating screening requests or commands in transitional V1

Follow-up:

- replace direct provider-dispatch assumptions with `screening-service` integration

### 2. Screening result lifecycle and review

#### `api/usecases/screening_usecase.go`

Status:

- `move`

Main responsibilities to move:

- screening lookup and listing
- screening execution wrapping around provider search
- refinement
- freeform search
- whitelist usage for screening
- match review
- match comments
- enrichment
- file upload and download support
- review-side case effects

Reason:

- this file is one of the clearest indicators of the future `screening-service` boundary

#### `api/api/handle_screening.go`

Status:

- `move`

Endpoints or behaviors represented here:

- dataset freshness
- dataset catalog
- list screenings
- update screening match status
- upload screening files
- list screening files
- download screening files
- refine screening
- search screening
- enrich screening match
- freeform search

Reason:

- these are service-facing screening APIs
- they belong in `screening-service`, not the decision engine

#### `api/api/handle_screening_ai_suggestion.go`

Status:

- `defer`

Reason:

- AI screening suggestions are related to screening review, but they introduce a different extraction concern
- this should not block core screening-service extraction

### 3. Continuous or list-screening domain

#### `api/usecases/continuous_screening/screening.go`

Status:

- `move`

Responsibilities:

- continuous-screening review lifecycle
- dismiss and load-more flows
- whitelist creation from no-hit review
- risk-tag side effects
- object-triggered versus dataset-triggered review semantics

Reason:

- this is core service-owned behavior for continuous or list screening

#### `api/usecases/continuous_screening/monitored_object.go`

Status:

- `move`

Responsibilities:

- create monitored object
- delete monitored object
- fetch data-model mapping
- read ingested object
- do object-triggered screening
- create case for monitored-object screenings
- list monitored objects

Reason:

- this file contains the heart of monitored-object orchestration

#### `api/usecases/continuous_screening/org_config.go`

Status:

- `move`

Responsibilities:

- create and update continuous-screening config
- validate algorithms
- validate inbox target
- apply mapping configuration
- validate data-model configuration

Reason:

- config, mapping, and inbox routing are part of continuous-screening service ownership

#### `api/usecases/continuous_screening/worker_do_screening.go`

Status:

- `move`

Responsibilities:

- object-added and object-updated worker flow
- screening deduplication
- case-creation trigger
- delta-track recording

Reason:

- canonical worker flow for monitored-object screening

#### `api/usecases/continuous_screening/worker_apply_delta_file.go`

Status:

- `move`

Reason:

- provider dataset update application is squarely in screening-service scope

#### `api/usecases/continuous_screening/worker_scan_dataset_update.go`

Status:

- `move`

Reason:

- dataset update discovery and scheduling is screening-domain work

#### `api/usecases/continuous_screening/worker_match_enrichment.go`

Status:

- `move`

Reason:

- continuous-screening match enrichment belongs with screening enrichment overall

### 4. Screening persistence and repository layer

#### `api/repositories/screening_repository.go`

Status:

- `move`

Reason:

- core screening result and match persistence

#### `api/repositories/screening_whitelist_repository.go`

Status:

- `move`

Reason:

- screening-specific whitelist ownership should live in `screening-service`

#### `api/repositories/screening_config_repository.go`

Status:

- split

Recommended ownership:

- decision-time scenario-linked screening config persistence: `stay` with `decision-engine-service` in transitional V1
- continuous-screening config persistence: `move` through continuous-screening repositories and future screening-service schema

Reason:

- there are really two kinds of config today under similar terminology

#### `api/repositories/continuous_screening_repository.go`

Status:

- `move`

Reason:

- this is the main persistence owner for continuous or list-screening state

#### `api/repositories/continuous_screening_client_repository.go`

Status:

- `move`

Reason:

- monitored-object registry and client-db-side internal tracking are part of continuous-screening orchestration

#### `api/repositories/opensanctions_repository.go`

Status:

- `move`

Reason:

- OpenSanctions-compatible provider adapter belongs in `screening-service`

Note:

- this should become an adapter behind provider ports rather than remain monolith-shaped

#### `api/repositories/name_recognition_repository.go`

Status:

- `move`

Reason:

- optional preprocessing support for screening queries belongs beside screening provider adapters

#### `api/repositories/dbmodels/db_screening*.go`

Status:

- `move`

Reason:

- screening entity tables and db-model adapters are screening-owned persistence

#### `api/repositories/dbmodels/db_continuous_screening*.go`

Status:

- `move`

Reason:

- continuous or list-screening persistence is screening-owned

### 5. Provider and infra-specific code

#### `api/infra/opensanctions.go`

Status:

- `move`

Reason:

- provider configuration, SaaS versus self-hosted handling, Motiva capability detection, scoped-index support, and NER sidecar configuration are all screening-specific infrastructure concerns

Migration note:

- it should likely be reshaped into provider adapters and config loaders rather than copied as-is

#### `api/docker-compose.yml` entries for `yente` and `motiva`

Status:

- `move` conceptually into screening-service local-dev and ops ownership

Reason:

- these runtime dependencies belong to the screening stack

Implementation note:

- the future standalone backend may choose a different compose layout, but ownership should move with screening-service

#### `api/contrib/screening-manifest.json`

Status:

- `move`

Reason:

- screening dataset manifest belongs to screening provider operations

### 6. Shared but not screening-owned capabilities

#### `api/usecases/custom_list_usecase.go`

Status:

- `shared`

Reason:

- company custom lists are used by screening preprocessing, but they are not exclusively a screening concept
- they are also used by rule-engine functions and import or export flows

What screening-service likely needs:

- read-only access to list metadata and values

What should remain outside screening-service in V1:

- create custom list
- update custom list
- CSV replace values
- delete custom list values
- import and export ownership

#### `api/models/custom_list.go`

Status:

- `shared`

Reason:

- base model remains part of the general custom-list capability unless we later decide to centralize it elsewhere

#### `new/backend/decision-engine-service` custom-list helper endpoints

Status:

- `stay` for now

Reason:

- the standalone decision engine already owns helper-data support for rule evaluation
- screening-service should consume custom-list reads rather than take over that capability by default

#### `data-model-service`

Status:

- `shared`

Reason:

- screening-service depends on read contracts only

#### `ingestion-service`

Status:

- `shared`

Reason:

- screening-service depends on record reads and ingestion-trigger notifications

### 7. Case-management and workflow-adjacent behaviors

#### case creation and case event side effects currently embedded in screening usecases

Status:

- `shared`

Reason:

- these are triggered by screening behavior, but the underlying case domain should not automatically become screening-owned

V1 expectation:

- screening-service coordinates these through an explicit port

#### inbox validation for continuous-screening configs

Status:

- `shared`

Reason:

- screening-service needs inbox target validation, but inbox ownership is external

#### evidence upload contributor creation

Status:

- `shared`

Reason:

- screening-service likely triggers it
- case domain or collaboration domain likely owns it

### 8. Feature access and entitlement checks

#### sanctions and continuous-screening feature-access checks

Status:

- `shared`

Reason:

- screening-service must enforce them
- feature-access ownership should remain external or platform-level

#### screening-specific permission checks

Status:

- `move` in policy-adapter form

Reason:

- screening-service must own how it applies policy to its actions
- but it should consume identities and permission sources from shared auth or platform systems

### 9. Analytics and reporting

#### screening analytics export logic

Status:

- `defer`

Reason:

- important, but not necessary for first extraction of core runtime
- may later move to screening-service-owned analytics export, or to a shared analytics pipeline

### 10. AI and auxiliary features

#### AI screening suggestion flows

Status:

- `defer`

Reason:

- related to screening review
- not necessary to establish the core service boundary

#### KYC enrichment and broader AI case review

Status:

- `stay` external

Reason:

- not part of the screening-service core boundary

## Existing standalone modules impact map

### `new/backend/decision-engine-service`

Keep:

- scenario authoring
- decision evaluation
- decision persistence
- screening request orchestration in transitional V1

Change:

- replace provider-facing screening dispatch with `screening-service` integration
- reduce ownership of screening lifecycle details over time

### `new/backend/data-model-service`

Keep:

- schema authority
- field and mapping metadata authority

Add only by contract:

- read contracts needed by screening-service for mapping validation

### `new/backend/ingestion-service`

Keep:

- write authority
- tenant record read APIs
- ingestion event source role

Add only by contract:

- explicit screening-service-friendly read and event contracts if needed

## V1 extraction cut recommendation

### Extract first

- OpenSanctions-compatible provider adapter
- screening result persistence
- review APIs
- whitelist APIs
- enrichment APIs
- freeform screening

### Extract second

- continuous-screening config and monitored objects
- object-triggered workers

### Extract third

- dataset update jobs
- delta-processing and re-screening backfill flows

### Defer until after service boundary is stable

- AI screening suggestions
- analytics export ownership
- any attempt to absorb general custom-list CRUD into screening-service

## Final interpretation

If a source file or responsibility is mainly about:

- matching against external watchlist or sanctions data
- managing screening review state
- handling screening whitelist semantics
- orchestrating monitored-object or dataset-update screening

it belongs in `screening-service`.

If it is mainly about:

- deciding whether a scenario should trigger
- running rules
- storing decisions
- owning general custom-list CRUD
- owning schema or ingestion writes

it does not belong in `screening-service`.
