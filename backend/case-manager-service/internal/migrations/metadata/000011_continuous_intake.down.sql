BEGIN;
SET LOCAL lock_timeout = '5s';
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM case_manager.screening_intake_routes) THEN
  RAISE EXCEPTION 'continuous intake history exists; retain schema and roll back application only';
 END IF;
END $$;
ALTER TABLE case_manager.case_screenings DROP COLUMN partial;
DROP TABLE case_manager.screening_intake_routes;
COMMIT;
