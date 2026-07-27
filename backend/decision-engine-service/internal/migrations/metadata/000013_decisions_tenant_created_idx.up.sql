CREATE INDEX CONCURRENTLY IF NOT EXISTS decisions_tenant_created_idx
  ON core.decisions (tenant_id, created_at DESC, id DESC);
