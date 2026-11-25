ALTER TABLE core.scenario_iterations
  ADD COLUMN IF NOT EXISTS evaluation_semantics TEXT NOT NULL DEFAULT 'v1';
