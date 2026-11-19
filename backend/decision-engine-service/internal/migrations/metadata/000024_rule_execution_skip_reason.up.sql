ALTER TABLE core.rule_executions
  ADD COLUMN IF NOT EXISTS skip_reason TEXT;

ALTER TABLE core.phantom_rule_executions
  ADD COLUMN IF NOT EXISTS skip_reason TEXT;
