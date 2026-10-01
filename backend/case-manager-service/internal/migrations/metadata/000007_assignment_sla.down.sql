BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
DROP TRIGGER enforce_assignment_capacity ON case_manager.cases;
DROP FUNCTION case_manager.enforce_assignment_capacity();
DROP INDEX case_manager.cases_auto_assignment_idx;
DROP INDEX case_manager.cases_assignment_workload_idx;
ALTER TABLE case_manager.inbox_users DROP COLUMN capacity;
ALTER TABLE case_manager.inboxes DROP COLUMN sla_days;

COMMIT;
