CREATE TABLE {{database}}.data_model_navigation_options (
 id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL,
 source_table_id BINARY(16) NOT NULL, source_field_id BINARY(16) NOT NULL,
 target_table_id BINARY(16) NOT NULL, filter_field_id BINARY(16) NOT NULL, ordering_field_id BINARY(16) NOT NULL,
 created_at DATETIME(6) NOT NULL, KEY data_model_navigation_source (tenant_id,source_table_id,created_at,id),
 KEY data_model_navigation_created (tenant_id,created_at,id),
 FOREIGN KEY (tenant_id,source_table_id,source_field_id) REFERENCES {{database}}.data_model_fields(tenant_id,table_id,id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,target_table_id,filter_field_id) REFERENCES {{database}}.data_model_fields(tenant_id,table_id,id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,target_table_id,ordering_field_id) REFERENCES {{database}}.data_model_fields(tenant_id,table_id,id) ON DELETE CASCADE
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
