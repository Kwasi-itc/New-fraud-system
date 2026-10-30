CREATE TABLE {{database}}.decision_engine_exact_key_buckets (
 key_domain VARBINARY(64) NOT NULL,
 scope_bytes VARBINARY(32) NOT NULL,
 bucket_hash BINARY(32) NOT NULL,
 PRIMARY KEY (key_domain,scope_bytes,bucket_hash),
 CONSTRAINT de_bucket_domain CHECK (OCTET_LENGTH(key_domain)>0),
 CONSTRAINT de_bucket_scope CHECK (OCTET_LENGTH(scope_bytes) IN (16,32))
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
