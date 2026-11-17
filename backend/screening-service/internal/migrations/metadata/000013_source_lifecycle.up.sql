BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- An explicit, authenticated opt-in monitors a frozen query, not every historic
-- transaction. Dataset subscriptions include previous no-hits and removals.
CREATE TABLE screening.source_monitors (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, origin_screening_id uuid NOT NULL, latest_screening_id uuid,
 created_by text NOT NULL, enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,origin_screening_id,created_by),
 FOREIGN KEY(tenant_id,origin_screening_id) REFERENCES screening.screenings(tenant_id,id),
 FOREIGN KEY(tenant_id,latest_screening_id) REFERENCES screening.screenings(tenant_id,id)
);
CREATE TABLE screening.source_monitor_datasets (
 monitor_id uuid NOT NULL, tenant_id uuid NOT NULL, owner_scope uuid NOT NULL, dataset_id text NOT NULL,
 PRIMARY KEY(monitor_id,owner_scope,dataset_id),
 FOREIGN KEY(tenant_id,monitor_id) REFERENCES screening.source_monitors(tenant_id,id),
 FOREIGN KEY(owner_scope,dataset_id) REFERENCES screening.source_datasets(owner_scope,id),
 CHECK(owner_scope='00000000-0000-0000-0000-000000000000' OR owner_scope=tenant_id)
);
CREATE INDEX source_monitor_dataset_fanout ON screening.source_monitor_datasets(owner_scope,dataset_id,monitor_id);
CREATE TABLE screening.source_publication_delivery (
 publication_id bigint PRIMARY KEY REFERENCES screening.source_publications(id),
 after_monitor uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','completed')),
 processed bigint NOT NULL DEFAULT 0, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE screening.source_rescreen_targets (
 id uuid PRIMARY KEY, publication_id bigint NOT NULL REFERENCES screening.source_publications(id),
 tenant_id uuid NOT NULL, monitor_id uuid NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','queued','completed','failed','cancelled')),
 screening_id uuid, attempts integer NOT NULL DEFAULT 0, next_attempt_at timestamptz NOT NULL DEFAULT now(),
 last_error text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(publication_id,monitor_id),
 FOREIGN KEY(tenant_id,monitor_id) REFERENCES screening.source_monitors(tenant_id,id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES screening.screenings(tenant_id,id)
);
CREATE INDEX source_rescreen_pending ON screening.source_rescreen_targets(next_attempt_at,id) WHERE status='pending';
CREATE INDEX source_rescreen_monitor_history ON screening.source_rescreen_targets(tenant_id,monitor_id,created_at DESC,id);
CREATE TABLE screening.source_rescreen_attempts (
 target_id uuid NOT NULL REFERENCES screening.source_rescreen_targets(id),
 tenant_id uuid NOT NULL, screening_id uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(target_id,screening_id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES screening.screenings(tenant_id,id)
);
CREATE FUNCTION screening.enqueue_source_publication_v1() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO screening.source_publication_delivery(publication_id) VALUES(NEW.id);
 RETURN NEW;
END $$;
CREATE TRIGGER source_publication_delivery_trigger AFTER INSERT ON screening.source_publications
 FOR EACH ROW EXECUTE FUNCTION screening.enqueue_source_publication_v1();
INSERT INTO screening.source_publication_delivery(publication_id) SELECT id FROM screening.source_publications;
-- Completion follows durable result persistence, not queue acceptance. A failed
-- or incomplete run never replaces the last successful monitoring evidence.
CREATE FUNCTION screening.finish_source_rescreen_v1() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (OLD.completed_at IS NULL AND NEW.completed_at IS NOT NULL) OR (OLD.status<>'failed' AND NEW.status='failed') THEN
  UPDATE screening.source_rescreen_targets SET
   status=CASE WHEN NEW.status='failed' OR NEW.partial THEN 'failed' ELSE 'completed' END,
   last_error=CASE WHEN NEW.partial THEN 'screening_incomplete' ELSE NEW.last_error END,
   next_attempt_at=now()+make_interval(secs=>least(300,5*power(2,least(attempts,6))::int)),updated_at=now()
   WHERE tenant_id=NEW.tenant_id AND screening_id=NEW.id AND status='queued';
  IF NEW.completed_at IS NOT NULL AND NEW.status<>'failed' AND NOT NEW.partial THEN
   UPDATE screening.source_monitors m SET latest_screening_id=NEW.id,updated_at=now()
    FROM screening.source_rescreen_targets t WHERE t.tenant_id=NEW.tenant_id AND t.screening_id=NEW.id
     AND (m.tenant_id,m.id)=(t.tenant_id,t.monitor_id);
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER source_rescreen_completion_trigger AFTER UPDATE ON screening.screenings
 FOR EACH ROW EXECUTE FUNCTION screening.finish_source_rescreen_v1();
CREATE TABLE screening.source_artifacts (
 owner_scope uuid NOT NULL, dataset_id text NOT NULL, sha256 text NOT NULL,
 body bytea NOT NULL CHECK(octet_length(body)<=67108864), media_type text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(owner_scope,dataset_id,sha256),
 FOREIGN KEY(owner_scope,dataset_id) REFERENCES screening.source_datasets(owner_scope,id)
);
ALTER TABLE screening.source_versions ADD COLUMN lease_until timestamptz NOT NULL DEFAULT (now()+interval '10 minutes');
CREATE TABLE screening.source_version_artifacts (
 owner_scope uuid NOT NULL, dataset_id text NOT NULL, generation uuid NOT NULL, sha256 text NOT NULL,
 PRIMARY KEY(owner_scope,dataset_id,generation),
 FOREIGN KEY(owner_scope,dataset_id,generation) REFERENCES screening.source_versions(owner_scope,dataset_id,id),
 FOREIGN KEY(owner_scope,dataset_id,sha256) REFERENCES screening.source_artifacts(owner_scope,dataset_id,sha256)
);
CREATE TABLE screening.source_retention_policies (
 owner_scope uuid NOT NULL, dataset_id text NOT NULL,
 failed_after interval NOT NULL CHECK(failed_after>=interval '1 day'),
 staged_after interval NOT NULL CHECK(staged_after>=interval '1 day'),
 legal_hold boolean NOT NULL DEFAULT true,
 PRIMARY KEY(owner_scope,dataset_id),
 FOREIGN KEY(owner_scope,dataset_id) REFERENCES screening.source_datasets(owner_scope,id)
);
CREATE TABLE screening.source_lifecycle_audit (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, owner_scope uuid NOT NULL, dataset_id text NOT NULL,
 action text NOT NULL, actor text NOT NULL, detail jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(owner_scope,dataset_id) REFERENCES screening.source_datasets(owner_scope,id)
);
COMMIT;
