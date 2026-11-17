BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE screening.inline_evidence (
 tenant_id uuid NOT NULL,decision_id uuid NOT NULL,execution_id uuid NOT NULL,screening_id uuid NOT NULL,
 config_id uuid NOT NULL,scenario_id uuid NOT NULL,config_snapshot jsonb NOT NULL,request_snapshot jsonb NOT NULL,
 provider_evidence jsonb NOT NULL,policy_snapshot jsonb NOT NULL,
 lookup_status text NOT NULL CHECK(lookup_status IN ('no_candidates','candidates','failed','timed_out','incomplete')),
 evaluated_at timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,execution_id),UNIQUE(tenant_id,screening_id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES screening.screenings(tenant_id,id)
);
CREATE INDEX inline_decision_evidence ON screening.inline_evidence(tenant_id,decision_id,execution_id);
CREATE FUNCTION screening.preserve_inline_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'inline decision evidence is immutable'; END; $$;
CREATE TRIGGER immutable_inline_evidence BEFORE UPDATE OR DELETE ON screening.inline_evidence FOR EACH ROW EXECUTE FUNCTION screening.preserve_inline_evidence();
-- Resolve the decision owner's objects at invocation, preserving independent
-- migration installation. Inserts require the exact tenant/parent relationship.
CREATE FUNCTION screening.validate_inline_relationship() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM core.screening_executions e JOIN core.decisions d ON(d.tenant_id,d.id,d.scenario_id)=(e.tenant_id,e.decision_id,e.scenario_id) WHERE(e.tenant_id,e.id,e.decision_id,e.config_id,e.scenario_id)=(NEW.tenant_id,NEW.execution_id,NEW.decision_id,NEW.config_id,NEW.scenario_id)) THEN RAISE EXCEPTION 'inline evidence parent mismatch'; END IF;
 RETURN NEW;
END; $$;
CREATE CONSTRAINT TRIGGER inline_parent_relationship AFTER INSERT ON screening.inline_evidence DEFERRABLE INITIALLY IMMEDIATE FOR EACH ROW EXECUTE FUNCTION screening.validate_inline_relationship();
CREATE FUNCTION screening.protect_inline_run() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM screening.inline_evidence e WHERE(e.tenant_id,e.screening_id)=(OLD.tenant_id,OLD.id)) AND
 (NEW.status IN ('pending','queued','processing') OR
 (NEW.request_json,NEW.response_json,NEW.provider_reference,NEW.partial,NEW.provider,NEW.object_type,NEW.object_id,NEW.decision_id,NEW.scenario_id,NEW.screening_config_id,NEW.idempotency_key) IS DISTINCT FROM
 (OLD.request_json,OLD.response_json,OLD.provider_reference,OLD.partial,OLD.provider,OLD.object_type,OLD.object_id,OLD.decision_id,OLD.scenario_id,OLD.screening_config_id,OLD.idempotency_key)) THEN
 RAISE EXCEPTION 'saved inline evaluation cannot be retried or rewritten; create a new evaluation';
 END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER protect_inline_run BEFORE UPDATE ON screening.screenings FOR EACH ROW EXECUTE FUNCTION screening.protect_inline_run();
COMMIT;
