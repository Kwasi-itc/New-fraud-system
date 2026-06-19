CREATE TABLE IF NOT EXISTS core.custom_lists (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL DEFAULT 'generic_text',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS custom_lists_tenant_name_idx
  ON core.custom_lists (tenant_id, lower(name));

ALTER TABLE core.custom_list_entries
  ADD COLUMN IF NOT EXISTS list_id UUID NULL;

CREATE INDEX IF NOT EXISTS custom_list_entries_tenant_list_id_idx
  ON core.custom_list_entries (tenant_id, list_id, created_at desc);
