CREATE TABLE {{database}}.data_model_tables (
 id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL,
 name LONGTEXT NOT NULL, description LONGTEXT NOT NULL DEFAULT (''), alias LONGTEXT NOT NULL DEFAULT (''),
 semantic_type LONGTEXT NOT NULL DEFAULT (''), caption_field LONGTEXT NOT NULL DEFAULT (''),
 archived BOOLEAN NOT NULL DEFAULT FALSE CHECK (archived IN (0,1)),
 display_version BIGINT NOT NULL DEFAULT 0,
 created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
 name_key_id BINARY(16) NULL UNIQUE CHECK (name_key_id IS NOT NULL),
 UNIQUE (tenant_id,id),
 KEY data_model_tables_created (tenant_id,created_at,id),
 FOREIGN KEY (tenant_id) REFERENCES {{database}}.data_model_tenants(id) ON DELETE CASCADE,
 FOREIGN KEY (name_key_id) REFERENCES {{database}}.data_model_exact_keys(id)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
