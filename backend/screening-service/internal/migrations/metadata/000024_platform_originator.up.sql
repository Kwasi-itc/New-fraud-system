BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='30s';
-- Empty legacy provenance is deliberately not attributed to a fabricated user.
ALTER TABLE screening.case_event_outbox ADD COLUMN originator jsonb NOT NULL DEFAULT '{}';
COMMIT;
