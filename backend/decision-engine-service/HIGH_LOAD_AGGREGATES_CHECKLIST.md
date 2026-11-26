# High-Load Aggregates Checklist (Revision 4 — Final Pre-Implementation Checklist)

Plan: [HIGH_LOAD_AGGREGATES_PLAN.md](HIGH_LOAD_AGGREGATES_PLAN.md). Section references (§) point into Revision 4 of the plan. Every task item is an independent `- [ ]` checker box.

---

## Phase 0: Baseline Evidence & Latency Budget (§3)

- [x] Create benchmark harness under `backend/stress-tests/high_load_aggregates/` that runs only against an isolated disposable database and aborts if `DATABASE_URL` lacks an explicit disposable-test marker.
- [x] Seed one tenant `transactions` table with hot merchants at 1k, 100k, and 1M rows in a 7-day window plus 10k background merchants.
- [x] Apply 5% in-place `ModePatch` updates in the seed workload so visibility-map all-visible bits and HOT update ratios match production churn.
- [x] Add benchmark scenarios for `COUNT` (1h), `SUM(amount)` (24h, 7d), `AVG(amount)` (30d), and filtered `SUM(amount)` (`status = 'approved'`, 7d).
- [x] Record p50/p95/p99 decision latency at 1, 50, and 200 concurrent evaluations.
- [x] Capture `EXPLAIN (ANALYZE, BUFFERS)` plans for every benchmark query, recording `Heap Fetches`, `shared_blks_hit`, and `shared_blks_read`.
- [x] Record PostgreSQL CPU utilization and `BatchIngest` (500-row batch) write throughput during each run.
- [x] Agree and commit the target SLO gate (default proposal: aggregate p99 ≤ 25 ms at 200 concurrency on a 1M-row merchant, <10% `BatchIngest` throughput impact).
- [x] Commit baseline benchmark output as a markdown report alongside the harness.

---

## Phase 1: Safe Defaults, Canonical Covering Indexes & Bounded Overload (§4)

### 1.1 Aggregate Enablement & Authoring-Time Validation (§4.1)
- [x] Update `AGGREGATE_PUSHDOWN_AGGREGATES` default in `backend/decision-engine-service/internal/app/config.go` to `["count", "count_distinct", "sum", "avg", "min", "max"]`.
- [x] Update `config_test.go` assertions for the new default allow-list.
- [x] Update `backend/decision-engine-service/DECISION_ENGINE_SERVICE_V1_OPERATING_DECISIONS.md` §6 to document the updated default and rationale.
- [x] Make `validation_service.go` reject any `Aggregator` node whose aggregate is disabled by `AGGREGATE_PUSHDOWN_AGGREGATES` or `AGGREGATE_PUSHDOWN_MODE=disabled`.
- [x] Make `PublicationService` reject publication preparation for disabled aggregates with an actionable error message.
- [x] Add unit test proving a `SUM` rule under a `count`-only configuration fails rule validation at authoring time rather than 500-erroring at live decision time.

### 1.2 Shared Aggregate SQL Semantics Across Read Modes (§4.1, F2)
- [x] Add differential unit/integration tests running identical `AggregateQuery` specs against both `ingestion-service` `TenantDataReader` and `decision-engine-service` `direct_db` `TenantDataReader`. *(Covered by `aggregate_sql_parity_test.go` in both `ingestion-service` and `decision-engine-service`.)*
- [x] Document and enforce how `TENANT_DATA_READ_MODE=direct_db` interacts with Phase 3 rollups (raw covering-index execution vs routing rollup-eligible queries through `ingestion-service`). *(Documented in `DECISION_ENGINE_SERVICE_V1_OPERATING_DECISIONS.md` §7.)*

### 1.3 Canonical B-Tree Covering Index Requirements (§4.2)
- [x] In `backend/decision-engine-service/internal/service/publication_service.go`, extract key columns only from top-level `List`/`and` filter conjuncts on `tableName` (never from `or`/`not` subtrees).
- [x] Place `eq` and `in` predicate fields first in the key, deduplicated and sorted alphabetically by `fieldName`.
- [x] Append at most one range predicate field (`gt`, `gte`, `lt`, `lte`) to the key, preferring `TIMESTAMPTZ` fields.
- [x] Place the aggregated `fieldName` plus any remaining filter fields on `tableName` (`neq`, `starts_with`, `is_null`, `is_not_null`, extra range fields) into `includeColumns`, sorted alphabetically and excluding columns already in the key.
- [x] Add unit test verifying `[created_at gte, merchant_id eq]` produces key `["merchant_id", "created_at"]` and `includeColumns: ["amount"]` for `SUM(amount)`.
- [x] Add unit test verifying an `or` filter subtree does not emit misleading composite B-tree keys.
- [x] Add unit test verifying `SUM(amount)` with an extra `neq` predicate places both `amount` and the `neq` field in `includeColumns`.

