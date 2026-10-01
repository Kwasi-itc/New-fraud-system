BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM case_manager.suspicious_activity_reports WHERE status NOT IN ('draft','completed')) THEN
  RAISE EXCEPTION 'Repair unsupported report states before applying report lifecycle migration';
 END IF;
END $$;
ALTER TABLE case_manager.suspicious_activity_reports
 ADD COLUMN version integer NOT NULL DEFAULT 1 CHECK(version>0),
 ADD COLUMN created_by text NOT NULL DEFAULT 'legacy_import',
 ADD COLUMN completed_by text,
 ADD COLUMN completed_at timestamptz,
 ADD CONSTRAINT report_status_check CHECK(status IN ('draft','completed'));
CREATE INDEX reports_case_page_idx ON case_manager.suspicious_activity_reports(tenant_id,case_id,created_at DESC,id DESC);
COMMIT;
