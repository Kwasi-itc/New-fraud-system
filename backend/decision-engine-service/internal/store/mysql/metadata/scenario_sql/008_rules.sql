CREATE TABLE {{database}}.decision_engine_rules (
 id BINARY(16) PRIMARY KEY,
 iteration_id BINARY(16) NOT NULL,
 tenant_id BINARY(16) NOT NULL,
 display_order INT NOT NULL DEFAULT 0,
 name LONGTEXT NOT NULL,
 description LONGTEXT NOT NULL DEFAULT (''),
 formula JSON NOT NULL DEFAULT (JSON_OBJECT()),
 score_modifier INT NOT NULL DEFAULT 0,
 rule_group LONGTEXT NOT NULL DEFAULT (''),
 snooze_group_id BINARY(16) NULL,
 stable_rule_id LONGTEXT NOT NULL DEFAULT (''),
 created_at DATETIME(6) NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 UNIQUE KEY de_rule_binding (tenant_id,iteration_id,id),
 KEY de_rule_order (tenant_id,iteration_id,display_order,created_at,id),
 CONSTRAINT de_rule_iteration FOREIGN KEY(tenant_id,iteration_id)
 REFERENCES {{database}}.decision_engine_scenario_iterations(tenant_id,id) ON DELETE CASCADE,
 CONSTRAINT de_rule_name_text CHECK (LOCATE(X'00',CAST(name AS BINARY))=0),
 CONSTRAINT de_rule_desc_text CHECK (LOCATE(X'00',CAST(description AS BINARY))=0),
 CONSTRAINT de_rule_group_text CHECK (LOCATE(X'00',CAST(rule_group AS BINARY))=0),
 CONSTRAINT de_rule_stable_text CHECK (LOCATE(X'00',CAST(stable_rule_id AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
