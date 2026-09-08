ALTER TABLE core.model_fields
  DROP CONSTRAINT IF EXISTS model_fields_classification_source_check,
  DROP CONSTRAINT IF EXISTS model_fields_distribution_category_check,
  DROP COLUMN IF EXISTS classified_at,
  DROP COLUMN IF EXISTS classification_evidence,
  DROP COLUMN IF EXISTS classification_policy_version,
  DROP COLUMN IF EXISTS classification_source,
  DROP COLUMN IF EXISTS distribution_category;
