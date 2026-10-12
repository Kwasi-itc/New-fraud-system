# Screening Service

## Reliable case callbacks

Match reviews and file creation now enqueue case callbacks in `screening.case_event_outbox` in the same transaction as their mutations. The default Compose service `screening-case-delivery-worker` delivers them independently of provider jobs; its executable is `/app/case-delivery-worker`. It requires `DATABASE_URL`, `CASE_SERVICE_URL`, and `CASE_SERVICE_AUTH_TOKEN`. The case token's tenant allowlist must cover the producer tenants. Inbox lookups use `INBOX_SERVICE_URL` and the same case token.

Migration `000004_case_event_outbox` must run before producers and delivery workers. Delivery uses stable event IDs, recoverable leases, exponential backoff, and visible failure state; inspect `status`, `attempts`, and `last_error` in the outbox. Missing configuration fails worker startup. Review callbacks are supported; evidence callbacks currently return 501 and remain queued/failed until evidence intake is implemented.

See the [case intake contract](../case-manager-service/INTAKE_CONTRACT.md) for payloads, authentication, retry/replay operations, and coordinated migration requirements. Set `SCREENING_TEST_DATABASE_URL` to a local PostgreSQL admin DSN to enable disposable-database integration tests with `go test ./...`.

Standalone backend service workspace for extracting Marble's screening domains out of the monolith and away from the decision engine runtime.

Current location in the workspace:

- `new/backend/screening-service`

This folder now contains:

- planning documentation
- initial Go service scaffold
- V1 screening runtime and worker slice
- migrations for the first screening-owned schema
- rollout and operational runbooks

## Purpose

The target service is intended to own the screening-related domains that do not naturally belong inside:

- `data-model-service`
- `ingestion-service`
- `decision-engine-service`

In practical terms, this means the service covers:

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

## Design and planning documents

- [SCREENING_SERVICE_EXTRACTION_DESIGN.md](./SCREENING_SERVICE_EXTRACTION_DESIGN.md)
- [SCREENING_SERVICE_DOMAIN_BREAKDOWN.md](./SCREENING_SERVICE_DOMAIN_BREAKDOWN.md)
- [SCREENING_SERVICE_IMPLEMENTATION_BLUEPRINT.md](./SCREENING_SERVICE_IMPLEMENTATION_BLUEPRINT.md)
- [SCREENING_SERVICE_INTEGRATION_CONTRACTS.md](./SCREENING_SERVICE_INTEGRATION_CONTRACTS.md)
- [SCREENING_SERVICE_V1_OPERATING_DECISIONS.md](./SCREENING_SERVICE_V1_OPERATING_DECISIONS.md)
- [IMPLEMENTATION_TODO.md](./IMPLEMENTATION_TODO.md)

Operational follow-up documents:

- [SCREENING_SERVICE_ROLLOUT_CHECKLIST.md](./SCREENING_SERVICE_ROLLOUT_CHECKLIST.md)
- [SCREENING_SERVICE_OPERATIONS_RUNBOOK.md](./SCREENING_SERVICE_OPERATIONS_RUNBOOK.md)

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

The planning pack in this folder still documents the broader target boundary in detail. The main remaining work is live rollout confirmation, downstream environment wiring, and operational adoption, not missing core module boundaries inside `screening-service`.

## Operating notes

HTTP surfaces:

- `GET /healthz`: process health
- `GET /readyz`: database readiness
- `GET /metrics`: JSON request metrics snapshot
- `GET /openapi.yaml`: baseline screening-service OpenAPI spec
- `GET /docs`: Swagger UI for the embedded screening-service spec
- `GET /v1/service-info`: active downstream URL wiring
- `POST /internal/v1/tenants/:tenantId/decision-screenings`: decision-engine intake contract
- `POST /internal/screening-status-updates`: expected callback receiver on `decision-engine-service`

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
- `.env.example` includes `BLOB_SERVICE_URL` alongside the provider, ingestion, inbox, case, and decision-engine URLs expected by the current runtime
- `DECISION_ENGINE_URL` is optional, but when configured the worker and review flows publish screening status updates back to the decision engine
- when `SERVICE_AUTH_MODE=token`, internal intake and callback requests use `SERVICE_AUTH_TOKEN` bearer auth
- `SCREENING_PROVIDER_URLS` can be JSON or comma-separated `provider=url` pairs for provider-key routing; `SCREENING_PROVIDER_URL` remains the fallback default
- for direct OpenSanctions ownership inside `screening-service`, configure `OPENSANCTIONS_API_HOST`, `OPENSANCTIONS_AUTH_METHOD`, `OPENSANCTIONS_API_KEY`, `OPENSANCTIONS_SCOPE`, and `OPENSANCTIONS_ALGORITHM`

Rollout and validation references:

- use [SCREENING_SERVICE_ROLLOUT_CHECKLIST.md](./SCREENING_SERVICE_ROLLOUT_CHECKLIST.md) to confirm migration `000003_decision_engine_and_idempotency`, env wiring, and end-to-end flow coverage
- use [SCREENING_SERVICE_OPERATIONS_RUNBOOK.md](./SCREENING_SERVICE_OPERATIONS_RUNBOOK.md) for retry, backlog, provider outage, and callback verification procedures

Decision-engine callback payload fields currently include:

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

Minimum verification routine after changes:

- run `go test ./...`
- hit `/readyz` against a configured database
- hit `/metrics` after a few requests to confirm request accounting is working

## Continuous screening case intake

The continuous worker creates the screening, its `continuous_runs` provenance, the River job, and the monitored-object advancement in one transaction. It locks only the monitored row during this mutation; tenant-data HTTP lookup occurs beforehand. Retrying an already advanced monitored run does nothing. Marking the object pending again for a later dataset update produces a distinct run and investigation.

