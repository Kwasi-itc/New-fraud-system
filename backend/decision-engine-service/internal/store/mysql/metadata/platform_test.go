package metadata

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

func TestPlatformPlanRequiresExactReviewedReference(t *testing.T) {
	if _, err := PlatformStatements("fraud`; DROP DATABASE fraud"); err == nil {
		t.Fatal("unsafe database accepted")
	}
	steps, err := PlatformStatements("fraud")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 {
		t.Fatal("unexpected platform scope")
	}
	for i := range steps {
		if !strings.HasPrefix(steps[i].SQL, "CREATE TABLE `fraud`.decision_engine_") || strings.Contains(steps[i].SQL, "IF NOT EXISTS") {
			t.Fatal("not an explicit initial owner definition")
		}
		definition, _ := json.Marshal(map[string]string{"Table": steps[i].Name, "Create Table": "unit reference; not server acceptance"})
		steps[i].After = string(definition)
	}
	if _, err := PlatformPlan("fraud", steps); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func([]migrations.Step){
		func(v []migrations.Step) { v[0].SQL += " " },
		func(v []migrations.Step) { v[0].Name = "other_owner_table" },
		func(v []migrations.Step) { v[0].Before = v[0].After },
		func(v []migrations.Step) { v[0].After = "" },
		func(v []migrations.Step) { v[0], v[1] = v[1], v[0] },
	} {
		changed := append([]migrations.Step(nil), steps...)
		mutate(changed)
		if _, err := PlatformPlan("fraud", changed); err == nil {
			t.Fatal("changed/absent reference accepted")
		}
	}
	if _, err := PlatformPlan("fraud", steps[:2]); err == nil {
		t.Fatal("partial reference accepted")
	}
}
