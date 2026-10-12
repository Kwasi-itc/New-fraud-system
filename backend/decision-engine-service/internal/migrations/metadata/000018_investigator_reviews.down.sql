BEGIN;
SET LOCAL lock_timeout='5s';
LOCK TABLE core.investigator_reviews IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM core.investigator_reviews) THEN RAISE EXCEPTION 'Cannot discard investigator review history'; END IF; END $$;
DROP TABLE core.investigator_reviews;
DROP FUNCTION core.guard_investigator_review();
COMMIT;