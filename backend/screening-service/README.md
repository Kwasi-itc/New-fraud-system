# Screening Service

Standalone backend service workspace for extracting Marble's screening domains out of the monolith and away from the decision engine runtime.

Current location in the workspace:

- `new/backend/screening-service`

This folder now contains:

- planning documentation
- initial Go service scaffold
- V1 screening runtime and worker slice
- migrations for the first screening-owned schema

## Purpose

The target service is intended to own the screening-related domains that do not naturally belong inside:

- `data-model-service`
- `ingestion-service`
- `decision-engine-service`

In practical terms, this means the future service should cover:

- decision-time screening provider execution
- screening result persistence
- screening match review lifecycle
- screening refinement and re-run flows
- screening whitelist management
- screening enrichment flows
- screening file attachment flows
- list or continuous screening configuration
- monitored-object registration and deregistration
- continuous or list screening execution workers
- dataset update processing for provider lists
- delta tracking and re-screening orchestration
- screening-driven case creation and related side effects
- freeform or manual screening flows
- organization-level screening defaults when those remain screening-owned

## Why this is a service

The legacy Marble screening area is not just a helper attached to rule evaluation.

It combines:

- provider integration
- background job execution
- screening-specific persistence
- match review state
- whitelist and enrichment state
- monitored-object state
- dataset-update state
- case-facing review flows

That makes it a bounded context and operational service, not only a decision-engine submodule.

## Planning documents

- [SCREENING_SERVICE_EXTRACTION_DESIGN.md](./SCREENING_SERVICE_EXTRACTION_DESIGN.md)
- [SCREENING_SERVICE_DOMAIN_BREAKDOWN.md](./SCREENING_SERVICE_DOMAIN_BREAKDOWN.md)
- [SCREENING_SERVICE_IMPLEMENTATION_BLUEPRINT.md](./SCREENING_SERVICE_IMPLEMENTATION_BLUEPRINT.md)
- [SCREENING_SERVICE_INTEGRATION_CONTRACTS.md](./SCREENING_SERVICE_INTEGRATION_CONTRACTS.md)
- [SCREENING_SERVICE_V1_OPERATING_DECISIONS.md](./SCREENING_SERVICE_V1_OPERATING_DECISIONS.md)
- [IMPLEMENTATION_TODO.md](./IMPLEMENTATION_TODO.md)

## Current implementation position

The current implementation follows the extraction recommendation:

- keep `decision-engine-service` as the execution authority for scenarios and decisions
- let `decision-engine-service` keep scenario-linked screening configuration and screening request orchestration for now
- extract provider-facing screening logic and screening lifecycle into a dedicated `screening-service`

Implemented today:

- screening intake APIs
- freeform screening intake
- screening persistence and match persistence
- review and comment APIs
- screening whitelist APIs
- match enrichment APIs
- evidence file metadata APIs
- blob-backed upload session and download URL delegation
- continuous-screening config CRUD
- monitored-object CRUD and requeue flow
- ingestion-backed monitored-object worker
- screening dispatch worker
- provider HTTP adapter contract
- provider dataset catalog and freshness endpoints
- provider dataset delta sync support
- dataset update job APIs and worker lifecycle
- provider-triggered monitored-object re-screening
- provider routing by provider key
- inbox validation for continuous-screening config targets
- case-side-effect publishing for screening review and evidence upload
- decision-engine callback publishing for screening status changes
- dedicated internal decision-engine screening intake contract
- idempotent screening intake by `idempotency_key`
- repository, handler, and service test coverage
- request metrics and structured operational logging

Current integration ports include:

- inbox validation for continuous-screening config targets
- case-side-effect publishing for screening review and evidence upload
- blob upload/download URL delegation for evidence files
- decision-engine status callback publishing
- provider routing from `SCREENING_PROVIDER_URLS`

The planning pack in this folder still documents the broader target boundary in detail. The main remaining work is downstream cutover and operational adoption, not missing core module boundaries inside `screening-service`.

## Operating notes

HTTP surfaces:

- `GET /healthz`: process health
- `GET /readyz`: database readiness
- `GET /metrics`: JSON request metrics snapshot
- `GET /v1/service-info`: active downstream URL wiring
- `POST /internal/v1/tenants/:tenantId/decision-screenings`: decision-engine intake contract

Server logging:

- every HTTP request now carries `X-Request-Id`
- request logs include method, route, status code, duration, and client IP
- request-scoped loggers are attached to the Gin context

Worker logging:

- worker cycles log start and completion
- each worker phase logs its own duration:
  - screening dispatch
  - continuous screening
  - dataset update jobs

Operational expectations:

- run `cmd/server` for API traffic
- run `cmd/worker` for background processing
- run `cmd/migrate` before first startup against a fresh database
- `screening-service` expects reachable downstreams for provider, ingestion, inbox, case, and blob integrations when those flows are exercised
- `DECISION_ENGINE_URL` is optional, but when configured the worker and review flows publish screening status updates back to the decision engine
- `SCREENING_PROVIDER_URLS` can be JSON or comma-separated `provider=url` pairs for provider-key routing; `SCREENING_PROVIDER_URL` remains the fallback default

Minimum verification routine after changes:

- run `go test ./...`
- hit `/readyz` against a configured database
- hit `/metrics` after a few requests to confirm request accounting is working

## Important boundary note on lists

The future `screening-service` is expected to own:

- screening whitelists
- monitored-object lists for continuous or list screening
- provider-dataset tracking used for screening

It is not currently intended to own Marble's general-purpose company custom lists as a product domain.

Those general custom lists are currently used by:

- decision-engine rule functions such as custom-list checks
- screening preprocessing steps such as ignore-list cleanup
- organization import and export flows
- CSV-based bulk custom-list value replacement

So the planning assumption is:

- general custom lists remain a shared platform or decision-support capability outside `screening-service`
- screening-specific whitelist state remains inside `screening-service`
