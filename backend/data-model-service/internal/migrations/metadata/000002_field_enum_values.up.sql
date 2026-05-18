CREATE TABLE IF NOT EXISTS core.field_enum_values (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  value TEXT NOT NULL,
  label TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (field_id, value),
  UNIQUE (field_id, label)
);

CREATE INDEX IF NOT EXISTS field_enum_values_field_sort_idx
  ON core.field_enum_values (field_id, sort_order, created_at);
