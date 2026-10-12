-- Data-model-owned publication contract. Install before enabling SQL readers.
-- Migration runs transactionally; backfill holds metadata write locks until commit.
LOCK TABLE core.model_tables, core.model_fields, core.field_enum_values IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE core.ingestion_contracts (
    table_id UUID PRIMARY KEY REFERENCES core.model_tables(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
    table_name TEXT NOT NULL,
    -- Opaque tokens never repeat after a down/up or table recreation.
    version UUID NOT NULL DEFAULT gen_random_uuid(),
    payload JSONB NOT NULL,
    UNIQUE (tenant_id, table_name)
);

-- Serialize publication only for schema edits to the SAME table. Ingestion takes
-- no lock on this row. Each SPI query below gets a fresh READ COMMITTED snapshot
-- after acquiring the table metadata lock, so concurrent edits cannot lose fields.
CREATE FUNCTION core.publish_ingestion_contract(target UUID) RETURNS void
LANGUAGE plpgsql SET search_path = pg_catalog, core AS $$
DECLARE tab core.model_tables%ROWTYPE; document JSONB;
BEGIN
    SELECT * INTO tab FROM core.model_tables WHERE id = target FOR NO KEY UPDATE;
    IF NOT FOUND THEN RETURN; END IF;
    IF EXISTS (SELECT 1 FROM core.model_fields WHERE table_id = target AND tenant_id <> tab.tenant_id)
       OR EXISTS (SELECT 1 FROM core.field_enum_values e JOIN core.model_fields f ON f.id = e.field_id
                  WHERE f.table_id = target AND e.tenant_id <> tab.tenant_id) THEN
        RAISE EXCEPTION 'tenant-inconsistent ingestion schema for table %', target;
    END IF;
    SELECT jsonb_build_object(
        'id', tab.id, 'name', tab.name, 'archived', tab.archived,
        'fields', COALESCE(jsonb_object_agg(f.name, jsonb_build_object(
            'id', f.id, 'name', f.name, 'data_type', f.data_type,
            'nullable', f.nullable, 'is_enum', f.is_enum, 'is_unique', f.is_unique,
            'archived', f.archived,
            'enum_values', CASE WHEN f.is_enum THEN COALESCE((
                SELECT jsonb_agg(jsonb_build_object('id', e.id, 'value', e.value, 'label', e.label)
                                 ORDER BY e.sort_order, e.id)
                FROM core.field_enum_values e WHERE e.field_id = f.id
            ), '[]'::jsonb) ELSE '[]'::jsonb END
        )) FILTER (WHERE f.id IS NOT NULL), '{}'::jsonb)
    ) INTO document FROM core.model_fields f WHERE f.table_id = target;
    INSERT INTO core.ingestion_contracts (table_id, tenant_id, table_name, payload)
    VALUES (tab.id, tab.tenant_id, tab.name, document)
    ON CONFLICT (table_id) DO UPDATE
    SET tenant_id = EXCLUDED.tenant_id, table_name = EXCLUDED.table_name,
        payload = EXCLUDED.payload, version = gen_random_uuid()
    WHERE core.ingestion_contracts.payload IS DISTINCT FROM EXCLUDED.payload;
END $$;

CREATE FUNCTION core.refresh_ingestion_contract() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, core AS $$
DECLARE target UUID;
BEGIN
    IF TG_TABLE_NAME = 'model_tables' THEN
        IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
        IF TG_OP = 'UPDATE' AND (NEW.id <> OLD.id OR NEW.tenant_id <> OLD.tenant_id) THEN
            RAISE EXCEPTION 'cannot reparent a published table';
        END IF;
        target := NEW.id;
    ELSIF TG_TABLE_NAME = 'model_fields' THEN
        IF TG_OP = 'UPDATE' AND (NEW.id <> OLD.id OR NEW.table_id <> OLD.table_id OR NEW.tenant_id <> OLD.tenant_id) THEN
            RAISE EXCEPTION 'cannot reparent a published field';
        END IF;
        target := CASE WHEN TG_OP = 'DELETE' THEN OLD.table_id ELSE NEW.table_id END;
    ELSE
        IF TG_OP = 'UPDATE' AND (NEW.field_id <> OLD.field_id OR NEW.tenant_id <> OLD.tenant_id) THEN
            RAISE EXCEPTION 'cannot reparent a published enum value';
        END IF;
        SELECT table_id INTO target FROM core.model_fields
        WHERE id = CASE WHEN TG_OP = 'DELETE' THEN OLD.field_id ELSE NEW.field_id END;
    END IF;
    IF target IS NOT NULL THEN PERFORM core.publish_ingestion_contract(target); END IF;
    RETURN NULL;
END $$;

CREATE TRIGGER publish_table_contract AFTER INSERT OR UPDATE ON core.model_tables
FOR EACH ROW EXECUTE FUNCTION core.refresh_ingestion_contract();
CREATE TRIGGER publish_field_contract AFTER INSERT OR UPDATE OR DELETE ON core.model_fields
FOR EACH ROW EXECUTE FUNCTION core.refresh_ingestion_contract();
CREATE TRIGGER publish_enum_contract AFTER INSERT OR UPDATE OR DELETE ON core.field_enum_values
FOR EACH ROW EXECUTE FUNCTION core.refresh_ingestion_contract();

DO $$ DECLARE target UUID; BEGIN
    FOR target IN SELECT id FROM core.model_tables ORDER BY id LOOP
        PERFORM core.publish_ingestion_contract(target);
    END LOOP;
END $$;

-- Consumers receive SELECT on these views, never mutation rights on core tables.
CREATE VIEW core.ingestion_contracts_v1 AS
SELECT c.tenant_id, c.table_id, c.table_name, c.version, c.payload,
       t.status AS tenant_status, t.schema_name
FROM core.ingestion_contracts c JOIN core.tenants t ON t.id = c.tenant_id;
CREATE VIEW core.ingestion_tenants_v1 AS
SELECT id AS tenant_id, status AS tenant_status, schema_name FROM core.tenants;
REVOKE ALL ON FUNCTION core.publish_ingestion_contract(UUID) FROM PUBLIC;
REVOKE ALL ON FUNCTION core.refresh_ingestion_contract() FROM PUBLIC;
