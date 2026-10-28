package mysql

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/google/uuid"
)

func plannerFixture() (uuid.UUID, PhysicalTable, []MappedPhysicalField) {
	tenant, table := uuid.New(), uuid.New()
	physical := PhysicalTable{TableID: table, Name: "tenant_fixture_records_" + physicalUUID(table)}
	var fields []MappedPhysicalField
	for _, typ := range datamodel.SupportedDataTypes() {
		id := uuid.New()
		field := MappedPhysicalField{Field: datamodel.Field{ID: id, TenantID: tenant, TableID: table, Name: "logical_name", DataType: typ, Nullable: true}}
		components := []string{"value"}
		if typ == datamodel.DataTypeIPAddress {
			components = []string{"address", "family", "prefix"}
		}
		for _, component := range components {
			field.Components = append(field.Components, PhysicalField{FieldID: id, Name: "field_label_" + physicalUUID(id) + "_" + component, Component: component})
		}
		fields = append(fields, field)
	}
	return tenant, physical, fields
}

func TestTypedPhysicalPlanning(t *testing.T) {
	tenant, table, fields := plannerFixture()
	fields[1].Field.Nullable = false
	p, err := NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, len(fields))
	for i, f := range fields {
		ids[i] = f.Field.ID
	}
	add, err := p.AddFields(ids)
	if err != nil {
		t.Fatal(err)
	}
	if !add.requireEmpty {
		t.Fatal("NOT NULL addition lacks data precondition")
	}
	for _, want := range []string{"TINYINT NULL", "INT NOT NULL", "DOUBLE NULL", "LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin", "DATETIME(6)", "VARBINARY(16)", "OCTET_LENGTH", "<=128"} {
		if !strings.Contains(add.step.SQL, want) {
			t.Fatal("missing type/constraint", want)
		}
	}
	indexID := uuid.New()
	index, err := p.CreateIndex(indexID, []uuid.UUID{ids[1], ids[5]}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(index.step.SQL, "ADD UNIQUE INDEX `record_index_"+physicalUUID(indexID)) {
		t.Fatal("index identity lost")
	}
	if _, err := p.DropIndex(indexID); err != nil {
		t.Fatal(err)
	}
	fieldUnique, err := p.CreateFieldUniqueIndex(ids[1])
	if err != nil || !strings.Contains(fieldUnique.step.SQL, "ADD UNIQUE INDEX `record_unique_"+physicalUUID(ids[1])) {
		t.Fatal("field unique identity", err)
	}
	fieldDrop, err := p.DropFieldUniqueIndex(ids[1])
	if err != nil || !strings.Contains(fieldDrop.step.SQL, "DROP INDEX `record_unique_"+physicalUUID(ids[1])) {
		t.Fatal("field unique drop identity", err)
	}
	for _, plan := range []func(uuid.UUID) (PhysicalStatement, error){p.CreateFieldUniqueIndex, p.DropFieldUniqueIndex} {
		if _, err := plan(ids[3]); !errors.Is(err, ErrPhysicalPlanningUnsupported) {
			t.Fatal("text unique lifecycle weakened", err)
		}
		if _, err := plan(uuid.New()); err == nil {
			t.Fatal("foreign field unique lifecycle accepted")
		}
	}
	if _, err := p.CreateIndex(uuid.New(), []uuid.UUID{ids[3]}, true); !errors.Is(err, ErrPhysicalPlanningUnsupported) {
		t.Fatal("text uniqueness weakened", err)
	}
	if _, err := p.AddFields([]uuid.UUID{ids[0], ids[0]}); err == nil {
		t.Fatal("duplicate field accepted")
	}
	if _, err := p.AddFields([]uuid.UUID{uuid.New()}); err == nil {
		t.Fatal("foreign field accepted")
	}
	definition := func(value string) string {
		b, _ := json.Marshal(map[string]string{"Table": table.Name, "Create Table": value})
		return string(b)
	}
	reference := add.ReferenceStep()
	reference.Before = definition("before")
	reference.After = definition("after")
	plan, err := p.Bind([]PhysicalStatement{add}, []migrations.Step{reference})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.EmptyTableBefore) != 1 || plan.EmptyTableBefore[0] != reference.ID {
		t.Fatal("lost durable precondition")
	}
	reference.SQL += " "
	if _, err := p.Bind([]PhysicalStatement{add}, []migrations.Step{reference}); err == nil {
		t.Fatal("substituted SQL accepted")
	}
	// Constructor copies inputs: subsequent caller mutation cannot change SQL.
	fields[0].Components[0].Name = "malicious"
	again, err := p.AddFields(ids)
	if err != nil || again.step.SQL != add.step.SQL {
		t.Fatal("mutable caller mapping", err)
	}
}

