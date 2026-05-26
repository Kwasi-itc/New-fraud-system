CREATE SCHEMA IF NOT EXISTS core;

CREATE TABLE IF NOT EXISTS core.scenarios (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  name TEXT NOT NULL,
  trigger_object_type TEXT NOT NULL,
  live_iteration_id UUID NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS scenarios_tenant_name_idx
  ON core.scenarios (tenant_id, name);

CREATE TABLE IF NOT EXISTS core.scenario_iterations (
  id UUID PRIMARY KEY,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  tenant_id UUID NOT NULL,
  version INTEGER NOT NULL,
  status TEXT NOT NULL,
  trigger_formula JSONB NOT NULL DEFAULT '{}'::jsonb,
  score_review_threshold INTEGER NULL,
  score_block_and_review_threshold INTEGER NULL,
  score_decline_threshold INTEGER NULL,
  schedule TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  committed_at TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS scenario_iterations_scenario_version_idx
  ON core.scenario_iterations (scenario_id, version);

CREATE INDEX IF NOT EXISTS scenario_iterations_tenant_scenario_idx
  ON core.scenario_iterations (tenant_id, scenario_id);

ALTER TABLE core.scenarios
  ADD CONSTRAINT scenarios_live_iteration_fk
  FOREIGN KEY (live_iteration_id) REFERENCES core.scenario_iterations(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS core.scenario_publications (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  action TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS scenario_publications_tenant_scenario_idx
  ON core.scenario_publications (tenant_id, scenario_id, created_at desc);

CREATE TABLE IF NOT EXISTS core.rules (
  id UUID PRIMARY KEY,
  iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  tenant_id UUID NOT NULL,
  display_order INTEGER NOT NULL DEFAULT 0,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  formula JSONB NOT NULL DEFAULT '{}'::jsonb,
  score_modifier INTEGER NOT NULL DEFAULT 0,
  rule_group TEXT NOT NULL DEFAULT '',
  snooze_group_id UUID NULL,
  stable_rule_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS rules_iteration_idx
  ON core.rules (iteration_id, display_order, created_at);

CREATE TABLE IF NOT EXISTS core.decisions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  scenario_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  object_id TEXT NOT NULL,
  object_type TEXT NOT NULL,
  outcome TEXT NOT NULL,
  score INTEGER NOT NULL DEFAULT 0,
  triggered BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS decisions_tenant_scenario_idx
  ON core.decisions (tenant_id, scenario_id, created_at desc);

CREATE TABLE IF NOT EXISTS core.rule_executions (
  id UUID PRIMARY KEY,
  decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,
  rule_id UUID NOT NULL REFERENCES core.rules(id) ON DELETE CASCADE,
  rule_name TEXT NOT NULL,
  outcome TEXT NOT NULL,
  score_modifier INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS rule_executions_decision_idx
  ON core.rule_executions (decision_id, created_at);

CREATE TABLE IF NOT EXISTS core.test_runs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  live_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  phantom_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS test_runs_tenant_scenario_idx
  ON core.test_runs (tenant_id, scenario_id, created_at desc);

CREATE TABLE IF NOT EXISTS core.phantom_decisions (
  id UUID PRIMARY KEY,
  test_run_id UUID NOT NULL REFERENCES core.test_runs(id) ON DELETE CASCADE,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  scenario_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  object_id TEXT NOT NULL,
  object_type TEXT NOT NULL,
  outcome TEXT NOT NULL,
  score INTEGER NOT NULL DEFAULT 0,
  triggered BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS phantom_decisions_test_run_idx
  ON core.phantom_decisions (test_run_id, created_at desc);

CREATE TABLE IF NOT EXISTS core.phantom_rule_executions (
  id UUID PRIMARY KEY,
  phantom_decision_id UUID NOT NULL REFERENCES core.phantom_decisions(id) ON DELETE CASCADE,
  rule_id UUID NOT NULL REFERENCES core.rules(id) ON DELETE CASCADE,
  rule_name TEXT NOT NULL,
  outcome TEXT NOT NULL,
  score_modifier INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS phantom_rule_executions_decision_idx
  ON core.phantom_rule_executions (phantom_decision_id, created_at);

CREATE TABLE IF NOT EXISTS core.workflows (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  allowed_outcomes TEXT[] NOT NULL,
  action_type TEXT NOT NULL,
  action_config JSONB NOT NULL DEFAULT '{}'::jsonb,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS workflows_tenant_scenario_idx
  ON core.workflows (tenant_id, scenario_id, created_at desc);

CREATE TABLE IF NOT EXISTS core.workflow_executions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  workflow_id UUID NOT NULL REFERENCES core.workflows(id) ON DELETE CASCADE,
  decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  action_type TEXT NOT NULL,
  status TEXT NOT NULL,
  action_config JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS workflow_executions_tenant_decision_idx
  ON core.workflow_executions (tenant_id, decision_id, created_at);

CREATE TABLE IF NOT EXISTS core.rule_snoozes (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  snooze_group_id UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS rule_snoozes_active_lookup_idx
  ON core.rule_snoozes (tenant_id, scenario_id, object_type, object_id, expires_at desc);

CREATE TABLE IF NOT EXISTS core.outbox_events (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS outbox_events_tenant_created_idx
  ON core.outbox_events (tenant_id, created_at desc);

CREATE TABLE IF NOT EXISTS core.scheduled_executions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  scenario_iteration_id UUID NOT NULL REFERENCES core.scenario_iterations(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  scheduled_for TIMESTAMPTZ NOT NULL,
  request_body JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS scheduled_executions_tenant_scenario_idx
  ON core.scheduled_executions (tenant_id, scenario_id, created_at desc);

CREATE TABLE IF NOT EXISTS core.async_decision_executions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NULL REFERENCES core.scenarios(id) ON DELETE SET NULL,
  object_type TEXT NOT NULL,
  status TEXT NOT NULL,
  request_body JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS async_decision_executions_tenant_created_idx
  ON core.async_decision_executions (tenant_id, created_at desc);

CREATE TABLE IF NOT EXISTS core.screening_configs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  allowed_outcomes TEXT[] NOT NULL,
  provider TEXT NOT NULL,
  config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS screening_configs_tenant_scenario_idx
  ON core.screening_configs (tenant_id, scenario_id, created_at desc);

CREATE TABLE IF NOT EXISTS core.screening_executions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  config_id UUID NOT NULL REFERENCES core.screening_configs(id) ON DELETE CASCADE,
  decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  request_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS screening_executions_tenant_decision_idx
  ON core.screening_executions (tenant_id, decision_id, created_at);

CREATE TABLE IF NOT EXISTS core.scoring_configs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  allowed_outcomes TEXT[] NOT NULL,
  ruleset_ref TEXT NOT NULL,
  config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS scoring_configs_tenant_scenario_idx
  ON core.scoring_configs (tenant_id, scenario_id, created_at desc);

CREATE TABLE IF NOT EXISTS core.scoring_requests (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  config_id UUID NOT NULL REFERENCES core.scoring_configs(id) ON DELETE CASCADE,
  decision_id UUID NOT NULL REFERENCES core.decisions(id) ON DELETE CASCADE,
  scenario_id UUID NOT NULL REFERENCES core.scenarios(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  request_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS scoring_requests_tenant_decision_idx
  ON core.scoring_requests (tenant_id, decision_id, created_at);

CREATE TABLE IF NOT EXISTS core.custom_list_entries (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  list_name TEXT NOT NULL,
  value TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS custom_list_entries_tenant_list_idx
  ON core.custom_list_entries (tenant_id, list_name, value);

CREATE TABLE IF NOT EXISTS core.record_tags (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS record_tags_tenant_object_idx
  ON core.record_tags (tenant_id, object_type, object_id, tag);

CREATE TABLE IF NOT EXISTS core.risk_snapshots (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  risk_level TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS risk_snapshots_tenant_object_idx
  ON core.risk_snapshots (tenant_id, object_type, object_id, created_at desc);

CREATE TABLE IF NOT EXISTS core.ip_flags (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  ip_address TEXT NOT NULL,
  flag TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS ip_flags_tenant_ip_idx
  ON core.ip_flags (tenant_id, ip_address, flag);
