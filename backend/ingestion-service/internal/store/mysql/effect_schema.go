package mysql

import "github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"

// EffectSchema is staged DDL, not an accepted deployment migration. Stable table
// IDs replace text-prefix history indexes. Data-model lineage, installation and
// consumer/retention grants still require integration. Use one shared database.
func EffectSchema(database string) ([]string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	return []string{
		"CREATE TABLE " + s + `.ingestion_audit (
id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL,
object_type LONGTEXT NOT NULL, object_id LONGTEXT NOT NULL, mode VARBINARY(6) NOT NULL,
revision_id LONGTEXT NOT NULL, status VARBINARY(17) NOT NULL, payload JSON NOT NULL,
validation_errors JSON NOT NULL, idempotency_key LONGBLOB NULL, created_at DATETIME(6) NOT NULL,
INDEX ingestion_audit_history (tenant_id,table_id,created_at DESC,id),
CHECK(mode IN ('create','patch')), CHECK(status IN ('succeeded','validation_failed')),
CHECK(JSON_TYPE(payload)='OBJECT'), CHECK(JSON_TYPE(validation_errors)='ARRAY')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`,
		"CREATE TABLE " + s + `.ingestion_outbox_events (
id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL,
event_type VARBINARY(25) NOT NULL, aggregate_type LONGTEXT NOT NULL,
aggregate_key LONGTEXT NOT NULL, payload JSON NOT NULL, status VARBINARY(7) NOT NULL,
created_at DATETIME(6) NOT NULL,
INDEX ingestion_outbox_pending (status,created_at,id),
INDEX ingestion_outbox_history (tenant_id,table_id,created_at DESC,id),
CHECK(event_type IN ('record.ingested','record.updated','batch.ingestion.completed')),
CHECK(status='pending'), CHECK(JSON_TYPE(payload)='OBJECT')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`,
	}, nil
}
