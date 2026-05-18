CREATE TABLE IF NOT EXISTS core.navigation_options (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  source_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  source_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  target_table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  filter_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  ordering_field_id UUID NOT NULL REFERENCES core.model_fields(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS navigation_options_tenant_idx
  ON core.navigation_options (tenant_id, source_table_id, created_at);
