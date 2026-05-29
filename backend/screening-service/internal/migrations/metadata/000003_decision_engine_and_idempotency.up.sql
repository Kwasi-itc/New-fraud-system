ALTER TABLE screening.screenings
  ADD COLUMN IF NOT EXISTS idempotency_key TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS screenings_tenant_idempotency_key_idx
  ON screening.screenings (tenant_id, idempotency_key)
  WHERE idempotency_key <> '';
