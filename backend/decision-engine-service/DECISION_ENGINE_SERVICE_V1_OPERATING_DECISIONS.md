# Decision Engine Service V1 Operating Decisions

This document records the current V1 operating decisions for the standalone `decision-engine-service`.

It is intentionally narrower than the broader design documents. The goal is to remove ambiguity around the choices the current implementation is already making.

## 1. Worker model

V1 supports two worker operating modes:

- `batch`
  - run one processing cycle and exit
  - suitable for cron, scheduled jobs, or ad hoc maintenance runs
- `poll`
  - run one processing cycle immediately, then continue polling on an interval
  - suitable for long-running service deployments

Current configuration knobs:

- `WORKER_MODE`
  - allowed values: `batch`, `poll`
  - default: `batch`
- `WORKER_POLL_INTERVAL`
  - default: `15s`
- `WORKER_BATCH_LIMIT`
  - default: `100`

### Current worker responsibilities

Each worker cycle processes:

- due scheduled executions
- queued async decision executions
- pending workflow executions
- pending screening executions
- pending scoring requests
- pending outbox events

### V1 decision

V1 keeps both worker modes instead of forcing one deployment shape. This preserves compatibility with both:

- a long-running standalone service deployment
- a scheduled-runner or cron-style deployment

## 2. Workflow and case side effects

The decision engine does not own case management in V1.

Instead, it owns:

- workflow definitions
- workflow execution records
- dispatch state transitions

The actual side effect should be executed through an external workflow-action or case-management endpoint.

### Current dispatch contract

The workflow dispatcher currently sends:

- `workflow_execution_id`
- `tenant_id`
- `workflow_id`
- `workflow_rule_id`
- `workflow_action_id`
- `decision_id`
- `scenario_id`
- `action_type`
- `action_config`
- `created_at`

### V1 decision

Workflow side effects remain cross-service in V1.

That means:

- this service persists intent and dispatch state
- a downstream workflow-action or case-management service performs the actual case mutation
- retries and dispatch failures remain visible on workflow execution records

This keeps case-management ownership out of the decision engine boundary while preserving Marble-inspired workflow intent.

## 3. Helper data ownership

The current standalone service already owns helper persistence for:

- custom list entries
- record tags
- risk snapshots
- IP flags

### V1 decision

These helper datasets are treated as service-owned V1 dependencies, not temporary scaffolding.

That means:

- evaluator helper functions continue reading them locally
- their APIs remain in the decision engine service for V1
- they should be treated as part of the supported standalone decisioning boundary unless a later extraction is deliberate and planned

## 4. Screening and scoring ownership

The current service already owns:

- screening config authoring
- screening execution records
- scoring config authoring
- scoring request records
- dispatch and status progression
- inspection of one screening execution or scoring request
- explicit status updates for one screening execution or scoring request
- retry of one screening execution or scoring request by resetting it to `pending`

It does not own provider execution itself. Provider interaction is still adapter-based.

### V1 decision

Screening and scoring remain hybrid concerns in V1:

- orchestration state is owned by `decision-engine-service`
- provider execution remains external

This means the service is the source of truth for decision-time orchestration state, while provider contracts remain replaceable integrations.

### Current limitation

The service still does not ingest rich provider result payloads in V1.

That means:

- providers can drive lifecycle state back into the service
- providers cannot yet submit a first-class persisted result document through the current API

## 5. Rule-engine semantic baseline

The standalone rule engine currently treats the following as the baseline semantics to preserve:

- `past_decision_count`
  - counts internal decision-history records for the current object
  - optional `outcome` narrows the count
- `related_count`
  - reads records from `TenantDataReader`
  - requires `object_type` and `field`
  - without `equals`, counts non-`nil` values for the given field
  - with `equals`, counts records whose field value equals the evaluated right-hand value
- `related_field`
  - follows `links_to_single`
  - returns `nil` if the lookup value is missing or no related record is found
  - errors when the link path or target field is invalid against the tenant model

### V1 decision

These semantics are the current contract of the standalone runtime unless an explicit compatibility change is made.

Future Marble-alignment work should be treated as a conscious contract change, not as an implicit refactor.
