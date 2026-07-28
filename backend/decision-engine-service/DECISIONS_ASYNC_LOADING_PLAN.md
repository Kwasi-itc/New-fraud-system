# Decisions Async Loading Plan

Date: July 28, 2026

## Goal

Improve decision list responsiveness for search and filters by splitting record fetching from total-count fetching, then prefetching additional record pages in the frontend.

## Proposed Architecture

- `GET /v1/tenants/:tenantId/decisions`
  - returns decision records only
  - optimized for fast filtered/search reads
  - cursor-based for incremental loading
- `GET /v1/tenants/:tenantId/decisions/count`
  - returns only the total count for the same filter/search shape
  - runs independently from the record fetch
- frontend
  - loads records immediately
  - loads total count asynchronously
  - prefetches the next cursor page before the user reaches the bottom

## Implementation Plan

1. Define the API contract for the new count endpoint.
   Reuse the same filter and search parameters as the decisions list endpoint so both queries stay in sync.

2. Add a dedicated count handler in the decision-engine service.
   The handler should only run count logic and should not fetch records.

3. Add a dedicated count service or repository path if needed.
   Reuse the existing filter-building logic so list and count stay behaviorally aligned.

4. Keep the main decisions list endpoint record-focused.
   Treat count as a separate concern and avoid `include_total_count` on the primary row-loading flow.

5. Extend the frontend API client.
   Add a `countDecisions()` client method with the same filter shape used by `listDecisions()`.

6. Split the frontend data fetching into two queries.
   One query fetches rows. Another query fetches the count asynchronously.

7. Add debounced search behavior.
   Prevent firing a new rows and count pair on every keystroke.

8. Reset pagination state on every filter or search change.
   Old cursors and prefetched pages should be invalidated when the query changes.

9. Add next-page prefetching.
   Prefetch the next cursor page after the first page resolves or before the user needs it.

10. Decide on the list UX.
    Keep explicit pagination for now and prefetch the next page in the background for lower rollout risk.

11. Add loading states that reflect the split model.
    Rows should render as soon as available; count can appear later without blocking.

12. Add tests and documentation.
    Cover backend count behavior, frontend query behavior, and the final endpoint contract.

## Checklist

- [x] Define the `/decisions/count` response shape
- [x] Confirm the count endpoint accepts the same filters as `/decisions`
- [x] Add a count route to the decision-engine HTTP router
- [x] Implement a count handler in `internal/httpapi/handlers/decisions.go`
- [x] Reuse existing decision filter parsing for the count route
- [x] Add service support for filtered decision counts if a dedicated method is needed
- [x] Reuse repository filter SQL so list and count stay aligned
- [x] Update OpenAPI for the new count endpoint
- [x] Document the new endpoint in `documentation/decisions.md`
- [x] Add backend tests for the count endpoint response and filter behavior
- [x] Add `countDecisions()` to `new/frontend/src/lib/decision-engine-api.ts`
- [x] Split the frontend decisions view into `rows` and `count` queries
- [x] Remove blocking dependence on `include_total_count` for initial row rendering
- [x] Add debounced search for the decisions text box
- [x] Reset cursor or pagination state when filters/search change
- [x] Prefetch the next decisions page after the current page resolves
- [x] Add near-bottom scroll prefetch or intersection-based prefetch
- [x] Decide between infinite scroll and explicit pagination
- [x] Show rows immediately even when count is still loading
- [x] Render count asynchronously when it becomes available
- [x] Add frontend tests or verification notes for search/filter responsiveness
- [x] Measure behavior with broad searches and multiple filters
- [x] Verify count and records stay consistent under the same query params

## Recommended Sequence

1. Backend count endpoint
2. OpenAPI and docs
3. Frontend API client
4. Frontend split-query loading
5. Prefetch and UX refinement
6. Tests and measurement

## Likely File Targets

- `new/backend/decision-engine-service/internal/httpapi/router.go`
- `new/backend/decision-engine-service/internal/httpapi/handlers/decisions.go`
- `new/backend/decision-engine-service/internal/service/decision_service.go`
- `new/backend/decision-engine-service/internal/store/postgres/decision_repository.go`
- `new/backend/decision-engine-service/internal/httpapi/openapi.yaml`
- `new/backend/decision-engine-service/documentation/decisions.md`
- `new/frontend/src/lib/decision-engine-api.ts`
- `new/frontend/src/app/(authenticated)/detection/page.tsx`

## Notes

- This makes search and filters feel faster because record rendering is no longer blocked by count work.
- The count endpoint is treated as secondary UI data, not critical-path data.
- Cursor-based loading is still the better long-term fit if the UI later moves from explicit pagination to seamless scrolling.
- The current UI keeps explicit pagination for lower rollout risk while eagerly prefetching the next page in the background.
- Verification for search and filter responsiveness in this pass was done through query-shape review plus backend and TypeScript checks; no browser-session manual QA was run.