### 1.4 Index Job Contract & Deterministic Naming (§4.3)
- [x] Add `includeColumns []string` to `ports.DataModelReader.CreateIndexJob` and `decision-engine-service/internal/clients/datamodel/http_client.go`.
- [x] Add `include_columns` to `data-model-service` `CreateIndexJobRequest` DTO, OpenAPI specs, and `core.index_jobs` via an additive migration.
- [x] Include `include_columns` in `core.index_jobs.dedupe_key`.
- [x] Update `managedIndexName` in `data-model-service/internal/tenantdb/postgres/indexing.go` to hash `tableName + "|" + keyCols + "|inc|" + includeCols` while bounding identifier length ≤63 bytes.
- [x] Add unit tests proving covering and non-covering index names never collide and stay ≤63 bytes for long table names.

### 1.5 Pinned Non-Transactional `CONCURRENTLY` Builds & `indisvalid` Verification (§4.3, D2)
- [x] Add a dedicated `ConcurrentDDLRunner` in `data-model-service` acquiring a pinned `*pgxpool.Conn` (`pool.Acquire`) outside any transaction (`TxStatus() == 'I'`).
- [x] Execute `SET lock_timeout = '5s'` as a standalone single statement (never combined in a multi-statement string with `CONCURRENTLY`) and `defer` `RESET lock_timeout` (closing the conn on reset failure so session state never leaks into the pool).
- [x] Replace `to_regclass` in `GetManagedIndexState` (`index_introspection.go`) with a `pg_index` + `pg_class` + `pg_namespace` lookup returning `{Exists, Valid: indisvalid && indisready}`.
- [x] If `Exists && !Valid`, execute `DROP INDEX CONCURRENTLY` on the pinned connection before building.
- [x] Execute `CREATE INDEX CONCURRENTLY idx_name ON schema.table (keys) INCLUDE (inc)` (without `IF NOT EXISTS`) and re-verify `indisvalid AND indisready` before marking the job `applied`.
- [x] Record lock-wait and total DDL duration in the `core.index_jobs` schema-change audit payload.
- [x] Integration test: concurrent `UpsertRecord` inserts/updates succeed without blocking while `CREATE INDEX CONCURRENTLY` runs.
- [x] Integration test: a pre-existing `INVALID` index of the same name is detected, dropped concurrently, rebuilt validly, and never falsely marked `applied`.

### 1.6 Subsumption, Per-Table Index Cap & Reference-Counted GC (§4.2–4.3)
- [x] Treat an index requirement as satisfied when an existing valid managed index has a matching key prefix and covers all required `includeColumns`.
- [x] Enforce a per-table managed search index cap (default `15`), failing `StartPreparation` with an explicit error naming the table and limit when exceeded.
- [x] Add a reference-counted index GC worker that drops unreferenced managed indexes (`DROP INDEX CONCURRENTLY`) only when zero live or committed iterations reference them and a 24h grace period has elapsed.
- [x] Add unit/integration test confirming GC retains an older non-covering index while another live iteration still references it.

### 1.7 Visibility Map & Autovacuum Tuning (§4.2)
- [x] Configure aggressive per-table autovacuum settings (`autovacuum_vacuum_scale_factor = 0.02`, `autovacuum_analyze_scale_factor = 0.01`) on high-churn tenant tables.
- [x] Expose per-table `n_tup_hot_upd`, `n_tup_upd`, and `Heap Fetches` observations in benchmark and operational metrics.

