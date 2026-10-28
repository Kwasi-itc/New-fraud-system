CREATE TABLE {{database}}.data_model_tenants (
 id BINARY(16) PRIMARY KEY,
 external_key LONGTEXT NULL, name LONGTEXT NOT NULL, schema_name LONGTEXT NOT NULL,
 status VARCHAR(20) NOT NULL CHECK (status IN ('pending','active')),
 created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
 external_key_id BINARY(16) NULL UNIQUE,
 schema_name_key_id BINARY(16) NULL UNIQUE CHECK (schema_name_key_id IS NOT NULL),
 KEY data_model_tenants_created (created_at DESC,id DESC),
 FOREIGN KEY (external_key_id) REFERENCES {{database}}.data_model_exact_keys(id),
 FOREIGN KEY (schema_name_key_id) REFERENCES {{database}}.data_model_exact_keys(id)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
