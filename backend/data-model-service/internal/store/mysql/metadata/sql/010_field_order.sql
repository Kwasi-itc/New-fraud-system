CREATE TABLE {{database}}.data_model_field_order (
 tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL,
 ordinal INT NOT NULL CHECK (ordinal>=0), field_id BINARY(16) NOT NULL,
 PRIMARY KEY (tenant_id,table_id,ordinal), UNIQUE (tenant_id,table_id,field_id),
 FOREIGN KEY (tenant_id,table_id) REFERENCES {{database}}.data_model_table_options(tenant_id,table_id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,table_id,field_id) REFERENCES {{database}}.data_model_fields(tenant_id,table_id,id) ON DELETE CASCADE
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
