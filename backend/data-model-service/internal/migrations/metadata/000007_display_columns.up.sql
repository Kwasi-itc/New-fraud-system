ALTER TABLE core.table_options ADD COLUMN display_columns JSONB NOT NULL DEFAULT '[]'::jsonb
  CHECK (jsonb_typeof(display_columns) = 'array');

CREATE OR REPLACE FUNCTION core.validate_display_options_v1() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  col JSONB;
  f_id UUID;
  l_id UUID;
  tf_id UUID;
  col_type TEXT;
BEGIN
  IF jsonb_array_length(NEW.display_columns) > 0 THEN
    IF jsonb_array_length(NEW.display_columns) > 12 THEN
      RAISE EXCEPTION 'at most 12 displayed columns are allowed' USING ERRCODE='23514';
    END IF;

    FOR col IN SELECT * FROM jsonb_array_elements(NEW.display_columns) LOOP
      col_type := col->>'type';
      IF col_type = 'direct' THEN
        f_id := (col->>'field_id')::uuid;
        IF NOT EXISTS (
          SELECT 1 FROM core.model_fields f
          WHERE f.id = f_id AND f.table_id = NEW.table_id AND f.tenant_id = NEW.tenant_id AND NOT f.archived
        ) THEN
          RAISE EXCEPTION 'direct display column must be an active field of this table' USING ERRCODE='23514';
        END IF;
      ELSIF col_type = 'relation' THEN
        l_id := (col->>'link_id')::uuid;
        tf_id := (col->>'target_field_id')::uuid;
        IF NOT EXISTS (
          SELECT 1 FROM core.model_links l
          JOIN core.model_tables p ON p.id = l.parent_table_id AND p.tenant_id = NEW.tenant_id AND NOT p.archived
          JOIN core.model_fields pf ON pf.id = l.parent_field_id AND pf.table_id = p.id AND pf.tenant_id = NEW.tenant_id AND pf.is_unique AND NOT pf.archived
          JOIN core.model_fields cf ON cf.id = l.child_field_id AND cf.table_id = NEW.table_id AND cf.tenant_id = NEW.tenant_id AND NOT cf.archived AND cf.data_type = pf.data_type
          JOIN core.model_fields tf ON tf.id = tf_id AND tf.table_id = p.id AND tf.tenant_id = NEW.tenant_id AND NOT tf.archived
          WHERE l.id = l_id AND l.child_table_id = NEW.table_id AND l.tenant_id = NEW.tenant_id
        ) THEN
          RAISE EXCEPTION 'relation display column requires an active, unique relationship and active target field' USING ERRCODE='23514';
        END IF;
      ELSE
        RAISE EXCEPTION 'unknown display column type: %', col_type USING ERRCODE='23514';
      END IF;
    END LOOP;
  ELSE
    IF cardinality(NEW.displayed_fields)>12 OR
      cardinality(NEW.displayed_fields)<>(SELECT count(DISTINCT id) FROM unnest(NEW.displayed_fields) id) OR
      cardinality(NEW.field_order)<>(SELECT count(DISTINCT id) FROM unnest(NEW.field_order) id) OR
      EXISTS(SELECT 1 FROM unnest(NEW.displayed_fields||NEW.field_order) requested(id) WHERE NOT EXISTS
        (SELECT 1 FROM core.model_fields f WHERE f.id=requested.id AND f.table_id=NEW.table_id AND f.tenant_id=NEW.tenant_id AND NOT f.archived)) OR
      NOT EXISTS(SELECT 1 FROM core.model_tables t WHERE t.id=NEW.table_id AND t.tenant_id=NEW.tenant_id)
    THEN RAISE EXCEPTION 'display columns must be distinct active fields of this table, with at most 12 displayed fields' USING ERRCODE='23514'; END IF;
  END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION core.clean_field_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' OR (NEW.archived AND NOT OLD.archived) THEN
    UPDATE core.table_options SET displayed_fields=array_remove(displayed_fields,OLD.id),
      field_order=array_remove(field_order,OLD.id), field_display=field_display-OLD.id::text,
      display_columns=(
        SELECT coalesce(jsonb_agg(elem), '[]'::jsonb)
        FROM jsonb_array_elements(display_columns) elem
        WHERE NOT (elem->>'field_id' = OLD.id::text OR elem->>'target_field_id' = OLD.id::text)
      ),
      updated_at=clock_timestamp()
    WHERE tenant_id=OLD.tenant_id AND (
      table_id=OLD.table_id OR
      display_columns @> jsonb_build_array(jsonb_build_object('field_id', OLD.id::text)) OR
      display_columns @> jsonb_build_array(jsonb_build_object('target_field_id', OLD.id::text))
    );

    UPDATE core.model_tables SET caption_field=CASE WHEN caption_field=OLD.name THEN '' ELSE caption_field END,
      display_version=display_version+1 WHERE id=OLD.table_id;
  END IF;
  RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION core.clean_link_display_v1() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE core.table_options SET field_display=(SELECT coalesce(jsonb_object_agg(key,
    CASE WHEN value->>'link_id'=OLD.id::text THEN value-'link_id' ELSE value END),'{}'::jsonb)
    FROM jsonb_each(field_display)),
    display_columns=(
      SELECT coalesce(jsonb_agg(elem), '[]'::jsonb)
      FROM jsonb_array_elements(display_columns) elem
      WHERE elem->>'link_id' IS DISTINCT FROM OLD.id::text
    ),
    updated_at=clock_timestamp()
  WHERE tenant_id=OLD.tenant_id AND (
    table_id=OLD.child_table_id OR
    display_columns @> jsonb_build_array(jsonb_build_object('link_id', OLD.id::text))
  );

  UPDATE core.model_tables SET display_version=display_version+1 WHERE id=OLD.child_table_id;
  RETURN NULL;
END $$;
