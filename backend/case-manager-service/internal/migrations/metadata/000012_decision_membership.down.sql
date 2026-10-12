BEGIN;
SET LOCAL lock_timeout='5s';
DROP INDEX case_manager.case_decisions_primary_unique;
COMMIT;
