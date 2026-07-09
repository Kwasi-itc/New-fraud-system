ALTER TABLE core.async_decision_executions
  DROP COLUMN IF EXISTS attempt_count,
  DROP COLUMN IF EXISTS max_attempts,
  DROP COLUMN IF EXISTS next_attempt_at,
  DROP COLUMN IF EXISTS last_error,
  DROP COLUMN IF EXISTS failed_at;

ALTER TABLE core.scheduled_executions
  DROP COLUMN IF EXISTS attempt_count,
  DROP COLUMN IF EXISTS max_attempts,
  DROP COLUMN IF EXISTS next_attempt_at,
  DROP COLUMN IF EXISTS last_error,
  DROP COLUMN IF EXISTS failed_at;
