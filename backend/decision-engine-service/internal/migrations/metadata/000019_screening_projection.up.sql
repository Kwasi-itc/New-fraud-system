BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE core.screening_executions ADD COLUMN IF NOT EXISTS projection_source_id uuid, ADD COLUMN IF NOT EXISTS projection_version bigint NOT NULL DEFAULT 0;
-- PL/pgSQL resolves the screening-owner tables at invocation, after screening 000007.
-- SECURITY INVOKER: no database privilege escalation.
CREATE FUNCTION core.reconcile_screening_source(tenant uuid, source_id uuid, expected_decision uuid DEFAULT NULL, expected_config uuid DEFAULT NULL) RETURNS uuid LANGUAGE plpgsql AS $$
DECLARE s record; e core.screening_executions%ROWTYPE; next_status text; count_matches integer; stamp timestamptz;
BEGIN
 SELECT * INTO STRICT s FROM screening.screenings WHERE tenant_id=tenant AND id=source_id FOR UPDATE;
 IF s.decision_id IS NULL OR s.screening_config_id IS NULL THEN RAISE EXCEPTION 'Source has no decision execution relationship'; END IF;
 IF (expected_decision IS NOT NULL AND expected_decision<>s.decision_id) OR (expected_config IS NOT NULL AND expected_config<>s.screening_config_id) THEN RAISE EXCEPTION 'Source relationship mismatch'; END IF;
 -- Producers use execution ID as idempotency key. Legacy sources without a key
 -- require exactly one tenant/decision/config candidate; ambiguity fails closed.
 SELECT * INTO STRICT e FROM core.screening_executions
 WHERE tenant_id=tenant AND decision_id=s.decision_id AND config_id=s.screening_config_id
 AND (coalesce(s.idempotency_key,'')='' OR id::text=s.idempotency_key) FOR UPDATE;
 IF e.scenario_id IS DISTINCT FROM s.scenario_id THEN RAISE EXCEPTION 'Source scenario mismatch'; END IF;
 IF e.projection_source_id IS NOT NULL AND e.projection_source_id<>s.id THEN RAISE EXCEPTION 'Execution already bound to another source'; END IF;
 IF e.projection_version>=s.projection_version THEN RETURN e.id; END IF;
 next_status:=CASE s.status WHEN 'pending' THEN 'pending' WHEN 'queued' THEN 'pending' WHEN 'processing' THEN 'sent' WHEN 'awaiting_review' THEN 'completed' WHEN 'confirmed_hit' THEN 'completed' WHEN 'no_hit' THEN 'completed' WHEN 'archived' THEN 'completed' WHEN 'failed' THEN 'failed' END;
 IF next_status IS NULL THEN RAISE EXCEPTION 'Unsupported source status'; END IF;
 SELECT count(*) INTO count_matches FROM screening.screening_matches WHERE tenant_id=tenant AND screening_id=s.id;
 stamp:=s.updated_at;
 UPDATE core.screening_executions SET status=next_status,provider_reference=s.provider_reference,last_error=s.last_error,
 response_json=jsonb_build_object('screening_id',s.id,'decision_id',s.decision_id,'scenario_id',s.scenario_id,'screening_config_id',s.screening_config_id,'status',s.status,'provider',s.provider,'object_type',s.object_type,'object_id',s.object_id,'provider_reference',s.provider_reference,'last_error',s.last_error,'partial',s.partial,'idempotency_key',s.idempotency_key,'match_count',count_matches,'completed_at',s.completed_at,'source_version',s.projection_version),
 sent_at=CASE WHEN next_status='pending' THEN NULL WHEN next_status='sent' THEN coalesce(s.sent_at,stamp) WHEN next_status='completed' THEN coalesce(e.sent_at,s.sent_at,stamp) ELSE coalesce(e.sent_at,s.sent_at) END,
 completed_at=CASE WHEN next_status='completed' THEN coalesce(s.completed_at,stamp) ELSE NULL END,
 failed_at=CASE WHEN next_status='failed' THEN coalesce(s.failed_at,stamp) ELSE NULL END,
 updated_at=now(),projection_source_id=s.id,projection_version=s.projection_version
 WHERE id=e.id AND tenant_id=tenant;
 RETURN e.id;
END $$;
COMMIT;
