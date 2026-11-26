# High-Load Aggregates Plan (Revision 4 — Final Pre-Implementation Spec)

Scope: keep `Aggregator(...)` rules (`COUNT`, `COUNT_DISTINCT`, `SUM`, `AVG`, `MIN`, `MAX` over time windows) exact and within latency budget for high-volume entities (such as merchants with 100k–1M+ transactions in a window).

Companion checklist: [HIGH_LOAD_AGGREGATES_CHECKLIST.md](HIGH_LOAD_AGGREGATES_CHECKLIST.md)

> [!IMPORTANT]
> All latency numbers in this document are **hypotheses until measured** with the Phase 0 benchmark harness, as `AGENTS.md` requires. No phase may claim a speedup without `EXPLAIN (ANALYZE, BUFFERS)` evidence and benchmark output committed alongside the change.

---

## 1. Verified Codebase Facts

| # | Fact | Codebase Evidence |
| :- | :--- | :--- |
| F1 | Aggregates are pushed down as `SELECT agg(field) FROM tenant_x.table WHERE ...` with **no** in-memory fallback (`fallback_used: false`). Broad list helpers (`related_count`, `related_records`) are already disabled. | `ast_eval/evaluator.go` (`evaluateMarbleAggregator`, lines 411, 914, 1260) |
| F2 | Two SQL implementations of `AggregateRecords` exist: `ingestion-service` (`internal/store/postgres/tenant_data_reader.go`) and `decision-engine-service` direct DB mode (`internal/store/postgres/tenant_data_reader.go`, selected via `TENANT_DATA_READ_MODE=direct_db` in `internal/tenantdata/reader_factory.go`). | both `tenant_data_reader.go` files |
| F3 | `AGGREGATE_PUSHDOWN_AGGREGATES` defaults to `["count"]`. Rules using `SUM`, `AVG`, `MIN`, `MAX`, or `COUNT_DISTINCT` error on **every evaluation** under default env vars. | `decision-engine-service/internal/app/config.go:263` |
| F4 | **Tenant rows are mutable.** `TenantDataWriter.UpsertRecord` runs inside `IngestService`'s `txManager.Run` (`READ COMMITTED`): it executes `INSERT ... ON CONFLICT (object_id) DO NOTHING RETURNING id`, falling back to `UPDATE ... WHERE object_id = $n`. `ModePatch` updates in place. Idempotent replays return early before `UpsertRecord`. | `ingestion-service/internal/store/postgres/tenant_data_writer.go:25-126`, `ingest_service.go:112-136` |
| F5 | Tenant column SQL types today are `BOOLEAN`, `INTEGER` (`int4`), `DOUBLE PRECISION` (`float8`), `TEXT`, `TIMESTAMPTZ`, and `INET`—there is no `NUMERIC` tenant column type. | `data-model-service/internal/tenantdb/postgres/indexing.go:113-130` |
| F6 | **Two AST evaluators exist**: `EvaluateFormula` (`ast_eval/evaluator.go`) evaluates scenario `TriggerFormula`, while `EvaluateFormulaWithEvidence` (`ast_eval/evidence.go`) evaluates live/testrun **rules** (`service/rule_evaluation.go:55`). Both already short-circuit `and`/`or` left-to-right, and `evidence.go` already marks skipped subtrees using `domainast.EvaluationNode{Skipped: true}`. A `NullOperandError` marks a rule `Outcome = "skipped"` (0 score), whereas other errors abort the decision. | `ast_eval/evaluator.go:53-88`, `ast_eval/evidence.go:42-91, 159-178`, `service/rule_evaluation.go:55-68` |
| F7 | `EvaluateAllLiveScenarios` only stages scenario DB writes when `s.InlinePolicy.Enabled` is `true`; when `false`, each scenario in the `errgroup` commits its own decision, workflow executions, and outbox events immediately. | `decision-engine-service/internal/service/decision_service.go:706-720, 2168-2195` |
| F8 | `aggregatorIndexRequirement` collects filter columns in AST-authored order from direct `Filter` children only, with no `INCLUDE` columns. `Publish` blocks while `GetPreparationStatus` reports `PreparationRequired == true`. | `decision-engine-service/internal/service/publication_service.go:56-62, 376-416` |
| F9 | `SchemaManager` wraps a 3-method `executor` interface, hashes only `table + columns` in `managedIndexName`, checks existence via `to_regclass($1) IS NOT NULL` (ignoring `pg_index.indisvalid`), and runs blocking `CREATE INDEX IF NOT EXISTS` without `CONCURRENTLY`. | `data-model-service/internal/tenantdb/postgres/schema_manager.go`, `indexing.go:92-140`, `index_introspection.go:17-27` |
| F10 | `AGGREGATE_REMOTE_CONCURRENCY_LIMIT` blocks callers until `ctx.Done()`. Async fallback is triggered only at handler entry via `LIVE_DECISION_CONCURRENCY_LIMIT` (`deferAsyncScenarioExecution` / `deferAsyncAllScenariosExecution` gated by `LIVE_ASYNC_FALLBACK_ENABLED`, and `enqueueAsyncAllScenariosExecution` on `HandleRecordIngested`). | `ast_eval/aggregate_remote_limiter.go`, `handlers/decisions.go:60-135, 426-520` |
| F11 | `AggregateResultCache` (`singleflight` + `sync.Map`) is scoped to one decision request (`EvaluateScenario` or one `EvaluateAllLiveScenarios` call). | `ast_eval/aggregate_result_cache.go`, `decision_service.go:387, 2157` |
| F12 | Tenant tables live in `tenant_<uuid>`; ingestion metadata lives in `core_ingestion`. | `tenant_data_writer.go:135`, `migrations/metadata/000001_init.up.sql` |

