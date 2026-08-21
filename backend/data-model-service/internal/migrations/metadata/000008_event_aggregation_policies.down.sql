CREATE OR REPLACE FUNCTION core.prevent_locked_event_field_change()
RETURNS trigger AS $$
BEGIN
  IF TG_OP IN ('UPDATE', 'DELETE') THEN
    PERFORM core.assert_event_table_schema_unlocked(OLD.table_id);
  END IF;
  IF TG_OP IN ('INSERT', 'UPDATE') AND (TG_OP = 'INSERT' OR NEW.table_id IS DISTINCT FROM OLD.table_id) THEN
    PERFORM core.assert_event_table_schema_unlocked(NEW.table_id);
  END IF;
  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

ALTER TABLE core.model_fields
  DROP CONSTRAINT IF EXISTS model_fields_aggregation_policy_check,
  DROP CONSTRAINT IF EXISTS model_fields_aggregation_default_value_check,
  DROP CONSTRAINT IF EXISTS model_fields_aggregation_cold_behavior_check,
  DROP CONSTRAINT IF EXISTS model_fields_aggregation_mode_check,
  DROP COLUMN IF EXISTS aggregation_default_value,
  DROP COLUMN IF EXISTS aggregation_cold_behavior,
  DROP COLUMN IF EXISTS aggregation_mode;
