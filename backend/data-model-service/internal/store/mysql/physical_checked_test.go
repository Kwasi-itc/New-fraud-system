package mysql

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
	"testing"
)

func TestCheckedNullabilityRetainsLegacyPlanPrecondition(t *testing.T) {
	tenant, table, fields := plannerFixture()
	p, err := NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		legacy, err := p.SetFieldNullable(field.Field.ID, false)
		if err != nil || !legacy.requireEmpty {
			t.Fatalf("legacy precondition changed: %v", err)
		}
		checked, err := p.SetFieldNullableChecked(field.Field.ID, false)
		if err != nil || checked.requireEmpty || checked.native.Kind != "field_nullability_checked" {
			t.Fatalf("checked precondition: %v", err)
		}
		regenerated, err := p.regenerateNative(checked.native)
		if err != nil || regenerated.step != checked.step || regenerated.requireEmpty {
			t.Fatalf("typed replay differs: %v", err)
		}
	}
}

func TestEnumFieldsUseNativeBaseColumns(t *testing.T) {
	tenant, table, fields := plannerFixture()
	for _, kind := range []datamodel.DataType{datamodel.DataTypeInt, datamodel.DataTypeFloat, datamodel.DataTypeString} {
		f := fields[0]
		f.Field.DataType = kind
		f.Field.IsEnum = true
		f.Field.IsUnique = false
		p, err := NewPhysicalPlanner("shared_app", tenant, table, []MappedPhysicalField{f})
		if err != nil {
			t.Fatal(err)
		}
		enum, err := p.AddFields([]uuid.UUID{f.Field.ID})
		if err != nil {
			t.Fatal(err)
		}
		f.Field.IsEnum = false
		p, err = NewPhysicalPlanner("shared_app", tenant, table, []MappedPhysicalField{f})
		if err != nil {
			t.Fatal(err)
		}
		plain, err := p.AddFields([]uuid.UUID{f.Field.ID})
		if err != nil || plain.step != enum.step {
			t.Fatalf("enum changed physical base type: %v", err)
		}
	}
}
