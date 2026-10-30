CREATE TABLE {{database}}.decision_engine_record_tags (
 id BINARY(16) PRIMARY KEY,
 tenant_id BINARY(16) NOT NULL,
 object_type LONGTEXT NOT NULL,
 object_id LONGTEXT NOT NULL,
 tag LONGTEXT NOT NULL,
 created_at DATETIME(6) NOT NULL,
 object_type_sha BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(CAST(object_type AS BINARY),256))) STORED,
 object_id_sha BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(CAST(object_id AS BINARY),256))) STORED,
 tag_sha BINARY(32) GENERATED ALWAYS AS (UNHEX(SHA2(CAST(tag AS BINARY),256))) STORED,
 CONSTRAINT de_record_tags_type_text CHECK (LOCATE(X'00',CAST(object_type AS BINARY))=0),
 CONSTRAINT de_record_tags_id_text CHECK (LOCATE(X'00',CAST(object_id AS BINARY))=0),
 CONSTRAINT de_record_tags_tag_text CHECK (LOCATE(X'00',CAST(tag AS BINARY))=0),
 KEY de_record_tags_object (tenant_id,object_type_sha,object_id_sha,created_at DESC,id DESC),
 KEY de_record_tags_value (tenant_id,object_type_sha,object_id_sha,tag_sha)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
