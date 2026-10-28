CREATE TABLE {{database}}.data_model_schema_change_steps (
 tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL,
 operation_id BINARY(16) NOT NULL,
 step_no INT NOT NULL CHECK (step_no BETWEEN 1 AND 1000),
 checksum BINARY(32) NOT NULL,
 state VARCHAR(7) CHARACTER SET ascii COLLATE ascii_bin NOT NULL
   CHECK (state IN ('dirty','applied')),
 started_at DATETIME(6) NOT NULL, applied_at DATETIME(6) NULL,
 PRIMARY KEY (tenant_id,operation_id,step_no),
 FOREIGN KEY (tenant_id,table_id,operation_id)
   REFERENCES {{database}}.data_model_schema_change_intents(tenant_id,table_id,operation_id) ON DELETE RESTRICT,
 CHECK ((state='dirty' AND applied_at IS NULL) OR (state='applied' AND applied_at IS NOT NULL))
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
