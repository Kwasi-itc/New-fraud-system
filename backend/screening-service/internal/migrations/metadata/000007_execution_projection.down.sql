BEGIN;
SET LOCAL lock_timeout='5s';
LOCK TABLE screening.screenings IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM screening.execution_projection_queue) THEN RAISE EXCEPTION 'Drain execution projection queue before rollback'; END IF; END $$;
DROP TRIGGER screening_projection_queue ON screening.screenings;
DROP TRIGGER screening_projection_version ON screening.screenings;
DROP FUNCTION screening.queue_execution_projection();
DROP TABLE screening.execution_projection_queue;
-- Preserve monotonic versions across rollback/re-upgrade.
COMMIT;
