CREATE TABLE {{database}}.data_model_schema_change_intents (
 tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL, operation_id BINARY(16) NOT NULL,
 request_hash BINARY(32) NOT NULL,
 actor_kind VARCHAR(9) CHARACTER SET ascii COLLATE ascii_bin NOT NULL
   CHECK (actor_kind IN ('api','worker','reconcile','migrate')),
 actor_id VARCHAR(512) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL,
 expected_epoch BIGINT NOT NULL CHECK (expected_epoch>=0),
 expected_version BINARY(16) NULL,
 desired_shape JSON NOT NULL CHECK (JSON_TYPE(desired_shape)='OBJECT'),
 state VARCHAR(10) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'prepared'
   CHECK (state IN ('prepared','applying','verifying','blocked','published')),
 last_error_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
 created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
 PRIMARY KEY (tenant_id,operation_id), UNIQUE (tenant_id,table_id,operation_id),
 KEY data_model_schema_change_reconcile (tenant_id,state,operation_id),
 FOREIGN KEY (tenant_id,table_id) REFERENCES {{database}}.data_model_physical_tables(tenant_id,table_id) ON DELETE RESTRICT
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
