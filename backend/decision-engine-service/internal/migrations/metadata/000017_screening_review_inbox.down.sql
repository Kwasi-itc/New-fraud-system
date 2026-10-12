BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
LOCK TABLE core.screening_configs IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM core.screening_configs WHERE review_inbox_id IS NOT NULL) THEN
  RAISE EXCEPTION 'Cannot discard configured screening review routes; preserve and explicitly clear routing before downgrade';
 END IF;
END $$;
ALTER TABLE core.screening_configs DROP COLUMN review_inbox_id;
COMMIT;