---

## 2. Final Pre-Coding Audit: 8 Defects Fixed Over Revision 3

| # | Defect in Revision 3 | Why It Would Fail in Production | Fix in Revision 4 |
| :- | :--- | :--- | :--- |
| D1 | Mapping `aggregate_overloaded` mid-evaluation to async fallback in `EvaluateAllLiveScenarios` | When `InlinePolicy.Enabled == false` (F7), fast scenarios in the `errgroup` have **already committed** decisions and outbox events before a slow scenario hits `aggregate_overloaded`. Re-queuing async execution duplicates those decisions, cases, and outbox deliveries. | Always stage scenario persistence in `EvaluateAllLiveScenarios` (commit all scenarios in one final transaction after all evaluations succeed). Only when zero writes have committed can `aggregate_overloaded` defer to async or return 429 (§3.4). |
| D2 | `CREATE INDEX CONCURRENTLY IF NOT EXISTS` with `lock_timeout` on `SchemaManager.db` | Three Postgres/`pgx` bugs: (a) multi-statement `SET lock_timeout; CREATE INDEX CONCURRENTLY` fails with `25001` (implicit tx block); (b) separate `pool.Exec` calls hit different connections and poison session `lock_timeout`; (c) `IF NOT EXISTS` silently no-ops when a prior failed build left an `INVALID` index of the same name. | Acquire a dedicated `*pgxpool.Conn`, `defer` `RESET lock_timeout` before release, inspect `pg_index` (`indisvalid`, `indisready`) first, `DROP INDEX CONCURRENTLY` any `!indisvalid` leftover, run `CREATE INDEX CONCURRENTLY` **without** `IF NOT EXISTS`, and re-verify `indisvalid AND indisready` (§3.3). |
| D3 | Implementing `v2` ordering only in `evaluator.go` and inventing custom skip JSON | Live rules execute through `EvaluateFormulaWithEvidence` in `evidence.go`, not `evaluator.go` (F6). Reordering `evaluation.Children` would scramble UI AST child positions, and `EvaluationNode.Skipped` already exists. Also `NullOperandError` yields rule outcome `"skipped"` (0 score), not a decision error. | Implement shared `v2` Kleene-safe ordering in both `evaluator.go` and `evidence.go`, write `evaluation.Children` into their original authored indices, reuse `Skipped: true`, and preserve Kleene `false`-dominates-`null` behavior (§4.2). |
| D4 | `rollup_deltas` had only `PRIMARY KEY (tenant_id, id)` | The §5.5 single-statement query filters `rollup_deltas` by `(tenant_id, definition_id, group_value, event_time)`. Without a composite index, every decision sequentially scans the entire tenant's pending delta heap (plus dead tuples from continuous `DELETE` churn). | Add covering index `(tenant_id, definition_id, group_value, event_time) INCLUDE (d_rows, d_value_count, d_value_sum, xid)` and aggressive queue-table autovacuum settings on `rollup_deltas` (§5.3). |
| D5 | Backfill + pending deltas double-count race | In Rev 3 §5.6, if `status` flips to `ready` before pre-snapshot-`S` deltas are drained (or if the applier runs concurrently with non-additive backfill writes), §5.5 reads **both** backfilled buckets and pre-`S` deltas. | Keep the applier paused during `capturing`/`backfilling`, clear target buckets before backfill, drain `pg_visible_in_snapshot(xid, S)` deltas before promoting to `ready`, **and** filter `NOT pg_visible_in_snapshot(xid, backfill_snapshot)` in both applier and §5.5 query (§5.5–5.6). |
| D6 | Separate `SELECT ... FOR UPDATE` before `UPDATE` in `BatchIngest` | Up to 500 extra round-trips per 500-record batch, extending row lock hold time. | Capture pre-image + update in a **single CTE** (`WITH old_row AS (... FOR UPDATE), updated AS (UPDATE ... RETURNING ...)`) and emit batch deltas in one multi-row `INSERT` (§5.3). |
| D7 | Uncast `NUMERIC` scanned into `any` by `pgx` | Raw `SUM(int4)` scans as `int64` and `SUM(float8)` / `AVG` as `float64`, whereas `NUMERIC` scans into `any` as `pgtype.Numeric` struct, breaking JSON number encoding and AST numeric comparisons. | Cast rollup query outputs explicitly in SQL (`::bigint` for `COUNT`/`SUM(int)`, `::double precision` for `SUM(float)`/`AVG`) to match raw `AggregateRecords` return types byte-for-byte (§5.5). |
| D8 | Blocking `Publish` on rollup backfill | Backfilling millions of rows takes minutes; blocking `Publish` would stall urgent fraud rule releases during an active attack. | Only Phase 1 index jobs gate `Publish`. Rollup definitions backfill asynchronously and cut over when `ready`; until then, live queries run safely on the Phase 1 covering index (§5.2). |

