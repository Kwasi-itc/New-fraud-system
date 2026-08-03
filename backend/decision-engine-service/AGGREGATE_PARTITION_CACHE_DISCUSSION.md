# Aggregate Partition Cache Discussion

Date captured: 2026-07-30

## Context

The current aggregate path starts in the decision engine but executes in the ingestion service:

```text
decision-engine rule evaluation
-> ingestion HTTP client
-> POST /v1/tenants/{tenant}/query/aggregate
-> ingestion service
-> TenantDataReader.AggregateRecords
-> direct SQL aggregate over tenant schema table
```

Current key files:

```text
backend/decision-engine-service/internal/runtime/ast_eval/aggregate_compiler.go
backend/decision-engine-service/internal/runtime/ast_eval/evaluator.go
backend/decision-engine-service/internal/clients/ingestion/http_client.go
backend/ingestion-service/internal/httpapi/handlers/ingest.go
backend/ingestion-service/internal/service/ingest_service.go
backend/ingestion-service/internal/store/postgres/tenant_data_reader.go
backend/ingestion-service/internal/store/postgres/tenant_data_writer.go
backend/data-model-service/internal/tenantdb/postgres/indexing.go
```

The optimization being discussed is for large time-series data, especially transactions and other tenant-selected time-series tables.

## Proposed Direction

Use day-level partitions for large time-series tables and add lazy partition-scoped aggregate caching.

The first request for a specific aggregate signature should calculate the aggregate over the relevant partitions. Closed/sealed partition results can then be cached in Redis. Later requests for the same normalized aggregate signature should read already computed sealed partition values from Redis, calculate only missing partitions, and always query the live/current partition directly.

Example aggregate:

```text
COUNT transactions
from accountB
to merchantC
where product_id IN (product1, product2, product3)
for the last 30 days
```

Possible cache signature inputs:

```text
tenant_id
object_type
aggregate
field
normalized_filter_hash
partition_date
model_revision
cache_version
```

For `avg`, store composable values:

```text
sum
count
```

Do not store only the average per partition, because partition averages cannot be averaged together safely.

## Partition-Aware Query Shape

The first/cache-miss query can use partition-aware SQL to calculate per-partition results in one pass.

Example shape:

```sql
SELECT
    tableoid::regclass::text AS partition_name,
    COUNT(*) AS partition_count,
    SUM(COUNT(*)) OVER () AS overall_count
FROM sales
WHERE sale_date BETWEEN '2026-07-01' AND '2026-07-31'
GROUP BY tableoid;
```

For the actual system, this would need to use the tenant schema table, the configured time field, the normalized aggregate expression, and the compiled filter SQL.

## User Decisions Captured

1. Partitioning should be part of the initial design, not deferred as an afterthought.

Some tenants may not have enough daily volume to justify many tiny physical daily partitions. However, the alternative of querying years of historical data, potentially over 100 million records, is also not acceptable. The system should be designed around partitions from the start, even if some tenants/tables later use different partition granularity or partition policy.

2. The user should choose the timestamp field used for partitioning.

The partition time field should not be inferred blindly. It should be explicit table configuration, for example `date`, `transaction_date`, or another event-time field.

3. Scope should be tenant plus any other time-series data.

This should not be limited forever to only `transactions`, but it should apply only to tenant-selected time-series tables. It should not automatically apply to every tenant table.

4. Supported aggregates should start with composable aggregates.

Initial supported aggregate set:

```text
count
sum
avg
min
max
```

`count_distinct` should not be included initially unless a separate exact or approximate design is chosen.

5. Cache-eligible filters should be restricted at first.

Initial cacheable filters should be strict and normalizable. Examples:

```text
eq
in
gt
gte
lt
lte
AND groups
simple bounded time ranges
```

Complex filters should fall back to normal SQL until safely supported.

6. Similar requests should mean the same exact normalized signature.

The first version should not attempt subset/superset reuse. For example, cached results for `product_id IN (p1,p2,p3)` should not be reused to answer `product_id IN (p1,p2)`.

7. Current/open partition caching is not fully settled.

The initial recommendation was to avoid caching current/open partitions and always query them live. The user partially agrees, but this should be revisited together with partition strategy, because low-volume tenants may not justify overly fine daily behavior in all cases.

8. Late-arriving data needs a sealing delay.

A partition should not be considered sealed immediately at the end of the day. A delay such as 48 hours was discussed as a starting point.

9. Dirty partitions should be tracked.

If late-arriving data or updates affect an old partition, mark that partition dirty. While dirty, bypass cached values or rebuild before serving from cache.

10. Redis must be optional.

Redis outage must not break decisions. The system should fall back to Postgres and log/measure the fallback.

## Main Warnings

