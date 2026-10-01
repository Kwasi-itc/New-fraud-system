BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
DO $$ BEGIN
  IF EXISTS(SELECT 1 FROM case_manager.outbox_events WHERE status='delivering') THEN
    RAISE EXCEPTION 'Stop workers and recover in-flight outbox deliveries before downgrade';
  END IF;
END $$;
DROP INDEX case_manager.cases_snooze_expiry_idx;
DROP INDEX case_manager.outbox_ready_idx;
DROP INDEX case_manager.outbox_tenant_status_idx;
ALTER TABLE case_manager.outbox_events DROP COLUMN attempts, DROP COLUMN next_attempt_at, DROP COLUMN lease_token, DROP COLUMN lease_until, DROP COLUMN delivered_at, DROP COLUMN last_error;

COMMIT;
