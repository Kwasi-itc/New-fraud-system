CREATE SCHEMA IF NOT EXISTS core;

CREATE TABLE IF NOT EXISTS core.tenants (
  id UUID PRIMARY KEY,
  external_key TEXT UNIQUE,
  name TEXT NOT NULL,
  schema_name TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS core.model_tables (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  alias TEXT NOT NULL DEFAULT '',
  semantic_type TEXT NOT NULL DEFAULT '',
  caption_field TEXT NOT NULL DEFAULT '',
  archived BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS core.model_fields (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  data_type TEXT NOT NULL,
  nullable BOOLEAN NOT NULL DEFAULT FALSE,
  is_enum BOOLEAN NOT NULL DEFAULT FALSE,
  is_unique BOOLEAN NOT NULL DEFAULT FALSE,
  archived BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (table_id, name)
);

CREATE TABLE IF NOT EXISTS core.model_links (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  parent_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  parent_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  child_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  child_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS model_links_tenant_child_name_idx
  ON core.model_links (tenant_id, child_table_id, name);

CREATE TABLE IF NOT EXISTS core.model_pivots (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  base_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  field_id UUID NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  path_link_ids UUID[] NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS core.table_options (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  displayed_fields UUID[] NOT NULL DEFAULT '{}',
  field_order UUID[] NOT NULL DEFAULT '{}',
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (table_id)
);

CREATE TABLE IF NOT EXISTS core.schema_change_log (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  operation TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id UUID NOT NULL,
  status TEXT NOT NULL,
  details JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS core.tenant_schema_migrations (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  version TEXT NOT NULL,
  applied_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS core.index_jobs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  table_name TEXT NOT NULL,
  index_type TEXT NOT NULL,
  columns TEXT[] NOT NULL,
  status TEXT NOT NULL,
  requested_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ NULL
);

