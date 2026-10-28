CREATE TABLE {{database}}.data_model_index_deliveries (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL, job_id BINARY(16) NOT NULL,
 kind VARCHAR(32) NOT NULL DEFAULT 'index_job_execute' CHECK (kind='index_job_execute'),
 payload_version INT NOT NULL DEFAULT 1 CHECK (payload_version=1),
 queue_name VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 state VARCHAR(16) NOT NULL CHECK (state IN ('available','running','completed','cancelled','discarded')),
 available_at DATETIME(6) NOT NULL, created_at DATETIME(6) NOT NULL,
 attempt INT NOT NULL DEFAULT 0 CHECK (attempt>=0),
 max_attempts INT NOT NULL CHECK (max_attempts>0),
 lease_token BINARY(16) NULL, lease_until DATETIME(6) NULL,
 heartbeat_version BIGINT NOT NULL DEFAULT 0 CHECK (heartbeat_version>=0),
 finalized_at DATETIME(6) NULL, retain_until DATETIME(6) NULL,
 last_error_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
 UNIQUE (tenant_id,job_id,id),
 KEY data_model_delivery_ready (queue_name,state,available_at,id),
 KEY data_model_delivery_expired (queue_name,state,lease_until,id),
 KEY data_model_delivery_retention (queue_name,retain_until,id),
 KEY data_model_delivery_tenant (tenant_id,created_at DESC,id DESC),
 FOREIGN KEY (tenant_id,job_id) REFERENCES {{database}}.data_model_index_jobs(tenant_id,id) ON DELETE RESTRICT,
 CHECK ((state='running' AND lease_token IS NOT NULL AND lease_until IS NOT NULL) OR
        (state<>'running' AND lease_token IS NULL AND lease_until IS NULL)),
 CHECK ((state IN ('completed','cancelled','discarded') AND finalized_at IS NOT NULL AND retain_until IS NOT NULL) OR
        (state IN ('available','running') AND finalized_at IS NULL AND retain_until IS NULL))
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
