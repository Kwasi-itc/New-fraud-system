package mysql

import (
	"encoding/json"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/google/uuid"
)

func creationFixture() (uuid.UUID, PhysicalTable, []MappedPhysicalField) {
	tenant, table, all := plannerFixture()
	fields := []MappedPhysicalField{all[3], all[4]}
	fields[0].Field.Name, fields[0].Field.Nullable, fields[0].Field.IsUnique = "object_id", false, true
	fields[1].Field.Name, fields[1].Field.Nullable = "updated_at", false
	return tenant, table, fields
}

func TestPhysicalCreationBoundary(t *testing.T) {
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
	steps := creation.ReferenceSteps()
	if len(steps) != 3 || steps[0].Kind != "TABLE" || steps[1].Kind != "TRIGGER" || steps[2].Kind != "TRIGGER" {
		t.Fatal(steps)
	}
	steps[0].SQL = "changed"
	if creation.ReferenceSteps()[0].SQL == "changed" {
		t.Fatal("mutable reference")
	}
	policy := creation.DefinerPolicy()
	policy.Objects[0].Privileges[0] = "DELETE"
	if creation.DefinerPolicy().Objects[0].Privileges[0] != "SELECT" {
		t.Fatal("mutable policy")
	}
	if _, err := installation.GrantStatements("shared_app", creation.DefinerPolicy()); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(PhysicalDDLPlan{Version: 1, Steps: creation.ReferenceSteps()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodePhysicalPlan(raw, "shared_app", table.Name); err == nil {
		t.Fatal("trigger creation accepted by table-only executor")
	}
	for _, invalid := range []string{"nullable_object", "nonunique_object", "object_type", "object_enum", "object_name", "nullable_updated", "unique_updated", "updated_type", "updated_enum", "updated_name", "archived", "extra", "foreign", "same_id", "root"} {
		t.Run(invalid, func(t *testing.T) {
			tenant, table, fields := creationFixture()
			objectID, updatedID := fields[0].Field.ID, fields[1].Field.ID
			principal := definer
			switch invalid {
			case "nullable_object":
				fields[0].Field.Nullable = true
			case "nonunique_object":
				fields[0].Field.IsUnique = false
			case "object_type":
				fields[0].Field.DataType = datamodel.DataTypeInt
			case "object_enum":
				fields[0].Field.IsEnum = true
			case "object_name":
				fields[0].Field.Name = "not_object_id"
			case "nullable_updated":
				fields[1].Field.Nullable = true
			case "unique_updated":
				fields[1].Field.IsUnique = true
			case "updated_type":
				fields[1].Field.DataType = datamodel.DataTypeString
			case "updated_enum":
				fields[1].Field.IsEnum = true
			case "updated_name":
				fields[1].Field.Name = "not_updated_at"
			case "archived":
				fields[0].Field.Archived = true
			case "extra":
				id := uuid.New()
				fields = append(fields, MappedPhysicalField{Field: datamodel.Field{ID: id, TenantID: tenant, TableID: table.TableID, DataType: datamodel.DataTypeInt}, Components: []PhysicalField{{FieldID: id, Name: "field_extra_" + physicalUUID(id) + "_value", Component: "value"}}})
			case "foreign":
				objectID = uuid.New()
			case "same_id":
				updatedID = objectID
			case "root":
				principal.User = "root"
			}
			p, err := NewPhysicalPlanner("shared_app", tenant, table, fields)
			if err != nil {
				t.Fatal(err)
			}
			c, err := p.CreateTableReference(objectID, updatedID, principal)
			if err == nil || len(c.ReferenceSteps()) != 0 {
				t.Fatal("invalid creation returned statements", err)
			}
		})
	}
}
