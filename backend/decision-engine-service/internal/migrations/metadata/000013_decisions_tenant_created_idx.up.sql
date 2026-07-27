DO $$
BEGIN
  IF to_regclass('core.decisions') IS NOT NULL THEN
    EXECUTE 'CREATE INDEX IF NOT EXISTS decisions_tenant_created_idx ON core.decisions (tenant_id, created_at DESC, id DESC)';
  END IF;
END
$$;
