# MF Handoff

This file is a compact handoff for the planned standalone decision engine service in `new/backend/decision-engine-service`.

## What this service is

This is not just an AST evaluator.

It is the standalone extraction of Marble's full decisioning domain, including:

- scenario and rule authoring
- rule snooze management
- scenario iteration versioning
- publication/live version handling
- publication preparation handling
- AST validation
- runtime evaluation
- decision persistence
- analytics field persistence and optional evaluation offloading
- phantom decisions and scenario test runs
- scheduled executions
- async decision executions
- workflow triggering
- workflow-driven case creation / add-to-case behavior
- webhook and event creation tied to decisions and workflows
- payload parsing/enrichment behavior for raw evaluation requests
- optional screening and scoring integrations

## What it depends on

- `data-model-service`
  - assembled model contract
  - links, pivots, navigation, field types, revision
- tenant data reads
  - for payload lookups, database access, aggregators, pivots, scheduled scans
- custom list reads
- feature-access checks where still external
- case-management integration if workflow actions stay cross-service
- payload enrichment dependencies if evaluation starts from raw payloads
- `ingestion-service`
  - for post-ingestion evaluation triggering

## Key design rule

The decision engine should be the execution authority, while `data-model-service` remains schema authority and `ingestion-service` remains write authority.

## Immediate next planning tasks

- define the exact contract from `data-model-service`
- define the tenant data read abstraction
- define test-run and phantom-decision persistence needs
- define rule-snooze ownership and APIs
- define workflow-to-case integration ownership
- define webhook/outbox ownership
- define payload enrichment ownership
- define watermark ownership for summaries/offloading if retained
- decide V1 screening scope
- decide V1 scoring scope
- define the service-owned persistence model
