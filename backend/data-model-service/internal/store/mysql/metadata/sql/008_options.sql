CREATE TABLE {{database}}.data_model_table_options (
 id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL,
 field_display JSON NOT NULL DEFAULT (JSON_OBJECT()) CHECK (JSON_TYPE(field_display)='OBJECT'),
 display_columns JSON NOT NULL DEFAULT (JSON_ARRAY()) CHECK (JSON_TYPE(display_columns)='ARRAY'),
 updated_at DATETIME(6) NOT NULL, UNIQUE (table_id), UNIQUE (tenant_id,table_id),
 FOREIGN KEY (tenant_id,table_id) REFERENCES {{database}}.data_model_tables(tenant_id,id) ON DELETE CASCADE
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
