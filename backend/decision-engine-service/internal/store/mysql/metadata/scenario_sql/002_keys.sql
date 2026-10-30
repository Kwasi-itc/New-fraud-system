CREATE TABLE {{database}}.decision_engine_exact_keys (
 id BINARY(16) PRIMARY KEY,
 key_domain VARBINARY(64) NOT NULL,
 scope_bytes VARBINARY(32) NOT NULL,
 bucket_hash BINARY(32) NOT NULL,
 raw_value LONGBLOB NOT NULL,
 KEY de_exact_lookup (key_domain,scope_bytes,bucket_hash),
 CONSTRAINT de_exact_bucket FOREIGN KEY (key_domain,scope_bytes,bucket_hash)
 REFERENCES {{database}}.decision_engine_exact_key_buckets(key_domain,scope_bytes,bucket_hash)
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
