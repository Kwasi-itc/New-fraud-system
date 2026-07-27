DO $$
BEGIN
  IF to_regclass('core.decisions') IS NOT NULL THEN
    EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_outcome_created_idx ON core.decisions (tenant_id, outcome, created_at DESC, id DESC)';
    EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_object_lookup_created_idx ON core.decisions (tenant_id, object_type, object_id, created_at DESC, id DESC)';
  END IF;
END
$$;
