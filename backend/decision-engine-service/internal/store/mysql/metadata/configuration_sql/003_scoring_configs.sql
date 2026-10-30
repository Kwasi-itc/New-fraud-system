CREATE TABLE {{database}}.decision_engine_scoring_configs (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 scenario_id BINARY(16) NOT NULL,
 name LONGTEXT NOT NULL,
 ruleset_ref LONGTEXT NOT NULL,
 config_json JSON NOT NULL DEFAULT (JSON_OBJECT()),
 active BOOLEAN NOT NULL DEFAULT TRUE CHECK(active IN (0,1)),
 created_at DATETIME(6) NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 UNIQUE KEY de_score_config_tenant(tenant_id,id),
 UNIQUE KEY de_score_config_parent(tenant_id,scenario_id,id),
 KEY de_score_config_history(tenant_id,scenario_id,created_at DESC,id DESC),
 FOREIGN KEY(tenant_id,scenario_id) REFERENCES {{database}}.decision_engine_scenarios(tenant_id,id) ON DELETE CASCADE,
 CHECK(LOCATE(X'00',CAST(name AS BINARY))=0),
 CHECK(LOCATE(X'00',CAST(ruleset_ref AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
