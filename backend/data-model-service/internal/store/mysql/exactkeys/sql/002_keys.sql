CREATE TABLE {{database}}.data_model_exact_keys (
  id BINARY(16) NOT NULL,
  key_domain VARBINARY(64) NOT NULL,
  scope_bytes VARBINARY(32) NOT NULL,
  bucket_hash BINARY(32) NOT NULL,
  raw_value LONGBLOB NOT NULL,
  PRIMARY KEY (id),
  KEY data_model_exact_lookup (key_domain, scope_bytes, bucket_hash),
  CONSTRAINT data_model_exact_bucket_fk FOREIGN KEY (key_domain, scope_bytes, bucket_hash)
    REFERENCES {{database}}.data_model_exact_key_buckets (key_domain, scope_bytes, bucket_hash)
    ON UPDATE RESTRICT ON DELETE RESTRICT
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC
