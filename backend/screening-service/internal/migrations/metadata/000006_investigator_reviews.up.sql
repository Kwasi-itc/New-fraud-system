BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE screening.investigator_reviews (
 tenant_id uuid NOT NULL, operation_id uuid NOT NULL, event_id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 case_id uuid NOT NULL, resource_id text NOT NULL, actor_id text NOT NULL CHECK(length(btrim(actor_id))>0),
 previous_status text NOT NULL CHECK(previous_status='pending'), status text NOT NULL CHECK(status IN ('confirmed_hit','no_hit')),
 findings text NOT NULL CHECK(octet_length(btrim(findings)) BETWEEN 1 AND 10000),
 payload_hash text NOT NULL, result jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,operation_id), UNIQUE(tenant_id,resource_id)
);
CREATE FUNCTION screening.guard_investigator_review() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP <> 'INSERT' THEN RAISE EXCEPTION 'Investigator review history is immutable'; END IF;
 PERFORM 1 FROM screening.screening_matches WHERE tenant_id=NEW.tenant_id AND id=NEW.resource_id FOR KEY SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'Review source tenant/resource does not exist'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER guard_investigator_review BEFORE INSERT OR UPDATE OR DELETE ON screening.investigator_reviews FOR EACH ROW EXECUTE FUNCTION screening.guard_investigator_review();
COMMIT;