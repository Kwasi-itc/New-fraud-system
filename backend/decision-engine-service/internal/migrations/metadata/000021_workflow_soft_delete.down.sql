-- Retain tombstones on rollback: dropping deleted_at would reactivate deleted rules.
-- Older applications must not be deployed after rules have been deleted.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM core.workflow_rules WHERE deleted_at IS NOT NULL) THEN
    RAISE EXCEPTION 'cannot roll back workflow deletion while tombstones exist';
  END IF;
END $$;
DROP INDEX core.workflow_rules_active_order_idx;
ALTER TABLE core.workflow_rules DROP COLUMN deleted_at;
