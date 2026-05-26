CREATE TABLE IF NOT EXISTS core.workflow_rules (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0,
  fallthrough BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS workflow_rules_tenant_scenario_priority_idx
  ON core.workflow_rules (tenant_id, scenario_id, priority, created_at);

CREATE TABLE IF NOT EXISTS core.workflow_conditions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  rule_id UUID NOT NULL REFERENCES core.workflow_rules(id) ON DELETE CASCADE,
  function TEXT NOT NULL,
  params JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS workflow_conditions_rule_idx
  ON core.workflow_conditions (tenant_id, rule_id, created_at);

CREATE TABLE IF NOT EXISTS core.workflow_actions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  rule_id UUID NOT NULL REFERENCES core.workflow_rules(id) ON DELETE CASCADE,
  action_type TEXT NOT NULL,
  action_config JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS workflow_actions_rule_idx
  ON core.workflow_actions (tenant_id, rule_id, created_at);

ALTER TABLE core.workflow_executions
  DROP CONSTRAINT IF EXISTS workflow_executions_workflow_id_fkey;

ALTER TABLE core.workflow_executions
  ALTER COLUMN workflow_id DROP NOT NULL;

ALTER TABLE core.workflow_executions
  ADD COLUMN IF NOT EXISTS workflow_rule_id UUID NULL REFERENCES core.workflow_rules(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS workflow_action_id UUID NULL REFERENCES core.workflow_actions(id) ON DELETE CASCADE;
