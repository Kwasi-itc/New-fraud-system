BEGIN;
SET LOCAL lock_timeout = '5s';
ALTER TABLE case_manager.inbox_events DROP CONSTRAINT inbox_events_inbox_tenant_fk;
-- Audit records and source provenance are retained on downgrade and reused on
-- re-upgrade; never erase membership history to roll back application code.
DROP INDEX case_manager.cases_open_assignee_idx;
DROP INDEX case_manager.inbox_users_access_idx;
DROP INDEX case_manager.case_screenings_lookup_idx;
DROP INDEX case_manager.case_decisions_lookup_idx;
DROP INDEX case_manager.case_files_source_unique;
-- Preserve source_file_id values for re-upgrade and provenance.
ALTER TABLE case_manager.inbox_users DROP CONSTRAINT inbox_users_name_valid;
ALTER TABLE case_manager.tags DROP CONSTRAINT tags_tenant_fk;
ALTER TABLE case_manager.inboxes DROP CONSTRAINT inboxes_tenant_fk;
COMMIT;