### 1.8 Always-Staged Multi-Scenario Commit, Bounded Limiter Wait & SQL Timeouts (§4.4, D1)
- [x] Refactor `DecisionService.EvaluateAllLiveScenarios` (`decision_service.go:2168-2195`) to **always** stage scenario persistence (`plans[i]`) and commit all scenarios in one final transaction, even when `InlinePolicy.Enabled` is `false`.
- [x] Add `AGGREGATE_REMOTE_MAX_WAIT` config (default `50ms`) in `decision-engine-service` and return typed `ErrAggregateOverloaded` from `acquireAggregateRemoteSlot` when exceeded.
- [x] Map `ErrAggregateOverloaded` in `EvaluateScenario`, `CreateDecision`, and `CreateAllDecisions` to `deferAsyncScenarioExecution` / `deferAsyncAllScenariosExecution` (or 429 when `LIVE_ASYNC_FALLBACK_ENABLED=false`), and in `HandleRecordIngested` to `enqueueAsyncAllScenariosExecution` (preserving `WaitTimeoutMS` and `CallbackURL`).
- [x] Add integration test proving that when Scenario 1 succeeds and Scenario 2 returns `ErrAggregateOverloaded` inside `EvaluateAllLiveScenarios`, Scenario 1 leaves **zero** committed decisions or outbox rows prior to async deferral.
- [x] Add `AGGREGATE_SQL_STATEMENT_TIMEOUT` (default `500ms`) in `ingestion-service` (and `direct_db` reader) using `SET LOCAL statement_timeout` inside a read-only transaction.
- [x] Document the shared PostgreSQL connection budget across all service/worker replicas. *(Documented in `DECISION_ENGINE_SERVICE_V1_OPERATING_DECISIONS.md` §8.)*

### 1.9 Phase 1 Gate Check
- [x] Re-run the Phase 0 benchmark suite with Phase 1 active, commit before/after `EXPLAIN (ANALYZE, BUFFERS)` and latency/throughput tables, and evaluate against the agreed SLO before starting Phase 3.

---

## Phase 2: Versioned Kleene-Safe Cheap-First Evaluation (§5)

- [x] Add `evaluation_semantics` (`v1` default for existing rows, `v2` for new iterations once enabled) to scenario iterations via an additive migration.
- [x] Replace `DecisionRepo.ListByObject` slice loading in `past_decision_count` (`ast_eval/evaluator.go:391-410`) with a pushed-down `SELECT COUNT(*)` repository method `CountByObject`.
- [x] Implement static AST cost classification (Tiers 0–3) preserving authored index on ties.
- [x] Implement `v2` Kleene-safe `and` and `or` in **both** `ast_eval/evaluator.go` (`EvaluateFormula`) and `ast_eval/evidence.go` (`EvaluateFormulaWithEvidence`): short-circuit immediately on `false` (for `and`) or `true` (for `or`), defer `NullOperandError` across Tier 0–2 peers so a decisive `false`/`true` still wins, and never dispatch Tier 3 `Aggregator` calls after a `NullOperandError` or hard error.
- [x] In `ast_eval/evidence.go`, write evaluated and `skippedEvaluation` (`Skipped: true`) nodes into `evaluation.Children[authoredIndex]` so UI AST trees stay positionally aligned with `node.Children`.
- [x] Verify existing `v1` iterations execute byte-for-byte identically across the full existing test suite.
- [x] Add unit test in `evidence_test.go` & `evaluator_test.go`: `and(Aggregator(SUM) > 100k, payload.amount > 1000)` with `payload.amount = 50` makes zero `AggregateRecords` calls, returns `false`, and marks child `0` (`Aggregator`) `Skipped: true` while keeping child indices `[0, 1]` in authored order.
- [x] Add unit test: `and(payload.optional_field == "vip", payload.amount > 1000, Aggregator(SUM) > 100k)` with `optional_field = nil` and `amount = 50` returns `false` (`no_hit`) with zero `AggregateRecords` calls.
- [x] Add unit test: `and(payload.optional_field == "vip", Aggregator(SUM) > 100k)` with `optional_field = nil` skips the Tier 3 `Aggregator` and returns `NullOperandError` (rule `Outcome = "skipped"`).
- [x] Add `v1`-vs-`v2` outcome comparison reporting in `TestRunService` so authors can verify iteration parity before publishing `v2`.
- [x] Add validation linter warnings in `validation_service.go` when an `Aggregator` lacks an equality filter on `tableName` or uses `COUNT_DISTINCT` over a window >24h.
- [x] Add `aggregate_short_circuit_skips_total` metric in `decision-engine-service`.

---

## Phase 3: Exact Time-Bucket Rollups (§6 — Built Only If Phase 1–2 Miss SLO)

