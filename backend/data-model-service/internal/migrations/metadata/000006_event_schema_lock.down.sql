DROP TRIGGER IF EXISTS field_enum_values_prevent_locked_event_change ON core.field_enum_values;
DROP FUNCTION IF EXISTS core.prevent_locked_event_enum_change();

DROP TRIGGER IF EXISTS model_fields_prevent_locked_event_change ON core.model_fields;
DROP FUNCTION IF EXISTS core.prevent_locked_event_field_change();

DROP TRIGGER IF EXISTS model_tables_prevent_locked_event_change ON core.model_tables;
DROP FUNCTION IF EXISTS core.prevent_locked_event_table_change();
DROP FUNCTION IF EXISTS core.assert_event_table_schema_unlocked(UUID);

ALTER TABLE core.model_tables
  DROP COLUMN IF EXISTS event_schema_locked_at,
  DROP COLUMN IF EXISTS event_schema_revision;
