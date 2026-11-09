package mysql

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/google/uuid"
)

func textFixtureReferences(previous PhysicalDDLPlan, statements []PhysicalStatement) []migrations.Step {
	table := previous.FinalTableDefinition()
	guards := map[string]string{}
	for _, g := range previous.ActiveGuards() {
		guards[g.Name] = g.After
	}
	var refs []migrations.Step
	for i, s := range statements {
		step := s.ReferenceStep()
		if step.Kind == "TABLE" {
			step.Before = table
			step.After = fmt.Sprintf(`{"definition":"%s_%d","type":"table"}`, step.ID, i)
			table = step.After
		} else {
			step.Before = guards[step.Name]
			if !strings.HasPrefix(step.SQL, "DROP TRIGGER ") {
				step.After = fmt.Sprintf(`{"definition":"%s","type":"trigger"}`, step.ID)
			}
			guards[step.Name] = step.After
		}
		refs = append(refs, step)
	}
	return refs
}

func TestTextConstraintPromotionRetirementAndReenable(t *testing.T) {
	tenant, table, fields := creationFixture()
	definer := installation.Definer{User: "record_guard", Host: "localhost"}
	p, err := NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	creation, err := p.CreateTableReference(fields[0].Field.ID, fields[1].Field.ID, definer)
	if err != nil {
		t.Fatal(err)
	}
	refs := creation.ReferenceSteps()
	for i := range refs {
		refs[i].After = `{"definition":"initial","type":"fixture"}`
	}
	previous, err := creation.Bind(refs)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	field := datamodel.Field{ID: id, TenantID: tenant, TableID: table.TableID, Name: "reference", DataType: datamodel.DataTypeString, Nullable: true}
	mapped, err := PreviewFieldMapping(field)
	if err != nil {
		t.Fatal(err)
	}
	fields = append(fields, mapped)
	p, err = NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	add, err := p.AddFields([]uuid.UUID{id})
	if err != nil {
		t.Fatal(err)
	}
	previous, err = p.BindRecordChanges(previous, []PhysicalStatement{add}, textFixtureReferences(previous, []PhysicalStatement{add}))
	if err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 2; cycle++ {
		steps, err := p.EnableTextUnique(id, definer)
		if err != nil {
			t.Fatal(err)
		}
		enabled, err := p.BindRecordChanges(previous, steps, textFixtureReferences(previous, steps))
		if err != nil {
			t.Fatal(err)
		}
		if enabled.TextBackfillColumn() != mapped.Components[0].Name || len(enabled.ActiveGuards()) != 4 {
			t.Fatal("promotion lost backfill or guards")
		}
		for _, mutate := range []func(*PhysicalDDLPlan){
			func(v *PhysicalDDLPlan) { v.Steps = v.Steps[:3]; v.Native.Changes = v.Native.Changes[:3] },
			func(v *PhysicalDDLPlan) { v.Native.Changes[3].Kind = "create_field_unique" },
			func(v *PhysicalDDLPlan) { v.Steps[1].SQL += " " },
		} {
			raw, _ := json.Marshal(enabled)
			var next PhysicalDDLPlan
			_ = json.Unmarshal(raw, &next)
			mutate(&next)
			raw, _ = json.Marshal(next)
			if _, err := decodePhysicalPlan(raw, "shared_app", table.Name); err == nil {
				t.Fatal("accepted corrupt promotion")
			}
		}
		fields[2].Field.IsUnique = true
		p, err = NewPhysicalPlanner("shared_app", tenant, table, fields)
		if err != nil {
			t.Fatal(err)
		}
		nullable, err := p.SetTextNullable(id, false)
		if err != nil {
			t.Fatal(err)
		}
		required, err := p.BindRecordChanges(enabled, []PhysicalStatement{nullable}, textFixtureReferences(enabled, []PhysicalStatement{nullable}))
		if err != nil {
			t.Fatal(err)
		}
		if len(required.ActiveGuards()) != 4 || len(required.EmptyTableBefore) != 0 {
			t.Fatal("nullability removed guards or imposed empty-table restriction")
		}
		fields[2].Field.Nullable = false
		p, err = NewPhysicalPlanner("shared_app", tenant, table, fields)
		if err != nil {
			t.Fatal(err)
		}
		remove, err := p.DisableTextUnique(id, definer)
		if err != nil {
			t.Fatal(err)
		}
		disabled, err := p.BindRecordChanges(required, remove, textFixtureReferences(required, remove))
		if err != nil {
			t.Fatal(err)
		}
		if len(disabled.ActiveGuards()) != 2 || disabled.InstallsTextGuards() || disabled.TextBackfillColumn() != "" {
			t.Fatal("retirement inventory incorrect")
		}
		raw, _ := json.Marshal(disabled)
		var tampered PhysicalDDLPlan
		_ = json.Unmarshal(raw, &tampered)
		tampered.Steps[1].Before = `{"definition":"forged","type":"trigger"}`
		raw, _ = json.Marshal(tampered)
		if _, err := decodePhysicalPlan(raw, "shared_app", table.Name); err == nil {
			t.Fatal("retirement accepted wrong historical definition")
		}
		fields[2].Field.IsUnique = false
		p, err = NewPhysicalPlanner("shared_app", tenant, table, fields)
		if err != nil {
			t.Fatal(err)
		}
		previous = disabled
	}
}

func TestBackfillGrantIsColumnScopedAndTemporary(t *testing.T) {
	p := installation.RecordInstallerPolicy{Principal: installation.Definer{User: "installer", Host: "localhost"}, Table: "tenant_records", NativeChanges: true, TextGuards: true, BackfillColumn: "field_reference_value"}
	active, err := p.Account(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(active.Objects[0].Columns["UPDATE"]) != 1 {
		t.Fatal("missing scoped update")
	}
	for _, privilege := range active.Objects[0].Privileges {
		if privilege == "UPDATE" {
			t.Fatal("table-wide DML grant")
		}
	}
	inactive, err := p.Account(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(inactive.Objects[0].Columns) != 0 {
		t.Fatal("backfill grant retained")
	}
	p.TextGuards = false
	if _, err := p.Account(false); err == nil {
		t.Fatal("backfill without guards accepted")
	}
}
