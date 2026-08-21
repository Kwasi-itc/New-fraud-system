# Shared Event Repository

This Go module owns append-only event data and event aggregates. Ingestion and
decision-engine binaries import it directly; the normal Compose stack does not
run an event-store HTTP service.

- ClickHouse is the durable source for event records.
- Event writes do not create a per-event PostgreSQL ledger. Single event writes create no PostgreSQL row; a keyed event batch stores only one optional request-level receipt for the complete batch.
- Valkey is an optional, bounded accelerator for hot sealed-bucket series. A Valkey outage falls back to the durable ClickHouse facts and never fails a correct read or write.
- Event aggregates must contain a lower-bound filter on the table's configured `event_time_field`; unbounded historical scans are rejected.
- Eligible rolling `count`, `sum`, and `avg` queries are decomposed into exact raw boundary hours plus reusable sealed full-hour facts when the equality dimension field opts into an accelerated aggregation mode. An optional `lt`/`lte` as-of bound is honored, so replay and live decisions never include events later than the evaluated record. The current open UTC hour always stays on the raw path.
- Promoted hourly `sum` and `count` components are durable in ClickHouse. `avg` is derived from those two components. Each event table has a small materialized view that advances only the generation of the event-time hour touched by a write. Older hours remain valid, and a changed hour is rebuilt before it is read.
- Hot composed bucket series are cached in Valkey under the shared `FEATURE_NAMESPACE` (default `fraud:event-feature:v1`). Admission is capped globally by `FEATURE_MAX_KEYS` and per tenant by `FEATURE_MAX_KEYS_PER_TENANT` and expires by `FEATURE_TTL`.
- `adaptive_cache` creates a durable per-value summary only after repeated use, so one-off account references consume neither durable fact rows nor Valkey result entries. `tiered_summary` stores all dimension values for a promoted query shape on ClickHouse disk but still admits only hot values to Valkey. `always_online` admits each queried value immediately while preserving the same hard Valkey caps.
- For aggregates constrained by several equality fields, every dimension must opt in. The least expansive selected mode wins, so an adaptive account reference prevents a broader merchant policy from materializing every account/merchant combination.
- A cold accelerated query follows the field's explicit policy: exact raw ClickHouse fallback, synchronous durable build, async deferral, rule non-match, or configured numeric default. `projection_only` never enters this path.
- `count_distinct`, `min`, `max`, OR/NOT filters, duplicate lower or upper event-time bounds, and other unsupported shapes continue to use the exact raw ClickHouse path.
- Set `EVENT_AGGREGATE_FACTS_ENABLED=false` to disable the entire hourly-fact path. In that mode the repository performs raw or batched ClickHouse aggregates, does not read or write Valkey feature entries, does not promote or build durable hourly facts, and removes the per-table fact-generation view when a table is next verified.

ClickHouse uses one physical table per tenant data-model table. Every active data-model field is created as a typed ClickHouse column, including fields used by filters and aggregates; there is no shared JSON payload column. Tables use monthly event-time partitions, event-time/object ordering, and a bloom filter for object IDs. Fields explicitly marked `is_projection=true` receive a full-row projection ordered by that field, event time, object ID, and event ID. ClickHouse maintains these alternate layouts during inserts and rebuilds them during `ReplacingMergeTree` deduplicating merges. Event IDs are stable for exact retries, allowing compaction to collapse duplicate physical rows. Concurrent synchronous ingestion requests use ClickHouse asynchronous insert batching with `wait_for_async_insert=1`: callers receive success only after ClickHouse confirms the batch.

The repository also supports batches of up to 64 conditional aggregates in one
ClickHouse statement. The decision engine batches cold raw queries for any
projected field when they share the same user-selected projection value,
including `adaptive_cache` and `tiered_summary`. The repository still applies
each query's aggregation policy independently before combining the remaining
raw work.

The shared repository owns these generic durable fact tables in the configured ClickHouse database:

- `event_fact_bucket_generations`
- `event_fact_sources`
- `event_fact_shapes`
- `event_fact_bucket_builds`
- `event_fact_bucket_values`

Fact promotion runs through one bounded background builder per repository process. Whether a decision reads raw data, waits, defers, skips, or uses a default while the historical build runs is controlled by the field's cold behavior.

The physical table is created lazily on its first write from the schema contract supplied by ingestion-service. The stored ClickHouse schema is verified before a process begins writing to it. The data-model service separately locks the event schema before that first write, and subsequent physical table, field, or enum mutations are rejected until versioned event-table evolution is implemented. Runtime aggregation policy remains editable on fields that were projected before the lock.

Legacy rows in the former shared `event_records` JSON table are not silently ignored. Startup fails with an explicit migration/fresh-volume error when that table contains data. There is currently no automatic conversion of those rows into typed tables.

`cmd/server` remains as a compatibility adapter and exposes internal endpoints only:

- `POST /internal/v1/events`
- `POST /internal/v1/events/batch`
- `POST /internal/v1/records/get`
- `POST /internal/v1/records/list`
- `POST /internal/v1/aggregates`

Configuration is documented in the root `docker-compose.yml`. Each importing
process owns a bounded ClickHouse HTTP connection pool. The decision server has
the largest default pool because aggregation is part of its critical path.
Read-only HTTP queries request server-side cancellation when their client
disconnects and carry an execution timeout just below the client timeout, so a
timed-out decision cannot leave unbounded abandoned SELECTs behind.
