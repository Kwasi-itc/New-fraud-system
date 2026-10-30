CREATE TABLE {{database}}.decision_engine_iteration_creations (
 tenant_id BINARY(16) NOT NULL,
 scenario_id BINARY(16) NOT NULL,
 operation_id BINARY(16) NOT NULL,
 request_hash BINARY(32) NOT NULL,
 result_body LONGBLOB NOT NULL,
 PRIMARY KEY(tenant_id,scenario_id,operation_id),
 CONSTRAINT de_creation_response CHECK (OCTET_LENGTH(result_body)<=2097152 AND JSON_VALID(CONVERT(result_body USING utf8mb4))),
 CONSTRAINT de_creation_scenario FOREIGN KEY(tenant_id,scenario_id)
 REFERENCES {{database}}.decision_engine_scenarios(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
