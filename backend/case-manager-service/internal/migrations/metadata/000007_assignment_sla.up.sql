BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
ALTER TABLE case_manager.inboxes ADD COLUMN sla_days integer CHECK(sla_days BETWEEN 1 AND 3650);
ALTER TABLE case_manager.inbox_users ADD COLUMN capacity integer NOT NULL DEFAULT 20 CHECK(capacity BETWEEN 0 AND 1000);
CREATE INDEX cases_assignment_workload_idx ON case_manager.cases(tenant_id,inbox_id,assigned_to) WHERE status<>'closed' AND assigned_to IS NOT NULL;
CREATE INDEX cases_auto_assignment_idx ON case_manager.cases(created_at,id) WHERE status<>'closed' AND assigned_to IS NULL;
-- All assignment paths, including human actions and reopen/move, share the same
-- member lock. Existing assignments survive a reduction in configured capacity.
CREATE FUNCTION case_manager.enforce_assignment_capacity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE maximum integer; workload integer;
BEGIN
 IF NEW.assigned_to IS NULL OR NEW.status='closed' THEN RETURN NEW; END IF;
 IF TG_OP='UPDATE' THEN
  IF OLD.assigned_to IS NOT DISTINCT FROM NEW.assigned_to AND OLD.inbox_id=NEW.inbox_id AND OLD.status<>'closed' THEN RETURN NEW; END IF;
 END IF;
 SELECT capacity INTO maximum FROM case_manager.inbox_users WHERE tenant_id=NEW.tenant_id AND inbox_id=NEW.inbox_id AND user_id=NEW.assigned_to FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'assignee is not an inbox member' USING ERRCODE='23514'; END IF;
 SELECT count(*) INTO workload FROM case_manager.cases WHERE tenant_id=NEW.tenant_id AND inbox_id=NEW.inbox_id AND assigned_to=NEW.assigned_to AND status<>'closed' AND id<>NEW.id;
 IF workload>=maximum THEN RAISE EXCEPTION 'assignee capacity reached' USING ERRCODE='23514',CONSTRAINT='assignment_capacity'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER enforce_assignment_capacity BEFORE INSERT OR UPDATE OF assigned_to,inbox_id,status ON case_manager.cases FOR EACH ROW EXECUTE FUNCTION case_manager.enforce_assignment_capacity();

COMMIT;
