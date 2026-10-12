BEGIN;
SET LOCAL lock_timeout = '5s';
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM screening.continuous_runs) OR EXISTS(SELECT 1 FROM screening.case_event_outbox WHERE kind='requires-review') OR EXISTS(SELECT 1 FROM screening.screenings WHERE processing_token IS NOT NULL) THEN
  RAISE EXCEPTION 'continuous intake history exists; refusing destructive downgrade';
 END IF;
END $$;
ALTER TABLE screening.case_event_outbox DROP CONSTRAINT case_event_outbox_kind_check;
ALTER TABLE screening.case_event_outbox ADD CONSTRAINT case_event_outbox_kind_check CHECK(kind IN ('reviewed','evidence-uploaded'));
DROP TABLE screening.continuous_runs;
DROP INDEX screening.screenings_recovery_idx;
ALTER TABLE screening.monitored_objects DROP CONSTRAINT monitored_tenant_config_id_unique;
ALTER TABLE screening.continuous_screening_configs DROP CONSTRAINT continuous_configs_tenant_id_unique;
ALTER TABLE screening.screenings DROP CONSTRAINT screenings_tenant_id_unique, DROP COLUMN processing_token, DROP COLUMN processing_until;
COMMIT;
