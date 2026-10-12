BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Stop writers between preflight and constraint creation. Never pick a winner
-- or erase existing case histories to make this migration pass.
LOCK TABLE case_manager.case_decisions IN SHARE ROW EXCLUSIVE MODE;
DO $$ DECLARE duplicate record; BEGIN
 SELECT tenant_id,decision_id,count(*) AS memberships INTO duplicate
 FROM case_manager.case_decisions GROUP BY tenant_id,decision_id HAVING count(*)>1 LIMIT 1;
 IF FOUND THEN
  RAISE EXCEPTION 'Decision membership preflight failed: tenant %, decision %, % memberships. Reconcile with preserved history before retrying.',duplicate.tenant_id,duplicate.decision_id,duplicate.memberships;
 END IF;
END $$;
CREATE UNIQUE INDEX case_decisions_primary_unique ON case_manager.case_decisions(tenant_id,decision_id);
COMMIT;
