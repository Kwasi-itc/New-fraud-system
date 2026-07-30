CREATE TABLE IF NOT EXISTS core_ingestion.logical_bucket_generations (
  tenant_id UUID NOT NULL,
  table_id UUID NOT NULL,
  bucket_definition_id UUID NOT NULL,
  definition_version INTEGER NOT NULL,
  bucket_start_utc TIMESTAMPTZ NOT NULL,
  generation BIGINT NOT NULL DEFAULT 0 CHECK (generation >= 0),
  last_changed_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (
    tenant_id,
    table_id,
    bucket_definition_id,
    definition_version,
    bucket_start_utc
  )
);

CREATE INDEX IF NOT EXISTS logical_bucket_generations_changed_idx
  ON core_ingestion.logical_bucket_generations (
    last_changed_at,
    tenant_id,
    table_id,
    bucket_definition_id,
    bucket_start_utc
  );
