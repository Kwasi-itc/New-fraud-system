CREATE TABLE {{database}}.decision_engine_scenario_publications (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 scenario_id BINARY(16) NOT NULL,
 iteration_id BINARY(16) NOT NULL,
 action LONGTEXT NOT NULL,
 created_at DATETIME(6) NOT NULL,
 KEY de_publication_history (tenant_id,scenario_id,created_at DESC,id DESC),
 CONSTRAINT de_publication_iteration FOREIGN KEY(tenant_id,scenario_id,iteration_id)
 REFERENCES {{database}}.decision_engine_scenario_iterations(tenant_id,scenario_id,id) ON DELETE CASCADE,
 CONSTRAINT de_publication_action_text CHECK (LOCATE(X'00',CAST(action AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
