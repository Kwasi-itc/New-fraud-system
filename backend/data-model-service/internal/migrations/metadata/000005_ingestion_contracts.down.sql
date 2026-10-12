-- Switch ingestion APIs/workers to HTTP mode before reverting this migration.
DROP VIEW core.ingestion_contracts_v1;
DROP VIEW core.ingestion_tenants_v1;
DROP TRIGGER publish_enum_contract ON core.field_enum_values;
DROP TRIGGER publish_field_contract ON core.model_fields;
DROP TRIGGER publish_table_contract ON core.model_tables;
DROP FUNCTION core.refresh_ingestion_contract();
DROP FUNCTION core.publish_ingestion_contract(UUID);
DROP TABLE core.ingestion_contracts;
