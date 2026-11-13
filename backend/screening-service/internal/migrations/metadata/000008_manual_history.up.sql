BEGIN;
SET LOCAL lock_timeout='5s';
CREATE INDEX screenings_manual_creator_history_idx
 ON screening.screenings (tenant_id, (split_part(idempotency_key,':',2)), created_at DESC, id DESC)
 WHERE is_manual AND decision_id IS NULL AND scenario_id IS NULL AND idempotency_key LIKE 'manual:%';
COMMIT;
