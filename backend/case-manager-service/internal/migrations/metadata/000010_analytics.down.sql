BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DROP INDEX case_manager.events_analytics_case_idx;
DROP INDEX case_manager.cases_analytics_created_idx;
COMMIT;
