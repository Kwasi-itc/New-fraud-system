BEGIN;
SET LOCAL lock_timeout='5s';
DROP FUNCTION core.reconcile_screening_source(uuid,uuid,uuid,uuid);
-- Retain provenance/version fences; drain new workers/callbacks before rollback.
COMMIT;
