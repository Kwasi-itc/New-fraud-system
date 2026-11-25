CREATE TABLE IF NOT EXISTS core_ingestion.rollup_definitions (
  tenant_id UUID NOT NULL,
  id UUID NOT NULL,
  table_name TEXT NOT NULL,
  group_field TEXT NOT NULL,
  time_field TEXT NOT NULL,
  value_field TEXT NOT NULL,
  value_type TEXT NOT NULL,
  model_version TEXT NOT NULL,
  status TEXT NOT NULL,
  backfill_snapshot pg_snapshot NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, table_name, group_field, time_field, value_field)
);

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
  FOREIGN KEY (tenant_id, definition_id)
    REFERENCES core_ingestion.rollup_definitions (tenant_id, id) ON DELETE CASCADE
) WITH (
  autovacuum_vacuum_scale_factor = 0.0,
  autovacuum_vacuum_threshold = 2000,
  autovacuum_analyze_scale_factor = 0.0,
  autovacuum_analyze_threshold = 2000,
  autovacuum_vacuum_cost_delay = 2
);

CREATE INDEX IF NOT EXISTS idx_rollup_deltas_query_covering
  ON core_ingestion.rollup_deltas (tenant_id, definition_id, group_value, event_time)
  INCLUDE (d_rows, d_value_count, d_value_sum, xid);

CREATE INDEX IF NOT EXISTS idx_rollup_deltas_drain
  ON core_ingestion.rollup_deltas (tenant_id, definition_id, id);

CREATE TABLE IF NOT EXISTS core_ingestion.rollup_buckets (
  tenant_id UUID NOT NULL,
  definition_id UUID NOT NULL,
  resolution INTEGER NOT NULL,
  group_value TEXT NOT NULL,
  bucket_start TIMESTAMPTZ NOT NULL,
  rows BIGINT NOT NULL DEFAULT 0,
  value_count BIGINT NOT NULL DEFAULT 0,
  value_sum NUMERIC NOT NULL DEFAULT 0,
  PRIMARY KEY (tenant_id, definition_id, resolution, group_value, bucket_start),
  FOREIGN KEY (tenant_id, definition_id)
    REFERENCES core_ingestion.rollup_definitions (tenant_id, id) ON DELETE CASCADE
);
