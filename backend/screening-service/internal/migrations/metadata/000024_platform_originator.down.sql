BEGIN;
ALTER TABLE screening.case_event_outbox DROP COLUMN originator;
COMMIT;
