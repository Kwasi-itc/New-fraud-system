CREATE TABLE {{database}}.data_model_display_link_references (
 tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL, link_id BINARY(16) NOT NULL,
 PRIMARY KEY (tenant_id,table_id,link_id),
 KEY data_model_display_link_dependents (tenant_id,link_id,table_id),
 FOREIGN KEY (tenant_id,table_id) REFERENCES {{database}}.data_model_tables(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,link_id) REFERENCES {{database}}.data_model_links(tenant_id,id) ON DELETE RESTRICT
) ENGINE=InnoDB
