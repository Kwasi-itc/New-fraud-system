CREATE TABLE {{database}}.decision_engine_execution_job_deliveries(
 id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,kind VARBINARY(40) NOT NULL,queue_name VARBINARY(128) NOT NULL,
 state VARBINARY(16) NOT NULL DEFAULT 'available' CHECK(state IN('available','running','completed','discarded','cancelled')),
 available_at DATETIME(6) NOT NULL,created_at DATETIME(6) NOT NULL,attempt INT NOT NULL DEFAULT 0 CHECK(attempt>=0),max_attempts INT NOT NULL CHECK(max_attempts>0),
 error_count INT NOT NULL DEFAULT 0 CHECK(error_count>=0),lease_token BINARY(16) NULL,lease_until DATETIME(6) NULL,heartbeat_version BIGINT NOT NULL DEFAULT 0 CHECK(heartbeat_version>=0),
 finished_at DATETIME(6) NULL,last_error_code VARBINARY(64) NOT NULL DEFAULT '',
 CHECK(attempt<=max_attempts),CHECK(error_count<=attempt),
 CHECK((state=_binary'running' AND lease_token IS NOT NULL AND lease_until IS NOT NULL AND attempt>0) OR(state<>_binary'running' AND lease_token IS NULL AND lease_until IS NULL)),
 CHECK((state IN('completed','discarded','cancelled') AND finished_at IS NOT NULL) OR(state IN('available','running') AND finished_at IS NULL)),
 KEY de_execution_delivery_claim(tenant_id,kind,queue_name,state,available_at,id),KEY de_execution_delivery_rescue(tenant_id,kind,queue_name,state,lease_until,id),
 FOREIGN KEY(tenant_id,kind,id) REFERENCES {{database}}.decision_engine_execution_job_receipts(tenant_id,kind,id) ON DELETE RESTRICT
) ENGINE=InnoDB
