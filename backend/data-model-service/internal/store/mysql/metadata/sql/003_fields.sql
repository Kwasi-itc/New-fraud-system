CREATE TABLE {{database}}.data_model_fields (
 id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL,
 name LONGTEXT NOT NULL, description LONGTEXT NOT NULL DEFAULT (''), alias LONGTEXT NOT NULL DEFAULT (''),
 data_type VARCHAR(20) NOT NULL CHECK (data_type IN ('bool','int','float','string','timestamp','ip_address')),
 nullable BOOLEAN NOT NULL DEFAULT FALSE CHECK (nullable IN (0,1)),
 is_enum BOOLEAN NOT NULL DEFAULT FALSE CHECK (is_enum IN (0,1)),
 is_unique BOOLEAN NOT NULL DEFAULT FALSE CHECK (is_unique IN (0,1)),
 archived BOOLEAN NOT NULL DEFAULT FALSE CHECK (archived IN (0,1)),
 created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
 name_key_id BINARY(16) NULL UNIQUE CHECK (name_key_id IS NOT NULL),
 UNIQUE (tenant_id,id), UNIQUE (tenant_id,table_id,id),
 KEY data_model_fields_created (tenant_id,table_id,created_at,id),
 FOREIGN KEY (tenant_id,table_id) REFERENCES {{database}}.data_model_tables(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY (name_key_id) REFERENCES {{database}}.data_model_exact_keys(id)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