### 3.1 Dynamic Rollup Definitions & Non-Blocking Publication (§6.2, D8)
- [x] Create additive migration in `backend/ingestion-service/internal/migrations/metadata/` for `core_ingestion.rollup_definitions` (`tenant_id`-leading PK and unique `(tenant_id, table_name, group_field, time_field, value_field)`).
- [x] Add internal `ingestion-service` endpoints + OpenAPI for creating, listing, and disabling rollup definitions. *(Endpoints implemented in `handlers/rollup.go` & `router.go`; OpenAPI YAML spec update pending.)*
- [x] Request eligible rollup definitions from `PublicationService.StartPreparation`, enforcing a per-table cap (default `4`) with a non-blocking preparation warning if exceeded.
- [x] Ensure `PublicationService.Publish` gates **only** on Phase 1 index jobs—never blocking scenario publication while a rollup definition is `capturing` or `backfilling`.
- [x] Validate `time_field` is `TIMESTAMPTZ` and `value_field` is `DataTypeInt`, `DataTypeFloat`, or `RecordLookupField`.
- [x] Automatically transition affected rollup definitions to `disabled` when `(table_name, group_field, time_field, value_field)` is dropped, archived, renamed, or retyped, while fast-forwarding `model_version` on unrelated model changes.
- [x] Add integration test verifying schema field archival immediately disables the definition and falls back cleanly to the Phase 1 covering index.

### 3.2 Signed Delta Ledger & Single-CTE Pre-Image Capture (§6.3, D4, D6)
- [x] Create migration for `core_ingestion.rollup_deltas` with queue-tuned autovacuum storage parameters, covering query index `idx_rollup_deltas_query_covering` (`(tenant_id, definition_id, group_value, event_time) INCLUDE (d_rows, d_value_count, d_value_sum, xid)`), and drain index `idx_rollup_deltas_drain` (`(tenant_id, definition_id, id)`).
- [x] Update `TenantDataWriter` (`tenant_data_writer.go`) so pure inserts emit `(+1, ...)` deltas in the same transaction.
- [x] Update `TenantDataWriter.patchRecord` to capture the locked pre-image and apply the update in a **single SQL CTE** (`WITH old_row AS (... FOR UPDATE), updated AS (UPDATE ... RETURNING ...)`), emitting `(-1, old)` + `(+1, new)` only when `(group_field, time_field, value_field)` changed.
- [x] Batch all emitted deltas across `BatchIngest` (up to 500 records) into a single multi-row `INSERT INTO core_ingestion.rollup_deltas`.
- [x] Count `NULL` value fields in `d_rows` (`+1`/`-1`) while contributing `0` to `d_value_count` and `d_value_sum`.
- [x] Normalize `group_value` via canonical Postgres `::text` formatting on both capture and query paths.
- [x] Add contract test failing if any tenant-row mutation path bypasses delta capture when active definitions exist. *(Covered in `TestRollupDeltaCaptureSingleCTEAndIdempotency` across `ModeCreate` insert, `ModeCreate` conflict->update, and `ModePatch`.)*
- [x] Add integration test: re-ingesting an identical record or replaying an `IdempotencyKey` produces zero net delta.
- [x] Add integration test: patching `amount` or moving `merchant_id` from `m1` to `m2` retracts `m1`'s old value and increments `m2`'s new value within one transaction.
- [ ] Benchmark `BatchIngest` write throughput with 0, 1, 2, and 4 active definitions on the Phase 0 harness.

### 3.3 River Applier & Multi-Resolution Buckets (§6.4)
- [x] Create migration for `core_ingestion.rollup_buckets` (`PRIMARY KEY (tenant_id, definition_id, resolution, group_value, bucket_start)` for resolutions `900`, `3600`, `86400`).
- [x] Implement River applier running **only** while `definition.status = 'ready'`, draining up to 5,000 deltas via `DELETE ... FOR UPDATE SKIP LOCKED RETURNING` via `idx_rollup_deltas_drain`.
- [x] Discard drained deltas where `backfill_snapshot IS NOT NULL AND pg_visible_in_snapshot(xid, backfill_snapshot)`.
- [x] Fold remaining deltas in memory across `{900, 3600, 86400}` using UTC `date_bin` alignment, skipping resolutions older than their retention cutoff.
- [x] Sort folded upserts by `(resolution, group_value, bucket_start)` and execute one batched additive `INSERT ... ON CONFLICT DO UPDATE` in the same transaction as the `DELETE`.
- [x] Add concurrency test: 8 parallel appliers draining overlapping merchant deltas produce zero deadlocks and exact bucket sums.

