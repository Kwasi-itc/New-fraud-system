DROP INDEX IF EXISTS scheduled_executions_tenant_scenario_time_source_idx;

CREATE UNIQUE INDEX IF NOT EXISTS scheduled_executions_recurring_unique_idx
  ON core.scheduled_executions (tenant_id, scenario_id, scheduled_for, source)
  WHERE source = 'recurring';