---

## 3. Phase 0: Evidence & Latency Budget (Gates All Optimization Claims)

- **Disposable benchmark harness** (`backend/stress-tests/high_load_aggregates/`): refuses to start unless the target DB name carries an explicit disposable test marker (never the application database).
- **Representative dataset**: one tenant `transactions` table with 1k, 100k, and 1M rows in 7 days for hot merchants plus 10k background merchants (~2M rows total), including 5% in-place `ModePatch` updates so Postgres visibility-map bits and HOT update ratios reflect real churn.
- **Queries measured**: `COUNT` (1h), `SUM(amount)` (24h, 7d), `AVG(amount)` (30d), and filtered `SUM(amount)` (`status = 'approved'`, 7d).
- **Metrics captured**: p50/p95/p99 at 1, 50, and 200 concurrent evaluations; `EXPLAIN (ANALYZE, BUFFERS)` including `Heap Fetches`, `shared_blks_hit`, and `shared_blks_read`; Postgres CPU; and `BatchIngest` write throughput (rows/sec).
- **Gate**: agree on the concrete target SLO (default proposal: aggregate p99 ≤ 25 ms at 200 concurrent evaluations on a 1M-row merchant, with <10% `BatchIngest` throughput regression). Phase 3 is built **only** if Phases 1–2 miss that SLO on the benchmark.

---

## 4. Phase 1: Safe Defaults, Canonical Covering Indexes & Bounded Overload

### 4.1 Aggregate Enablement & Single Read Contract
1. **Default allow-list**: change `AGGREGATE_PUSHDOWN_AGGREGATES` default in `decision-engine-service/internal/app/config.go` to `["count", "count_distinct", "sum", "avg", "min", "max"]` and update `DECISION_ENGINE_SERVICE_V1_OPERATING_DECISIONS.md §6`.
2. **Fail at authoring/publication time**: `validation_service.go` and `PublicationService` reject any `Aggregator` whose aggregate function is disabled by `AGGREGATE_PUSHDOWN_AGGREGATES` or `AGGREGATE_PUSHDOWN_MODE=disabled`, so misconfiguration never surfaces first as live decision 500s.
3. **Single SQL semantics across read modes (F2)**: extract shared filter/aggregate SQL generation tests across `ingestion-service` and `decision-engine-service` (`direct_db` mode). In Phase 3, rollup execution lives solely in `ingestion-service` (table owner); if `TENANT_DATA_READ_MODE=direct_db` is configured, aggregates either stay on the covering-index raw query or call ingestion's contract, documented explicitly.

