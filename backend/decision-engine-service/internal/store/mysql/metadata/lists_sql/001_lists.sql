CREATE TABLE {{database}}.decision_engine_custom_lists (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 name LONGTEXT NOT NULL,
 description LONGTEXT NOT NULL DEFAULT (''),
 kind LONGTEXT NOT NULL DEFAULT ('generic_text'),
 created_at DATETIME(6) NOT NULL,
 updated_at DATETIME(6) NOT NULL,
 name_key_id BINARY(16) NULL CHECK(name_key_id IS NOT NULL),
 name_fold_sha BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(CAST(LOWER(name COLLATE utf8mb4_0900_as_cs) AS BINARY),256))) STORED,
 UNIQUE KEY de_list_tenant(tenant_id,id),
 UNIQUE KEY de_list_name(tenant_id,name_key_id),
 KEY de_list_lookup(tenant_id,name_fold_sha),
 KEY de_list_history(tenant_id,updated_at DESC,created_at DESC,id DESC),
 FOREIGN KEY(name_key_id) REFERENCES {{database}}.decision_engine_exact_keys(id),
 CHECK(LOCATE(X'00',CAST(name AS BINARY))=0),
 CHECK(LOCATE(X'00',CAST(description AS BINARY))=0),
 CHECK(LOCATE(X'00',CAST(kind AS BINARY))=0)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
