BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM case_manager.evidence_uploads) THEN
  RAISE EXCEPTION 'Evidence uploads contain data; migrate retained evidence before downgrade';
 END IF;
END $$;
DROP TABLE case_manager.evidence_uploads;

COMMIT;
