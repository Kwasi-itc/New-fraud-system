# Decision Engine Service

Standalone Go service for Marble's decision engine domain, extracted from the monolithic `api` service and designed to work alongside `data-model-service` and `ingestion-service`.

Current location in the workspace:

- `new/backend/decision-engine-service`

This folder now contains an active standalone service implementation plus the original planning documents.

## Purpose

The service is intended to own the full Marble decision-engine behavior:

- scenario authoring
- scenario iteration authoring
- rule authoring
- rule snoozes
- screening config authoring
- scenario test runs
- phantom decisions for test runs
- AST validation
- publication and live-version management
- publication preparation checks
- runtime scenario evaluation
- decision creation and persistence
- rule execution persistence
- analytics field capture for decisions
- optional offloading and rehydration of large rule-evaluation payloads
- test-run summary generation
- scheduled execution orchestration
- async batch decision execution
- workflow triggering after decisions
- case-creation and case-attachment workflow actions
- decision-created and workflow-related webhook/event creation
- payload parsing and enrichment on decision-ingest evaluation paths
- optional screening integration
- optional scoring integration

Notably separate from this scope unless explicitly pulled in later:

- Marble's broader `continuous_screening` subsystem and dataset-update workers

## Intended service boundary

The decision engine service should own:

- executable scenario definitions and versions
- runtime evaluation logic
- decisions and execution history
- scheduling and async execution state
- workflow definitions and dispatch triggers

The decision engine service should not own:

- tenant schema management
- physical table and field lifecycle
- raw ingestion writes
- being the source of truth for tenant data storage layout

## Expected dependencies

The service is expected to depend on:

- `data-model-service`
  - assembled tenant model contract
  - links, pivots, navigation options, field typing, tenant/model revision
- tenant data read access
  - direct PostgreSQL tenant-schema reads or an equivalent read abstraction
- `ingestion-service`
  - post-ingestion trigger integration, likely through events or explicit callbacks
- optional external services
  - screening providers
  - scoring services
  - webhook/event delivery infrastructure
  - case management / case review infrastructure if kept external

## Planned documents

- [DECISION_ENGINE_SERVICE_EXTRACTION_DESIGN.md](./DECISION_ENGINE_SERVICE_EXTRACTION_DESIGN.md)
- [DECISION_ENGINE_SERVICE_IMPLEMENTATION_BLUEPRINT.md](./DECISION_ENGINE_SERVICE_IMPLEMENTATION_BLUEPRINT.md)
- [DECISION_ENGINE_SERVICE_INTEGRATION_CONTRACTS.md](./DECISION_ENGINE_SERVICE_INTEGRATION_CONTRACTS.md)
- [DECISION_ENGINE_SERVICE_DOMAIN_BREAKDOWN.md](./DECISION_ENGINE_SERVICE_DOMAIN_BREAKDOWN.md)
- [DECISION_ENGINE_SERVICE_V1_OPERATING_DECISIONS.md](./DECISION_ENGINE_SERVICE_V1_OPERATING_DECISIONS.md)
- [IMPLEMENTATION_TODO.md](./IMPLEMENTATION_TODO.md)
- [MF_HANDOFF.md](./MF_HANDOFF.md)

## Current status

Implemented:

- Go service scaffold with `server`, `worker`, and `migrate` commands
- PostgreSQL-backed persistence and follow-up metadata migrations
- scenario authoring: list, create, get, update, copy, latest-rules
- iteration authoring: list, create draft, get, update, metadata, create-from-existing, commit
- publication lifecycle: publish, unpublish, preparation status, preparation start
- publication preparation readiness using `data-model-service`
- rule authoring APIs
- AST validation against `data-model-service`
- runtime evaluation, decision persistence, and rule execution persistence
- decision reads plus tenant-level create/list/create-all flows
- ingestion-triggered evaluation endpoint
- test runs, phantom decisions, phantom rule executions, cancel, and summaries/stats
- rule snoozes
- legacy flat workflow definitions and workflow execution persistence
- structured workflow rule/condition/action authoring and reorder
- structured workflow runtime matching and execution creation
- screening/scoring config authoring
- screening execution and scoring request persistence
- screening/scoring lifecycle inspection, status update, retry, and provider-result payload ingestion
- outbox event persistence
- scheduled and async decision execution processing
- worker support for both one-shot batch runs and poll-loop execution
- maintained OpenAPI spec for the implemented route surface

Still intentionally provisional or deferred:

- worker behavior still lives in the `cmd/worker` entrypoint rather than a dedicated internal worker package
- workflow side effects are still dispatch-shell behavior, not a settled case-management integration
- screening and scoring still depend on external provider execution even though orchestration state is service-owned
- relation-heavy evaluator semantics still need tighter definition beyond the current baseline tests
- several planning documents still describe future-state architecture rather than the exact implemented state
- payload parsing/enrichment and evaluation offloading are still design-level scope, not implemented runtime features

## Current implementation shape

The current package layout differs slightly from the original planning docs:

- AST runtime and AST validation now live in `internal/runtime/ast_eval`
- application orchestration remains in `internal/service`
- worker behavior currently lives in the `cmd/worker` entrypoint rather than a dedicated `internal/worker` package
- screening, scoring, and platform helper integrations currently use service-owned repositories plus dispatch/provider shells
- workflow support now exists in two layers:
  - legacy flat workflow definitions for backward-compatible V1 behavior
  - structured workflow rules, conditions, and actions for closer monolith parity

## Environment

The service loads `.env` automatically if present. Use `.env.example` as the starting point.

Required variables:

- `DATABASE_URL`
- `DATA_MODEL_SERVICE_URL`
- `INGESTION_SERVICE_URL`

Common local defaults in `.env.example`:

- `DATA_MODEL_SERVICE_URL=http://localhost:8081`
- `INGESTION_SERVICE_URL=http://localhost:8080`
- `PORT=8082`

## Provider status callback shape

Screening executions and scoring requests can now be updated with provider-result metadata, not just a status string.

Example screening execution update:

```json
{
  "status": "completed",
  "provider_reference": "screening-job-42",
  "response_json": {
    "matches": [
      {
        "dataset": "pep",
        "score": 0.98
      }
    ],
    "provider_status": "cleared"
  },
  "last_error": ""
}
```

Example scoring request update:

```json
{
  "status": "failed",
  "provider_reference": "score-run-17",
  "response_json": {
    "provider_status": "error",
    "reason_code": "upstream_timeout"
  },
  "last_error": "provider timeout after 30s"
}
```

Persisted screening/scoring execution records now include:

- `request_json`
- `response_json`
- `provider_reference`
- `last_error`
- `created_at`
- `updated_at`
- `sent_at`
- `completed_at`
- `failed_at`
