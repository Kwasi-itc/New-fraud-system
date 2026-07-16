DROP INDEX IF EXISTS async_decision_executions_tenant_idempotency_idx;
ALTER TABLE core.async_decision_executions
  DROP COLUMN IF EXISTS idempotency_key;

DROP INDEX IF EXISTS scheduled_executions_tenant_idempotency_idx;
ALTER TABLE core.scheduled_executions
  DROP COLUMN IF EXISTS idempotency_key;
