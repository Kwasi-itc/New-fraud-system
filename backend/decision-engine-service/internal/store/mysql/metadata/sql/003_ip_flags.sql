CREATE TABLE {{database}}.decision_engine_ip_flags (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 ip_address LONGTEXT NOT NULL,
 flag LONGTEXT NOT NULL,
 created_at DATETIME(6) NOT NULL,
 ip_address_sha BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(CAST(ip_address AS BINARY),256))) STORED,
 flag_sha BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(CAST(flag AS BINARY),256))) STORED,
 CONSTRAINT de_ip_address_text CHECK (LOCATE(X'00',CAST(ip_address AS BINARY))=0),
 CONSTRAINT de_ip_flag_text CHECK (LOCATE(X'00',CAST(flag AS BINARY))=0),
 KEY de_ip_object (tenant_id,ip_address_sha,created_at DESC,id DESC),
 KEY de_ip_value (tenant_id,ip_address_sha,flag_sha)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
