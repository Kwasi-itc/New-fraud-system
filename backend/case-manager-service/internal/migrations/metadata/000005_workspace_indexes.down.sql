BEGIN;
SET LOCAL lock_timeout = '5s';
DROP INDEX case_manager.case_decisions_object_idx;
DROP INDEX case_manager.case_files_page_idx;
DROP INDEX case_manager.case_screenings_page_idx;
DROP INDEX case_manager.case_decisions_page_idx;
DROP INDEX case_manager.cases_workspace_queue_idx;
COMMIT;
