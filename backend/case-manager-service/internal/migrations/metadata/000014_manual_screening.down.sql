BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
-- Validation intentionally refuses rollback when manual cases exist. Never
-- silently delete or misclassify investigation records during downgrade.
ALTER TABLE case_manager.cases DROP CONSTRAINT cases_type_check;
ALTER TABLE case_manager.cases ADD CONSTRAINT cases_type_check CHECK (type IN ('decision', 'continuous_screening'));
COMMIT;
