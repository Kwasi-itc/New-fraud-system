CREATE TABLE IF NOT EXISTS core.aggregate_fact_definitions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  table_id UUID NOT NULL REFERENCES core.model_tables(id) ON DELETE CASCADE,
  table_name TEXT NOT NULL,
  signature TEXT NOT NULL,
  dimension_fields TEXT[] NOT NULL,
  event_time_field TEXT NOT NULL,
  measure_field TEXT NOT NULL DEFAULT '',
  needs_sum BOOLEAN NOT NULL DEFAULT FALSE,
  needs_count BOOLEAN NOT NULL DEFAULT FALSE,
  max_window_seconds BIGINT NOT NULL,
  minute_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  backfill_required BOOLEAN NOT NULL DEFAULT TRUE,
  coverage_token UUID NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE (tenant_id, signature)
);

CREATE TABLE IF NOT EXISTS core.aggregate_fact_bindings (
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  definition_id UUID NOT NULL REFERENCES core.aggregate_fact_definitions(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (scenario_id, iteration_id, definition_id)
);

CREATE INDEX IF NOT EXISTS aggregate_fact_bindings_definition_idx
  ON core.aggregate_fact_bindings (tenant_id, definition_id);

CREATE TABLE IF NOT EXISTS core.aggregate_fact_registry_versions (
  tenant_id UUID NOT NULL REFERENCES core.tenants(id) ON DELETE CASCADE,
  table_name TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, table_name)
);