### Physical Partitioning Conflicts With Current Upserts

Current ingestion writes use:

```sql
ON CONFLICT (object_id) DO UPDATE
```

Native PostgreSQL partitioning by date can complicate unique constraints. On partitioned tables, uniqueness usually needs to include the partition key. This may conflict with the current assumption that `object_id` is globally unique for the table.

This is the largest structural risk.

Potential options:

```text
1. Change uniqueness to include partition field.
2. Keep a separate object_id lookup table.
3. Use manual partition routing instead of native declarative partitioning.
4. Start with logical partition cache and add physical partitioning carefully.
```

### Wrong Time Field Means Wrong Decisions

Partitioning by `updated_at` would likely be wrong for fraud windows, because `updated_at` is ingestion/update time, not transaction event time. The partition field must be explicit and should represent event time.

### Complex Filters Can Make Cache Reuse Unsafe

If the filter normalization is wrong, the cache can return a value for a different logical query. This would directly affect fraud decisions.

Complex examples that should be treated carefully:

```text
OR across different fields
NOT groups
starts_with
ends_with
unbounded ranges
relative time expressions that cannot be normalized
```

### First Requests May Be Slower

For one-off aggregate signatures, the new flow can be slower than the current direct SQL path because it adds:

```text
query normalization
partition planning
Redis lookup
per-partition grouping
Redis writes
result combining
```

This only pays off when aggregate signatures repeat.

### Redis Key Explosion Is Possible

Entity combinations can become very high-cardinality:

```text
tenant x account x merchant x product-set x channel x country x partition day
```

If most combinations are one-off, Redis memory usage can grow without useful hit rate.

Mitigations:

```text
TTL
max cacheable partitions per request
hot-signature tracking
feature flags
cache admission policy
```

### Hot Aggregate Promotion Can Create Background Load

Frequently accessed aggregate signatures may be promoted to precompute-on-partition-seal. This is useful, but too many promoted signatures could overload the system during sealing.

Promotion needs throttling and caps.

### Schema Changes Need Cache Versioning

Cache keys must include model revision or another schema/cache version. Otherwise, cached values can survive table or field changes and become invalid.

## Suggested Implementation Walkthrough

### 1. Add Time-Series Table Configuration

Likely data-model changes:

```text
table partitioning enabled/disabled
partition timestamp field
partition grain
seal delay
```

The partition field should be immutable once data exists, or at least require an explicit migration.

### 2. Add Partition-Aware Table Creation

Likely data-model service changes:

```text
backend/data-model-service/internal/tenantdb/postgres/indexing.go
```

For time-series tables, create a partitioned parent table and daily partitions, or create the supporting structures needed for manual partition routing.

This must be designed around the `object_id` uniqueness issue.

### 3. Route Writes To The Correct Partition

Likely ingestion changes:

```text
backend/ingestion-service/internal/store/postgres/tenant_data_writer.go
```

Writes must determine partition from the configured timestamp field. Late writes into sealed/old partitions must mark that partition dirty.

### 4. Add Aggregate Cache Metadata

Likely ingestion migration changes:

```text
backend/ingestion-service/internal/migrations/metadata
```

Useful metadata tables:

```text
core_ingestion.aggregate_partition_state
core_ingestion.aggregate_cache_signatures
core_ingestion.aggregate_hot_signatures
core_ingestion.aggregate_rebuild_jobs
```

Redis stores values. Postgres stores durable state.

### 5. Add Aggregate Signature Normalization

Likely ingestion or shared aggregate-planner code:

```text
normalize aggregate
normalize field
normalize filters
sort IN values
sort AND predicates where safe
extract bounded partition range
hash normalized signature
```

If the query cannot be normalized safely, bypass cache.

### 6. Add Redis Read-Through Cache

Likely ingestion changes:

```text
backend/ingestion-service/internal/app/config.go
backend/ingestion-service/internal/service/ingest_service.go
backend/ingestion-service/internal/store/postgres/tenant_data_reader.go
```

Flow:

```text
1. Determine required partitions.
2. Split into sealed, dirty, open/current, and missing.
3. MGET sealed partition keys from Redis.
4. Query Postgres for misses and dirty/open partitions.
5. SET Redis values only for clean sealed partitions.
6. Combine partition results.
```

### 7. Add Hot Signature Promotion

Later phase.

Track repeated access counts. Promote signatures only after a clear threshold and cap total promoted signatures.

Example threshold:

```text
>= 100 hits in 15 minutes
or >= 1000 hits per day
```

### 8. Add Observability And Safe Rollout

Metrics needed:

```text
aggregate cache hit rate
aggregate cache miss rate
Redis latency
Postgres fallback count
dirty partition count
partition rebuild time
aggregate result comparison in shadow mode
```

