ALTER TABLE core.index_jobs
  ADD COLUMN IF NOT EXISTS include_columns TEXT[] NOT NULL DEFAULT '{}';
