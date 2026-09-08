ALTER TABLE core_ingestion.idempotency_keys
  DROP CONSTRAINT IF EXISTS idempotency_keys_fact_metadata_check,
  DROP CONSTRAINT IF EXISTS idempotency_keys_fact_status_check,
  DROP COLUMN IF EXISTS fact_updated_at,
  DROP COLUMN IF EXISTS fact_status,
  DROP COLUMN IF EXISTS fact_manifest,
  DROP COLUMN IF EXISTS fact_marker;