Screening and monitored-object enqueue operations deliberately do not deduplicate by River arguments: a retained completed job or a finishing failed attempt must not suppress a later legitimate request for that same source ID. The monitored-row transaction and screening claims provide execution idempotency. Redundant jobs can be queued but cannot publish duplicate runs/results. Dataset-job enqueue behavior is unchanged.

Processing uses a six-minute UUID claim. The River worker timeout is five minutes. A live claim snoozes another job attempt for one minute; expired/legacy processing claims can be recovered. Result publication locks and verifies the claim token, so an older provider response cannot overwrite a replacement worker's results. Failed publication/cancellation releases its own claim back to pending. Process crashes rely on lease expiry and River rescue/retry. Provider failures retain the existing explicit failed-screening/retry workflow. Retry now rechecks the failed state under the screening row lock before clearing matches. New screening and monitored jobs allow ten attempts for infrastructure failures; existing jobs retain their recorded attempt policy. Exhausted jobs require operator inspection and requeue.

For newly created continuous runs and decision-linked screenings, results with matches **or partial results** atomically persist result, matches, and a `requires-review` case outbox event. Partial zero-match results become `awaiting_review`; complete zero-match results become `no_hit` without an arrival event. Freeform screening eligibility is unchanged. Version 1 sends only event/tenant/screening identities; the consumer reads tenant-scoped origin/configuration and current match state from the shared database. Event ID is UUID v5, URL namespace, name `screening.requires-review/v1/<tenant UUID>/<screening UUID>`.

Decision-linked intake reuses primary decision membership. Without membership it creates a pending decision case in the originating scenario screening configuration's explicit `review_inbox_id`. Missing/invalid routing retains delivery for retry; do not infer a destination from continuous-monitoring configuration. Apply decision-engine migration `000017_screening_review_inbox` and case migrations through 000012, deploy the accepting case consumer and configure routes before enabling this producer extension. See [decision-engine rollout](../decision-engine-service/README.md#screening-review-routing). No additional screening migration or event transport is required. Historical unpublished decision reviews synchronize only to an existing primary case; they do not synthesize case arrival. New arrivals and reviewed-first delivery share the same intake path. Primary closure/snooze state is preserved, and transfers determine subsequent evidence routing. Review remains an independent owner operation; this is asynchronous intake, not a blocking review gate.

Run `cmd/case-delivery-worker` with `CASE_SERVICE_URL` and its service token configured. It posts to `/v1/screening-events/requires-review`, independently from result publication. Failed requests retain retry state; existing delivery policy retries up to 20 attempts, then marks the event failed. Case creation failure does not erase results or rerun the provider. After fixing routing/authentication/consumer availability, operators must inspect and replay exhausted deliveries using the same event ID/payload. Do not create a replacement event or discard the source record. Keep published arrival records: the consumer uses them as durable eligibility evidence for subsequent reviewed callbacks.

Roll out in this order:

1. Pause/drain screening processing and delivery workers while upgrading; do not run old unfenced workers alongside new workers. Retain the shared database and existing service migration histories.
2. Apply screening migration `000005_continuous_case_arrival` and case-manager migration `000011_continuous_intake` after their existing predecessors. Screening provenance must exist before the new case API handles reviewed callbacks. Migrations use bounded lock waits; validate index/constraint build duration on representative data before production rollout.
3. Deploy the new case-manager consumer first, then screening API/processing/delivery workers and frontend. Resume producers only with the compatible consumer in place. Verify tenant allowlists, current enabled config/inbox routing, worker readiness and delivery backlog.
4. Verify a new matched run appears as pending before review, partial zero-match visibility, retry deduplication, and a reviewed callback updating that same case. No automatic historical backfill is performed.

For rollback, pause producers/delivery and preserve database additions, source outbox records and canonical routes. New history blocks destructive down migrations. Processing tokens also block producer downgrade. An application rollback must retain a compatible reviewed/arrival consumer for new runs; an old consumer is not safe for them. Empty-schema up/down/up is supported and tested. Provenance FKs deliberately prevent deleting originating configurations/monitored objects that have runs; disable monitoring instead of deleting investigation history. This is an additive shared-schema contract, not a separate application database.

Verification: `SCREENING_TEST_DATABASE_URL` enables isolated PostgreSQL tests. Build the case-manager `cmd/server` executable, set `CASE_MANAGER_INTEGRATION_BINARY` to its absolute path, and run `go test ./... -count=1` here to include `TestContinuousProducerToCaseHTTPJourney`. That test creates a disposable shared database, applies owner migrations, runs real River processing, starts the actual case HTTP server, exercises delivery failure/retry and source review, and removes its database/processes. Provider/tenant-data responses are controlled inputs; external sanctions-provider availability and production identity/deployment are not certified.

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

## Initial investigator review

The case workspace now uses human-authenticated owner review commands. See [the investigator review contract](../investigator/README.md) for API semantics, migration order, shared Docker build context, trusted JWT configuration, and synchronization limitations.

## Durable execution projection

Screening status now reconciles through an atomic source queue and decision-owned SQL contract. See [deployment and recovery](../screening-service/EXECUTION_PROJECTION.md) for migrations, worker operation, callback compatibility and source-managed retry behavior.

## Screening evidence configuration

An empty blob-service URL now fails upload/download requests explicitly. It does not provide a local storage fallback or create file metadata for an upload with no destination. Configure a working blob integration before using screening-owned files. Source byte finalization, authenticated case download and the durable case evidence callback remain tracked in R09 of the Case Management implementation plan; native case-manager uploads are separate and unchanged.