Rollout should be per tenant/table, not global.

## Open Decisions

1. Should low-volume tenants use daily partitions, monthly partitions, or logical partition caching without physical daily partitions?
2. What exact table metadata should represent time-series partitioning?
3. What is the default seal delay?
4. Should open/current partition ever be cached with a very short TTL?
5. What is the maximum number of partitions one aggregate request can touch before forcing direct SQL or rejecting?
6. What Redis TTL should sealed aggregate keys use?
7. What hot-signature promotion threshold should be used?
8. How should native partitioning handle global `object_id` uniqueness?

## Current Recommendation

Design the system around partition-awareness now, but be careful about immediately switching all writes to native PostgreSQL daily partitions.

The safest path is:

```text
1. Add explicit time-series/partition metadata.
2. Add aggregate signature normalization.
3. Add partition-aware read-through aggregate caching.
4. Add dirty/sealed partition metadata.
5. Add native physical partitioning only after the object_id/upsert strategy is resolved.
6. Add hot aggregate promotion after cache hit rates prove value.
```

This question made me think that there are some tenants that will just not have that many transactions coming in per day but I would not want the choice to be querying over 100million historical data over years vs having 100 or 1000 record partitions. I would like us to look
  at partitions initially I do not want to build something that was initially supposed to be built arround partitions and come back to implement partitions and it is not available. 2. The user should choose the timestamp field to do partitioning on. 3. tenant plus any other
  timeseries data. 4. I agree with you. 5. I aggree with you 6. I agree with you.  7. I aggree with you partially but it comes back to my question 1 analysis 8. I aggree with you. 9. I aggree with you, 10 i agree with you (the numbers are mixed up for the entire decion set and your
  final suggested decision set. Save everything about this conversation in a partition discussion file in the decision engine service so we comeback to it

## Continued Analysis: Logical Bucket Pivot

Date captured: 2026-07-30

The physical-partition proposal and open decisions above are retained as historical
context. Where they conflict with this section or later confirmed decisions, the
logical-bucket sections take precedence.

The design has pivoted away from native physical table partitioning for the initial implementation.

The new direction is:

```text
one physical tenant table
+ mandatory B-tree indexes on configured event-time fields
+ versioned logical aggregate buckets
+ Redis aggregate-result caching
```

Physical partitioning and its `object_id`/primary-key uniqueness problems are out of scope for this initial design. They may be reconsidered only if large-table benchmarks show that indexed bounded queries cannot meet the required performance.

### Target Aggregate Read Path

The decision engine will own the complete aggregate read path:

```text
rule evaluation
-> compile and normalize aggregate query
-> select a logical bucket definition
-> read bucket generations directly from PostgreSQL
-> look up sealed bucket results directly in Redis
-> query missing/open bucket ranges directly from tenant PostgreSQL tables
-> combine bucket components
-> return the aggregate to rule evaluation
```

The decision engine will have direct PostgreSQL and Redis access for aggregate execution. It will not call ingestion-service for aggregate execution or bucket-version lookups.

Existing ingestion aggregate endpoints will remain available for compatibility, but the decision engine will not use them in the new path.

Non-aggregate tenant record reads may continue using the existing ingestion-service read endpoints unless separately redesigned.

### Service Ownership After The Pivot

#### Data-model service

Owns:

```text
table and field definitions
field types and uniqueness declarations
unique indexes and other schema-correctness constraints
published model revision
user-authored logical bucket definitions
the worker that validates and physically applies all managed index jobs
```

The data-model worker remains the only component that executes managed index DDL. Other services submit index intent through its job contract.

#### Ingestion service

Owns:

```text
tenant record writes and updates
logical bucket state and generation metadata
incrementing affected bucket generations in the same transaction as a write
logical-bucket-related performance index intent
loading old configured timestamp values before updates
incrementing both old and new buckets when a timestamp moves
```

The ingestion service requests logical-bucket-related indexes from the data-model index worker. It does not execute the index DDL itself.

#### Decision engine service

Owns:

```text
aggregate AST compilation
cache eligibility
filter and signature normalization
logical bucket selection and range planning
direct PostgreSQL aggregate SQL
direct bucket-generation reads
direct Redis reads and writes
combining count/sum/avg/min/max components
rule/aggregate-related performance index intent
aggregate cache observability and fallback
```

The decision engine requests rule/aggregate-related indexes from the data-model index worker. It does not execute index DDL itself.

### Distributed Index Intent

Index intent may originate from all three services:

```text
data-model service -> unique/schema-correctness indexes
ingestion service  -> logical-bucket and ingestion-access indexes
decision engine    -> rule and aggregate filter indexes
```

All physical managed index creation is queued to and executed by the data-model worker.

The index job model will need to distinguish at least:

```text
requesting service
index purpose
tenant and table
ordered key columns
optional included columns
index method
model revision
canonical physical index specification hash
```

Deduplication must use the canonical physical specification, not only the requesting service or purpose. Two services may request the same useful physical index. Dropping an index must not occur while another active intent still requires it.

### Multiple Logical Bucket Definitions

A time-series table may have more than one logical bucket definition.

Examples:

```text
transaction_time -> daily buckets in Africa/Accra
settlement_time  -> daily buckets in Africa/Accra
created_at       -> daily buckets in UTC
```

Each definition needs a stable identifier and version:

```text
bucket_definition_id
table_id
timestamp_field_id
grain
timezone
seal_delay
definition_version
status
```

A query uses one driving bucket definition whose timestamp field matches the bounded time filter being planned. Initial support should not build multidimensional buckets that combine two timestamp fields in the same cache key.

Changing the timestamp field is not allowed as an in-place edit initially. Changing
field, grain, or timezone should create a new versioned definition and retire the old
definition after its activation/retirement grace window. Versioned keys keep old and
new cache entries isolated, and the cleanup worker later removes unreachable keys.

The ingestion write path updates generation state for every active bucket definition whose configured timestamp is present. An update that moves a timestamp increments the old and new logical buckets.

### User-Configured Timezones

Bucket timezone is user-configured per logical bucket definition.

Timezone values should use IANA timezone names, for example:

```text
Africa/Accra
Europe/London
America/New_York
```

Bucket boundaries are calculated in the configured timezone and persisted/queried as UTC instants. This is necessary because daylight-saving time can make a local calendar day 23 or 25 hours.

The cache key must include the bucket definition identifier/version, which covers field, grain, and timezone changes.

### Confirmed Runtime Decisions

1. New records on configured time-series definitions must contain a non-null timestamp value.
2. Configured timestamp fields cannot be edited in place initially.
3. Open/unsealed logical buckets are not cached initially.
4. The initial sealing delay is 48 hours after the bucket's configured-timezone end.
5. Late writes increment a durable bucket generation.
6. Scenario publication waits for required aggregate indexes to be applied.
7. Redis failure falls back to direct PostgreSQL aggregate queries in the decision engine.
8. PostgreSQL aggregate failure returns an aggregate evaluation error. The decision engine must not use the existing 5,000-record in-memory fallback for this path.
9. Existing large tables receive required indexes through background index jobs; logical caching is not enabled until the required index is applied.
10. Existing ingestion aggregate endpoints are retained but are not used by the decision engine's new aggregate path.
11. The maximum live aggregation range decision is shelved for later.

### Direct Database Access Requirements

Direct decision-engine database access introduces the following requirements:

```text
use the decision engine's existing merged-database connection
a separate internal aggregate-read adapter
validated physical tenant schema/table resolution
parameterized filters and sanitized identifiers
read timeouts independent from decision metadata writes
aggregate-read concurrency controls
consistent reads of bucket generation and aggregate data
```

The existing ingestion SQL builder may remain for compatibility endpoints, but the new decision-engine SQL implementation becomes the active aggregate behavior and needs contract tests to prevent semantic drift.

### Still-Open Decisions After The Pivot

1. Resolved: only daily logical buckets are supported initially.
2. Resolved: a table may have at most three active bucket definitions initially.
3. Resolved: a timestamp field may have only one active bucket definition initially.
4. Where is the user-facing bucket definition authored and stored? Current recommendation: data-model metadata and published model contract.
5. Does the 48-hour seal delay remain global, or can each definition override it?
6. What read-consistency strategy coordinates PostgreSQL bucket generations, Redis reads, and PostgreSQL cache-miss queries?
7. Resolved: use the decision engine's existing merged-database access, with aggregate-specific internal timeout and concurrency controls.
8. What exact physical schema/table identifiers must the data-model contract expose for safe direct reads?
9. Resolved: Redis aggregate values do not expire by TTL initially.
10. What Redis memory limit and eviction policy should be used?
11. Should the first computed signature be cached, or only signatures seen more than once?
12. How should concurrent identical cache misses be coalesced?
13. What is the maximum number of performance indexes per tenant table?
14. Which aggregate index shapes may include aggregate fields as covering columns?
15. When may an unused index intent be retired and its physical index dropped?
16. Partially resolved: the data-model worker must create large-table indexes concurrently; the exact execution and retry design remains open.
17. Partially resolved: requests must deduplicate by canonical physical index specification; the exact cross-service ownership/reference model remains open.
18. How long should shadow comparison run before cached values affect live decisions?
19. What benchmark dataset size, out-of-order rate, latency target, and buffer-read limit define success?
20. What hot-signature promotion threshold should be used later?
21. What maximum live aggregate time range should eventually be enforced or routed to async execution?

## Continued Decision Update

Date captured: 2026-07-30

The following additional decisions are confirmed:

1. Only daily logical buckets are supported initially.
2. A table may have at most three active logical bucket definitions initially.
3. A timestamp field may have only one active bucket definition initially.
4. The services use the merged PostgreSQL database. The decision engine already writes decision records and will use its existing database access for direct aggregate reads.
5. The decision engine does not require a separate service-wide read-only database role. Aggregate reads should still have a separate internal adapter and independently configured query timeout/concurrency controls.
6. Cross-service managed-index requests must use one canonical physical index specification for deduplication.
7. The data-model worker must support nonblocking/concurrent index creation for existing large tables.
8. Redis aggregate values do not use TTL expiration initially.
9. Redis staleness is handled using bucket generation, model revision, bucket definition version, and cache format version.

### Read Consistency Recommendation

The consistency problem occurs when a late write commits while the decision engine is reading a cached value or calculating a missing bucket.

Example:

```text
decision engine reads July 1 generation 4
decision engine reads Redis key for generation 4
late July 1 record is committed
ingestion increments July 1 to generation 5
```

The recommended initial solution is optimistic generation validation:

```text
1. Read the required bucket generations from PostgreSQL.
2. Read matching values from Redis.
3. Query PostgreSQL for missing or open buckets.
4. Read the bucket generations from PostgreSQL again.
5. If every generation is unchanged, return the result.
6. If a generation changed, discard and retry only the affected bucket once.
7. If it changes again, bypass its cache and calculate it directly under a database snapshot or return an aggregate error if consistency cannot be established.
```

This avoids keeping a database transaction open while waiting for Redis. A write that commits after the final generation check is treated as occurring after the aggregate's consistent read point.

For a cache miss, the result is written under the generation that passed the final validation. If a concurrent write increments the generation immediately afterward, the newly written older-generation key becomes unreachable and cannot be used by a later request.

### Redis Without TTL

No TTL means stale values must be separated into two concerns:

```text
correctness: old values must never be selected
memory: old and unused values must eventually be removed or evicted
```

Correctness is provided by versioned keys containing:

```text
tenant
table
bucket definition id and version
bucket start
bucket generation
model revision
cache format version
normalized aggregate signature
```

When a generation or revision changes, old keys no longer match and cannot answer a new request.

Because unreachable keys still consume memory, Redis also requires:

```text
a configured maximum memory limit
an eviction policy that can remove cold keys
a decision-engine cache cleanup worker
tracked key groups or cursor-based SCAN cleanup, never blocking KEYS
cleanup on bucket generation changes
cleanup when a bucket definition is retired
cleanup when model/cache versions are retired
```

The current recommendation is a frequency-aware all-key eviction policy so frequently reused aggregates remain longer than one-off signatures. Exact memory limits and the cleanup trigger mechanism remain open.

## Final Service Feasibility Pass

Date captured: 2026-07-30

### Overall Verdict

The logical-bucket design is feasible in the current three-service architecture. No
physical table partitioning or database split is required.

The current code already provides most of the required foundations:

```text
data-model service -> published tenant model and managed-index worker
ingestion service  -> one transaction around each row mutation and its metadata
decision engine    -> merged PostgreSQL access, aggregate AST compiler, and publication preparation
```

The work is not only a Redis addition. Each service needs a defined set of changes,
and several current behaviors must be replaced before the cached path is correct.

### Data-Model Service

#### Feasible Changes

The data-model service can own logical bucket definitions because it already owns:

```text
tenant tables and fields
field data types and nullability
published model revisions
tenant physical schema names
managed index jobs
schema-change history
```

Add durable logical bucket definition metadata with at least:

```text
id
tenant_id
table_id
timestamp_field_id
grain                 # daily only initially
timezone              # validated IANA name
seal_delay             # initially 48 hours
definition_version
status                 # pending_index, activating, active, retiring, retired
cache_eligible_at
created_at
updated_at
```

Validation must enforce:

```text
timestamp field belongs to the configured table
field data type is timestamp
field is non-nullable before activation
no more than three active/activating definitions per table
only one active/activating definition per timestamp field
timestamp field, grain, and timezone are not changed in place
```

The per-field rule can use a partial unique database index. The three-per-table rule
also needs transaction-safe serialization, such as locking the table metadata row
while counting active definitions. Service-only count validation would race under
two concurrent requests.

The published model must add:

```text
physical_schema_name
logical_bucket_definitions
bucket definition status and version
cache_eligible_at
```

Publishing `physical_schema_name` is preferable to duplicating the current
`tenant_<uuid-without-hyphens>` naming rule in another service. The decision engine
must still resolve table and field identifiers only from this validated model, never
from untrusted rule text.

Creating, activating, retiring, or replacing a definition must change the published
model revision. In the current implementation this means also recording the
appropriate tenant schema migration/revision input.

#### Managed Index Worker Changes

The managed-index worker is reusable, but its current contract is too narrow. Its
job currently describes only an index type and columns, and its deduplication key
includes the purpose. Replace that identity with a canonical physical specification:

```text
tenant and physical schema
table
index method
unique flag
ordered key columns
sort/null options when applicable
optional INCLUDE columns
optional predicate
canonical physical specification hash
```

Store requesting service and purpose as one or more intents that reference the
physical specification. This allows an ingestion logical-bucket intent and a
decision-engine aggregate intent to share one physical index without either service
being able to remove an index still required by the other.

For existing large tables, the worker must use `CREATE INDEX CONCURRENTLY`. The
current River worker has a hard 30-second timeout and currently runs ordinary
`CREATE INDEX`; both must change. The timeout must be configurable for long builds.
Retries must inspect `pg_index.indisvalid` and `indisready`, remove or repair an
invalid concurrent-build artifact, and then retry. Checking only whether an index
name exists is insufficient.

Only a verified, valid physical index may mark the physical specification applied.

Unique indexes need an additional two-phase state because they enforce correctness:

```text
requested -> building -> valid -> uniqueness metadata active
```

For example, setting an existing field to unique must not publish `is_unique=true`
before the concurrent unique index has succeeded. Creating `object_id` uniqueness
on a new empty table can still be submitted to the same worker, but the table must
not become writable before that job is valid.

#### Data-Model Feasibility Result

Feasible with migrations, repository/service/API additions, a richer index model,
and worker hardening. The present worker must not be used unchanged for large
tables.

### Ingestion Service

#### Feasible Changes

The ingestion service already places a row upsert, audit record, outbox record, and
idempotency record in one PostgreSQL transaction. Bucket generation changes can join
that same transaction.

Add an ingestion-owned durable generation table, for example:

```text
core_ingestion.logical_bucket_generations

tenant_id
table_id
bucket_definition_id
definition_version
bucket_start_utc
generation
last_changed_at

PRIMARY KEY (
  tenant_id,
  table_id,
  bucket_definition_id,
  definition_version,
  bucket_start_utc
)
```

An absent row means generation zero. Existing historical data therefore does not
require a generation-row backfill before rollout. The first later mutation creates
generation one.

For every active definition, a row mutation must:

```text
1. Serialize mutations for the same tenant/table/object.
2. Read the old configured timestamp values.
3. Apply the insert or update.
4. Determine the new configured timestamp values.
5. Increment every affected old and new logical day.
6. Commit the row and generation changes together.
```

The increment is required for every update, even when the timestamp remains in the
same day, because an aggregate field or non-time filter field may have changed.
Increment once when old and new resolve to the same bucket; increment both when a
record moves between buckets.

The current `SELECT EXISTS` before `INSERT ... ON CONFLICT DO UPDATE` is not enough
for this. Two concurrent inserts for the same previously absent object can both
observe no row, after which one becomes an update without knowing the prior bucket.
Use a transaction-scoped advisory lock keyed by tenant/table/object, followed by a
locked read and the upsert. Batch processing must acquire these locks in a stable
object-ID order to avoid cross-batch deadlocks.

The existing timestamp normalization already converts RFC3339 values to UTC. Daily
bucket boundaries must be calculated in the configured IANA timezone, then stored
as UTC instants. Embed Go timezone data in binaries that perform this calculation so
container image contents do not determine which IANA zones work.

New records must provide every timestamp used by an active definition. Patch
requests may omit those fields, but cannot set them to null. Moving a record's
timestamp remains supported; the earlier immutability decision applies to changing
a bucket definition's timestamp field in place, not to ordinary record corrections.

All production paths currently writing tenant rows go through this writer, including
batch and CSV ingestion. That must remain an explicit invariant. Any future direct
SQL import, delete endpoint, repair tool, or alternate writer must also update
generations transactionally or use a database trigger that provides the same
guarantee.

#### Definition Activation Race

Definition activation needs a short transition protocol. A write may have fetched
the old model immediately before a new definition becomes active. Without a guard,
that write could commit without bumping the new generation while the decision engine
starts caching it.

Use:

```text
pending_index
-> index verified
-> activating with cache_eligible_at in the future
-> active/cache eligible after the maximum model-cache and in-flight-write window
```

During replacement or retirement, ingestion must continue maintaining generations
for the old definition through the same grace window. The decision engine must query
PostgreSQL directly, without Redis population, until `cache_eligible_at`.

#### Logical-Bucket Index Request

The ingestion code currently has no data-model index-job client and no model-change
event consumer. Its outbox records are persisted, but there is no ingestion outbox
dispatcher that can deliver a bucket-definition-created event.

Therefore, if ingestion must literally initiate logical-bucket index requests, add
an ingestion reconciliation worker that periodically reads bucket definitions and
submits any missing logical-bucket intents. The simpler initial option is for the
data-model service to submit the physical request when it creates the definition,
while recording the intent owner and purpose as `ingestion/logical_bucket`.

#### Ingestion Feasibility Result

Feasible within the existing transaction boundary. The same-object concurrency lock,
activation grace period, and prohibition on untracked writers are correctness
requirements, not optional optimizations.

### Decision Engine Service

#### Feasible Changes

The decision engine already connects to the merged PostgreSQL database for decision
writes and River jobs. It can use that database access for tenant aggregate reads.
No database topology change is required.

Split the current combined tenant reader responsibility:

```text
tenant record reader -> existing ingestion HTTP client for non-aggregate lookups
aggregate reader     -> new decision-engine PostgreSQL/Redis adapter
```

Wire the direct aggregate reader into live decisions, scheduled/async decisions,
test runs, and phantom evaluations so all evaluation modes have the same aggregate
semantics.

The current aggregate compiler can be extended rather than replaced. The new planner
must:

```text
validate the table, aggregate field, and filter fields against the published model
normalize safe aggregate/filter signatures
identify one configured timestamp field with a simple bounded range
split the range into full sealed days and partial/open ranges
read durable generations for all required days
MGET cacheable sealed-day components from Redis
query PostgreSQL for misses, partial days, and open days
re-read generations and perform the agreed affected-bucket retry
combine composable components
```

Initial cacheable aggregate components are:

```text
count -> count
sum   -> sum
avg   -> sum plus non-null count
min   -> min
max   -> max
```

`count_distinct` is not supported by the initial decision-engine aggregate path.
Scenario validation must reject it until a separate exact or approximate design is
approved. The retained ingestion compatibility endpoint may keep its existing
behavior because the decision engine no longer uses that endpoint.

Cacheable SQL must use sargable half-open bounds:

```sql
timestamp_field >= $bucket_start_utc
AND timestamp_field < $bucket_end_utc
```

It must not wrap the indexed column in `DATE`, `DATE_TRUNC`, a timezone conversion,
or another function in the `WHERE` clause. Timezone conversion belongs in boundary
calculation. A B-tree index on the configured timestamp field then lets PostgreSQL
reach a small out-of-order date range without reading rows day by day from the
beginning of the table.

Rule-specific composite index candidates should put equality/`IN` filter columns
first and the timestamp range column next. A standalone timestamp index remains the
base logical-bucket requirement. Covering `INCLUDE` columns should be added only
where the index policy allows them; they are not required for correctness.

The current evaluator calls ingestion for a supported aggregate and, on failure,
loads up to 5,000 rows for local aggregation. Remove that behavior. A Redis failure
goes to direct PostgreSQL. A PostgreSQL aggregate failure returns an aggregate
evaluation error. Unsupported aggregate/filter shapes must either use a complete
direct SQL query or be rejected; they must never return an aggregate over a truncated
record list.

The current publication preparation code also needs correction:

```text
cancelled index jobs must not count as applied
only a verified valid physical index can unblock publication
requirements must use the canonical physical specification
aggregate requirements must include the driving timestamp index shape
```

Publication currently derives index columns from only a limited filter shape and
uses AST order. The new planner must generate a stable ordered specification from
the normalized query plan.

#### PostgreSQL Load Isolation

Use an aggregate-specific semaphore and query timeout. The same database URL and
credentials can remain, but a separately budgeted pgx pool is preferable if
aggregate waits must not consume every connection needed for decision writes and
River jobs. This is connection-pool isolation inside the decision service, not a
return to separate databases.

The implemented cold-cache path batches generation reads and Redis operations. All
missing sealed-day components are returned by one `UNION ALL` PostgreSQL statement,
so their generation and aggregate values share the same MVCC statement snapshot.
The configured query timeout applies separately to each bounded database operation;
the request context remains the overall deadline. Cache hits are accepted only when
batched generation reads before and after the Redis lookup agree.

#### Redis Addition

The decision engine currently has no Redis dependency, configuration, or container.
Add:

```text
Redis client dependency
optional REDIS_URL configuration
short Redis operation timeout
Redis container/deployment with an explicit memory limit
allkeys-lfu eviction policy
cache metrics
periodic cleanup worker
```

Cache metrics, admission policy, obsolete-generation cleanup, and retired-definition
cleanup remain deferred. Redis bounded-memory LFU eviction is the current memory
safety mechanism.

Redis must not be a readiness dependency. Missing configuration, connection failure,
timeout, eviction, or a malformed cached value all produce a cache miss and direct
PostgreSQL execution.

With no TTL, versioned keys provide correctness while eviction and cleanup provide
memory control. Since ingestion currently emits no usable invalidation event, the
first cleanup implementation should be periodic and cursor-based. It must use
`SCAN`, bounded work batches, and a maintained key registry or version metadata;
it must never use blocking `KEYS`.

#### Decision Engine Feasibility Result

Feasible as a new aggregate adapter and planner behind the existing AST evaluator.
The current ingestion HTTP aggregate call and 5,000-row fallback cannot remain in
the active decision path.

### Cross-Service Correctness Contract

The minimum end-to-end contract is:

```text
1. Data-model publishes a versioned definition and its physical schema identifiers.
2. A canonical base timestamp index is requested and built concurrently.
3. The physical index is verified valid.
4. The definition enters an activation grace window.
5. Ingestion transactionally maintains durable generations.
6. Decision engine enables cache reads only after cache_eligible_at.
7. Decision engine generation-checks Redis/SQL results and retries one changed bucket.
8. Redis errors fall back to PostgreSQL; PostgreSQL errors fail aggregate evaluation.
9. Publication requires every mandatory aggregate index to be physically valid.
```

### Large-Table Query Assurance

This design removes application-level whole-table lookup from aggregate evaluation.
A request for a small date range produces bounded SQL and has an applied B-tree index
that can seek to that range, including when records arrived out of order.

PostgreSQL still owns its physical plan. It can choose a sequential scan when its
statistics say that most of the table will be returned, when a predicate is not
sargable, or when statistics are stale. A literal promise that PostgreSQL will never
choose a table scan is not provided by a normal B-tree index.

The rollout gate should therefore require representative:

```text
EXPLAIN (ANALYZE, BUFFERS)
small-range and large-range plans
out-of-order data
fresh and late-written buckets
production-scale row counts
autovacuum/analyze behavior
query timeout and buffer-read thresholds
```

Caching must remain disabled for a table until the base timestamp index is valid and
the small-range benchmark uses an index/bitmap plan within the agreed buffer and
latency budget. Physical partitioning remains the later fallback if measured bounded
queries cannot meet that budget.

## Confirmed Implementation Decisions

The implementation proceeds with the following confirmed choices.

1. **Definition system of record:** should logical bucket definitions be authored
   and stored in the data-model service?

   **Confirmed: yes.** It owns tables, fields, physical schema names, revisions,
   and validation. Ingestion owns generation maintenance, not definition metadata.

2. **Logical-bucket index initiator:** may the data-model service enqueue the base
   timestamp index immediately on behalf of the
   `ingestion/logical_bucket` intent, or must ingestion itself submit it?

   **Confirmed:** data-model enqueues it on ingestion's behalf initially. The
   alternative requires a new ingestion reconciliation worker because no
   model-change event delivery exists.

3. **Existing null timestamps:** when enabling a definition on an existing table
   whose selected timestamp field is nullable or contains nulls, should activation
   be rejected until the data is backfilled?

   **Confirmed:** reject activation until data is corrected. Silently excluding
   those rows makes time-window aggregates incomplete.

4. **External tenant-table writers:** can production imports or administrators ever
   write tenant rows without the ingestion service?

   **Confirmed: no.** All tenant writes must use ingestion. If this changes,
   generation maintenance must move to
   PostgreSQL triggers so direct writes cannot bypass it.

5. **Literal table-scan prohibition:** is the requirement that the application never
   fetch a whole table for aggregation, or must PostgreSQL itself be prevented from
   ever choosing a sequential scan?

   **Confirmed:** prohibit application fallbacks, require bounded indexed SQL and
   benchmark gates, but let PostgreSQL choose a sequential scan when it is genuinely
   cheaper. A hard planner prohibition is brittle and can make broad queries slower.

6. **Index budget:** what maximum number of decision-engine performance index
   specifications may be active per tenant table?

   **Confirmed:** eight decision-engine performance specifications per table. Base
   logical-bucket and uniqueness indexes do not count
   against the performance-index cap. When the cap is reached, publication should
   report the unmet requirement rather than silently publish or drop another
   service's index.

7. **Seal delay scope:** is 48 hours one global initial value, or may each definition
   override it?

   **Confirmed:** use one global 48-hour value initially while retaining the field
   in the definition schema for a later controlled override.

Additional confirmed choices are daily grain only, at most three active definitions
per table, timestamp edits disabled in V1, Redis populated on the first eligible
miss, no Redis TTL, and a 512 MB `allkeys-lfu` Redis memory policy.
