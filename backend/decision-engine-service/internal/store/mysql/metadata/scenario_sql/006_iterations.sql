CREATE TABLE {{database}}.decision_engine_scenario_iterations (
 id BINARY(16) PRIMARY KEY,
 scenario_id BINARY(16) NOT NULL,
 tenant_id BINARY(16) NOT NULL,
 version INT NOT NULL,
 status LONGTEXT NOT NULL,
 trigger_formula JSON NOT NULL DEFAULT (JSON_OBJECT()),
 score_review_threshold INT NULL,
 score_block_and_review_threshold INT NULL,
 score_decline_threshold INT NULL,
 schedule LONGTEXT NOT NULL DEFAULT (''),
 created_at DATETIME(6) NOT NULL,
 committed_at DATETIME(6) NULL,
 UNIQUE KEY de_iteration_binding (tenant_id,scenario_id,id),
 UNIQUE KEY de_iteration_tenant (tenant_id,id),
 UNIQUE KEY de_iteration_version (tenant_id,scenario_id,version),
 CONSTRAINT de_iteration_scenario FOREIGN KEY(tenant_id,scenario_id)
 REFERENCES {{database}}.decision_engine_scenarios(tenant_id,id) ON DELETE CASCADE,
 CONSTRAINT de_iteration_status_text CHECK (LOCATE(X'00',CAST(status AS BINARY))=0),
 CONSTRAINT de_iteration_schedule_text CHECK (LOCATE(X'00',CAST(schedule AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
