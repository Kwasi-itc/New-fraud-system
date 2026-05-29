CREATE TABLE IF NOT EXISTS screening.screening_files (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  screening_id UUID NOT NULL REFERENCES screening.screenings(id) ON DELETE CASCADE,
  file_name TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT '',
  file_size BIGINT NOT NULL DEFAULT 0,
  storage_key TEXT NOT NULL DEFAULT '',
  uploaded_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS screening_files_screening_idx
  ON screening.screening_files (tenant_id, screening_id, created_at desc);

CREATE TABLE IF NOT EXISTS screening.continuous_screening_configs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  name TEXT NOT NULL,
  object_type TEXT NOT NULL,
  provider TEXT NOT NULL,
  field_map_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  review_inbox_id TEXT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS continuous_screening_configs_tenant_idx
  ON screening.continuous_screening_configs (tenant_id, created_at desc);

CREATE TABLE IF NOT EXISTS screening.monitored_objects (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  config_id UUID NOT NULL REFERENCES screening.continuous_screening_configs(id) ON DELETE CASCADE,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  status TEXT NOT NULL,
  attributes_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  last_screened_at TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS monitored_objects_tenant_config_object_idx
  ON screening.monitored_objects (tenant_id, config_id, object_id);

CREATE INDEX IF NOT EXISTS monitored_objects_config_idx
  ON screening.monitored_objects (tenant_id, config_id, created_at desc);

CREATE TABLE IF NOT EXISTS screening.dataset_update_jobs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  provider TEXT NOT NULL,
  job_type TEXT NOT NULL,
  status TEXT NOT NULL,
  cursor TEXT NOT NULL DEFAULT '',
  result_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  last_error TEXT NOT NULL DEFAULT '',
  attempt_count INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ NULL,
  completed_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS dataset_update_jobs_tenant_created_idx
  ON screening.dataset_update_jobs (tenant_id, created_at desc);

CREATE INDEX IF NOT EXISTS dataset_update_jobs_status_created_idx
  ON screening.dataset_update_jobs (status, created_at asc);
