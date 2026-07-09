ALTER TABLE core.scheduled_executions
  ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 3,
  ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS failed_at TIMESTAMPTZ NULL;

UPDATE core.scheduled_executions
SET
  attempt_count = COALESCE(attempt_count, 0),
  max_attempts = COALESCE(NULLIF(max_attempts, 0), 3),
  last_error = COALESCE(last_error, '');

ALTER TABLE core.async_decision_executions
  ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 3,
  ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS failed_at TIMESTAMPTZ NULL;

UPDATE core.async_decision_executions
SET
  attempt_count = COALESCE(attempt_count, 0),
  max_attempts = COALESCE(NULLIF(max_attempts, 0), 3),
  last_error = COALESCE(last_error, '');
