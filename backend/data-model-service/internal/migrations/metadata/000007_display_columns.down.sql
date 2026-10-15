ALTER TABLE core.table_options DROP COLUMN IF EXISTS display_columns;

CREATE OR REPLACE FUNCTION core.validate_display_options_v1() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF cardinality(NEW.displayed_fields)>12 OR
    cardinality(NEW.displayed_fields)<>(SELECT count(DISTINCT id) FROM unnest(NEW.displayed_fields) id) OR
    cardinality(NEW.field_order)<>(SELECT count(DISTINCT id) FROM unnest(NEW.field_order) id) OR
    EXISTS(SELECT 1 FROM unnest(NEW.displayed_fields||NEW.field_order) requested(id) WHERE NOT EXISTS
      (SELECT 1 FROM core.model_fields f WHERE f.id=requested.id AND f.table_id=NEW.table_id AND f.tenant_id=NEW.tenant_id AND NOT f.archived)) OR
    NOT EXISTS(SELECT 1 FROM core.model_tables t WHERE t.id=NEW.table_id AND t.tenant_id=NEW.tenant_id)
  THEN RAISE EXCEPTION 'display columns must be distinct active fields of this table, with at most 12 displayed fields' USING ERRCODE='23514'; END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION core.clean_field_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (NEW.archived AND NOT OLD.archived) THEN
    UPDATE core.table_options SET displayed_fields=array_remove(displayed_fields,OLD.id),
      field_order=array_remove(field_order,OLD.id), field_display=field_display-OLD.id::text,
      updated_at=clock_timestamp() WHERE table_id=OLD.table_id;
    UPDATE core.model_tables SET caption_field=CASE WHEN caption_field=OLD.name THEN '' ELSE caption_field END,
      display_version=display_version+1 WHERE id=OLD.table_id;
  END IF;
  RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION core.clean_link_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE core.table_options SET field_display=(SELECT coalesce(jsonb_object_agg(key,
    CASE WHEN value->>'link_id'=OLD.id::text THEN value-'link_id' ELSE value END),'{}'::jsonb)
    FROM jsonb_each(field_display)), updated_at=clock_timestamp() WHERE table_id=OLD.child_table_id;
  UPDATE core.model_tables SET display_version=display_version+1 WHERE id=OLD.child_table_id;
  RETURN NULL;
END $$;
