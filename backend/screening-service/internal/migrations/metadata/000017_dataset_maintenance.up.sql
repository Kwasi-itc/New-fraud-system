BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE screening.dataset_update_jobs ADD CONSTRAINT dataset_jobs_tenant_id UNIQUE(tenant_id,id);
CREATE TABLE screening.dataset_maintenance_progress(
 tenant_id uuid NOT NULL,job_id uuid NOT NULL,lease_token uuid NOT NULL,lease_until timestamptz NOT NULL,
 delta_cursor text NOT NULL DEFAULT '',delta_done boolean NOT NULL DEFAULT false,needs_rescreen boolean NOT NULL DEFAULT false,
 after_object uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',objects_done boolean NOT NULL DEFAULT false,
 delta_pages integer NOT NULL DEFAULT 0,queued_objects integer NOT NULL DEFAULT 0,
 PRIMARY KEY(tenant_id,job_id),FOREIGN KEY(tenant_id,job_id) REFERENCES screening.dataset_update_jobs(tenant_id,id)
);
CREATE INDEX dataset_maintenance_expired ON screening.dataset_maintenance_progress(lease_until,tenant_id,job_id);
CREATE INDEX monitored_provider_page ON screening.monitored_objects(tenant_id,id) INCLUDE(config_id) WHERE status IN ('active','pending');
CREATE TABLE screening.dataset_maintenance_receipts(
 tenant_id uuid NOT NULL,job_id uuid NOT NULL,config_id uuid NOT NULL,object_id uuid NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,job_id,object_id),
 FOREIGN KEY(tenant_id,job_id) REFERENCES screening.dataset_update_jobs(tenant_id,id),
 FOREIGN KEY(tenant_id,config_id,object_id) REFERENCES screening.monitored_objects(tenant_id,config_id,id)
);
COMMIT;
