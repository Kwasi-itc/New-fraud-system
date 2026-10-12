BEGIN;
SET LOCAL lock_timeout='5s';
-- Drain owner review APIs before removing their atomic recording contract.
DROP FUNCTION case_manager.record_investigator_review(uuid,text,uuid);
COMMIT;
