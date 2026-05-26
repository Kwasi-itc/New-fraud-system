DROP INDEX IF EXISTS workflows_tenant_scenario_display_order_idx;

ALTER TABLE core.workflows
  DROP COLUMN IF EXISTS display_order;
