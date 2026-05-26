ALTER TABLE core.workflows
  ADD COLUMN IF NOT EXISTS display_order INTEGER NOT NULL DEFAULT 0;

UPDATE core.workflows AS wf
SET display_order = ordered.position - 1
FROM (
  SELECT id, row_number() OVER (PARTITION BY tenant_id, scenario_id ORDER BY created_at, id) AS position
  FROM core.workflows
) AS ordered
WHERE wf.id = ordered.id;

CREATE INDEX IF NOT EXISTS workflows_tenant_scenario_display_order_idx
  ON core.workflows (tenant_id, scenario_id, display_order, created_at);
