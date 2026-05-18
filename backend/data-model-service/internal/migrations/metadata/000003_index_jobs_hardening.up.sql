ALTER TABLE core.index_jobs
  ADD COLUMN IF NOT EXISTS table_id UUID NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS requested_by_operation TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS error_message TEXT NULL,
  ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS dedupe_key TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS index_jobs_status_schedule_requested_idx
  ON core.index_jobs (status, scheduled_at, requested_at);

CREATE INDEX IF NOT EXISTS index_jobs_tenant_requested_idx
  ON core.index_jobs (tenant_id, requested_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS index_jobs_dedupe_key_idx
  ON core.index_jobs (dedupe_key)
  WHERE dedupe_key <> '';
