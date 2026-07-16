ALTER TABLE core.scheduled_executions
  ADD COLUMN IF NOT EXISTS idempotency_key TEXT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS scheduled_executions_tenant_idempotency_idx
  ON core.scheduled_executions (tenant_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

ALTER TABLE core.async_decision_executions
  ADD COLUMN IF NOT EXISTS idempotency_key TEXT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS async_decision_executions_tenant_idempotency_idx
  ON core.async_decision_executions (tenant_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';
