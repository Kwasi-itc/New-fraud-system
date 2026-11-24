BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$ BEGIN
 IF to_regprocedure('identity.authorize_capability_v1(text,uuid,uuid,bigint,text,boolean)') IS NULL THEN
  RAISE EXCEPTION 'identity schema v5 must be applied before case_manager platform authority';
 END IF;
END $$;
CREATE TABLE case_manager.client_inbox_access (
 tenant_id uuid NOT NULL, client_id uuid NOT NULL, inbox_id uuid NOT NULL,
 granted_by uuid NOT NULL REFERENCES identity.users(id), granted_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,client_id,inbox_id),
 FOREIGN KEY(tenant_id,client_id) REFERENCES identity.clients(tenant_id,id),
 FOREIGN KEY(tenant_id,inbox_id) REFERENCES case_manager.inboxes(tenant_id,id)
);
CREATE INDEX cases_client_inbox_page_idx ON case_manager.cases(tenant_id,inbox_id,id);
CREATE INDEX client_inbox_access_inbox_idx ON case_manager.client_inbox_access(tenant_id,inbox_id,client_id);
CREATE FUNCTION case_manager.authorize_case_access_v1(
 tenant uuid, case_ref uuid, actor text, owner_name text, resource_ref text,
 sid uuid, expected_version bigint, take_locks boolean, read_admin boolean DEFAULT false, capability text DEFAULT 'cases:review')
RETURNS TABLE(allowed boolean,case_status text,inbox_status text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE inbox uuid; administrator boolean; linked boolean;
BEGIN
 allowed:=false;case_status:='';inbox_status:='';
 IF take_locks THEN
 IF NOT identity.authorize_capability_v1(actor,tenant,sid,expected_version,capability,true) THEN RETURN NEXT;RETURN;END IF;
 administrator:=identity.authorize_capability_v1(actor,tenant,sid,expected_version,'cases:access-all',true);
 ELSE administrator:=read_admin; END IF;
 IF take_locks THEN
  SELECT c.inbox_id,c.status INTO inbox,case_status FROM case_manager.cases c WHERE c.tenant_id=tenant AND c.id=case_ref FOR UPDATE;
  IF NOT FOUND THEN case_status:='';RETURN NEXT;RETURN;END IF;
  SELECT i.status INTO inbox_status FROM case_manager.inboxes i WHERE i.tenant_id=tenant AND i.id=inbox FOR SHARE;
 ELSE
  SELECT c.inbox_id,c.status INTO inbox,case_status FROM case_manager.cases c WHERE c.tenant_id=tenant AND c.id=case_ref;
  IF NOT FOUND THEN case_status:='';RETURN NEXT;RETURN;END IF;
  SELECT i.status INTO inbox_status FROM case_manager.inboxes i WHERE i.tenant_id=tenant AND i.id=inbox;
 END IF;
 IF NOT administrator AND NOT EXISTS(SELECT 1 FROM case_manager.inbox_users u WHERE u.tenant_id=tenant AND u.inbox_id=inbox AND u.user_id=actor) THEN RETURN NEXT;RETURN;END IF;
 IF owner_name='core' THEN
  SELECT EXISTS(SELECT 1 FROM case_manager.case_decisions l WHERE l.tenant_id=tenant AND l.case_id=case_ref AND l.decision_id::text=resource_ref) INTO linked;
 ELSIF owner_name='screening' THEN
  SELECT EXISTS(SELECT 1 FROM case_manager.case_screenings l JOIN screening.screening_matches m
   ON m.tenant_id=l.tenant_id AND m.screening_id=l.screening_id AND m.id=l.match_id
   WHERE l.tenant_id=tenant AND l.case_id=case_ref AND l.match_id=resource_ref) INTO linked;
 ELSIF owner_name='screening-evidence' THEN
 SELECT EXISTS(SELECT 1 FROM case_manager.case_screenings l WHERE l.tenant_id=tenant AND l.case_id=case_ref AND l.screening_id::text=resource_ref) INTO linked;
 ELSE linked:=false;
 END IF;
 allowed:=coalesce(linked,false);RETURN NEXT;
END $$;
REVOKE ALL ON FUNCTION case_manager.authorize_case_access_v1(uuid,uuid,text,text,text,uuid,bigint,boolean,boolean,text) FROM PUBLIC;
COMMIT;

