CREATE TABLE {{database}}.decision_engine_rule_executions (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 scenario_iteration_id BINARY(16) NOT NULL,
 decision_id BINARY(16) NOT NULL,
 rule_id BINARY(16) NOT NULL,
 rule_name LONGTEXT NOT NULL,
 outcome LONGTEXT NOT NULL,
 score_modifier INT NOT NULL DEFAULT 0,
 evaluation JSON NULL,
 created_at DATETIME(6) NOT NULL,
 KEY de_evidence_decision (tenant_id,decision_id,created_at,id),
 CONSTRAINT de_evidence_decision_fk FOREIGN KEY(tenant_id,decision_id,scenario_iteration_id)
 REFERENCES {{database}}.decision_engine_decisions(tenant_id,id,scenario_iteration_id) ON DELETE CASCADE,
 CONSTRAINT de_evidence_rule_fk FOREIGN KEY(tenant_id,scenario_iteration_id,rule_id)
 REFERENCES {{database}}.decision_engine_rules(tenant_id,iteration_id,id) ON DELETE CASCADE,
 CONSTRAINT de_evidence_name_text CHECK (LOCATE(X'00',CAST(rule_name AS BINARY))=0),
 CONSTRAINT de_evidence_outcome_text CHECK (LOCATE(X'00',CAST(outcome AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
