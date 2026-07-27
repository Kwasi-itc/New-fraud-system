DO $$
BEGIN
  IF to_regclass('core.decisions') IS NOT NULL THEN
    EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_object_id_prefix_idx ON core.decisions (tenant_id, object_id text_pattern_ops)';
  END IF;
END
$$;
