CREATE TABLE {{database}}.data_model_publication_operations (
 tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL, operation_id BINARY(16) NOT NULL,
 request_hash BINARY(32) NOT NULL, condition_mode VARCHAR(8) NOT NULL CHECK (condition_mode IN ('any','absent','exact')),
 expected_version BINARY(16) NOT NULL, version BINARY(16) NOT NULL, payload JSON NOT NULL,
 PRIMARY KEY (tenant_id,table_id,operation_id),
 FOREIGN KEY (tenant_id,table_id) REFERENCES {{database}}.data_model_tables(tenant_id,id) ON DELETE RESTRICT
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
