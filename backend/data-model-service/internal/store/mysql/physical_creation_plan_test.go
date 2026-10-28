package mysql

import (
	"encoding/json"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/google/uuid"
)

func TestPhysicalCreationPlan(t *testing.T) {
	tenant, table, fields := creationFixture()
	p, err := NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	c, err := p.CreateTableReference(fields[0].Field.ID, fields[1].Field.ID, installation.Definer{User: "record_guard", Host: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	reference := c.ReferenceSteps()
	for i := range reference {
		reference[i].After = `{"definition":"unit-reference","type":"fixture"}`
	}
	plan, err := c.Bind(reference)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Version != 2 || plan.Creation == nil || plan.Creation.TenantID != tenant {
		t.Fatal(plan)
	}
	for _, scenario := range []string{"missing_identity", "version", "tenant", "table", "column", "definer", "sql", "kind", "name", "id", "before", "missing_trigger", "reordered", "precondition", "definition"} {
		t.Run(scenario, func(t *testing.T) {
			raw, _ := json.Marshal(plan)
			var changed PhysicalDDLPlan
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "missing_identity":
				changed.Creation = nil
			case "version":
				changed.Version = 1
			case "tenant":
				changed.Creation.TenantID = uuid.New()
			case "table":
				changed.Creation.TableID = uuid.New()
			case "column":
				changed.Creation.ObjectColumn = "foreign_column"
			case "definer":
				changed.Creation.Definer.User = "other_guard"
			case "sql":
				changed.Steps[1].SQL += " "
			case "kind":
				changed.Steps[1].Kind = "TABLE"
			case "name":
				changed.Steps[1].Name = "foreign_trigger"
			case "id":
				changed.Steps[1].ID = "substituted"
			case "before":
				changed.Steps[1].Before = changed.Steps[1].After
			case "missing_trigger":
				changed.Steps = changed.Steps[:2]
			case "reordered":
				changed.Steps[1], changed.Steps[2] = changed.Steps[2], changed.Steps[1]
			case "precondition":
				changed.EmptyTableBefore = []string{changed.Steps[0].ID}
			case "definition":
				changed.Steps[1].After = "malformed"
			}
			raw, _ = json.Marshal(changed)
			if _, err := decodePhysicalPlan(raw, "shared_app", table.Name); err == nil {
				t.Fatal("invalid plan accepted")
			}
		})
	}
	reference[1].SQL = "substituted"
	if _, err := c.Bind(reference); err == nil {
		t.Fatal("substituted reference accepted")
	}
	if _, err := c.Bind(nil); err == nil {
		t.Fatal("missing reference accepted")
	}
	// Returned plan values cannot mutate the generator's pinned identity or SQL.
	plan.Creation.TenantID = uuid.New()
	plan.Steps[0].SQL = "changed"
	if c.identity.TenantID != tenant || c.ReferenceSteps()[0].SQL == "changed" {
		t.Fatal("mutable generator")
	}
}
