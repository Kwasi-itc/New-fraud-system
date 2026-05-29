CREATE SCHEMA IF NOT EXISTS screening;

CREATE TABLE IF NOT EXISTS screening.screenings (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  decision_id UUID NULL,
  scenario_id UUID NULL,
  screening_config_id UUID NULL,
  provider TEXT NOT NULL,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  status TEXT NOT NULL,
  request_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  response_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  provider_reference TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  is_manual BOOLEAN NOT NULL DEFAULT FALSE,
  is_archived BOOLEAN NOT NULL DEFAULT FALSE,
  partial BOOLEAN NOT NULL DEFAULT FALSE,
  unique_counterparty_identifier TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  sent_at TIMESTAMPTZ NULL,
  completed_at TIMESTAMPTZ NULL,
  failed_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS screenings_tenant_decision_idx
  ON screening.screenings (tenant_id, decision_id, created_at desc);

CREATE INDEX IF NOT EXISTS screenings_status_created_idx
  ON screening.screenings (status, created_at asc);

CREATE TABLE IF NOT EXISTS screening.screening_matches (
  id TEXT PRIMARY KEY,
  tenant_id UUID NOT NULL,
  screening_id UUID NOT NULL REFERENCES screening.screenings(id) ON DELETE CASCADE,
  entity_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  status TEXT NOT NULL,
  name TEXT NOT NULL,
  score DOUBLE PRECISION NOT NULL DEFAULT 0,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  matched_texts TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
  unique_counterparty_identifier TEXT NULL,
  enriched BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS screening_matches_tenant_screening_idx
  ON screening.screening_matches (tenant_id, screening_id, score desc);

CREATE TABLE IF NOT EXISTS screening.screening_match_comments (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  match_id TEXT NOT NULL REFERENCES screening.screening_matches(id) ON DELETE CASCADE,
  comment_text TEXT NOT NULL,
  author_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS screening_match_comments_tenant_match_idx
  ON screening.screening_match_comments (tenant_id, match_id, created_at asc);

CREATE TABLE IF NOT EXISTS screening.screening_whitelist_entries (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  entity_id TEXT NOT NULL,
  unique_counterparty_identifier TEXT NULL,
  reviewer_id TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS screening_whitelist_entries_lookup_idx
  ON screening.screening_whitelist_entries (tenant_id, entity_id, unique_counterparty_identifier, created_at desc);
