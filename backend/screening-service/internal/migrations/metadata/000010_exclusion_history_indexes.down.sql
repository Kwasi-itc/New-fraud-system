BEGIN;
SET LOCAL lock_timeout='5s';
DROP INDEX screening.object_exclusion_applications_history_idx;
DROP INDEX screening.investigator_reviews_case_history_idx;
COMMIT;
