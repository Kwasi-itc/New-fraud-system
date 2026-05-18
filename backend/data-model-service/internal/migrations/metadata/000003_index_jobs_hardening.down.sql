DROP INDEX IF EXISTS core.index_jobs_dedupe_key_idx;
DROP INDEX IF EXISTS core.index_jobs_tenant_requested_idx;
DROP INDEX IF EXISTS core.index_jobs_status_schedule_requested_idx;

ALTER TABLE core.index_jobs
  DROP COLUMN IF EXISTS dedupe_key,
  DROP COLUMN IF EXISTS scheduled_at,
  DROP COLUMN IF EXISTS started_at,
  DROP COLUMN IF EXISTS attempt_count,
  DROP COLUMN IF EXISTS error_message,
  DROP COLUMN IF EXISTS requested_by_operation,
  DROP COLUMN IF EXISTS table_id;
