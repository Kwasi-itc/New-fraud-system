BEGIN;
SET LOCAL lock_timeout='5s';
DROP VIEW screening.source_readiness_v1;
DROP TABLE screening.source_monitor_audit,screening.source_import_runs,screening.source_connections,screening.source_identifiers;
ALTER TABLE screening.source_versions DROP COLUMN source_key,DROP COLUMN parser_version;
-- Downgrade must refuse incompatible retained data rather than delete it.
ALTER TABLE screening.source_records DROP CONSTRAINT source_records_entity_type_check;
ALTER TABLE screening.source_records ADD CONSTRAINT source_records_entity_type_check CHECK(entity_type IN ('Person','Organization'));
ALTER TABLE screening.source_datasets DROP CONSTRAINT source_datasets_category_check;
ALTER TABLE screening.source_datasets ADD CONSTRAINT source_datasets_category_check CHECK(category IN ('controlled_fixture','tenant_watchlist'));
COMMIT;
