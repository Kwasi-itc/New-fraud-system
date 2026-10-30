package installation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
)

func TestContractLookupUpgrade(t *testing.T) {
	d := Definer{"contract_lookup_guard", "%"}
	steps, err := ContractLookupUpgradeStatements("shared_app", d)
	if err != nil || len(steps) != 1 {
		t.Fatal(steps, err)
	}
	for _, required := range []string{"SQL SECURITY DEFINER", "READS SQL DATA", "key_domain=_binary'dm.contract.name'", "scope_bytes=p_tenant", "AND raw_value=p_value"} {
		if !strings.Contains(steps[0].SQL, required) {
			t.Fatal("missing bound lookup", required)
		}
	}
	grants, err := GrantStatements("shared_app", ContractLookupDefinerPolicy(d))
	if err != nil || len(grants) != 3 {
		t.Fatal(grants, err)
	}
	for _, grant := range grants {
		if strings.Contains(grant, "INSERT") || strings.Contains(grant, "UPDATE") || strings.Contains(grant, ".*") {
			t.Fatal("excess lookup grant", grant)
		}
	}
	// Synthetic definitions test binding, not live installation acceptance.
	baseDefinition, _ := json.Marshal(map[string]string{"Table": "data_model_example", "Create Table": "example"})
	base := migrations.Plan{Database: "shared_app", Steps: []migrations.Step{{ID: "data_model_v005_record_lookup", Kind: "TABLE", Name: "data_model_example", SQL: "CREATE TABLE `shared_app`.data_model_example(id INT)", After: string(baseDefinition)}}}
	ref := steps[0]
	definition, _ := json.Marshal(map[string]string{"Function": "data_model_find_ingestion_contract_key_v1", "Create Function": "reference"})
	ref.After = string(definition)
	plan, err := ExtendContractLookupPlan(base, d, []migrations.Step{ref})
	if err != nil || len(plan.Steps) != 2 || !reflect.DeepEqual(base.Steps, plan.Steps[:1]) {
		t.Fatal(plan, err)
	}
	for _, change := range []func(*migrations.Step){func(s *migrations.Step) { s.SQL += " " }, func(s *migrations.Step) { s.ID = "substituted" }, func(s *migrations.Step) { s.Name = "substituted" }, func(s *migrations.Step) { s.Kind = "PROCEDURE" }, func(s *migrations.Step) { s.Before = s.After }} {
		bad := ref
		change(&bad)
		if _, err := ExtendContractLookupPlan(base, d, []migrations.Step{bad}); err == nil {
			t.Fatal("substitution accepted")
		}
	}
	if _, err := ExtendContractLookupPlan(base, d, nil); err == nil {
		t.Fatal("missing reference accepted")
	}
	if _, err := ContractLookupUpgradeStatements("bad-schema", d); err == nil {
		t.Fatal("bad schema accepted")
	}
}
