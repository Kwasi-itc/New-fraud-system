SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';
ALTER TABLE core.workflow_rules ADD COLUMN deleted_at timestamptz;
CREATE INDEX workflow_rules_active_order_idx ON core.workflow_rules(tenant_id,scenario_id,priority,created_at,id) WHERE deleted_at IS NULL;