### 4.2 Canonical B-Tree Covering Index Specification
For each `Aggregator` node whose `filters` child is a top-level `List` / `and` group on `tableName`:
1. **Leading equality keys**: fields with `eq` or `in` predicates on `tableName`, deduplicated and sorted alphabetically by `fieldName` for deterministic index reuse.
2. **At most one range key**: one `gt`/`gte`/`lt`/`lte` field on `tableName` (preferring `TIMESTAMPTZ` fields when multiple range predicates exist). Additional range columns in a B-tree key widen leaf tuples without narrowing the index range scan.
3. **Covering `INCLUDE` columns**: the aggregated `fieldName` (when not `*` / lookup key already in key) plus any remaining top-level predicate fields on `tableName` (`neq`, `starts_with`, `is_null`, `is_not_null`, secondary range fields), sorted alphabetically and excluding columns already in the key.
4. **No index keys from `or` / `not` subtrees**: predicates nested inside `or` or `not` are never placed into the B-tree key (they may appear in `INCLUDE` if the outer query still has a leading equality key).
5. **`COUNT_DISTINCT`**: uses `(equality_keys, range_key) INCLUDE (distinct_field)` so Postgres can satisfy the scan from the index without heap fetches, while linting warns on windows >24h because per-query distinct hashing still scales linearly with matched rows.
6. **Visibility map & HOT trade-off**: adding `INCLUDE` columns prevents HOT updates when any included column is updated (F4) and requires all-visible pages for zero-heap-fetch Index-Only Scans. Configure per-table storage settings on high-volume tenant tables (`autovacuum_vacuum_scale_factor = 0.02`, `autovacuum_analyze_scale_factor = 0.01`) and track `Heap Fetches` and `n_tup_hot_upd / NULLIF(n_tup_upd, 0)`.
7. **Per-table index budget**: cap managed search indexes per tenant table (default `15`). Subsumption (§4.3) runs before checking the cap; exceeding the cap fails `StartPreparation` with an error naming the table and limit.

### 4.3 Non-Blocking, Validated Index Lifecycle in `data-model-service`
1. **Distinct deterministic index naming**: `managedIndexName(tableName, keyColumns, includeColumns, unique)` hashes `tableName + "|" + join(keyColumns, ",") + "|inc|" + join(includeColumns, ",")`, staying well under Postgres's 63-byte `NAMEDATALEN` limit even for long table names (truncate `tableName` prefix in the identifier to ≤32 bytes before appending the 8-hex digest).
2. **Pinned non-transactional connection**: introduce a dedicated `ConcurrentDDLRunner` backed by `*pgxpool.Pool.Acquire(ctx)` (separate from transactional `executor`):
   - Assert `conn.Conn().PgConn().TxStatus() == 'I'` (idle, not in tx).
   - Execute single-statement `SET lock_timeout = '5s'` (never combined into a multi-statement query string).
   - `defer` single-statement `RESET lock_timeout` before `conn.Release()` (if `RESET` fails, close the underlying connection so poisoned session state never returns to the pool).
3. **Validity-aware introspection (`pg_index`)**: replace `to_regclass($1) IS NOT NULL` in `GetManagedIndexState` with a join on `pg_class`, `pg_namespace`, and `pg_index` returning `{Exists, Valid: indisvalid && indisready}`.
   - If `Exists && Valid`: mark job `applied`.
   - If `Exists && !Valid` (leftover from an interrupted/deadlocked concurrent build): execute `DROP INDEX CONCURRENTLY` on the pinned connection before building.
   - Build using `CREATE INDEX CONCURRENTLY idx_name ON schema.table (keys) INCLUDE (inc)` (**without** `IF NOT EXISTS`), then re-query `pg_index` to confirm `indisvalid AND indisready` before marking `applied`.
4. **Prefix + covering subsumption**: an existing valid managed index `(K1, K2, K3) INCLUDE (I1, I2)` subsumes a requirement `(K1, K2) INCLUDE (I1)` because its key has `(K1, K2)` as a prefix and `K3 ∪ {I1, I2}` covers `{I1}`.
5. **Reference-counted index GC**: never drop an older index immediately upon applying a newer covering index (another live or committed iteration may still reference it). A background GC job drops managed indexes (`DROP INDEX CONCURRENTLY`) only when referenced by zero live/committed iterations across the tenant and past a configurable grace period (default 24h).

### 4.4 Atomic Multi-Scenario Staging, Bounded Wait & Statement Timeouts
1. **Always stage `EvaluateAllLiveScenarios` (Fixes D1)**: in `decision_service.go`, stage `plans[i]` for **all** `EvaluateAllLiveScenarios` calls (not only when `InlinePolicy.Enabled` is `true`) and persist all triggered scenarios in a single short database transaction after `group.Wait()` succeeds. This keeps DB transactions free of network calls (`AGENTS.md`) and guarantees zero partial commits if any scenario fails or times out.
2. **Bounded aggregate slot wait**: add `AGGREGATE_REMOTE_MAX_WAIT` (default `50ms` when `AGGREGATE_REMOTE_CONCURRENCY_LIMIT > 0`). Exceeding the wait returns a typed `ErrAggregateOverloaded` without touching the database.
3. **Safe handler overload mapping**: because `EvaluateScenario` and `EvaluateAllLiveScenarios` perform zero DB writes prior to `persist`, handlers cleanly map `ErrAggregateOverloaded`:
   - `EvaluateScenario` / `CreateDecision` / `CreateAllDecisions`: defer via `deferAsyncScenarioExecution` / `deferAsyncAllScenariosExecution` when `LIVE_ASYNC_FALLBACK_ENABLED=true`, else return `429 live_decision_overloaded`.
   - `HandleRecordIngested`: enqueue via `enqueueAsyncAllScenariosExecution` (preserving `WaitTimeoutMS` and `CallbackURL`).
