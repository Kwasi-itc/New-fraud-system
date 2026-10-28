CREATE TABLE {{database}}.data_model_display_table_references (
 tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL, parent_table_id BINARY(16) NOT NULL,
 PRIMARY KEY (tenant_id,table_id,parent_table_id),
 KEY data_model_display_table_dependents (tenant_id,parent_table_id,table_id),
 FOREIGN KEY (tenant_id,table_id) REFERENCES {{database}}.data_model_tables(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,parent_table_id) REFERENCES {{database}}.data_model_tables(tenant_id,id) ON DELETE RESTRICT
) ENGINE=InnoDB
