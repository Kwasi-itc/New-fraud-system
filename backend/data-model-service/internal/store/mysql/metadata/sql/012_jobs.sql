CREATE TABLE {{database}}.data_model_index_jobs (
 id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NULL,
 UNIQUE (tenant_id,id),
 table_name LONGTEXT NOT NULL, index_type VARCHAR(20) NOT NULL CHECK (index_type IN ('search','navigation','repair')),
 columns_json JSON NOT NULL CHECK (JSON_TYPE(columns_json)='ARRAY'),
 status VARCHAR(20) NOT NULL CHECK (status IN ('pending','running','applied','failed','cancelled')),
 requested_by_operation LONGTEXT NOT NULL DEFAULT (''), error_message LONGTEXT NULL,
 attempt_count INT NOT NULL DEFAULT 0 CHECK (attempt_count>=0),
 requested_at DATETIME(6) NOT NULL, started_at DATETIME(6) NULL, completed_at DATETIME(6) NULL, scheduled_at DATETIME(6) NULL,
 dedupe_key LONGTEXT NOT NULL DEFAULT (''), dedupe_key_id BINARY(16) NULL UNIQUE,
 dedupe_lookup_hash BINARY(32) GENERATED ALWAYS AS (CASE WHEN OCTET_LENGTH(dedupe_key)=0 THEN NULL ELSE UNHEX(SHA2(CAST(dedupe_key AS BINARY),256)) END) STORED,
 KEY data_model_jobs_dedupe_lookup (tenant_id,dedupe_lookup_hash),
 KEY data_model_jobs_schedule (status,scheduled_at,requested_at,id), KEY data_model_jobs_tenant (tenant_id,requested_at DESC,id DESC),
 FOREIGN KEY (tenant_id) REFERENCES {{database}}.data_model_tenants(id) ON DELETE CASCADE,
 FOREIGN KEY (tenant_id,table_id) REFERENCES {{database}}.data_model_tables(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY (dedupe_key_id) REFERENCES {{database}}.data_model_exact_keys(id)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
