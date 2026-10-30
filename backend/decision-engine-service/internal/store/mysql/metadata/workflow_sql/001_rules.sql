CREATE TABLE {{database}}.decision_engine_workflow_rules (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 scenario_id BINARY(16) NOT NULL,
 name LONGTEXT NOT NULL,
 priority INT NOT NULL DEFAULT 0,
 fallthrough BOOLEAN NOT NULL DEFAULT FALSE CHECK(fallthrough IN (0,1)),
 created_at DATETIME(6) NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 deleted_at DATETIME(6) NULL,
 UNIQUE KEY de_wf_rule_tenant(tenant_id,id),
 UNIQUE KEY de_wf_rule_parent(tenant_id,scenario_id,id),
 KEY de_wf_rule_active(tenant_id,scenario_id,deleted_at,priority,created_at,id),
 FOREIGN KEY(tenant_id,scenario_id) REFERENCES {{database}}.decision_engine_scenarios(tenant_id,id) ON DELETE CASCADE,
 CHECK(LOCATE(X'00',CAST(name AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
