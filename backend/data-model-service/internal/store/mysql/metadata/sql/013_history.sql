CREATE TABLE {{database}}.data_model_schema_change_log (
 id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, operation LONGTEXT NOT NULL,
 resource_type LONGTEXT NOT NULL, resource_id BINARY(16) NOT NULL, status LONGTEXT NOT NULL,
 details JSON NOT NULL DEFAULT (JSON_OBJECT()), created_at DATETIME(6) NOT NULL,
 KEY data_model_history_tenant (tenant_id,created_at DESC,id DESC),
 FOREIGN KEY (tenant_id) REFERENCES {{database}}.data_model_tenants(id) ON DELETE CASCADE
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
