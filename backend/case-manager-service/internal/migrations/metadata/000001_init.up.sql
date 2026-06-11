CREATE SCHEMA IF NOT EXISTS case_manager;

CREATE TABLE IF NOT EXISTS case_manager.inboxes (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  escalation_inbox_id UUID NULL,
  auto_assign_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  case_review_manual BOOLEAN NOT NULL DEFAULT FALSE,
  case_review_on_case_created BOOLEAN NOT NULL DEFAULT FALSE,
  case_review_on_escalate BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS inboxes_tenant_name_active_idx
  ON case_manager.inboxes (tenant_id, lower(name))
  WHERE status <> 'archived';

CREATE TABLE IF NOT EXISTS case_manager.inbox_users (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  inbox_id UUID NOT NULL REFERENCES case_manager.inboxes(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,
  auto_assign_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS inbox_users_tenant_inbox_user_idx
  ON case_manager.inbox_users (tenant_id, inbox_id, user_id);

CREATE TABLE IF NOT EXISTS case_manager.cases (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  inbox_id UUID NOT NULL REFERENCES case_manager.inboxes(id),
  name TEXT NOT NULL,
  status TEXT NOT NULL,
  outcome TEXT NOT NULL,
  type TEXT NOT NULL,
  assigned_to TEXT NULL,
  snoozed_until TIMESTAMPTZ NULL,
  boost_reason TEXT NULL,
  review_level TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  CONSTRAINT cases_status_check CHECK (status IN ('pending', 'investigating', 'closed')),
  CONSTRAINT cases_outcome_check CHECK (outcome IN ('unset', 'false_positive', 'valuable_alert', 'confirmed_risk')),
  CONSTRAINT cases_type_check CHECK (type IN ('decision', 'continuous_screening')),
  CONSTRAINT cases_review_level_check CHECK (review_level IS NULL OR review_level IN ('probable_false_positive', 'investigate', 'escalate'))
);

CREATE INDEX IF NOT EXISTS cases_tenant_inbox_idx
  ON case_manager.cases (tenant_id, inbox_id, (boost_reason IS NULL), (assigned_to IS NOT NULL), created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS cases_tenant_status_idx
  ON case_manager.cases (tenant_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS case_manager.case_decisions (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  decision_id UUID NOT NULL,
  scenario_id UUID NULL,
  object_type TEXT NOT NULL DEFAULT '',
  object_id TEXT NOT NULL DEFAULT '',
  pivot_value TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS case_decisions_case_decision_idx
  ON case_manager.case_decisions (tenant_id, case_id, decision_id);

CREATE INDEX IF NOT EXISTS case_decisions_pivot_idx
  ON case_manager.case_decisions (tenant_id, pivot_value, created_at DESC)
  WHERE pivot_value IS NOT NULL;

CREATE TABLE IF NOT EXISTS case_manager.case_screenings (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  screening_id UUID NOT NULL,
  match_id TEXT NULL,
  status TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS case_screenings_case_screening_idx
  ON case_manager.case_screenings (tenant_id, case_id, screening_id);

CREATE TABLE IF NOT EXISTS case_manager.tags (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  target TEXT NOT NULL DEFAULT 'case',
  name TEXT NOT NULL,
  color TEXT NOT NULL,
  deleted_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS tags_tenant_name_target_idx
  ON case_manager.tags (tenant_id, lower(name), target)
  WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS case_manager.case_tags (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  tag_id UUID NOT NULL REFERENCES case_manager.tags(id) ON DELETE CASCADE,
  deleted_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS case_tags_unique_active_idx
  ON case_manager.case_tags (tenant_id, case_id, tag_id)
  WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS case_manager.case_files (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  file_name TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT '',
  file_size BIGINT NOT NULL DEFAULT 0,
  storage_key TEXT NOT NULL DEFAULT '',
  uploaded_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS case_manager.case_contributors (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS case_contributors_unique_idx
  ON case_manager.case_contributors (tenant_id, case_id, user_id);

CREATE TABLE IF NOT EXISTS case_manager.case_events (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  user_id TEXT NULL,
  event_type TEXT NOT NULL,
  additional_note TEXT NOT NULL DEFAULT '',
  resource_id TEXT NOT NULL DEFAULT '',
  resource_type TEXT NOT NULL DEFAULT '',
  new_value TEXT NOT NULL DEFAULT '',
  previous_value TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS case_events_case_idx
  ON case_manager.case_events (tenant_id, case_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS case_events_kinds_idx
  ON case_manager.case_events (tenant_id, event_type, created_at DESC);

CREATE TABLE IF NOT EXISTS case_manager.ai_case_reviews (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  file_temp_reference TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ NULL
);

CREATE TABLE IF NOT EXISTS case_manager.suspicious_activity_reports (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  case_id UUID NOT NULL REFERENCES case_manager.cases(id) ON DELETE CASCADE,
  status TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS case_manager.outbox_events (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS outbox_events_status_created_idx
  ON case_manager.outbox_events (status, created_at);
