ALTER TABLE core.async_decision_executions
  DROP COLUMN IF EXISTS completed_at,
  DROP COLUMN IF EXISTS callback_sent_at,
  DROP COLUMN IF EXISTS callback_last_error,
  DROP COLUMN IF EXISTS callback_attempt_count,
  DROP COLUMN IF EXISTS callback_status,
  DROP COLUMN IF EXISTS callback_url,
  DROP COLUMN IF EXISTS result_body;
