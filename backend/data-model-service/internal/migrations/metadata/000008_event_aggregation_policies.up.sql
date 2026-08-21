ALTER TABLE core.model_fields
  ADD COLUMN IF NOT EXISTS aggregation_mode TEXT NOT NULL DEFAULT 'projection_only',
  ADD COLUMN IF NOT EXISTS aggregation_cold_behavior TEXT NOT NULL DEFAULT 'query_clickhouse',
  ADD COLUMN IF NOT EXISTS aggregation_default_value DOUBLE PRECISION NULL;

ALTER TABLE core.model_fields
  DROP CONSTRAINT IF EXISTS model_fields_aggregation_mode_check,
  ADD CONSTRAINT model_fields_aggregation_mode_check CHECK (
    aggregation_mode IN ('projection_only', 'adaptive_cache', 'tiered_summary', 'always_online')
  ),
  DROP CONSTRAINT IF EXISTS model_fields_aggregation_cold_behavior_check,
  ADD CONSTRAINT model_fields_aggregation_cold_behavior_check CHECK (
    aggregation_cold_behavior IN ('query_clickhouse', 'durable_summary', 'defer_async', 'skip_rule', 'use_default')
  ),
  DROP CONSTRAINT IF EXISTS model_fields_aggregation_default_value_check,
  ADD CONSTRAINT model_fields_aggregation_default_value_check CHECK (
    (aggregation_cold_behavior = 'use_default' AND aggregation_default_value IS NOT NULL)
    OR (aggregation_cold_behavior <> 'use_default' AND aggregation_default_value IS NULL)
  ),
  DROP CONSTRAINT IF EXISTS model_fields_aggregation_policy_check,
  ADD CONSTRAINT model_fields_aggregation_policy_check CHECK (
    (aggregation_mode = 'projection_only' AND aggregation_cold_behavior = 'query_clickhouse')
    OR (aggregation_mode <> 'projection_only' AND is_projection)
  );

-- Aggregation policy is runtime metadata, not part of the immutable physical
-- ClickHouse schema. Keep every physical field attribute locked while allowing
-- this policy to be tuned for an already-ingested projected event field.
CREATE OR REPLACE FUNCTION core.prevent_locked_event_field_change()
RETURNS trigger AS $$
DECLARE
  target_storage_class TEXT;
  target_locked_at TIMESTAMPTZ;
BEGIN
  IF TG_OP = 'DELETE' THEN
    PERFORM core.assert_event_table_schema_unlocked(OLD.table_id);
    RETURN OLD;
  END IF;

  IF TG_OP = 'INSERT' THEN
    PERFORM core.assert_event_table_schema_unlocked(NEW.table_id);
    RETURN NEW;
  END IF;

  IF NEW.table_id IS DISTINCT FROM OLD.table_id THEN
    PERFORM core.assert_event_table_schema_unlocked(OLD.table_id);
    PERFORM core.assert_event_table_schema_unlocked(NEW.table_id);
    RETURN NEW;
  END IF;

  SELECT storage_class, event_schema_locked_at
    INTO target_storage_class, target_locked_at
  FROM core.model_tables
  WHERE id = OLD.table_id
  FOR SHARE;

  IF target_storage_class = 'event' AND target_locked_at IS NOT NULL THEN
    IF NEW.name IS DISTINCT FROM OLD.name
       OR NEW.description IS DISTINCT FROM OLD.description
       OR NEW.data_type IS DISTINCT FROM OLD.data_type
       OR NEW.nullable IS DISTINCT FROM OLD.nullable
       OR NEW.is_enum IS DISTINCT FROM OLD.is_enum
       OR NEW.is_unique IS DISTINCT FROM OLD.is_unique
       OR NEW.is_projection IS DISTINCT FROM OLD.is_projection
       OR NEW.archived IS DISTINCT FROM OLD.archived THEN
      RAISE EXCEPTION 'event table data model is immutable after ingestion has started'
        USING ERRCODE = '55000';
    END IF;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
