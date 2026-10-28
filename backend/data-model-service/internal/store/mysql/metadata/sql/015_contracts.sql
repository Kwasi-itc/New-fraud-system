CREATE TABLE {{database}}.data_model_ingestion_contracts (
 table_id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, table_name LONGTEXT NOT NULL,
 version BINARY(16) NOT NULL, payload JSON NOT NULL,
 table_name_key_id BINARY(16) NULL UNIQUE CHECK (table_name_key_id IS NOT NULL),
 UNIQUE (tenant_id,table_id),
 FOREIGN KEY (tenant_id,table_id) REFERENCES {{database}}.data_model_tables(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY (table_name_key_id) REFERENCES {{database}}.data_model_exact_keys(id)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
