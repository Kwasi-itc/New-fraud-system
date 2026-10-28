CREATE TABLE {{database}}.data_model_pivots (
 id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, base_table_id BINARY(16) NOT NULL,
 field_id BINARY(16) NULL, created_at DATETIME(6) NOT NULL, UNIQUE (tenant_id,id),
 KEY data_model_pivots_created (tenant_id,created_at,id),
 FOREIGN KEY (tenant_id,base_table_id) REFERENCES {{database}}.data_model_tables(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,base_table_id,field_id) REFERENCES {{database}}.data_model_fields(tenant_id,table_id,id) ON DELETE CASCADE
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
