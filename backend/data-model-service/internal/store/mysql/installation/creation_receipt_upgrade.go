package installation

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// CreationReceiptUpgradeStatements is append-only v003. Its separate locked
// definer does not change the frozen shared resolver's grants or prior history.
// Receipts retain allocated identities and original results after logical edits;
// deletion requires a future explicit retention protocol, never a cascade.
func CreationReceiptUpgradeStatements(database string, definer Definer) ([]migrations.Step, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	account, err := definer.SQLAccount()
	if err != nil {
		return nil, err
	}
	name := "data_model_table_creation_operations"
	table := schema + "." + name
	result := []migrations.Step{{ID: "data_model_v003_creation_receipts", Kind: "TABLE", Name: name, SQL: "CREATE TABLE " + table + ` (
 tenant_id BINARY(16) NOT NULL, operation_id BINARY(16) NOT NULL,
 table_id BINARY(16) NOT NULL, actor_id BINARY(16) NOT NULL,
 request_hash BINARY(32) NOT NULL, publication_hash BINARY(32) NOT NULL,
 original_snapshot JSON NOT NULL,
 created_at DATETIME(6) NOT NULL,
 PRIMARY KEY (tenant_id,operation_id), UNIQUE (tenant_id,table_id),
 CHECK (JSON_TYPE(original_snapshot)='OBJECT'),
 CHECK (OCTET_LENGTH(CAST(original_snapshot AS CHAR CHARACTER SET utf8mb4))<=1048576),
 FOREIGN KEY (tenant_id) REFERENCES ` + schema + `.data_model_tenants(id) ON DELETE RESTRICT
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC`}}
	for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
		body := "IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='creation receipt integrity checks must be enabled'; END IF; "
		if event == "UPDATE" {
			for _, column := range []string{"tenant_id", "operation_id", "table_id", "actor_id", "request_hash", "publication_hash", "original_snapshot", "created_at"} {
				body += fmt.Sprintf("IF NOT (BINARY NEW.%s <=> BINARY OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable creation receipt'; END IF; ", column, column)
			}
		}
		if event == "DELETE" {
			body += "SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='creation receipt retention protocol required'; "
		}
		trigger := name + "_" + strings.ToLower(event) + "_guard"
		result = append(result, migrations.Step{ID: "data_model_v003_creation_receipts_" + strings.ToLower(event), Kind: "TRIGGER", Name: trigger, SQL: "CREATE DEFINER=" + account + " TRIGGER " + schema + "." + trigger + " BEFORE " + event + " ON " + table + " FOR EACH ROW BEGIN " + body + "END"})
	}
	return result, nil
}

func CreationReceiptDefinerPolicy(definer Definer) AccountPolicy {
	return AccountPolicy{User: definer.User, Host: definer.Host, Locked: true, Objects: []ObjectGrant{{Kind: "TABLE", Name: "data_model_table_creation_operations", Privileges: []string{"SELECT", "TRIGGER"}}}}
}

func ExtendCreationReceiptPlan(installed migrations.Plan, definer Definer, reference []migrations.Step) (migrations.Plan, error) {
	if len(installed.Steps) == 0 || installed.Steps[len(installed.Steps)-1].ID != "data_model_v002_delivery_error_count" {
		return migrations.Plan{}, errors.New("creation receipts require the v002 lineage")
	}
	expected, err := CreationReceiptUpgradeStatements(installed.Database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	if len(reference) != len(expected) {
		return migrations.Plan{}, errors.New("complete creation receipt reference required")
	}
	for i, want := range expected {
		got := reference[i]
		if got.ID != want.ID || got.Kind != want.Kind || got.Name != want.Name || got.SQL != want.SQL || got.Before != "" {
			return migrations.Plan{}, errors.New("creation receipt reference differs from source")
		}
	}
	return ExtendPlan(installed, reference)
}
