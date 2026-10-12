BEGIN;
SET LOCAL lock_timeout='5s';
LOCK TABLE screening.investigator_reviews IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM screening.investigator_reviews) THEN RAISE EXCEPTION 'Cannot discard investigator review history'; END IF; END $$;
DROP TABLE screening.investigator_reviews;
DROP FUNCTION screening.guard_investigator_review();
COMMIT;