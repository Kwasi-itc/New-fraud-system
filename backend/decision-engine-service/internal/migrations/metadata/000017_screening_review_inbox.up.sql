BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Case-manager validates tenant, existence and current inbox state at intake.
-- No reverse migration dependency on case-manager's later-created schema.
ALTER TABLE core.screening_configs ADD COLUMN review_inbox_id UUID NULL
 CHECK (review_inbox_id IS NULL OR review_inbox_id <> '00000000-0000-0000-0000-000000000000');
COMMIT;
