BEGIN;
SET LOCAL lock_timeout='5s';
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM case_manager.suspicious_activity_reports WHERE created_by<>'legacy_import' OR payload->>'format'='internal_sar_v1' OR version>1) THEN
  RAISE EXCEPTION 'Preserve report provenance before downgrading report lifecycle';
 END IF;
END $$;
DROP INDEX case_manager.reports_case_page_idx;
ALTER TABLE case_manager.suspicious_activity_reports DROP CONSTRAINT report_status_check,
 DROP COLUMN version,DROP COLUMN created_by,DROP COLUMN completed_by,DROP COLUMN completed_at;
COMMIT;
