CREATE DEFINER={{definer}} PROCEDURE {{database}}.decision_engine_resolve_exact_key(
 IN p_domain VARBINARY(64), IN p_scope VARBINARY(32),
 IN p_value LONGBLOB, OUT p_key BINARY(16)
)
MODIFIES SQL DATA SQL SECURITY DEFINER
BEGIN
 DECLARE v_digest BINARY(32);
 DECLARE v_locked BINARY(32);
 DECLARE CONTINUE HANDLER FOR NOT FOUND SET p_key=NULL;
 IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN
   SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision integrity checks must be enabled';
 END IF;
 IF p_domain IS NULL OR OCTET_LENGTH(p_domain)=0 OR p_scope IS NULL
    OR OCTET_LENGTH(p_scope) NOT IN (16,32) OR p_value IS NULL THEN
   SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='invalid decision exact key';
 END IF;
 SET p_key=NULL;
 SET v_digest={{database}}.decision_engine_exact_key_digest(p_value);
 IF v_digest IS NULL THEN
   SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='decision digest unavailable';
 END IF;
 -- Exactly one unique key: this upsert only locks the selected bucket.
 INSERT INTO {{database}}.decision_engine_exact_key_buckets(key_domain,scope_bytes,bucket_hash)
 VALUES(p_domain,p_scope,v_digest) ON DUPLICATE KEY UPDATE bucket_hash=bucket_hash;
 SELECT bucket_hash INTO v_locked FROM {{database}}.decision_engine_exact_key_buckets
 WHERE key_domain=p_domain AND scope_bytes=p_scope AND bucket_hash=v_digest FOR UPDATE;
 -- Current read even under an older RR snapshot. No LIMIT hiding corruption.
 SELECT id INTO p_key FROM {{database}}.decision_engine_exact_keys
 WHERE key_domain=p_domain AND scope_bytes=p_scope AND bucket_hash=v_digest AND raw_value=p_value FOR SHARE;
 IF p_key IS NULL THEN
   SET p_key=UUID_TO_BIN(UUID(),0);
   INSERT INTO {{database}}.decision_engine_exact_keys(id,key_domain,scope_bytes,bucket_hash,raw_value)
   VALUES(p_key,p_domain,p_scope,v_digest,p_value);
 END IF;
END
