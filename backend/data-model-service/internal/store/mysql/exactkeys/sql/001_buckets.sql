CREATE TABLE {{database}}.data_model_exact_key_buckets (
  key_domain VARBINARY(64) NOT NULL,
  scope_bytes VARBINARY(32) NOT NULL,
  bucket_hash BINARY(32) NOT NULL,
  PRIMARY KEY (key_domain, scope_bytes, bucket_hash),
  CONSTRAINT data_model_exact_bucket_domain CHECK (OCTET_LENGTH(key_domain) > 0),
  CONSTRAINT data_model_exact_bucket_scope CHECK (OCTET_LENGTH(scope_bytes) IN (0,16,32))
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
