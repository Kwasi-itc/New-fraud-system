DROP INDEX IF EXISTS scheduled_executions_recurring_unique_idx;
DROP INDEX IF EXISTS scheduled_executions_tenant_scenario_time_source_idx;

ALTER TABLE core.scheduled_executions
  DROP COLUMN IF EXISTS source;
