CREATE TABLE {{database}}.data_model_links (
 id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, name LONGTEXT NOT NULL,
 parent_table_id BINARY(16) NOT NULL, parent_field_id BINARY(16) NOT NULL,
 child_table_id BINARY(16) NOT NULL, child_field_id BINARY(16) NOT NULL, created_at DATETIME(6) NOT NULL,
 name_key_id BINARY(16) NULL UNIQUE CHECK (name_key_id IS NOT NULL), UNIQUE (tenant_id,id),
 KEY data_model_links_created (tenant_id,created_at,id),
 FOREIGN KEY (tenant_id,parent_table_id,parent_field_id) REFERENCES {{database}}.data_model_fields(tenant_id,table_id,id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,child_table_id,child_field_id) REFERENCES {{database}}.data_model_fields(tenant_id,table_id,id) ON DELETE CASCADE,
 FOREIGN KEY (name_key_id) REFERENCES {{database}}.data_model_exact_keys(id)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
