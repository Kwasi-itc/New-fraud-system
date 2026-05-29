DROP INDEX IF EXISTS screening.screenings_tenant_idempotency_key_idx;

ALTER TABLE screening.screenings
  DROP COLUMN IF EXISTS idempotency_key;