4. **Per-query SQL timeout**: in `ingestion-service` (and `direct_db` reader), execute aggregate queries inside a read-only transaction with `SET LOCAL statement_timeout = $timeout` (default `500ms`, configurable via `AGGREGATE_SQL_STATEMENT_TIMEOUT`) so a degraded plan cannot pin pool connections.
5. **Shared DB connection budget**: document explicit pool sizing across `decision-engine` server/worker, `ingestion` server/worker, and `data-model` worker against `max_connections`.

---

## 5. Phase 2: Versioned Kleene-Safe Cheap-First Evaluation

### 5.1 Why Versioning Is Required & How Kleene Semantics Keep Outcomes Stable
- Today both `evaluator.go` (`TriggerFormula`) and `evidence.go` (rules) evaluate `and`/`or` left-to-right (F6).
- In `rule_evaluation.go:60-66`, `NullOperandError` marks a rule `Outcome = "skipped"` (`score += 0`), while `false` marks it `Outcome = "no_hit"` (`score += 0`), and hard runtime errors fail the evaluation.
- Under **Kleene 3-valued logic** (`false AND null == false`, `true OR null == true`), evaluating cheap children before expensive `Aggregator` nodes **preserves exact `v1` truth values whenever any child is decisively `false` (in `and`) or `true` (in `or`)**, even when another cheap child is `null`! Only the relative order between two hard errors (or a hard error vs a short-circuited `false`) can differ from `v1`, which is why semantics are versioned per scenario iteration (`evaluation_semantics`: `v1` existing default, `v2` new iterations).

### 5.2 `v2` Evaluation Rules (Shared Across `evaluator.go` and `evidence.go`)
1. **Static cost tiers** assigned to each child of `and` / `or` (ties preserve authored index):
   - **Tier 0**: constants, `field_ref` / payload access, pure boolean/math/string/time operators over Tier 0 inputs.
   - **Tier 1**: local DB helpers (`custom_list`, `record_tag`, `risk_snapshot`, `ip_flag`, `geoip`).
   - **Tier 2**: `related_field`, `database_access`, `past_decision_count`.
   - **Tier 3**: `Aggregator` (and any remote provider call).
2. **`v2` `and` execution**:
   - Iterate children in ascending `(costTier, authoredIndex)` order:
     - If child evaluates to `false`: stop immediately! Mark all unvisited children `Skipped: true` (`skippedEvaluation` in `evidence.go`) and return `false`. **Zero Tier 3 `Aggregator` calls occur when any Tier 0–2 child is `false`.**
     - If child returns an error (including `NullOperandError`): record `(authoredIndex, err)` and **continue** checking remaining cheaper/equal/later children ONLY if a `false` could still short-circuit? **Wait—be deliberate here!** If a Tier 0 child throws `NullOperandError`, evaluating a Tier 3 `Aggregator` solely to find out whether the rule outcome label is `"no_hit"` vs `"skipped"` (both 0 score!) would defeat the performance goal on sparse payloads! Therefore:
       - If a child returns `NullOperandError`, defer it while evaluating remaining **Tier 0–2** children (if any Tier 0–2 child is `false`, return `false` immediately); do **not** dispatch Tier 3 `Aggregator` calls once a `NullOperandError` or hard error has already occurred—return the earliest-authored error (`NullOperandError` -> rule `"skipped"`).
     - If all children evaluate to `true`: return `true`.
   - `v2` `or` is the exact dual (`true` short-circuits immediately; `NullOperandError` checks remaining Tier 0–2 children for `true`, never dispatching Tier 3 after null/error).
3. **Authored-order evidence tree**: in `evidence.go`, allocate `evaluation.Children = make([]domainast.EvaluationNode, len(node.Children))` up front and write each evaluated or `skippedEvaluation` node to `evaluation.Children[authoredIndex]`, preserving 1:1 positional alignment with `node.Children` and existing `EvaluationNode.Skipped` UI rendering.
4. **Authoring linter & `past_decision_count` guard**:
   - Warn during validation when an `Aggregator` lacks an equality predicate on `tableName` or uses `COUNT_DISTINCT` over a window >24h.
   - Add `DecisionRepository.CountByObject(ctx, tenantID, objectType, objectID, outcomeFilter)` (`SELECT COUNT(*)`) to replace the unbounded `ListByObject` slice load in `evaluator.go:391-410`.

