CREATE TABLE {{database}}.data_model_display_field_references (
 tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL, field_id BINARY(16) NOT NULL,
 PRIMARY KEY (tenant_id,table_id,field_id),
 KEY data_model_display_field_dependents (tenant_id,field_id,table_id),
 FOREIGN KEY (tenant_id,table_id) REFERENCES {{database}}.data_model_tables(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,field_id) REFERENCES {{database}}.data_model_fields(tenant_id,id) ON DELETE RESTRICT
) ENGINE=InnoDB
