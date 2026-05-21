CREATE SCHEMA IF NOT EXISTS core_ingestion;

CREATE TABLE IF NOT EXISTS core_ingestion.ingestion_audit (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  object_type TEXT NOT NULL,
  object_id TEXT NOT NULL,
  mode TEXT NOT NULL,
  revision_id TEXT NOT NULL,
  status TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  validation_errors JSONB NOT NULL DEFAULT '[]'::jsonb,
  idempotency_key TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS ingestion_audit_tenant_object_type_created_idx
  ON core_ingestion.ingestion_audit (tenant_id, object_type, created_at DESC);

CREATE TABLE IF NOT EXISTS core_ingestion.outbox_events (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  event_type TEXT NOT NULL,
  aggregate_type TEXT NOT NULL,
  aggregate_key TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS outbox_events_status_created_idx
  ON core_ingestion.outbox_events (status, created_at);

CREATE TABLE IF NOT EXISTS core_ingestion.idempotency_keys (
  tenant_id UUID NOT NULL,
  key TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  response_kind TEXT NOT NULL,
  response_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, key)
);

CREATE TABLE IF NOT EXISTS core_ingestion.upload_logs (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  object_type TEXT NOT NULL,
  mode TEXT NOT NULL,
  filename TEXT NOT NULL,
  content_type TEXT NOT NULL,
  status TEXT NOT NULL,
  total_rows INTEGER NOT NULL DEFAULT 0,
  successful_rows INTEGER NOT NULL DEFAULT 0,
  failed_rows INTEGER NOT NULL DEFAULT 0,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NULL,
  payload BYTEA NULL,
  requested_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ NULL,
  completed_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS upload_logs_tenant_object_type_requested_idx
  ON core_ingestion.upload_logs (tenant_id, object_type, requested_at DESC);
