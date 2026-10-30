package installation

import (
	_ "embed"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

//go:embed sql/contract_lookup.sql
var contractLookupSQL string

// ContractLookupUpgradeStatements is append-only v006: a read-only, fixed-domain
// lookup for authorized contract consumers, not dictionary allocation or raw reads.
// Its independent locked definer does not expand the frozen resolver's grants.
func ContractLookupUpgradeStatements(database string, definer Definer) ([]migrations.Step, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	account, err := definer.SQLAccount()
	if err != nil {
		return nil, err
	}
	statement := strings.NewReplacer("{{database}}", schema, "{{definer}}", account).Replace(contractLookupSQL)
	return []migrations.Step{{ID: "data_model_v006_contract_lookup", Kind: "FUNCTION", Name: "data_model_find_ingestion_contract_key_v1", SQL: statement}}, nil
}

func ContractLookupDefinerPolicy(definer Definer) AccountPolicy {
	return AccountPolicy{User: definer.User, Host: definer.Host, Locked: true, Objects: []ObjectGrant{
		{Kind: "TABLE", Name: "data_model_exact_keys", Privileges: []string{"SELECT"}},
		{Kind: "FUNCTION", Name: "data_model_exact_key_digest", Privileges: []string{"EXECUTE"}},
		{Kind: "FUNCTION", Name: "data_model_find_ingestion_contract_key_v1", Privileges: []string{"EXECUTE"}},
	}}
}

// ExtendContractLookupPlan pins the exact shipped source and an initially absent
// function to isolated reference observations. Previous history stays untouched.
// Deployment/readiness wiring must explicitly adopt v006 before runtime use.
func ExtendContractLookupPlan(installed migrations.Plan, definer Definer, reference []migrations.Step) (migrations.Plan, error) {
	if len(installed.Steps) == 0 || installed.Steps[len(installed.Steps)-1].ID != "data_model_v005_record_lookup" {
		return migrations.Plan{}, errors.New("contract lookup requires v005 lineage")
	}
	expected, err := ContractLookupUpgradeStatements(installed.Database, definer)
	if err != nil {
		return migrations.Plan{}, err
	}
	if len(reference) != 1 {
		return migrations.Plan{}, errors.New("complete contract lookup reference required")
	}
	want, got := expected[0], reference[0]
	if got.ID != want.ID || got.Kind != want.Kind || got.Name != want.Name || got.SQL != want.SQL || got.Before != "" {
		return migrations.Plan{}, errors.New("contract lookup reference differs from source")
	}
	return ExtendPlan(installed, reference)
}
