CREATE INDEX CONCURRENTLY IF NOT EXISTS decisions_tenant_object_id_prefix_idx
  ON core.decisions (tenant_id, object_id text_pattern_ops);
