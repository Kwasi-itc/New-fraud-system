CREATE INDEX index_jobs_tenant_table_requested_idx
ON core.index_jobs (tenant_id, table_id, requested_at DESC, id DESC);
