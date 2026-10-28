package mysql

import (
	"encoding/json"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
)

func TestPhysicalPlanBoundary(t *testing.T) {
	definition, _ := json.Marshal(map[string]string{"Table": "tenant_fixture", "Create Table": "reference"})
	step := migrations.Step{ID: "create", Kind: "TABLE", Name: "tenant_fixture", SQL: "CREATE TABLE `shared_app`.`tenant_fixture` (id INT)", After: string(definition)}
	encode := func(p PhysicalDDLPlan) []byte {
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	valid := PhysicalDDLPlan{Version: 1, Steps: []migrations.Step{step}}
	if _, err := decodePhysicalPlan(encode(valid), "shared_app", "tenant_fixture"); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*PhysicalDDLPlan){
		func(p *PhysicalDDLPlan) { p.Version = 2 },
		func(p *PhysicalDDLPlan) { p.Steps = nil },
		func(p *PhysicalDDLPlan) { p.Steps[0].Name = "other_tenant" },
		func(p *PhysicalDDLPlan) { p.Steps[0].SQL = "ALTER TABLE `other_app`.`tenant_fixture` ADD value INT" },
		func(p *PhysicalDDLPlan) { p.Steps[0].Kind = "TRIGGER" },
		func(p *PhysicalDDLPlan) { p.Steps[0].After = "{}" },
	} {
		p := PhysicalDDLPlan{Version: 1, Steps: []migrations.Step{step}}
		change(&p)
		if _, err := decodePhysicalPlan(encode(p), "shared_app", "tenant_fixture"); err == nil {
			t.Fatal("invalid physical plan accepted")
		}
	}
	for _, data := range [][]byte{append(encode(valid), []byte(" {}")...), []byte(`{"version":1,"steps":[],"unknown":true}`), make([]byte, (1<<20)+1)} {
		if _, err := decodePhysicalPlan(data, "shared_app", "tenant_fixture"); err == nil {
			t.Fatal("malformed physical plan accepted")
		}
	}
}
