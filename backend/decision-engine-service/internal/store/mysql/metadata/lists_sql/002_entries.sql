CREATE TABLE {{database}}.decision_engine_custom_list_entries (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 list_id BINARY(16) NULL,
 list_name LONGTEXT NOT NULL,
 value LONGTEXT NOT NULL,
 created_at DATETIME(6) NOT NULL,
 list_fold_sha BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(CAST(LOWER(list_name COLLATE utf8mb4_0900_as_cs) AS BINARY),256))) STORED,
 value_sha BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(CAST(value AS BINARY),256))) STORED,
 KEY de_entry_list(tenant_id,list_id,created_at DESC,id DESC),
 KEY de_entry_value(tenant_id,list_id,value_sha),
 KEY de_entry_legacy(tenant_id,list_id,list_fold_sha,value_sha),
 FOREIGN KEY(tenant_id,list_id) REFERENCES {{database}}.decision_engine_custom_lists(tenant_id,id) ON DELETE CASCADE,
 CHECK(LOCATE(X'00',CAST(list_name AS BINARY))=0),
 CHECK(LOCATE(X'00',CAST(value AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
