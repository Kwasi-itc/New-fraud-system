ALTER TABLE core.scheduled_executions
  ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual';
