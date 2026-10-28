CREATE TABLE {{database}}.data_model_pivot_path_links (
 tenant_id BINARY(16) NOT NULL, pivot_id BINARY(16) NOT NULL,
 ordinal INT NOT NULL CHECK (ordinal>=0), link_id BINARY(16) NOT NULL,
 PRIMARY KEY (tenant_id,pivot_id,ordinal),
 FOREIGN KEY (tenant_id,pivot_id) REFERENCES {{database}}.data_model_pivots(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,link_id) REFERENCES {{database}}.data_model_links(tenant_id,id) ON DELETE RESTRICT
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
