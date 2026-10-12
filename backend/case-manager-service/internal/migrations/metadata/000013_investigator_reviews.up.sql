BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Owner APIs call this case-owned contract in their existing transaction.
-- SECURITY INVOKER deliberately does not elevate database privileges.
CREATE FUNCTION case_manager.record_investigator_review(tenant uuid, owner_name text, operation uuid) RETURNS void LANGUAGE plpgsql AS $$
DECLARE r record; c record; event_kind text; resource_kind text;
BEGIN
 IF owner_name='core' THEN
  SELECT * INTO STRICT r FROM core.investigator_reviews WHERE tenant_id=tenant AND operation_id=operation;
  event_kind:='decision_reviewed'; resource_kind:='decision';
 ELSIF owner_name='screening' THEN
  SELECT * INTO STRICT r FROM screening.investigator_reviews WHERE tenant_id=tenant AND operation_id=operation;
  event_kind:='screening_match_reviewed'; resource_kind:='screening_match';
 ELSE RAISE EXCEPTION 'Unknown investigator review owner'; END IF;
 SELECT * INTO STRICT c FROM case_manager.cases WHERE tenant_id=tenant AND id=r.case_id FOR UPDATE;
 IF c.status='closed' THEN RAISE EXCEPTION 'Case must be open'; END IF;
 IF owner_name='core' THEN
  IF NOT EXISTS(SELECT 1 FROM case_manager.case_decisions WHERE tenant_id=tenant AND case_id=r.case_id AND decision_id=r.resource_id::uuid) THEN RAISE EXCEPTION 'Decision not linked'; END IF;
 ELSE
  UPDATE case_manager.case_screenings l SET status=m.status
  FROM screening.screening_matches m WHERE l.tenant_id=tenant AND l.case_id=r.case_id AND l.match_id=r.resource_id
   AND m.tenant_id=l.tenant_id AND m.id=l.match_id AND m.screening_id=l.screening_id;
  IF NOT FOUND THEN RAISE EXCEPTION 'Match not linked'; END IF;
 END IF;
 INSERT INTO case_manager.case_events(id,tenant_id,case_id,user_id,event_type,additional_note,resource_id,resource_type,new_value,previous_value,created_at)
 VALUES(r.event_id,tenant,r.case_id,r.actor_id,event_kind,r.findings,r.resource_id,resource_kind,r.status,r.previous_status,r.created_at);
 INSERT INTO case_manager.outbox_events(id,tenant_id,aggregate_type,aggregate_id,event_type,payload,status,created_at)
 SELECT id,tenant_id,'case',case_id::text,event_type,to_jsonb(e),'pending',created_at FROM case_manager.case_events e WHERE id=r.event_id;
END $$;
COMMIT;
