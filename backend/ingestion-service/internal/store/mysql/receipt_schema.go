package mysql

import "github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"

// ReceiptSchema returns staged owner DDL, not a deployment migration. Statements
// must be installed through reviewed checksum/dirty-DDL lineage before runtime
// adoption. All objects belong to the caller's one shared application database.
// The owner account requires INSERT/SELECT receipts (no UPDATE/DELETE), and
// INSERT plus SELECT/UPDATE(key_digest) on buckets. Retention is not yet implemented.
func ReceiptSchema(database string) ([]string, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	return []string{
		"CREATE TABLE " + schema + ".ingestion_receipt_buckets (tenant_id BINARY(16) NOT NULL,key_digest BINARY(32) NOT NULL,PRIMARY KEY(tenant_id,key_digest)) ENGINE=InnoDB",
		"CREATE TABLE " + schema + ".ingestion_request_receipts (tenant_id BINARY(16) NOT NULL,key_digest BINARY(32) NOT NULL,slot SMALLINT UNSIGNED NOT NULL,request_key LONGBLOB NOT NULL,table_id BINARY(16) NOT NULL,mode VARBINARY(6) NOT NULL,response_kind VARBINARY(6) NOT NULL,request_hash BINARY(32) NOT NULL,created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),response_payload LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL,PRIMARY KEY(tenant_id,key_digest,slot),CONSTRAINT ingestion_receipt_bucket_fk FOREIGN KEY(tenant_id,key_digest) REFERENCES " + schema + ".ingestion_receipt_buckets(tenant_id,key_digest),CHECK(OCTET_LENGTH(request_key)>0),CHECK(mode IN ('create','patch')),CHECK(response_kind IN ('single','batch')),CHECK(JSON_VALID(response_payload))) ENGINE=InnoDB",
	}, nil
}
