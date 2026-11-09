// Package localdelivery owns durable local development callback/event inboxes
// and deterministic scoring receipts. It is not a production provider adapter.
package localdelivery

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

const Owner = "decision_local"

func Statements(database, definer string) ([]migrations.Step, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	u, err := mysqlstore.Identifier(definer)
	if err != nil {
		return nil, err
	}
	table := "decision_local_delivery_receipts"
	out := []migrations.Step{{ID: "decision_local_v001_0001", Kind: "TABLE", Name: table, SQL: "CREATE TABLE " + s + "." + table + `(
 tenant_id BINARY(16) NOT NULL,kind VARBINARY(40) NOT NULL,resource_id BINARY(16) NOT NULL,
 domain_attempt INT NOT NULL CHECK(domain_attempt>0),redrive_generation INT NOT NULL CHECK(redrive_generation>=0),
 provider_key VARBINARY(76) NOT NULL,payload_hash BINARY(32) NOT NULL,reference BINARY(16) NOT NULL,
 payload LONGBLOB NOT NULL CHECK(OCTET_LENGTH(payload)<=1048576 AND JSON_VALID(CONVERT(payload USING utf8mb4))),
 result JSON NOT NULL,outcome VARBINARY(16) NOT NULL CHECK(outcome IN('accepted','rejected')),
 disposition VARBINARY(16) NOT NULL DEFAULT 'pending' CHECK(disposition IN('pending','applied','superseded')),
 created_at DATETIME(6) NOT NULL,applied_at DATETIME(6),
 PRIMARY KEY(tenant_id,kind,resource_id,domain_attempt,redrive_generation),
 UNIQUE KEY local_delivery_key(tenant_id,provider_key),
 KEY local_delivery_pending(tenant_id,kind,outcome,disposition,created_at,resource_id),
 FOREIGN KEY(tenant_id) REFERENCES ` + s + `.data_model_tenants(id)
 ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`}}
	for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
		body := "IF @@session.foreign_key_checks<>1 OR @@session.unique_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='local delivery integrity required'; END IF; "
		if event == "INSERT" {
			body += "IF NEW.payload_hash<>UNHEX(SHA2(NEW.payload,256)) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='delivery payload hash mismatch'; END IF; "
		}
		if event == "UPDATE" {
			for _, col := range []string{"tenant_id", "kind", "resource_id", "domain_attempt", "redrive_generation", "provider_key", "payload_hash", "reference", "payload", "result", "outcome", "created_at"} {
				body += "IF NOT(NEW." + col + "<=>OLD." + col + ") THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable local delivery receipt'; END IF; "
			}
		}
		if event == "DELETE" {
			body += "SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='permanent local delivery receipt'; "
		}
		name := map[string]string{"INSERT": "local_delivery_insert", "UPDATE": "local_delivery_update", "DELETE": "local_delivery_delete"}[event]
		out = append(out, migrations.Step{ID: "decision_local_v001_000" + map[string]string{"INSERT": "2", "UPDATE": "3", "DELETE": "4"}[event], Kind: "TRIGGER", Name: name, SQL: "CREATE DEFINER=" + u + "@`localhost` TRIGGER " + s + "." + name + " BEFORE " + event + " ON " + s + "." + table + " FOR EACH ROW BEGIN " + body + " END"})
	}
	// Append to the retained v1 lineage; do not rewrite its original definition.
	out = append(out, migrations.Step{ID: "decision_local_v002_0001", Kind: "TABLE", Name: table, SQL: "ALTER TABLE " + s + "." + table + " DROP CHECK decision_local_delivery_receipts_chk_3, ADD CONSTRAINT local_delivery_payload_v2 CHECK(OCTET_LENGTH(payload)<=4194304 AND JSON_VALID(CONVERT(payload USING utf8mb4)))"})
	return out, nil
}
