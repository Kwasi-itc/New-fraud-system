# Auto Indexing Plan

## Goal

Bring `new/backend/data-model-service` to a clearer automatic-indexing model, closer to the old Marble backend where useful, while keeping the newer explicit metadata-first design:

- keep `core.index_jobs` as the visible job record
- keep River as the execution engine
- auto-enqueue the right managed indexes when data-model changes make them necessary
- avoid hiding heavy background work behind undocumented side effects

## Current state

The service already does some automatic indexing:

- table creation creates the unique `object_id` index synchronously
- field create/update creates or drops unique indexes synchronously when `is_unique` changes
- navigation option creation automatically creates a `navigation` index job
- `reconcile` automatically schedules `repair` index jobs for missing managed indexes

What is missing compared with the old `api` backend:

- no automatic `search` index job when `caption_field` is set or changed
- no explicit policy document for what should be:
  - synchronous
  - auto-enqueued
  - manual only
- no cleanup path that reacts when a table no longer needs a managed search index

## Recommended indexing policy

Keep this split:

- synchronous:
  - required base indexes
  - field-level unique indexes
- auto-enqueued:
  - navigation indexes
  - search indexes tied to `caption_field`
  - repair indexes created by reconcile
- manual only:
  - ad hoc index-job creation through the API for operator use

This keeps correctness-critical indexes immediate, but keeps optional or heavier secondary indexes in the worker path.

## Main gap to implement

### 1. Auto search index on `caption_field`

When a table gets a non-empty `caption_field`, the service should:

- verify the field exists
- verify it is a string field
- enqueue a `search` index job for that table and field
- dedupe against existing pending/running/applied jobs for the same table and columns

When `caption_field` changes from one field to another, the service should:

- ensure the new search index job exists
- leave old index cleanup as a later cleanup/reconcile concern unless we explicitly add retirement logic now

When `caption_field` is cleared:

- do not enqueue a new search job
- optionally report stale search indexes in reconcile later

### 2. Make auto-index intent explicit in code

Right now the auto behavior is spread across:

- `field_service.go`
- `navigation_option_service.go`
- `reconcile/service.go`

We should make this easier to reason about by introducing a small helper or service-level pattern for:

- deciding whether a managed index should exist
- building the `IndexJob`
- deduping against existing jobs
- enqueueing through the same path

This does not need to become a large framework. A focused helper for managed-index requests is enough.

### 3. Decide whether search indexes need cleanup logic now

There are two reasonable options:

- simple V1:
  - only auto-create search index jobs
  - leave cleanup to future reconcile/delete work
- fuller V1:
  - extend reconcile to detect stale search indexes when `caption_field` changes or is cleared
  - schedule cleanup or retirement jobs later

Recommended:

- implement simple V1 first
- keep cleanup as a follow-up unless product behavior requires automatic retirement immediately

## Proposed implementation shape

### Table update path

Extend `TableService.Update(...)` so that when `caption_field` changes:

- it validates the new field as it already does
- after the table update is persisted, it requests a `search` index job when the caption field is non-empty
- it records the job id in schema-change details when a new job is created

Suggested `requested_by_operation` values:

- `update_table_caption_field`
- `create_table_caption_field` only if we later allow setting this during create

### Managed-index helper

Add a small internal helper for:

- building `navigation`, `search`, and `repair` jobs consistently
- checking existing jobs by dedupe key
- avoiding duplicate job creation
- reusing the River enqueue path through `IndexJobService`

This can either be:

- a new small helper in `internal/service`, or
- a new method on `IndexJobService`

Recommended:

- extend `IndexJobService` with a managed helper instead of duplicating create logic elsewhere

### Reconcile follow-up

Not required for the first slice, but likely next:

- teach reconcile how to compute expected `search` indexes from tables whose `caption_field` is non-empty
- report missing search indexes
- optionally schedule `repair` jobs for missing search indexes too

That would make `caption_field` search indexing self-healing the same way navigation indexing is.

## Suggested rollout order

1. Add auto search-index enqueue on table caption-field updates
2. Refactor index-job creation so navigation/search/repair share the same dedupe path
3. Add tests for caption-field-triggered search job creation
4. Update OpenAPI/docs to say `caption_field` may trigger background search indexing
5. Decide whether reconcile should include search expectations in this same slice or in a follow-up

## Recommended first slice

Ship this first:

- auto-enqueue `search` index jobs on `caption_field` set/change
- reuse `IndexJobService` for deduped job creation
- add tests
- update docs

Do not block that on:

- search-index cleanup of stale old indexes
- broader background cleanup automation

That gives us the missing Marble-like automatic behavior without mixing in extra lifecycle complexity.
