-- Export audit data before rollback. Old application versions must be restored
-- before dropping these tables; rollback removes exclusion/audit history.
BEGIN;
SET LOCAL lock_timeout='5s';
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM screening.investigator_reviews WHERE previous_status<>'pending') THEN
  RAISE EXCEPTION 'Cannot roll back after exclusion removal reviews; preserve audit and use a forward migration';
 END IF;
END $$;
DROP TABLE screening.object_exclusion_applications;
DROP TABLE screening.object_exclusions;
DROP TABLE screening.object_exclusion_events;
DROP FUNCTION screening.guard_object_exclusion_event();
DROP INDEX screening.investigator_reviews_initial_review_idx;
ALTER TABLE screening.investigator_reviews ADD CONSTRAINT investigator_reviews_tenant_id_resource_id_key UNIQUE(tenant_id,resource_id);
ALTER TABLE screening.investigator_reviews DROP CONSTRAINT investigator_reviews_previous_status_check;
ALTER TABLE screening.investigator_reviews ADD CONSTRAINT investigator_reviews_previous_status_check CHECK(previous_status='pending');
COMMIT;
