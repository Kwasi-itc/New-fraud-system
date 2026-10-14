ALTER TABLE core.table_options ADD COLUMN field_display JSONB NOT NULL DEFAULT '{}'::jsonb
  CHECK (jsonb_typeof(field_display) = 'object');
ALTER TABLE core.model_tables ADD COLUMN display_version BIGINT NOT NULL DEFAULT 0;

CREATE FUNCTION core.validate_display_options_v1() RETURNS trigger LANGUAGE plpgsql AS $$
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
CREATE TRIGGER validate_display_options BEFORE INSERT OR UPDATE ON core.table_options
  FOR EACH ROW EXECUTE FUNCTION core.validate_display_options_v1();
CREATE FUNCTION core.touch_display_options_v1() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE core.model_tables SET display_version=display_version+1 WHERE id=NEW.table_id;
  RETURN NULL;
END $$;
CREATE TRIGGER touch_display_options AFTER INSERT OR UPDATE ON core.table_options
  FOR EACH ROW EXECUTE FUNCTION core.touch_display_options_v1();
CREATE FUNCTION core.touch_display_caption_v1() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.caption_field IS DISTINCT FROM OLD.caption_field OR NEW.archived IS DISTINCT FROM OLD.archived THEN
    NEW.display_version=OLD.display_version+1;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER touch_display_caption BEFORE UPDATE OF caption_field,archived ON core.model_tables
  FOR EACH ROW EXECUTE FUNCTION core.touch_display_caption_v1();

-- Remove stale presentation references as part of the owning schema mutation.
CREATE FUNCTION core.clean_field_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$
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
CREATE TRIGGER clean_field_display AFTER DELETE OR UPDATE OF archived ON core.model_fields
  FOR EACH ROW EXECUTE FUNCTION core.clean_field_display_v1();

CREATE FUNCTION core.clean_link_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE core.table_options SET field_display=(SELECT coalesce(jsonb_object_agg(key,
    CASE WHEN value->>'link_id'=OLD.id::text THEN value-'link_id' ELSE value END),'{}'::jsonb)
    FROM jsonb_each(field_display)), updated_at=clock_timestamp() WHERE table_id=OLD.child_table_id;
  UPDATE core.model_tables SET display_version=display_version+1 WHERE id=OLD.child_table_id;
  RETURN NULL;
END $$;
CREATE TRIGGER clean_link_display AFTER DELETE ON core.model_links
  FOR EACH ROW EXECUTE FUNCTION core.clean_link_display_v1();
