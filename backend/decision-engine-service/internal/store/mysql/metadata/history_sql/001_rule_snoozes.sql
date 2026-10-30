CREATE TABLE {{database}}.decision_engine_rule_snoozes (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 scenario_id BINARY(16) NOT NULL,
 object_type LONGTEXT NOT NULL,
 object_id LONGTEXT NOT NULL,
 snooze_group_id BINARY(16) NOT NULL,
 created_at DATETIME(6) NOT NULL,
 expires_at DATETIME(6) NOT NULL,
 object_type_sha BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(CAST(object_type AS BINARY),256))) STORED,
 object_id_sha BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(CAST(object_id AS BINARY),256))) STORED,
 KEY de_snooze_lookup (tenant_id,scenario_id,object_type_sha,object_id_sha,expires_at DESC,id DESC),
 CONSTRAINT de_snooze_scenario FOREIGN KEY(tenant_id,scenario_id)
 REFERENCES {{database}}.decision_engine_scenarios(tenant_id,id) ON DELETE CASCADE,
 CONSTRAINT de_snooze_type_text CHECK (LOCATE(X'00',CAST(object_type AS BINARY))=0),
 CONSTRAINT de_snooze_id_text CHECK (LOCATE(X'00',CAST(object_id AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
