BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE screening.screenings ADD COLUMN IF NOT EXISTS projection_version bigint NOT NULL DEFAULT 1 CHECK(projection_version>0);
-- Re-upgrades must invalidate projections if sources changed while triggers were absent.
UPDATE screening.screenings SET projection_version=projection_version+1 WHERE decision_id IS NOT NULL;
CREATE TABLE screening.execution_projection_queue (
 tenant_id uuid NOT NULL, screening_id uuid NOT NULL, version bigint NOT NULL,
 attempts integer NOT NULL DEFAULT 0, available_at timestamptz NOT NULL DEFAULT now(),
 last_error text NOT NULL DEFAULT '', updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,screening_id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES screening.screenings(tenant_id,id) ON DELETE CASCADE
);
CREATE INDEX execution_projection_ready_idx ON screening.execution_projection_queue(available_at,tenant_id,screening_id);
CREATE FUNCTION screening.queue_execution_projection() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_WHEN='BEFORE' THEN
  IF TG_OP='UPDATE' THEN NEW.projection_version:=OLD.projection_version+1; ELSE NEW.projection_version:=1; END IF;
  RETURN NEW;
 END IF;
 IF NEW.decision_id IS NOT NULL THEN
  INSERT INTO screening.execution_projection_queue(tenant_id,screening_id,version)
  VALUES(NEW.tenant_id,NEW.id,NEW.projection_version)
  ON CONFLICT(tenant_id,screening_id) DO UPDATE SET version=EXCLUDED.version,attempts=0,available_at=now(),last_error='',updated_at=now();
 ELSE
  DELETE FROM screening.execution_projection_queue WHERE tenant_id=NEW.tenant_id AND screening_id=NEW.id;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER screening_projection_version BEFORE INSERT OR UPDATE ON screening.screenings FOR EACH ROW EXECUTE FUNCTION screening.queue_execution_projection();
CREATE TRIGGER screening_projection_queue AFTER INSERT OR UPDATE ON screening.screenings FOR EACH ROW EXECUTE FUNCTION screening.queue_execution_projection();
-- Historical projections are reconciled from current source state, never invented review history.
INSERT INTO screening.execution_projection_queue(tenant_id,screening_id,version)
SELECT tenant_id,id,projection_version FROM screening.screenings WHERE decision_id IS NOT NULL;
COMMIT;
