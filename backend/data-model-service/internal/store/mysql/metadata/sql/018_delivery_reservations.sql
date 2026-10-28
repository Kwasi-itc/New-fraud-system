CREATE TABLE {{database}}.data_model_index_delivery_reservations (
 tenant_id BINARY(16) NOT NULL, job_id BINARY(16) NOT NULL, delivery_id BINARY(16) NOT NULL,
 PRIMARY KEY (tenant_id,job_id), UNIQUE (delivery_id),
 FOREIGN KEY (tenant_id,job_id,delivery_id) REFERENCES {{database}}.data_model_index_deliveries(tenant_id,job_id,id) ON DELETE RESTRICT
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
