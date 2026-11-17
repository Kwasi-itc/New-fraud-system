BEGIN;
-- Dropping evidence requires an explicit authorized downgrade/retention action.
DROP TRIGGER protect_inline_run ON screening.screenings;
DROP FUNCTION screening.protect_inline_run();
DROP TABLE screening.inline_evidence;
DROP FUNCTION screening.preserve_inline_evidence();
DROP FUNCTION screening.validate_inline_relationship();
COMMIT;
