CREATE TABLE IF NOT EXISTS core_ingestion.deferred_ingests (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL,
  object_type TEXT NOT NULL,
  mode TEXT NOT NULL,
  status TEXT NOT NULL,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NULL,
  idempotency_key TEXT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  requested_at TIMESTAMPTZ NOT NULL,
  started_at TIMESTAMPTZ NULL,
  completed_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS deferred_ingests_status_requested_idx
  ON core_ingestion.deferred_ingests (status, requested_at);

CREATE INDEX IF NOT EXISTS deferred_ingests_tenant_object_requested_idx
  ON core_ingestion.deferred_ingests (tenant_id, object_type, requested_at DESC);
