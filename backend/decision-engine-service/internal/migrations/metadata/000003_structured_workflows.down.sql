ALTER TABLE core.workflow_executions
  DROP COLUMN IF EXISTS workflow_action_id,
  DROP COLUMN IF EXISTS workflow_rule_id;

DROP INDEX IF EXISTS workflow_actions_rule_idx;
DROP TABLE IF EXISTS core.workflow_actions;

DROP INDEX IF EXISTS workflow_conditions_rule_idx;
DROP TABLE IF EXISTS core.workflow_conditions;

DROP INDEX IF EXISTS workflow_rules_tenant_scenario_priority_idx;
DROP TABLE IF EXISTS core.workflow_rules;
