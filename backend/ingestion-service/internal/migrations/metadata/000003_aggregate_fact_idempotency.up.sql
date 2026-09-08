ALTER TABLE core_ingestion.idempotency_keys
  ADD COLUMN IF NOT EXISTS fact_marker TEXT NULL,
  ADD COLUMN IF NOT EXISTS fact_manifest TEXT NULL,
  ADD COLUMN IF NOT EXISTS fact_status TEXT NULL,
  ADD COLUMN IF NOT EXISTS fact_updated_at TIMESTAMPTZ NULL;

ALTER TABLE core_ingestion.idempotency_keys
  DROP CONSTRAINT IF EXISTS idempotency_keys_fact_status_check;

ALTER TABLE core_ingestion.idempotency_keys
  ADD CONSTRAINT idempotency_keys_fact_status_check
  CHECK (fact_status IS NULL OR fact_status IN ('pending', 'applied'));

ALTER TABLE core_ingestion.idempotency_keys
  DROP CONSTRAINT IF EXISTS idempotency_keys_fact_metadata_check;

ALTER TABLE core_ingestion.idempotency_keys
  ADD CONSTRAINT idempotency_keys_fact_metadata_check
  CHECK (
    (fact_status IS NULL AND fact_marker IS NULL AND fact_manifest IS NULL)
    OR
    (fact_status IS NOT NULL AND fact_marker IS NOT NULL AND fact_manifest IS NOT NULL)
  );
