ALTER TABLE core.model_tables
  ADD COLUMN IF NOT EXISTS event_schema_revision TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS event_schema_locked_at TIMESTAMPTZ NULL;

CREATE OR REPLACE FUNCTION core.assert_event_table_schema_unlocked(target_table_id UUID)
RETURNS void AS $$
DECLARE
  target_storage_class TEXT;
  target_locked_at TIMESTAMPTZ;
BEGIN
  SELECT storage_class, event_schema_locked_at
    INTO target_storage_class, target_locked_at
  FROM core.model_tables
  WHERE id = target_table_id
  FOR SHARE;

  IF target_storage_class = 'event' AND target_locked_at IS NOT NULL THEN
    RAISE EXCEPTION 'event table data model is immutable after ingestion has started'
      USING ERRCODE = '55000';
  END IF;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION core.prevent_locked_event_table_change()
RETURNS trigger AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF OLD.storage_class = 'event' AND OLD.event_schema_locked_at IS NOT NULL THEN
      RAISE EXCEPTION 'event table data model is immutable after ingestion has started'
        USING ERRCODE = '55000';
    END IF;
    RETURN OLD;
  END IF;

  IF OLD.storage_class = 'event' AND OLD.event_schema_locked_at IS NOT NULL THEN
    IF NEW.name IS DISTINCT FROM OLD.name
       OR NEW.description IS DISTINCT FROM OLD.description
       OR NEW.alias IS DISTINCT FROM OLD.alias
       OR NEW.semantic_type IS DISTINCT FROM OLD.semantic_type
       OR NEW.caption_field IS DISTINCT FROM OLD.caption_field
       OR NEW.storage_class IS DISTINCT FROM OLD.storage_class
       OR NEW.event_time_field IS DISTINCT FROM OLD.event_time_field
       OR NEW.archived IS DISTINCT FROM OLD.archived
       OR NEW.event_schema_revision IS DISTINCT FROM OLD.event_schema_revision
       OR NEW.event_schema_locked_at IS DISTINCT FROM OLD.event_schema_locked_at THEN
      RAISE EXCEPTION 'event table data model is immutable after ingestion has started'
        USING ERRCODE = '55000';
    END IF;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS model_tables_prevent_locked_event_change ON core.model_tables;
CREATE TRIGGER model_tables_prevent_locked_event_change
BEFORE UPDATE OR DELETE ON core.model_tables
FOR EACH ROW EXECUTE FUNCTION core.prevent_locked_event_table_change();

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

DROP TRIGGER IF EXISTS model_fields_prevent_locked_event_change ON core.model_fields;
CREATE TRIGGER model_fields_prevent_locked_event_change
BEFORE INSERT OR UPDATE OR DELETE ON core.model_fields
FOR EACH ROW EXECUTE FUNCTION core.prevent_locked_event_field_change();

CREATE OR REPLACE FUNCTION core.prevent_locked_event_enum_change()
RETURNS trigger AS $$
DECLARE
  old_table_id UUID;
  new_table_id UUID;
BEGIN
  IF TG_OP IN ('UPDATE', 'DELETE') THEN
    SELECT table_id INTO old_table_id FROM core.model_fields WHERE id = OLD.field_id;
    IF old_table_id IS NOT NULL THEN
      PERFORM core.assert_event_table_schema_unlocked(old_table_id);
    END IF;
  END IF;
  IF TG_OP IN ('INSERT', 'UPDATE') THEN
    SELECT table_id INTO new_table_id FROM core.model_fields WHERE id = NEW.field_id;
    IF new_table_id IS NOT NULL AND (TG_OP = 'INSERT' OR new_table_id IS DISTINCT FROM old_table_id) THEN
      PERFORM core.assert_event_table_schema_unlocked(new_table_id);
    END IF;
  END IF;
  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS field_enum_values_prevent_locked_event_change ON core.field_enum_values;
CREATE TRIGGER field_enum_values_prevent_locked_event_change
BEFORE INSERT OR UPDATE OR DELETE ON core.field_enum_values
FOR EACH ROW EXECUTE FUNCTION core.prevent_locked_event_enum_change();
