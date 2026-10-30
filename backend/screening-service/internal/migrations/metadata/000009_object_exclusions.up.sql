BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Keep one initial review per candidate, but allow audited exclusion removal
-- after a dismissal. No existing review history is changed.
ALTER TABLE screening.investigator_reviews DROP CONSTRAINT investigator_reviews_previous_status_check;
ALTER TABLE screening.investigator_reviews ADD CONSTRAINT investigator_reviews_previous_status_check CHECK(previous_status IN ('pending','no_hit'));
ALTER TABLE screening.investigator_reviews DROP CONSTRAINT investigator_reviews_tenant_id_resource_id_key;
CREATE UNIQUE INDEX investigator_reviews_initial_review_idx ON screening.investigator_reviews(tenant_id,resource_id) WHERE previous_status='pending';
CREATE TABLE screening.object_exclusions (
 tenant_id uuid NOT NULL,
 object_type text NOT NULL,
 object_id text NOT NULL,
 provider text NOT NULL,
 query_fingerprint text NOT NULL,
 entity_id text NOT NULL,
 active boolean NOT NULL,
 operation_id uuid NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,object_type,object_id,provider,query_fingerprint,entity_id)
);
CREATE TABLE screening.object_exclusion_events (
 tenant_id uuid NOT NULL,
 operation_id uuid NOT NULL,
 screening_id uuid NOT NULL,
 match_id text NOT NULL,
 actor_id text NOT NULL,
 object_type text NOT NULL,
 object_id text NOT NULL,
 provider text NOT NULL,
 query_fingerprint text NOT NULL,
 entity_id text NOT NULL,
 active boolean NOT NULL,
 findings text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,operation_id),
 FOREIGN KEY (tenant_id,operation_id) REFERENCES screening.investigator_reviews(tenant_id,operation_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE FUNCTION screening.guard_object_exclusion_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'Object exclusion audit history is immutable'; END IF;
 IF NOT EXISTS(SELECT 1 FROM screening.screening_matches m JOIN screening.screenings s ON s.id=m.screening_id AND s.tenant_id=m.tenant_id
  WHERE m.tenant_id=NEW.tenant_id AND m.id=NEW.match_id AND m.screening_id=NEW.screening_id AND m.entity_id=NEW.entity_id
   AND s.object_type=NEW.object_type AND s.object_id=NEW.object_id AND s.provider=NEW.provider)
 THEN RAISE EXCEPTION 'Exclusion audit source mismatch'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER guard_object_exclusion_event BEFORE INSERT OR UPDATE OR DELETE ON screening.object_exclusion_events
FOR EACH ROW EXECUTE FUNCTION screening.guard_object_exclusion_event();
ALTER TABLE screening.object_exclusions ADD FOREIGN KEY(tenant_id,operation_id)
 REFERENCES screening.object_exclusion_events(tenant_id,operation_id) DEFERRABLE INITIALLY DEFERRED;
CREATE TABLE screening.object_exclusion_applications (
 tenant_id uuid NOT NULL,
 screening_id uuid NOT NULL,
 match_id text NOT NULL,
 operation_id uuid NOT NULL,
 applied_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,match_id),
 FOREIGN KEY(tenant_id,operation_id) REFERENCES screening.object_exclusion_events(tenant_id,operation_id)
);
COMMIT;
