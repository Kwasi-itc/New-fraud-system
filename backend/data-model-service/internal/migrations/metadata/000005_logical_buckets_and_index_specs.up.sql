ALTER TABLE core.index_jobs
  ADD COLUMN IF NOT EXISTS method TEXT NOT NULL DEFAULT 'btree',
  ADD COLUMN IF NOT EXISTS is_unique BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS include_columns TEXT[] NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS owner_service TEXT NOT NULL DEFAULT 'legacy',
  ADD COLUMN IF NOT EXISTS submitted_by_service TEXT NOT NULL DEFAULT 'legacy',
  ADD COLUMN IF NOT EXISTS purpose TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS model_revision TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS spec_hash TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS index_name TEXT NOT NULL DEFAULT '';

UPDATE core.index_jobs
SET spec_hash = dedupe_key
WHERE spec_hash = '';

DROP INDEX IF EXISTS core.index_jobs_dedupe_key_idx;

CREATE UNIQUE INDEX IF NOT EXISTS index_jobs_spec_hash_idx
  ON core.index_jobs (spec_hash)
  WHERE spec_hash <> '';

CREATE TABLE IF NOT EXISTS core.index_intents (
  id UUID PRIMARY KEY,
  index_job_id UUID NOT NULL REFERENCES core.index_jobs(id) ON DELETE CASCADE,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  owner_service TEXT NOT NULL,
  submitted_by_service TEXT NOT NULL,
  purpose TEXT NOT NULL,
  model_revision TEXT NOT NULL DEFAULT '',
  active BOOLEAN NOT NULL DEFAULT TRUE,
  requested_at TIMESTAMPTZ NOT NULL,
  retired_at TIMESTAMPTZ NULL,
  UNIQUE (index_job_id, owner_service, purpose)
);

CREATE INDEX IF NOT EXISTS index_intents_tenant_active_idx
  ON core.index_intents (tenant_id, active, requested_at DESC);

CREATE TABLE IF NOT EXISTS core.logical_bucket_definitions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  timestamp_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE RESTRICT,
  timestamp_field_name TEXT NOT NULL,
  grain TEXT NOT NULL CHECK (grain = 'daily'),
  timezone TEXT NOT NULL,
  seal_delay_seconds BIGINT NOT NULL CHECK (seal_delay_seconds > 0),
  definition_version INTEGER NOT NULL CHECK (definition_version > 0),
  status TEXT NOT NULL CHECK (
    status IN ('pending_index', 'activating', 'active', 'blocked_data', 'retiring', 'retired')
  ),
  index_job_id UUID NULL REFERENCES core.index_jobs(id) ON DELETE SET NULL,
  cache_eligible_at TIMESTAMPTZ NULL,
  maintenance_until TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  retired_at TIMESTAMPTZ NULL,
  UNIQUE (id, definition_version)
);

CREATE UNIQUE INDEX IF NOT EXISTS logical_bucket_active_field_idx
  ON core.logical_bucket_definitions (table_id, timestamp_field_id)
  WHERE status IN ('pending_index', 'activating', 'active', 'blocked_data');

CREATE INDEX IF NOT EXISTS logical_bucket_tenant_status_idx
  ON core.logical_bucket_definitions (tenant_id, status, table_id, created_at);

CREATE INDEX IF NOT EXISTS logical_bucket_index_job_idx
  ON core.logical_bucket_definitions (index_job_id)
  WHERE index_job_id IS NOT NULL;
