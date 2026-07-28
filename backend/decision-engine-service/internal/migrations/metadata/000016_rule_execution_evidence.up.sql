ALTER TABLE core.rule_executions
  ADD COLUMN IF NOT EXISTS evaluation JSONB;
