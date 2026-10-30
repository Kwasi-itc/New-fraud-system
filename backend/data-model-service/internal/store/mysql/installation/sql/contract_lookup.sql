CREATE DEFINER={{definer}} FUNCTION {{database}}.data_model_find_ingestion_contract_key_v1(
  p_tenant BINARY(16), p_value LONGBLOB
) RETURNS BINARY(16)
NOT DETERMINISTIC READS SQL DATA SQL SECURITY DEFINER
BEGIN
  DECLARE v_key BINARY(16) DEFAULT NULL;
  DECLARE CONTINUE HANDLER FOR NOT FOUND SET v_key=NULL;
  IF p_tenant IS NULL OR p_value IS NULL
     OR p_tenant=UNHEX(REPEAT('00',16))
     OR OCTET_LENGTH(p_value)=0 OR OCTET_LENGTH(p_value)>1048576 THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='bounded contract lookup scope required';
  END IF;
  SELECT id INTO v_key FROM {{database}}.data_model_exact_keys
    WHERE key_domain=_binary'dm.contract.name'
      AND scope_bytes=p_tenant
      AND bucket_hash={{database}}.data_model_exact_key_digest(p_value)
      AND raw_value=p_value;
  RETURN v_key;
END
