DROP TABLE IF EXISTS core.logical_bucket_definitions;
DROP TABLE IF EXISTS core.index_intents;

DROP INDEX IF EXISTS core.index_jobs_spec_hash_idx;

ALTER TABLE core.index_jobs
  DROP COLUMN IF EXISTS index_name,
  DROP COLUMN IF EXISTS spec_hash,
  DROP COLUMN IF EXISTS model_revision,
  DROP COLUMN IF EXISTS purpose,
  DROP COLUMN IF EXISTS submitted_by_service,
  DROP COLUMN IF EXISTS owner_service,
  DROP COLUMN IF EXISTS include_columns,
  DROP COLUMN IF EXISTS is_unique,
  DROP COLUMN IF EXISTS method;

CREATE UNIQUE INDEX IF NOT EXISTS index_jobs_dedupe_key_idx
  ON core.index_jobs (dedupe_key)
  WHERE dedupe_key <> '';
