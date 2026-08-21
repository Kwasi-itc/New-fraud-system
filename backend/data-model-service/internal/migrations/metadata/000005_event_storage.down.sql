DROP TRIGGER IF EXISTS model_tables_cancel_event_indexes ON core.model_tables;
DROP FUNCTION IF EXISTS core.cancel_event_table_index_jobs();

ALTER TABLE core.model_tables
  DROP CONSTRAINT IF EXISTS model_tables_storage_class_check;

ALTER TABLE core.model_tables
  DROP COLUMN IF EXISTS legacy_read_until,
  DROP COLUMN IF EXISTS storage_cutover_at,
  DROP COLUMN IF EXISTS event_time_field,
  DROP COLUMN IF EXISTS storage_class;
