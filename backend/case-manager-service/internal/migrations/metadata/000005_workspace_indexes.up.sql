BEGIN;
SET LOCAL lock_timeout = '5s';
CREATE INDEX cases_workspace_queue_idx ON case_manager.cases (tenant_id,(boost_reason IS NOT NULL) DESC,created_at DESC,id DESC);
CREATE INDEX case_decisions_page_idx ON case_manager.case_decisions (tenant_id,case_id,created_at DESC,id DESC);
CREATE INDEX case_screenings_page_idx ON case_manager.case_screenings (tenant_id,case_id,created_at DESC,id DESC);
CREATE INDEX case_files_page_idx ON case_manager.case_files (tenant_id,case_id,created_at DESC,id DESC);
CREATE INDEX case_decisions_object_idx ON case_manager.case_decisions (tenant_id,object_type,object_id,case_id);
COMMIT;
