ALTER TABLE core.model_fields
  ADD COLUMN IF NOT EXISTS distribution_category TEXT NOT NULL DEFAULT 'unknown',
  ADD COLUMN IF NOT EXISTS classification_source TEXT NOT NULL DEFAULT 'default',
  ADD COLUMN IF NOT EXISTS classification_policy_version TEXT NOT NULL DEFAULT 'distribution-v1',
  ADD COLUMN IF NOT EXISTS classification_evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS classified_at TIMESTAMPTZ NULL;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'model_fields_distribution_category_check'
      AND conrelid = 'core.model_fields'::regclass
  ) THEN
    ALTER TABLE core.model_fields
      ADD CONSTRAINT model_fields_distribution_category_check
      CHECK (distribution_category IN (
        'unknown',
        'few_value_dominated',
        'highly_distributed',
        'unique_or_near_unique'
      ));
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'model_fields_classification_source_check'
      AND conrelid = 'core.model_fields'::regclass
  ) THEN
    ALTER TABLE core.model_fields
      ADD CONSTRAINT model_fields_classification_source_check
      CHECK (classification_source IN ('default', 'manual', 'sample'));
  END IF;
END $$;
