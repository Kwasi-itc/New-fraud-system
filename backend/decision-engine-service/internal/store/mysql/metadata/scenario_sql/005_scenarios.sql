CREATE TABLE {{database}}.decision_engine_scenarios (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 name LONGTEXT NOT NULL,
 description LONGTEXT NOT NULL DEFAULT (''),
 trigger_object_type LONGTEXT NOT NULL,
 created_at DATETIME(6) NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 name_key_id BINARY(16) NULL,
 CONSTRAINT de_scenario_name_bound CHECK (name_key_id IS NOT NULL),
 CONSTRAINT de_scenario_name_text CHECK (LOCATE(X'00',CAST(name AS BINARY))=0),
 CONSTRAINT de_scenario_desc_text CHECK (LOCATE(X'00',CAST(description AS BINARY))=0),
 CONSTRAINT de_scenario_type_text CHECK (LOCATE(X'00',CAST(trigger_object_type AS BINARY))=0),
 UNIQUE KEY de_scenario_tenant (tenant_id,id),
 UNIQUE KEY de_scenario_name (tenant_id,name_key_id),
 KEY de_scenario_list (tenant_id,created_at DESC,id DESC),
 CONSTRAINT de_scenario_key FOREIGN KEY(name_key_id) REFERENCES {{database}}.decision_engine_exact_keys(id)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
