CREATE TABLE {{database}}.decision_engine_scheduled_executions(
 id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,scenario_id BINARY(16) NOT NULL,scenario_iteration_id BINARY(16) NOT NULL,
 source VARBINARY(16) NOT NULL CHECK(source IN('manual','recurring')),
 status VARBINARY(16) NOT NULL CHECK(status IN('pending','running','completed','failed','cancelled')),
 idempotency_key LONGTEXT NOT NULL,idempotency_key_id BINARY(16) NULL,
 idempotency_sha BINARY(32) GENERATED ALWAYS AS(CASE WHEN OCTET_LENGTH(idempotency_key)=0 THEN NULL ELSE UNHEX(SHA2(CAST(idempotency_key AS BINARY),256)) END) STORED,
 generation INT NOT NULL DEFAULT 0 CHECK(generation>=0),attempt_count INT NOT NULL DEFAULT 0 CHECK(attempt_count>=0),max_attempts INT NOT NULL CHECK(max_attempts>0),
 scheduled_for DATETIME(6) NOT NULL,next_attempt_at DATETIME(6) NULL,
 recurrence_slot DATETIME(6) GENERATED ALWAYS AS(CASE WHEN source=_binary'recurring' THEN scheduled_for ELSE NULL END) STORED,
 request_body JSON NOT NULL,last_error LONGTEXT NOT NULL,created_at DATETIME(6) NOT NULL,failed_at DATETIME(6) NULL,cancelled_at DATETIME(6) NULL,
 UNIQUE KEY de_scheduled_tenant(tenant_id,id),UNIQUE KEY de_scheduled_request(tenant_id,idempotency_key_id),UNIQUE KEY de_scheduled_occurrence(tenant_id,scenario_id,recurrence_slot),
 KEY de_scheduled_key_lookup(tenant_id,idempotency_sha),KEY de_scheduled_history(tenant_id,scenario_id,scheduled_for DESC,created_at DESC,id DESC),
 KEY de_scheduled_due(tenant_id,status,scheduled_for,id),KEY de_scheduled_count(tenant_id,scenario_id,status),
 FOREIGN KEY(tenant_id,scenario_id,scenario_iteration_id) REFERENCES {{database}}.decision_engine_scenario_iterations(tenant_id,scenario_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(idempotency_key_id) REFERENCES {{database}}.decision_engine_exact_keys(id) ON DELETE RESTRICT,
 CHECK(attempt_count<=max_attempts),CHECK((status=_binary'cancelled')=(cancelled_at IS NOT NULL)),CHECK((idempotency_key_id IS NULL)=(OCTET_LENGTH(idempotency_key)=0)),
 CHECK(LOCATE(X'00',CAST(idempotency_key AS BINARY))=0),CHECK(LOCATE(X'00',CAST(last_error AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
