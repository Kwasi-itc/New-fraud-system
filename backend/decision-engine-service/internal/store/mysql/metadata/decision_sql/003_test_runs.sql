CREATE TABLE {{database}}.decision_engine_test_runs (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 scenario_id BINARY(16) NOT NULL,
 live_iteration_id BINARY(16) NOT NULL,
 phantom_iteration_id BINARY(16) NOT NULL,
 status LONGTEXT NOT NULL,
 created_at DATETIME(6) NOT NULL,
 expires_at DATETIME(6) NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 UNIQUE KEY de_test_run_tenant (tenant_id,id),
 UNIQUE KEY de_test_run_phantom (tenant_id,scenario_id,id,phantom_iteration_id),
 KEY de_test_run_history (tenant_id,scenario_id,created_at DESC,id DESC),
 CONSTRAINT de_test_run_live FOREIGN KEY(tenant_id,scenario_id,live_iteration_id)
 REFERENCES {{database}}.decision_engine_scenario_iterations(tenant_id,scenario_id,id) ON DELETE CASCADE,
 CONSTRAINT de_test_run_candidate FOREIGN KEY(tenant_id,scenario_id,phantom_iteration_id)
 REFERENCES {{database}}.decision_engine_scenario_iterations(tenant_id,scenario_id,id) ON DELETE CASCADE,
 CONSTRAINT de_test_run_status_text CHECK (LOCATE(X'00',CAST(status AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
