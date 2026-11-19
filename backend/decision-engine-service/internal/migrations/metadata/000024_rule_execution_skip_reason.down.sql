ALTER TABLE core.phantom_rule_executions
  DROP COLUMN IF EXISTS skip_reason;

ALTER TABLE core.rule_executions
  DROP COLUMN IF EXISTS skip_reason;
