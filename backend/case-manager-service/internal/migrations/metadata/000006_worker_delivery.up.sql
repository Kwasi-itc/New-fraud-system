BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
ALTER TABLE case_manager.outbox_events
  ADD COLUMN attempts integer NOT NULL DEFAULT 0,
  ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN lease_token uuid,
  ADD COLUMN lease_until timestamptz,
  ADD COLUMN delivered_at timestamptz,
  ADD COLUMN last_error text;
CREATE INDEX outbox_ready_idx ON case_manager.outbox_events(next_attempt_at,created_at,id) WHERE status IN ('pending','delivering');
CREATE INDEX outbox_tenant_status_idx ON case_manager.outbox_events(tenant_id,status,created_at,id);
CREATE INDEX cases_snooze_expiry_idx ON case_manager.cases(snoozed_until,id) WHERE snoozed_until IS NOT NULL AND status <> 'closed';

COMMIT;