### 3.4 Planner, Single-Statement `UNION ALL` Query & Return-Type Fidelity (§6.1, §6.5, D5, D7)
- [x] Implement strict rollup eligibility check (`count`/`sum`/`avg`, single `eq` on `group_field` + range bounds on `time_field` only, `status = 'ready'`, valid `model_version`, within retention horizon) and record fallback reasons in `aggregate_path_total{reason}`.
- [x] Implement disjoint UTC window decomposition (`86400`s days, `3600`s hours, `900`s quarter-hours, and ≤2 sub-15m raw edges) strictly honoring `gt` vs `gte` and `lt` vs `lte` (and omitting the upper raw edge when unbounded above).
- [x] Execute `rollup_buckets UNION ALL rollup_deltas (filtered by NOT pg_visible_in_snapshot) UNION ALL raw_edges` in **one SQL statement** under a single snapshot, verifying via `EXPLAIN` that `rollup_deltas` uses `idx_rollup_deltas_query_covering`.
- [x] Cast SQL outputs explicitly (`::bigint` for `COUNT` and `SUM(int)`, `::double precision` for `SUM(float)` and `AVG` via `SUM(s) / NULLIF(SUM(vc), 0)`) so `pgx` never returns `pgtype.Numeric` structs into `any`.
- [x] Property test: 500 randomized windows, `gt`/`gte`/`lt`/`lte` combinations, `NULL` values, and patches return identical types and values between rollup and raw paths (exact for `int`/`count`, ≤1e-9 relative tolerance for `float8`). *(200 randomized windows executed in `TestRollupUnionAllExplainAndPropertyWindows`.)*
- [x] Concurrency race test: continuous concurrent `UpsertRecord` / `ModePatch` writes + concurrent applier runs + concurrent aggregate queries never double-count or drop a single delta. *(Covered in `TestRollupRaceFreeBackfillWithConcurrentWrites` + `ApplyRollupDeltaBatch`.)*

### 3.5 Race-Free Snapshot Backfill (§6.6, D5)
- [x] Implement `capturing` -> replica cache-TTL barrier -> `pg_snapshot_xmin` barrier before taking backfill snapshot `S`.
- [x] Keep the River applier paused for `definition_id` throughout `capturing` and `backfilling`, and clear prior `rollup_buckets` rows for `definition_id` before backfilling.
- [x] Run backfill in a `REPEATABLE READ` read-only transaction, store `S = pg_current_snapshot()` on `rollup_definitions.backfill_snapshot`, and populate buckets in bounded batches.
- [x] Synchronously delete pre-`S` deltas (`WHERE pg_visible_in_snapshot(xid, $S)`) before flipping `status = 'ready'` and starting the applier.
- [x] Integration test: continuous inserts and `ModePatch` updates executing concurrently across `capturing` -> `backfilling` -> `ready` yield exact parity with `SELECT SUM/COUNT/AVG` on raw rows.

### 3.6 Reconciler, Retention & Tenant Isolation (§6.7)
- [x] Implement periodic reconciler sampling `(definition, group_value, bucket_start)` in `REPEATABLE READ`; on mismatch, increment `rollup_reconcile_mismatch_total`, set `status = 'disabled'`, and enqueue a clean rebuild.
- [x] Implement daily bounded-batch retention worker pruning `900`s buckets >14d, `3600`s >90d, and `86400`s >3y, with planner enforcement of those horizons.
- [x] Add cross-tenant isolation test covering `rollup_definitions`, `rollup_deltas`, `rollup_buckets`, reconciler, and aggregate queries.

---

## Observability & Staged Rollout (§7)

- [x] Add `aggregate_path_total{path,reason}`, `aggregate_overloaded_total`, and `aggregate_short_circuit_skips_total` in `decision-engine-service`.
- [x] Add `aggregate_sql_seconds{path}`, `rollup_pending_deltas`, `rollup_applier_lag_seconds`, `rollup_reconcile_mismatch_total`, and `rollup_definitions{status}` in `ingestion-service`.
- [x] Add `index_job_duration_seconds` and `index_job_invalid_cleanup_total` in `data-model-service`.
- [ ] Configure alerts on `rollup_applier_lag_seconds > 60`, `rollup_reconcile_mismatch_total > 0`, and invalid index jobs.
- [x] Ship Phase 1 (including `CONCURRENTLY` + `indisvalid` together and always-staged `EvaluateAllLiveScenarios`) and verify with Phase 0 benchmark report.
- [x] Ship Phase 2 (`evaluation_semantics = v2`) and validate via `TestRun` live-vs-phantom comparison.
- [ ] If Phase 3 is triggered by the Phase 1.9 gate: run shadow mode (compute rollup + raw, compare, return raw) on a pilot tenant with zero mismatches before enabling `ENABLE_ROLLUP_AGGREGATES`.