---

## 6. Phase 3: Exact Time-Bucket Rollups (Built Only If Phase 1–2 Miss SLO)

### 6.1 Strict Rollup Eligibility
An `Aggregator` query uses rollups **if and only if** all hold:
1. Aggregate is `count`, `sum`, or `avg` (`min`, `max`, and `count_distinct` always use the Phase 1 covering index because in-place updates/retractions make `min`/`max` non-decomposable).
2. Filter tree is a single-field equality (`eq`) on `group_field` AND one or two range bounds (`gt`/`gte`/`lt`/`lte`) on `time_field`, with **zero** other predicates. Any extra filter (`status = 'approved'`, `currency = 'USD'`, `in`, `or`, `not`) uses the Phase 1 covering index.
3. Value field is `DataTypeInt`, `DataTypeFloat`, or `RecordLookupField` (for `COUNT`), and `time_field` is `TIMESTAMPTZ`.
4. Matching `core_ingestion.rollup_definitions` row has `status = 'ready'` and `model_version == model.RevisionID`.
5. Query lower bound is newer than the retention horizon of every bucket resolution selected by the planner.

### 6.2 Dynamic Rollup Definitions (Non-Blocking Publication)
```sql
CREATE TABLE IF NOT EXISTS core_ingestion.rollup_definitions (
    tenant_id UUID NOT NULL,
    id UUID NOT NULL,
    table_name TEXT NOT NULL,
    group_field TEXT NOT NULL,
    time_field TEXT NOT NULL,
    value_field TEXT NOT NULL,
    value_type TEXT NOT NULL, -- 'int' | 'float' | 'lookup_count'
    model_version TEXT NOT NULL,
    status TEXT NOT NULL,     -- 'capturing' | 'backfilling' | 'ready' | 'disabled'
    backfill_snapshot pg_snapshot,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, id),
    UNIQUE (tenant_id, table_name, group_field, time_field, value_field)
);
```
- **Non-blocking `Publish` (Fixes D8)**: `PublicationService.StartPreparation` requests eligible rollup definitions via `ingestion-service`, subject to a per-table cap (default `4`). Rollup status **does not block `Publish`** once Phase 1 covering indexes are `applied`; queries transparently use the covering index until `status = 'ready'`.
- **Schema change invalidation**: before `ingestion-service` serves or applies a rollup, if `definition.model_version != model.RevisionID` and the schema signature of `(table_name, group_field, time_field, value_field)` changed (dropped, archived, renamed, or retyped), `ingestion-service` transitions the definition to `disabled` and serves raw queries. If `RevisionID` bumped due to an unrelated table/field addition, `model_version` is fast-forwarded atomically.

### 6.3 Signed Delta Ledger & Single-CTE Pre-Image Capture (Fixes D4 & D6)
```sql
CREATE TABLE IF NOT EXISTS core_ingestion.rollup_deltas (
    tenant_id UUID NOT NULL,
    id BIGSERIAL NOT NULL,
    definition_id UUID NOT NULL,
    xid xid8 NOT NULL DEFAULT pg_current_xact_id(),
    group_value TEXT NOT NULL,
    event_time TIMESTAMPTZ NOT NULL,
    d_rows BIGINT NOT NULL,
    d_value_count BIGINT NOT NULL,
    d_value_sum NUMERIC NOT NULL,
    PRIMARY KEY (tenant_id, id),
    FOREIGN KEY (tenant_id, definition_id) REFERENCES core_ingestion.rollup_definitions (tenant_id, id) ON DELETE CASCADE
) WITH (
    autovacuum_vacuum_scale_factor = 0.0,
    autovacuum_vacuum_threshold = 2000,
    autovacuum_analyze_scale_factor = 0.0,
    autovacuum_analyze_threshold = 2000,
    autovacuum_vacuum_cost_delay = 2
);

-- Required so §6.5 single-statement queries do an O(log N) index-only seek per merchant, never a tenant-wide heap scan:
CREATE INDEX IF NOT EXISTS idx_rollup_deltas_query_covering
ON core_ingestion.rollup_deltas (tenant_id, definition_id, group_value, event_time)
INCLUDE (d_rows, d_value_count, d_value_sum, xid);

-- Required so the applier drains FIFO per definition without scanning other definitions:
CREATE INDEX IF NOT EXISTS idx_rollup_deltas_drain
ON core_ingestion.rollup_deltas (tenant_id, definition_id, id);
```
- **Atomic pre-image capture in `TenantDataWriter` (Fixes D6)**:
  - Active definitions (`status IN ('capturing', 'backfilling', 'ready')`) for `table_name` are cached briefly (e.g. 5s TTL + invalidated on definition mutation).
  - Pure `INSERT` (`ON CONFLICT DO NOTHING` succeeded): emit `(+1, +1 or 0, +val or 0)` for each active definition where `group_field` and `time_field` are non-null.
  - `UPDATE` (`ModePatch` or `ModeUpsert` conflict fallback): execute a **single CTE** locking the existing row (`WITH old_row AS (SELECT ... FROM tenant_x.t WHERE object_id = $n FOR UPDATE), updated AS (UPDATE tenant_x.t SET ... WHERE object_id = $n RETURNING ...) SELECT ...`) so old and new field values return in one DB round-trip. Emit `(-1, ...)` for the old tuple and `(+1, ...)` for the new tuple **only** if `(group_field, time_field, value_field)` changed.
  - In `BatchIngest` (up to 500 records), collect all emitted deltas across the transaction and insert them in **one** batched `INSERT INTO core_ingestion.rollup_deltas` statement.

