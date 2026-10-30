CREATE TABLE {{database}}.decision_engine_workflow_conditions (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 rule_id BINARY(16) NOT NULL,
 `function` LONGTEXT NOT NULL,
 params JSON NOT NULL DEFAULT (JSON_OBJECT()),
 created_at DATETIME(6) NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 UNIQUE KEY de_wf_condition_tenant(tenant_id,id),
 KEY de_wf_condition_order(tenant_id,rule_id,created_at,id),
 FOREIGN KEY(tenant_id,rule_id) REFERENCES {{database}}.decision_engine_workflow_rules(tenant_id,id) ON DELETE CASCADE,
 CHECK(LOCATE(X'00',CAST(`function` AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
