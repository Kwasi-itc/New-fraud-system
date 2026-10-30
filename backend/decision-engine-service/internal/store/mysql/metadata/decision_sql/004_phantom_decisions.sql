CREATE TABLE {{database}}.decision_engine_phantom_decisions (
 id BINARY(16) PRIMARY KEY,
 test_run_id BINARY(16) NOT NULL,
 tenant_id BINARY(16) NOT NULL,
 scenario_id BINARY(16) NOT NULL,
 scenario_iteration_id BINARY(16) NOT NULL,
 object_id LONGTEXT NOT NULL,
 object_type LONGTEXT NOT NULL,
 outcome LONGTEXT NOT NULL,
 score INT NOT NULL DEFAULT 0,
 triggered BOOLEAN NOT NULL DEFAULT TRUE,
 created_at DATETIME(6) NOT NULL,
 UNIQUE KEY de_phantom_iteration (tenant_id,id,scenario_iteration_id),
 KEY de_phantom_history (tenant_id,test_run_id,created_at DESC,id DESC),
 CONSTRAINT de_phantom_test_run FOREIGN KEY(tenant_id,scenario_id,test_run_id,scenario_iteration_id)
 REFERENCES {{database}}.decision_engine_test_runs(tenant_id,scenario_id,id,phantom_iteration_id) ON DELETE CASCADE,
 CONSTRAINT de_phantom_triggered CHECK (triggered IN (0,1)),
 CONSTRAINT de_phantom_id_text CHECK (LOCATE(X'00',CAST(object_id AS BINARY))=0),
 CONSTRAINT de_phantom_type_text CHECK (LOCATE(X'00',CAST(object_type AS BINARY))=0),
 CONSTRAINT de_phantom_outcome_text CHECK (LOCATE(X'00',CAST(outcome AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
