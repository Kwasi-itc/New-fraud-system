CREATE TABLE {{database}}.data_model_physical_tables (
 tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL,
 tenant_label VARCHAR(10) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 table_label VARCHAR(12) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 physical_name VARCHAR(63) CHARACTER SET ascii COLLATE ascii_bin
   GENERATED ALWAYS AS (CONCAT('tenant_',tenant_label,'_',table_label,'_',LOWER(HEX(table_id)))) STORED,
 published_version BINARY(16) NULL,
 epoch BIGINT NOT NULL DEFAULT 0 CHECK (epoch>=0),
 write_state VARCHAR(8) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'closed'
   CHECK (write_state IN ('closed','open','retired')),
 active_operation BINARY(16) NULL,
 PRIMARY KEY (tenant_id,table_id), UNIQUE (table_id), UNIQUE (physical_name),
 FOREIGN KEY (tenant_id,table_id) REFERENCES {{database}}.data_model_tables(tenant_id,id) ON DELETE RESTRICT,
 CHECK (write_state<>'open' OR (published_version IS NOT NULL AND active_operation IS NULL)),
 CHECK (write_state<>'retired' OR active_operation IS NULL)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
