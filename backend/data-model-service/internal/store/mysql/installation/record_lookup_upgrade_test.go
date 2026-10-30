package installation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
)

func TestRecordLookupUpgrade(t *testing.T) {
	d := Definer{"record_lookup_guard", "%"}
	steps, err := RecordLookupUpgradeStatements("shared_app", d)
	if err != nil || len(steps) != 1 {
		t.Fatal(steps, err)
	}
	for _, required := range []string{"SQL SECURITY DEFINER", "READS SQL DATA", "key_domain=_binary'record_object_id'", "scope_bytes=CONCAT(p_tenant,p_table)", "AND raw_value=p_value"} {
		if !strings.Contains(steps[0].SQL, required) {
			t.Fatal("missing bound lookup", required)
		}
	}
	grants, err := GrantStatements("shared_app", RecordLookupDefinerPolicy(d))
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
	base := migrations.Plan{Database: "shared_app", Steps: []migrations.Step{{ID: "data_model_v004_owner_results_delete", Kind: "TABLE", Name: "data_model_example", SQL: "CREATE TABLE `shared_app`.data_model_example(id INT)", After: string(baseDefinition)}}}
	ref := steps[0]
	definition, _ := json.Marshal(map[string]string{"Function": "data_model_find_record_object_key_v1", "Create Function": "reference"})
	ref.After = string(definition)
	plan, err := ExtendRecordLookupPlan(base, d, []migrations.Step{ref})
	if err != nil || len(plan.Steps) != 2 || !reflect.DeepEqual(base.Steps, plan.Steps[:1]) {
		t.Fatal(plan, err)
	}
	for _, change := range []func(*migrations.Step){func(s *migrations.Step) { s.SQL += " " }, func(s *migrations.Step) { s.ID = "substituted" }, func(s *migrations.Step) { s.Name = "substituted" }, func(s *migrations.Step) { s.Kind = "PROCEDURE" }, func(s *migrations.Step) { s.Before = s.After }} {
		bad := ref
		change(&bad)
		if _, err := ExtendRecordLookupPlan(base, d, []migrations.Step{bad}); err == nil {
			t.Fatal("substitution accepted")
		}
	}
	if _, err := ExtendRecordLookupPlan(base, d, nil); err == nil {
		t.Fatal("missing reference accepted")
	}
	if _, err := RecordLookupUpgradeStatements("bad-schema", d); err == nil {
		t.Fatal("bad schema accepted")
	}
}
