ALTER TABLE core.screening_executions
  ADD COLUMN IF NOT EXISTS response_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS provider_reference TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS failed_at TIMESTAMPTZ NULL;

UPDATE core.screening_executions
SET
  response_json = COALESCE(response_json, '{}'::jsonb),
  provider_reference = COALESCE(provider_reference, ''),
  last_error = COALESCE(last_error, ''),
  updated_at = COALESCE(updated_at, created_at);

ALTER TABLE core.scoring_requests
  ADD COLUMN IF NOT EXISTS response_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS provider_reference TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS failed_at TIMESTAMPTZ NULL;

UPDATE core.scoring_requests
SET
  response_json = COALESCE(response_json, '{}'::jsonb),
  provider_reference = COALESCE(provider_reference, ''),
  last_error = COALESCE(last_error, ''),
  updated_at = COALESCE(updated_at, created_at);
