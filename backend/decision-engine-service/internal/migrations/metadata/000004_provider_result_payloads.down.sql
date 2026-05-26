ALTER TABLE core.screening_executions
  DROP COLUMN IF EXISTS response_json,
  DROP COLUMN IF EXISTS provider_reference,
  DROP COLUMN IF EXISTS last_error,
  DROP COLUMN IF EXISTS updated_at,
  DROP COLUMN IF EXISTS sent_at,
  DROP COLUMN IF EXISTS completed_at,
  DROP COLUMN IF EXISTS failed_at;

ALTER TABLE core.scoring_requests
  DROP COLUMN IF EXISTS response_json,
  DROP COLUMN IF EXISTS provider_reference,
  DROP COLUMN IF EXISTS last_error,
  DROP COLUMN IF EXISTS updated_at,
  DROP COLUMN IF EXISTS sent_at,
  DROP COLUMN IF EXISTS completed_at,
  DROP COLUMN IF EXISTS failed_at;
