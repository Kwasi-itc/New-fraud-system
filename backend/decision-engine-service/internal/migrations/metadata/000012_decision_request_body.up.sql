ALTER TABLE core.decisions
  ADD COLUMN IF NOT EXISTS request_body JSONB NOT NULL DEFAULT '{}'::jsonb;
