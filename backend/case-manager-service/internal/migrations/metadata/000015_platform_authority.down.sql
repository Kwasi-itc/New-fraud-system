BEGIN;
DROP FUNCTION IF EXISTS case_manager.authorize_case_access_v1(uuid,uuid,text,text,text,uuid,bigint,boolean,boolean,text);
DROP TABLE IF EXISTS case_manager.client_inbox_access;
DROP INDEX IF EXISTS case_manager.cases_client_inbox_page_idx;
COMMIT;

