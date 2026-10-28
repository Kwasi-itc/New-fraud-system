package mysql

import (
	"encoding/json"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/google/uuid"
)

func TestRecordChangePlan(t *testing.T) {
	tenant, table, fields := creationFixture()
	initial, e := NewPhysicalPlanner("shared_app", tenant, table, fields)
	if e != nil {
		t.Fatal(e)
	}
	creation, e := initial.CreateTableReference(fields[0].Field.ID, fields[1].Field.ID, installation.Definer{User: "record_guard", Host: "localhost"})
	if e != nil {
		t.Fatal(e)
	}
	reference := creation.ReferenceSteps()
	for i := range reference {
		reference[i].After = `{"definition":"original","type":"fixture"}`
	}
	original, e := creation.Bind(reference)
	if e != nil {
		t.Fatal(e)
	}
	id := uuid.New()
	fields = append(fields, MappedPhysicalField{Field: datamodel.Field{ID: id, TenantID: tenant, TableID: table.TableID, Name: "score", DataType: datamodel.DataTypeInt, Nullable: true}, Components: []PhysicalField{{FieldID: id, Name: "field_score_" + physicalUUID(id) + "_value", Component: "value"}}})
	p, e := NewPhysicalPlanner("shared_app", tenant, table, fields)
	if e != nil {
		t.Fatal(e)
	}
	statement, e := p.AddFields([]uuid.UUID{id})
	if e != nil {
		t.Fatal(e)
	}
	altered := statement.ReferenceStep()
	altered.Before = reference[0].After
	altered.After = `{"definition":"altered","type":"fixture"}`
	plan, e := p.BindRecordChanges(original, []PhysicalStatement{statement}, []migrations.Step{altered})
	if e != nil {
		t.Fatal(e)
	}
	if plan.Version != 3 || len(plan.RetainedGuards) != 2 {
		t.Fatal(plan)
	}
	for _, change := range []func(*PhysicalDDLPlan){
		func(v *PhysicalDDLPlan) {
			for i := range v.Native.Fields {
				if v.Native.Fields[i].Field.Name == "object_id" {
					v.Native.Fields[i].Field.IsUnique = false
				}
			}
		},
		func(v *PhysicalDDLPlan) {
			for i := range v.Native.Fields {
				if v.Native.Fields[i].Field.Name == "updated_at" {
					v.Native.Fields[i].Field.Nullable = true
				}
			}
		},
		func(v *PhysicalDDLPlan) { v.Native = nil },
		func(v *PhysicalDDLPlan) { v.Native.Changes[0].Kind = "unknown" },
		func(v *PhysicalDDLPlan) { v.Native.Changes[0].FieldIDs = []uuid.UUID{uuid.New()} },
		func(v *PhysicalDDLPlan) {
			v.Steps[0].SQL = "ALTER TABLE `shared_app`.`" + table.Name + "` DROP PRIMARY KEY"
		},
		func(v *PhysicalDDLPlan) { v.EmptyTableBefore = []string{v.Steps[0].ID} },
		func(v *PhysicalDDLPlan) { v.Version = 1 }, func(v *PhysicalDDLPlan) { v.Version = 2 }, func(v *PhysicalDDLPlan) { v.Creation = nil },
		func(v *PhysicalDDLPlan) { v.Creation.TenantID = uuid.New() }, func(v *PhysicalDDLPlan) { v.Creation.Definer.User = "wrong" },
		func(v *PhysicalDDLPlan) { v.RetainedGuards = v.RetainedGuards[:1] }, func(v *PhysicalDDLPlan) {
			v.RetainedGuards[0], v.RetainedGuards[1] = v.RetainedGuards[1], v.RetainedGuards[0]
		},
		func(v *PhysicalDDLPlan) { v.RetainedGuards[0].SQL += " " }, func(v *PhysicalDDLPlan) { v.RetainedGuards[0].Before = v.RetainedGuards[0].After }, func(v *PhysicalDDLPlan) { v.RetainedGuards[0].After = "invalid" },
		func(v *PhysicalDDLPlan) { v.Steps[0].Before = "" }, func(v *PhysicalDDLPlan) { v.Steps[0].SQL = reference[0].SQL },
		func(v *PhysicalDDLPlan) {
			v.Steps[0].SQL = "ALTER TABLE `shared_app`.`" + table.Name + "` DROP COLUMN `" + v.Creation.ObjectColumn + "`"
		},
	} {
		raw, _ := json.Marshal(plan)
		var next PhysicalDDLPlan
		if e := json.Unmarshal(raw, &next); e != nil {
			t.Fatal(e)
		}
		change(&next)
		raw, _ = json.Marshal(next)
		if _, e := decodePhysicalPlan(raw, "shared_app", table.Name); e == nil {
			t.Fatal("invalid guarded alteration accepted")
		}
	}
	altered.Before = `{"definition":"drifted","type":"fixture"}`
	if _, e := p.BindRecordChanges(original, []PhysicalStatement{statement}, []migrations.Step{altered}); e == nil {
		t.Fatal("unrelated initial shape adopted")
	}
	indexID := uuid.New()
	for _, build := range []func() (PhysicalStatement, error){
		func() (PhysicalStatement, error) { return p.CreateIndex(indexID, []uuid.UUID{id}, true) },
		func() (PhysicalStatement, error) { return p.DropIndex(indexID) },
		func() (PhysicalStatement, error) { return p.CreateFieldUniqueIndex(id) },
		func() (PhysicalStatement, error) { return p.DropFieldUniqueIndex(id) },
		func() (PhysicalStatement, error) { return p.SetFieldNullable(id, false) },
	} {
		op, e := build()
		if e != nil {
			t.Fatal(e)
		}
		ref := op.ReferenceStep()
		ref.Before = plan.Steps[0].After
		ref.After = `{"definition":"next","type":"fixture"}`
		next, e := p.BindRecordChanges(plan, []PhysicalStatement{op}, []migrations.Step{ref})
		if e != nil || next.Version != 3 {
			t.Fatal("native typed operation chain", e)
		}
	}
	plan.Creation.Definer.User = "changed"
	plan.RetainedGuards[0].SQL = "changed"
	if original.Creation.Definer.User == "changed" || original.Steps[1].SQL == "changed" {
		t.Fatal("source aliased")
	}
}