### 6.4 River Applier & Multi-Resolution Buckets
```sql
CREATE TABLE IF NOT EXISTS core_ingestion.rollup_buckets (
    tenant_id UUID NOT NULL,
    definition_id UUID NOT NULL,
    resolution SMALLINT NOT NULL, -- 900 (15m) | 3600 (1h) | 86400 (1d) seconds
    group_value TEXT NOT NULL,
    bucket_start TIMESTAMPTZ NOT NULL,
    rows BIGINT NOT NULL DEFAULT 0,
    value_count BIGINT NOT NULL DEFAULT 0,
    value_sum NUMERIC NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, definition_id, resolution, group_value, bucket_start),
    FOREIGN KEY (tenant_id, definition_id) REFERENCES core_ingestion.rollup_definitions (tenant_id, id) ON DELETE CASCADE
);
```
- Runs **only** when `definition.status = 'ready'` (paused during `capturing` and `backfilling` — Fixes D5):
  1. `DELETE FROM core_ingestion.rollup_deltas WHERE tenant_id = $1 AND definition_id = $2 AND id IN (SELECT id FROM core_ingestion.rollup_deltas WHERE tenant_id = $1 AND definition_id = $2 ORDER BY id LIMIT 5000 FOR UPDATE SKIP LOCKED) RETURNING ...`
  2. Skip any delta where `backfill_snapshot IS NOT NULL AND pg_visible_in_snapshot(xid, backfill_snapshot)`.
  3. Fold remaining deltas in Go memory across resolutions `{900, 3600, 86400}` (skipping any resolution whose retention cutoff precedes `event_time`), aligned in UTC via `date_bin(interval, event_time, TIMESTAMPTZ '2000-01-01 00:00:00+00')`.
  4. Sort folded bucket keys by `(resolution, group_value, bucket_start)` (consistent lock order, zero deadlocks) and execute one batched `INSERT ... ON CONFLICT DO UPDATE SET rows = rollup_buckets.rows + EXCLUDED.rows, ...` inside the same short transaction.

### 6.5 Single-Statement Snapshot-Consistent Query & Exact Return Types (Fixes D5 & D7)
Given window bounds on `time_field`:
1. Decompose the interior of the window into disjoint whole UTC buckets: `86400`s days in the middle, `3600`s hours flanking them, `900`s quarter-hours flanking those, leaving at most two raw sub-15-minute edge intervals `[L, b_first)` and `[b_last, U]` (or zero upper raw edge when there is no upper bound, because open buckets + pending deltas cover up to `+∞`). Respect `gt` vs `gte` and `lt` vs `lte` strictly on the raw edges.
2. Execute **one SQL statement** combining three `UNION ALL` branches under a single `READ COMMITTED` snapshot:
   - **Branch 1 (`rollup_buckets`)**: matching `(tenant_id, definition_id, resolution, group_value, bucket_start)` ranges.
   - **Branch 2 (`rollup_deltas`)**: unapplied deltas in `[L..U]` for `(tenant_id, definition_id, group_value)` via `idx_rollup_deltas_query_covering`, filtered by `(backfill_snapshot IS NULL OR NOT pg_visible_in_snapshot(xid, backfill_snapshot))`.
   - **Branch 3 (raw edge intervals)**: `tenant_x.table` rows in the sub-15-minute edge(s) via the Phase 1 covering index.
