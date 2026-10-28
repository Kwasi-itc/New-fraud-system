CREATE TABLE {{database}}.data_model_field_enum_values (
 id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, field_id BINARY(16) NOT NULL,
 value LONGTEXT NOT NULL, label LONGTEXT NOT NULL, sort_order INT NOT NULL DEFAULT 0,
 created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
 value_key_id BINARY(16) NULL UNIQUE CHECK (value_key_id IS NOT NULL),
 label_key_id BINARY(16) NULL UNIQUE CHECK (label_key_id IS NOT NULL),
 KEY data_model_enum_sort (tenant_id,field_id,sort_order,id), KEY data_model_enum_created (field_id,sort_order,created_at),
 KEY data_model_enum_owner_created (tenant_id,field_id,sort_order,created_at,id),
 FOREIGN KEY (tenant_id,field_id) REFERENCES {{database}}.data_model_fields(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY (value_key_id) REFERENCES {{database}}.data_model_exact_keys(id),
 FOREIGN KEY (label_key_id) REFERENCES {{database}}.data_model_exact_keys(id)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
