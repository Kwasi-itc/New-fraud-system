CREATE TABLE {{database}}.data_model_physical_fields (
 tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL, field_id BINARY(16) NOT NULL,
 component VARCHAR(7) CHARACTER SET ascii COLLATE ascii_bin NOT NULL
   CHECK (component IN ('value','family','address','prefix')),
 field_label VARCHAR(12) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 physical_name VARCHAR(59) CHARACTER SET ascii COLLATE ascii_bin
   GENERATED ALWAYS AS (CONCAT('field_',field_label,'_',LOWER(HEX(field_id)),'_',component)) STORED,
 PRIMARY KEY (tenant_id,table_id,field_id,component),
 UNIQUE (tenant_id,table_id,physical_name),
 FOREIGN KEY (tenant_id,table_id) REFERENCES {{database}}.data_model_physical_tables(tenant_id,table_id) ON DELETE RESTRICT,
 FOREIGN KEY (tenant_id,table_id,field_id) REFERENCES {{database}}.data_model_fields(tenant_id,table_id,id) ON DELETE RESTRICT
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
