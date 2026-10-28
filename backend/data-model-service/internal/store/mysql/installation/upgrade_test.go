package installation

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
)

func TestUpgradePreservesInstalledHistory(t *testing.T) {
	definition := func(value string) string {
		b, _ := json.Marshal(map[string]string{"Table": "data_model_example", "Create Table": value})
		return string(b)
	}
	original := migrations.Step{ID: "initial", Kind: "TABLE", Name: "data_model_example", SQL: "CREATE TABLE `shared_app`.data_model_example(id INT)", After: definition("original")}
	installed := migrations.Plan{Database: "shared_app", Steps: []migrations.Step{original}}
	upgrade := migrations.Step{ID: "upgrade", Kind: "TABLE", Name: original.Name, SQL: "ALTER TABLE `shared_app`.data_model_example ADD value INT", Before: original.After, After: definition("upgraded")}
	plan, err := ExtendPlan(installed, []migrations.Step{upgrade})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Steps[:1], installed.Steps) {
		t.Fatal("installed history rewritten")
	}
	plan.Steps[0].SQL = "changed"
	if installed.Steps[0].SQL != original.SQL {
		t.Fatal("upgrade aliases caller history")
	}
	for _, change := range []func(*migrations.Step){
		func(s *migrations.Step) { s.ID = original.ID },
		func(s *migrations.Step) { s.Before = definition("wrong") },
		func(s *migrations.Step) { s.Name = "unjournaled_table" },
	} {
		bad := upgrade
		change(&bad)
		if _, err := ExtendPlan(installed, []migrations.Step{bad}); err == nil {
			t.Fatal("invalid upgrade accepted")
		}
	}
	if _, err := ExtendPlan(installed, nil); err == nil {
		t.Fatal("empty upgrade accepted")
	}
}
