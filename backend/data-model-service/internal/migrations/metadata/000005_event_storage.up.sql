ALTER TABLE core.model_tables
  ADD COLUMN IF NOT EXISTS storage_class TEXT NOT NULL DEFAULT 'operational',
  ADD COLUMN IF NOT EXISTS event_time_field TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS storage_cutover_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS legacy_read_until TIMESTAMPTZ NULL;

ALTER TABLE core.model_tables
  DROP CONSTRAINT IF EXISTS model_tables_storage_class_check;

ALTER TABLE core.model_tables
  ADD CONSTRAINT model_tables_storage_class_check
  CHECK (storage_class IN ('operational', 'event'));

CREATE OR REPLACE FUNCTION core.cancel_event_table_index_jobs()
RETURNS trigger AS $$
BEGIN
  IF NEW.storage_class = 'event' AND OLD.storage_class IS DISTINCT FROM NEW.storage_class THEN
    UPDATE core.index_jobs
    SET status = 'cancelled',
        completed_at = NOW(),
        error_message = 'event table moved to ClickHouse; PostgreSQL indexing disabled'
    WHERE table_id = NEW.id AND status IN ('pending', 'running', 'failed');
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS model_tables_cancel_event_indexes ON core.model_tables;
CREATE TRIGGER model_tables_cancel_event_indexes
AFTER UPDATE OF storage_class ON core.model_tables
FOR EACH ROW EXECUTE FUNCTION core.cancel_event_table_index_jobs();
