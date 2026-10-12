BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
ALTER TABLE screening.screenings ADD COLUMN processing_token uuid, ADD COLUMN processing_until timestamptz;
ALTER TABLE screening.screenings ADD CONSTRAINT screenings_tenant_id_unique UNIQUE(tenant_id,id);
ALTER TABLE screening.continuous_screening_configs ADD CONSTRAINT continuous_configs_tenant_id_unique UNIQUE(tenant_id,id);
ALTER TABLE screening.monitored_objects ADD CONSTRAINT monitored_tenant_config_id_unique UNIQUE(tenant_id,config_id,id);
-- No historical backfill: provenance can only be asserted by the creating worker.
CREATE TABLE screening.continuous_runs (
 tenant_id uuid NOT NULL,
 screening_id uuid NOT NULL,
 config_id uuid NOT NULL,
 monitored_object_id uuid NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,screening_id),
 FOREIGN KEY(tenant_id,screening_id) REFERENCES screening.screenings(tenant_id,id),
 FOREIGN KEY(tenant_id,config_id) REFERENCES screening.continuous_screening_configs(tenant_id,id),
 FOREIGN KEY(tenant_id,config_id,monitored_object_id) REFERENCES screening.monitored_objects(tenant_id,config_id,id)
);
CREATE INDEX continuous_runs_origin_idx ON screening.continuous_runs(tenant_id,config_id,monitored_object_id);
CREATE INDEX screenings_recovery_idx ON screening.screenings(processing_until,id) WHERE status='processing';
ALTER TABLE screening.case_event_outbox DROP CONSTRAINT case_event_outbox_kind_check;
ALTER TABLE screening.case_event_outbox ADD CONSTRAINT case_event_outbox_kind_check CHECK(kind IN ('reviewed','evidence-uploaded','requires-review'));
COMMIT;
