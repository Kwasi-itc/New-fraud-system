CREATE TABLE {{database}}.data_model_tenant_schema_migrations (
 id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, version LONGTEXT NOT NULL,
 applied_at DATETIME(6) NOT NULL, KEY data_model_revisions_tenant (tenant_id,applied_at DESC,id DESC),
 FOREIGN KEY (tenant_id) REFERENCES {{database}}.data_model_tenants(id) ON DELETE CASCADE
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
