package installation

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// OwnerResultUpgradeStatements is append-only v004. Its separate locked
// definer does not change the frozen shared resolver's grants or prior history.
// Receipts retain committed owner results after subsequent logical edits;
// deletion requires a future explicit retention protocol, never a cascade.
func OwnerResultUpgradeStatements(database string, definer Definer) ([]migrations.Step, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	account, err := definer.SQLAccount()
	if err != nil {
		return nil, err
	}
	name := "data_model_owner_results"
	table := schema + "." + name
	result := []migrations.Step{{ID: "data_model_v004_owner_results", Kind: "TABLE", Name: name, SQL: "CREATE TABLE " + table + ` (
 tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NOT NULL, operation_id BINARY(16) NOT NULL,
 result_kind VARBINARY(64) NOT NULL, original_result JSON NOT NULL, created_at DATETIME(6) NOT NULL,
 PRIMARY KEY (tenant_id,table_id,operation_id),
 CHECK (OCTET_LENGTH(result_kind)>0),
 CHECK (JSON_TYPE(original_result)='OBJECT'),
 CHECK (OCTET_LENGTH(CAST(original_result AS CHAR CHARACTER SET utf8mb4))<=1048576),
 FOREIGN KEY (tenant_id,table_id,operation_id) REFERENCES ` + schema + `.data_model_publication_operations(tenant_id,table_id,operation_id) ON DELETE RESTRICT
) ENGINE=InnoDB ROW_FORMAT=DYNAMIC`}}
	for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
		body := "IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='owner result integrity checks must be enabled'; END IF; "
		if event == "UPDATE" {
			for _, column := range []string{"tenant_id", "table_id", "operation_id", "result_kind", "original_result", "created_at"} {
				body += fmt.Sprintf("IF NOT (BINARY NEW.%s <=> BINARY OLD.%s) THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='immutable owner result'; END IF; ", column, column)
			}
		}
		if event == "DELETE" {
			body += "SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='owner result retention protocol required'; "
		}
		trigger := name + "_" + strings.ToLower(event) + "_guard"
		result = append(result, migrations.Step{ID: "data_model_v004_owner_results_" + strings.ToLower(event), Kind: "TRIGGER", Name: trigger, SQL: "CREATE DEFINER=" + account + " TRIGGER " + schema + "." + trigger + " BEFORE " + event + " ON " + table + " FOR EACH ROW BEGIN " + body + "END"})
	}
	return result, nil
}

func OwnerResultDefinerPolicy(definer Definer) AccountPolicy {
	return AccountPolicy{User: definer.User, Host: definer.Host, Locked: true, Objects: []ObjectGrant{{Kind: "TABLE", Name: "data_model_owner_results", Privileges: []string{"SELECT", "TRIGGER"}}}}
}

func ExtendOwnerResultPlan(installed migrations.Plan, definer Definer, reference []migrations.Step) (migrations.Plan, error) {
	if len(installed.Steps) == 0 || installed.Steps[len(installed.Steps)-1].ID != "data_model_v003_creation_receipts_delete" {
		return migrations.Plan{}, errors.New("owner results require the v003 lineage")
	}
	expected, err := OwnerResultUpgradeStatements(installed.Database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	if len(reference) != len(expected) {
		return migrations.Plan{}, errors.New("complete owner result reference required")
	}
	for i, want := range expected {
		got := reference[i]
		if got.ID != want.ID || got.Kind != want.Kind || got.Name != want.Name || got.SQL != want.SQL || got.Before != "" {
			return migrations.Plan{}, errors.New("owner result reference differs from source")
		}
	}
	return ExtendPlan(installed, reference)
}