func TestPhysicalPlannerArchivedSnapshot(t *testing.T) {
	tenant, table, fields := plannerFixture()
	for i := range fields {
		fields[i].Field.Archived = i != 1
	}
	p, err := NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	active := fields[1].Field.ID
	if _, err := p.CreateIndex(uuid.New(), []uuid.UUID{active}, false); err != nil {
		t.Fatal("archive blocked active field", err)
	}
	for _, f := range fields {
		if !f.Field.Archived {
			continue
		}
		id := f.Field.ID
		operations := map[string]func() (PhysicalStatement, error){
			"add":         func() (PhysicalStatement, error) { return p.AddFields([]uuid.UUID{id}) },
			"mixed_add":   func() (PhysicalStatement, error) { return p.AddFields([]uuid.UUID{active, id}) },
			"index":       func() (PhysicalStatement, error) { return p.CreateIndex(uuid.New(), []uuid.UUID{active, id}, false) },
			"unique":      func() (PhysicalStatement, error) { return p.CreateFieldUniqueIndex(id) },
			"drop_unique": func() (PhysicalStatement, error) { return p.DropFieldUniqueIndex(id) },
			"nullable":    func() (PhysicalStatement, error) { return p.SetFieldNullable(id, true) },
		}
		for name, operation := range operations {
			t.Run(string(f.Field.DataType)+"/"+name, func(t *testing.T) {
				statement, err := operation()
				if !errors.Is(err, ErrPhysicalConflict) || statement.ReferenceStep().SQL != "" {
					t.Fatalf("archived mutation returned SQL or wrong error: %+v %v", statement, err)
				}
			})
		}
	}
	// Callers cannot reactivate the planner's copied snapshot after validation.
	fields[0].Field.Archived = false
	if _, err := p.AddFields([]uuid.UUID{fields[0].Field.ID}); !errors.Is(err, ErrPhysicalConflict) {
		t.Fatal("caller changed archived snapshot", err)
	}
	for _, change := range []func(*MappedPhysicalField){
		func(f *MappedPhysicalField) { f.Field.TenantID = uuid.New() },
		func(f *MappedPhysicalField) { f.Field.TableID = uuid.New() },
		func(f *MappedPhysicalField) { f.Components = nil },
		func(f *MappedPhysicalField) { f.Components[0].Name = "invalid" },
	} {
		tenant, table, fields := plannerFixture()
		fields[5].Field.Archived = true
		change(&fields[5])
		if _, err := NewPhysicalPlanner("shared_app", tenant, table, fields); err == nil {
			t.Fatal("archiving bypassed mapping validation")
		}
	}
}

func TestPhysicalPlannerRejectsInvalidMappings(t *testing.T) {
	for _, change := range []func(*MappedPhysicalField){
		func(f *MappedPhysicalField) { f.Field.TenantID = uuid.New() },
		func(f *MappedPhysicalField) { f.Field.TableID = uuid.New() },
		func(f *MappedPhysicalField) { f.Components = nil },
		func(f *MappedPhysicalField) { f.Components[0].FieldID = uuid.New() },
		func(f *MappedPhysicalField) { f.Components[0].Name = "field_unsafe`;DROP TABLE x" },
		func(f *MappedPhysicalField) { f.Field.DataType = "unknown" },
	} {
		tenant, table, fields := plannerFixture()
		change(&fields[0])
		if _, err := NewPhysicalPlanner("shared_app", tenant, table, fields); err == nil {
			t.Fatal("invalid registry binding accepted")
		}
	}
	for _, enum := range []bool{false, true} {
		tenant, table, fields := plannerFixture()
		fields[3].Field.IsUnique = !enum
		fields[3].Field.IsEnum = enum
		p, err := NewPhysicalPlanner("shared_app", tenant, table, fields)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.AddFields([]uuid.UUID{fields[3].Field.ID}); !errors.Is(err, ErrPhysicalPlanningUnsupported) {
			t.Fatal("unsupported invariant silently omitted", err)
		}
	}
}
