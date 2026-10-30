CREATE TABLE {{database}}.decision_engine_phantom_rule_executions (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 scenario_iteration_id BINARY(16) NOT NULL,
 phantom_decision_id BINARY(16) NOT NULL,
 rule_id BINARY(16) NOT NULL,
 rule_name LONGTEXT NOT NULL,
 outcome LONGTEXT NOT NULL,
 score_modifier INT NOT NULL DEFAULT 0,
 created_at DATETIME(6) NOT NULL,
 KEY de_phantom_evidence (tenant_id,phantom_decision_id,created_at,id),
 CONSTRAINT de_phantom_evidence_parent FOREIGN KEY(tenant_id,phantom_decision_id,scenario_iteration_id)
 REFERENCES {{database}}.decision_engine_phantom_decisions(tenant_id,id,scenario_iteration_id) ON DELETE CASCADE,
 CONSTRAINT de_phantom_evidence_rule FOREIGN KEY(tenant_id,scenario_iteration_id,rule_id)
 REFERENCES {{database}}.decision_engine_rules(tenant_id,iteration_id,id) ON DELETE CASCADE,
 CONSTRAINT de_phantom_evidence_name CHECK (LOCATE(X'00',CAST(rule_name AS BINARY))=0),
 CONSTRAINT de_phantom_evidence_outcome CHECK (LOCATE(X'00',CAST(outcome AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
