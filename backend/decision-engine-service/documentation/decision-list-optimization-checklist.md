# Decision List Optimization Checklist

This checklist covers the optimizations needed to make `GET /v1/tenants/:tenantId/decisions` perform well on large tenants, including tenants with 700,000+ decision rows.

## Goals

- Reduce p95 and p99 latency for tenant-wide decision listing.
- Keep response time stable as page depth increases.
- Avoid full-table scans for common list and filter paths.
- Reduce database CPU, I/O, and row materialization cost.

## Current Bottlenecks

- [x] Remove `request_body` from list queries.
  - Current list queries select `request_body` even though the list DTO does not return it.
  - `request_body` is `JSONB`, so it makes row fetches wider and more expensive than necessary.
  - Keep `request_body` only for detail endpoints like `GET /decisions/:decisionId`.

- [x] Stop using a scenario join when it is not needed.
  - The filtered list query always `LEFT JOIN`s `core.scenarios`.
  - Most list requests do not need scenario-name search, so the join should be conditional.

- [x] Address tenant-wide sort inefficiency.
  - The existing index is optimized for `(tenant_id, scenario_id, created_at desc)`.
  - Tenant-wide feeds ordered only by `created_at desc` need a dedicated index.

- [x] Replace deep `OFFSET` pagination.
  - `LIMIT/OFFSET` gets slower as `offset` grows because PostgreSQL must still scan and discard earlier rows.

- [x] Avoid exact `COUNT(*)` on every paginated request.
  - The current paginated flow always computes total count.
  - On large tenants, count can be a significant share of total latency.

- [ ] Rework fuzzy filtering.
  - `ILIKE '%...%'` on `object_id`, `object_type`, and generic search is expensive.
  - These filters should be narrowed or backed by dedicated search indexes.

## Query Shape Changes

- [x] Create a dedicated narrow projection for decision list rows.
  - Select only:
    - `id`
    - `tenant_id`
    - `scenario_id`
    - `scenario_iteration_id`
    - `object_id`
    - `object_type`
    - `outcome`
    - `score`
    - `triggered`
    - `created_at`

- [x] Split repository methods between list and detail reads.
  - List methods should never fetch `request_body`.
  - Detail methods can continue to fetch full decision payloads.

- [x] Make the `core.scenarios` join conditional.
  - Join only when search explicitly includes scenario-name matching.
  - Keep the base tenant list query on `core.decisions` alone.

## Indexing Changes

- [x] Add a tenant feed index for the default sort order.
  - Proposed index:
  - `CREATE INDEX CONCURRENTLY IF NOT EXISTS decisions_tenant_created_idx ON core.decisions (tenant_id, created_at DESC, id DESC);`
  - Include `id` as a tiebreaker for stable keyset pagination.

- [ ] Review filter-specific indexes based on actual query usage.
- [ ] Review filter-specific indexes based on actual query usage.
  - Candidate patterns:
    - `(tenant_id, scenario_id, created_at DESC, id DESC)`
    - `(tenant_id, outcome, created_at DESC, id DESC)`
    - `(tenant_id, object_type, created_at DESC, id DESC)`
    - `(tenant_id, object_type, object_id, created_at DESC, id DESC)`

- [ ] Avoid adding broad indexes without confirming filter frequency.
  - Use query logs or API usage data first.
  - Keep write amplification under control.

- [ ] If fuzzy search must remain, add explicit search indexing.
  - Evaluate PostgreSQL trigram indexes for:
    - `object_id`
    - `object_type`
    - scenario name search
  - Keep fuzzy search separate from the fast-path default list query.

## Pagination Changes

- [x] Replace `offset` pagination with cursor/keyset pagination.
  - Preferred cursor columns:
    - `created_at`
    - `id`
  - Preferred ordering:
    - `ORDER BY created_at DESC, id DESC`

- [x] Add API support for `after_created_at` and `after_id`, or a single encoded cursor.
  - Cursor pagination keeps late pages fast and predictable.

- [x] Preserve `has_more` behavior without scanning old pages.
  - Fetch `limit + 1` rows to detect continuation.

- [x] Keep `offset` only as a temporary compatibility path if needed.
  - Mark it as deprecated if cursor pagination is introduced.

## Count Strategy Changes

- [x] Make `total_count` optional instead of default.
  - Add a query parameter such as `include_total_count=true` if exact count is needed.

- [x] Default the endpoint to returning:
  - `limit`
  - `has_more`
  - `next_cursor`

- [ ] Evaluate approximate count options for large result sets.
  - Use approximate counts only where UI tolerates them.
  - Do not block the main list query on exact count unless required.

## Filter Semantics Changes

- [x] Change `object_type` filtering from substring match to exact match where possible.
  - `object_type` usually behaves like a small controlled enum/category.

- [x] Change `object_id` filtering to exact or prefix match when product requirements allow it.
  - Avoid `%value%` if the actual use case is lookup or prefix search.

- [x] Split generic `search` into explicit search modes if needed.
  - Example:
    - exact id search
    - object id prefix search
    - scenario name fuzzy search

- [ ] Align search semantics with indexes.
  - Do not keep broad fuzzy semantics unless the database is explicitly indexed for them.

## Validation And Measurement

- [ ] Capture baseline timings before changes.
  - Measure:
    - first page
    - deep page
    - filtered page
    - filtered page with search
    - count-heavy requests

- [ ] Run `EXPLAIN (ANALYZE, BUFFERS)` for representative queries.
  - Capture plans before and after each major change.

- [ ] Validate improvements on realistic row volumes.
  - Test on a dataset at or above current production scale.

- [ ] Confirm the query uses the intended indexes.
  - Especially for:
    - tenant feed
    - scenario filter
    - outcome filter
    - object lookup

- [ ] Measure API-level latency and DB-level load separately.
  - Verify whether time is spent in:
    - query execution
    - row scanning
    - JSON serialization
    - count query

## Rollout Plan

- [x] Phase 1: low-risk wins
  - Remove `request_body` from list queries.
  - Make the scenario join conditional.
  - Add the tenant feed index.

- [x] Phase 2: pagination redesign
  - Introduce cursor pagination.
  - Keep `limit + 1` fetch for `has_more`.

- [ ] Phase 3: count and search refinement
  - Make exact count optional.
  - Rework fuzzy search semantics and indexing.

- [ ] Phase 4: cleanup
  - Remove deprecated offset paths if cursor pagination is fully adopted.
  - Remove any no-longer-useful indexes after observing production behavior.

## Acceptance Criteria

- [ ] First-page tenant decision list stays fast at 700,000+ rows.
- [ ] Deep-page latency does not degrade materially with page depth.
- [ ] Default list path avoids table scans.
- [ ] Exact count no longer blocks every paginated request.
- [ ] Search behavior is explicit and backed by appropriate indexes.
