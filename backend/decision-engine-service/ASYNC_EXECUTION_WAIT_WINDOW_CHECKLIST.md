# Async Execution Wait Window Checklist

## Goal

Add a hybrid async execution mode to the decision engine so an async request can:

- wait briefly for a result, using a short configurable time window
- return the completed decision immediately if execution finishes within that window
- otherwise return the normal async execution response without blocking longer
- optionally deliver the final result to a configured webhook when execution reaches a terminal state

This should build on the existing `async_decision_executions` model. River remains the worker engine, but `async_decision_executions` stays the source of truth for status and result state.

## Current baseline

- `POST /v1/tenants/{tenantId}/async-decision-executions` creates an async execution row and returns immediately
- background execution is handled through River-backed worker processing
- clients currently need to poll the async execution endpoints to observe completion
- there is no short wait window on create
- there is no webhook callback delivery on completion or failure

## Scope

Implement:

- optional request field for a short wait window
- optional request field for webhook callback configuration
- create endpoint support for opportunistic early return
- persistence of terminal result data needed for later retrieval or webhook delivery
- webhook delivery on completion or failure
- tests and OpenAPI updates

Out of scope for this slice:

- generic webhook management for the whole platform
- complex callback retry dashboards
- multi-destination fan-out callbacks
- replacing the existing polling endpoints

## API design

- [x] Add `wait_timeout_ms` to the async execution request
- [x] Default `wait_timeout_ms` to `300` when omitted
- [x] Cap `wait_timeout_ms` to a safe upper bound, for example `500` or `1000`
- [x] Document that the wait window is best-effort, not guaranteed
- [x] Add optional `callback_url` to the async execution request
- [x] Decide whether callback auth will be:
  - signed payloads only
  - static headers stored on the request
  - a referenced secret/config object
- [x] Decide whether callbacks fire on:
  - `completed` only
  - `completed` and `failed`
- [x] Decide whether the create endpoint response becomes a union shape or a stable envelope with:
  - execution metadata
  - current status
  - optional inline result when completed in time

## Domain and persistence

- [x] Review `async_decision_executions` and identify what new fields are needed
- [x] Add persistence for callback configuration
- [x] Add persistence for final result payload if current fields are not enough
- [x] Add persistence for callback delivery state if auditability is required
- [x] Decide whether callback attempt history belongs:
  - on `async_decision_executions`
  - in a dedicated callback-delivery table
- [x] Add migrations for any new fields or tables

## Service behavior

- [x] Extend `AsyncDecisionExecutionRequest` in the execution service
- [x] Keep the existing create-and-enqueue flow as the first step
- [x] After enqueue, wait on the execution record for up to `wait_timeout_ms`
- [x] Wait on the domain record state, not on River internals
- [x] Return the completed decision result when the execution reaches terminal success within the window
- [x] Return the normal async response when the timeout expires first
- [x] Confirm idempotency behavior when the same `idempotency_key` is reused with different wait-window values
- [x] Confirm idempotency behavior when the same `idempotency_key` is reused with different callback values

## Worker completion path

- [x] Identify the completion path where async executions are marked `completed`
- [x] Trigger callback delivery after terminal success
- [x] Trigger callback delivery after terminal failure if supported
- [x] Ensure callback sending does not block the worker from updating execution status
- [x] Decide whether callback delivery happens:
  - inline after completion
  - through a separate River-backed callback job
- [x] Prefer a separate callback job if delivery reliability and retries matter

## Webhook delivery design

- [x] Define the callback payload shape
- [x] Include at minimum:
  - `execution_id`
  - `tenant_id`
  - `scenario_id` when present
  - `status`
  - result data on success
  - error data on failure
- [x] Add payload signing strategy
- [x] Add timestamp and signature headers
- [x] Define timeout and retry rules for outbound webhook calls
- [x] Decide how duplicate deliveries are handled
- [x] Decide whether callback failures affect the async execution status
- [x] Keep callback delivery failure separate from decision execution success/failure

## HTTP handlers

- [x] Update the async execution create handler to accept the new request fields
- [x] Update handler responses to support early-return completed results
- [x] Preserve existing list/get/retry behavior for clients that continue polling
- [x] Validate malformed or unsafe callback URLs

## OpenAPI and Redoc

- [x] Update the async execution request schema
- [x] Update the async execution response schema
- [x] Add endpoint description text explaining the short wait window clearly
- [x] Add examples for:
  - plain async request
  - async request with `wait_timeout_ms`
  - async request with `callback_url`
  - quick completion response
  - queued response
  - failed callback example if callback status is exposed

## Testing

- [x] Add service tests for early-return completed responses
- [x] Add service tests for timeout returning normal async responses
- [x] Add service tests for idempotency interactions
- [x] Add service tests for callback enqueue or delivery triggering
- [x] Add handler tests for request validation
- [x] Add handler tests for both response modes
- [ ] Add DB-backed integration tests covering:
- [x] Add DB-backed integration tests covering:
  - create -> complete within wait window
  - create -> timeout -> later complete
  - create with callback -> successful callback delivery
  - create with callback -> callback failure and retry behavior

## Rollout and safeguards

- [ ] Gate the feature behind config if we want a staged rollout
- [x] Add config for default wait window
- [x] Add config for maximum wait window
- [x] Add config for webhook timeout and retry policy
- [ ] Add metrics for:
  - quick completions within wait window
  - timed-out create responses
  - callback successes
  - callback failures
- [x] Add logs that make callback troubleshooting possible without exposing sensitive payloads

## Recommended implementation order

- [x] Finalize API response shape
- [x] Add request fields and migrations
- [x] Persist final execution result cleanly
- [x] Implement short wait-window behavior in the create path
- [x] Add callback configuration and delivery path
- [x] Add signing and retry behavior
- [x] Update OpenAPI and examples
- [x] Run package tests
- [ ] Run DB-backed integration tests
- [x] Run DB-backed integration tests
- [ ] Review behavior with a real client flow before enabling widely

## Done when

- [x] Async create can return an inline completed result when execution finishes within the allowed window
- [x] Async create falls back cleanly to the normal async response when it does not finish in time
- [x] Final result can still be retrieved from existing async execution read endpoints
- [x] Optional webhook delivery works for terminal execution outcomes
- [x] OpenAPI and Redoc describe the feature clearly
- [x] Automated tests cover the two response modes and callback behavior
