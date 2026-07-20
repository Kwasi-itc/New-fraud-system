# Auto Indexing Checklist

## Goal

Close the main auto-indexing gap in `data-model-service` by adding automatic `search` index-job creation from `caption_field`, while keeping the existing automatic behaviors for unique, navigation, and repair indexes.

## Baseline confirmation

- [x] Confirm table creation already creates the `object_id` unique index synchronously
- [x] Confirm field create/update already handles unique indexes synchronously
- [x] Confirm navigation option creation already auto-creates a `navigation` index job
- [x] Confirm reconcile already auto-creates `repair` jobs for missing managed indexes
- [x] Confirm the missing Marble-like behavior is auto search indexing tied to `caption_field`

## Scope for this slice

- [x] Auto-enqueue `search` index jobs when `caption_field` is set to a valid string field
- [x] Auto-enqueue `search` index jobs when `caption_field` changes to a different valid string field
- [x] Keep no-op behavior when the same effective search index is already pending, running, or applied
- [x] Keep clearing `caption_field` as metadata-only for now unless explicit cleanup logic is added

## Service design

- [x] Decide whether the new auto-index behavior lives directly in `TableService.Update(...)`
- [ ] Prefer reusing `IndexJobService` instead of hand-creating jobs in multiple places
- [x] Add a helper or service path for deduped managed-index requests
- [x] Define the `requested_by_operation` value for caption-field search indexing

## Implementation

- [x] Extend the table update flow to request a `search` index job when `caption_field` becomes non-empty
- [x] Ensure the job uses:
  - `IndexJobTypeSearch`
  - table id and table name
  - columns containing the selected caption field
- [x] Ensure duplicate jobs are not created for the same table/index shape
- [x] Preserve the current validation that caption fields must exist and be string fields
- [x] Record useful schema-change details when a search job is auto-requested
- [x] Ensure navigation-option auto indexing goes through the same managed enqueue path
- [x] Ensure reconcile-created repair jobs are enqueued through River when created or retried

## Reconcile follow-up decision

- [x] Decide whether this slice should also make reconcile expect `search` indexes for non-empty `caption_field`
- [x] If yes, add missing-search-index reporting
- [x] If yes, add repair-job scheduling for missing search indexes
- [ ] If no, document that reconcile support for search indexes is a follow-up

## Tests

- [x] Add service test: setting `caption_field` creates a `search` index job
- [x] Add service test: changing `caption_field` creates the new `search` index job
- [x] Add service test: reapplying the same `caption_field` does not create duplicate jobs
- [x] Add integration test: table update persists and job is visible in `core.index_jobs`
- [x] If reconcile is included, add reconcile test coverage for expected search indexes
- [x] Verify service, reconcile, router, and command test suites pass

## Docs and API

- [x] Update `README.md` to explain automatic search indexing from `caption_field`
- [x] Update `SETUP_AND_RUN_GUIDE.md` with a simple example and expected job behavior
- [x] Update OpenAPI descriptions where `caption_field` behavior should be visible
- [x] Mention whether old search-index cleanup is out of scope for this slice

## Done when

- [x] Setting or changing `caption_field` automatically produces the correct deduped `search` index job
- [x] Existing automatic behaviors for unique/navigation/repair indexes still work unchanged
- [x] Tests pass
- [x] Docs explain the new automatic behavior clearly

## Follow-up

- [x] Teach reconcile to expect `search` indexes for non-empty `caption_field`
- [x] Add repair-job scheduling for missing caption-field search indexes
- [x] Add DB-backed integration coverage for the table-update search-index path
- [x] Decide whether old search indexes should be retired automatically when `caption_field` changes or is cleared

Decision:

- old search indexes are not retired automatically in this slice
- changing or clearing `caption_field` remains metadata-first plus forward indexing only
- cleanup/retirement of stale search indexes is a separate follow-up if product behavior requires it

Verification note:

- the DB-backed integration test was added, but this session could not execute it because `localhost:5434` was refusing connections on July 20, 2026