3. **Exact Go/JSON return-type casts (Fixes D7)**:
   - `count` -> `COALESCE(SUM(c), 0)::bigint` (`int64`, matching `COUNT(col)`).
   - `sum` on `DataTypeInt` -> `COALESCE(SUM(s), 0)::bigint` (`int64`).
   - `sum` on `DataTypeFloat` -> `COALESCE(SUM(s), 0)::double precision` (`float64`).
   - `avg` -> `(SUM(s) / NULLIF(SUM(vc), 0))::double precision` (`float64` or `nil` when `vc == 0`).

### 6.6 Race-Free Backfill State Machine (Fixes D5)
1. Insert/reset definition with `status = 'capturing'`, `backfill_snapshot = NULL`. Applier does **not** run while `status != 'ready'`.
2. Wait for definition cache TTL expiry across ingestion replicas, then poll until `pg_snapshot_xmin(pg_current_snapshot()) >= xid_at_cache_expiry` so every in-flight transaction that started before delta capture has finished.
3. Transition to `status = 'backfilling'`; `DELETE FROM core_ingestion.rollup_buckets WHERE tenant_id = $1 AND definition_id = $2`.
4. Open a `REPEATABLE READ` read-only transaction, record `S = pg_current_snapshot()`, persist `backfill_snapshot = S` on `rollup_definitions`, and stream grouped bucket aggregates from `tenant_x.table` (within each resolution's retention horizon) into `rollup_buckets` in bounded batches.
5. Drain pre-`S` deltas synchronously (`DELETE FROM core_ingestion.rollup_deltas WHERE tenant_id = $1 AND definition_id = $2 AND pg_visible_in_snapshot(xid, $S)`), then set `status = 'ready'` and enqueue the River applier.

### 6.7 Reconciler, Retention & Tenant Isolation
- **Periodic reconciler**: samples `(tenant_id, definition_id, group_value, bucket_start)` in a single `REPEATABLE READ` snapshot comparing `rollup_buckets + pending deltas` against `tenant_x.table`. Any mismatch increments `rollup_reconcile_mismatch_total`, sets `status = 'disabled'` (instant safe fallback to Phase 1 covering index), and schedules a clean §6.6 rebuild.
- **Retention worker**: daily bounded batch `DELETE` (`LIMIT 10000` loops) pruning `900`s buckets >14d, `3600`s >90d, `86400`s >3y.
- **Isolation**: `tenant_id` leads every primary key, index, and foreign key; mandatory cross-tenant test suite.

---

## 7. Observability & Rollout

- **Metrics**:
  - `decision-engine`: `aggregate_path_total{path=rollup|raw|cache, reason}`, `aggregate_overloaded_total`, `aggregate_short_circuit_skips_total`.
  - `ingestion`: `aggregate_sql_seconds{path}`, `rollup_pending_deltas`, `rollup_applier_lag_seconds`, `rollup_reconcile_mismatch_total`, `rollup_definitions{status}`.
  - `data-model`: `index_job_duration_seconds`, `index_job_invalid_cleanup_total`.
- **Rollout order**:
  1. Phase 0 benchmark + agreed SLO budget.
  2. Phase 1 (config defaults, `past_decision_count` SQL pushdown, canonical covering indexes + `CONCURRENTLY` + `indisvalid` shipped together, always-staged `EvaluateAllLiveScenarios`, bounded wait + `statement_timeout`). Re-run Phase 0 benchmark.
  3. Phase 2 (`evaluation_semantics = v2` across `evaluator.go` and `evidence.go`), verified via live-vs-phantom `TestRun` diffing.
  4. Phase 3 (only if Phase 1–2 miss SLO): shadow mode first (compute rollup + raw in parallel, log/metric any delta, return raw), then `ENABLE_ROLLUP_AGGREGATES` per pilot tenant, then general availability.

## 8. Material Limitations (Recorded Honestly)
- Rollups accelerate single-group time-window `COUNT`/`SUM`/`AVG` only; multi-predicate filters (`status`, `currency`), `OR`/`NOT` trees, `MIN`/`MAX`, and `COUNT_DISTINCT` rely on Phase 1 covering indexes.
- Tenant `DataTypeFloat` columns are IEEE-754 `DOUBLE PRECISION` (F5); floating-point addition order differs at ~1e-15 relative precision between raw `float8` sums and `numeric`-accumulated rollups. Threshold rules at exact float boundaries can differ by 1 ULP.
- `DataTypeInt` is 32-bit `INTEGER` in tenant schemas today (`max ~2.14e9`), while `SUM` widens to `BIGINT`.
- Delta capture adds batched inserts + single-CTE pre-image reads on tables with active rollup definitions; Phase 0/3 benchmarks gate acceptable write overhead.
