package datamodel

import (
	"testing"

	"github.com/google/uuid"
)

func TestParseDataTypeRejectsUnsupportedValues(t *testing.T) {
	t.Parallel()

	if _, err := ParseDataType("coords"); err == nil {
		t.Fatal("expected unsupported data type error")
	}
}

func TestValidateFieldCreateRejectsReservedID(t *testing.T) {
	t.Parallel()

	if err := ValidateFieldCreate("id", DataTypeString, false, false); err == nil {
		t.Fatal("expected reserved name error")
	}
}

func TestValidateFieldCreateRejectsReservedTenantColumns(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"object_id", "updated_at", "valid_from", "valid_until"} {
		if err := ValidateFieldCreate(name, DataTypeString, false, false); err == nil {
			t.Fatalf("expected reserved name error for %s", name)
		}
	}
}

func TestValidateFieldUpdateRejectsReservedFieldMutations(t *testing.T) {
	t.Parallel()

	field := Field{Name: "object_id", DataType: DataTypeString}
	nullable := true

	if err := ValidateFieldUpdate(field, field.DataType, nil, nil, &nullable); err == nil {
		t.Fatal("expected reserved field update error")
	}
}

func TestValidateSemanticType(t *testing.T) {
	t.Parallel()

	if err := ValidateSemanticType(""); err != nil {
		t.Fatalf("expected blank semantic type to be allowed, got %v", err)
	}
	if err := ValidateSemanticType("financial_record"); err != nil {
		t.Fatalf("expected normalized semantic type to be allowed, got %v", err)
	}
	if err := ValidateSemanticType("Financial Record"); err == nil {
		t.Fatal("expected semantic type with spaces and uppercase letters to be rejected")
	}
}

func TestValidatePivotRequiresExactlyOneMode(t *testing.T) {
	t.Parallel()

	fieldID := uuid.New()
	linkID := uuid.New()

	if err := ValidatePivot(nil, nil); err == nil {
		t.Fatal("expected validation error when neither field nor path is set")
	}
	if err := ValidatePivot(&fieldID, []uuid.UUID{linkID}); err == nil {
		t.Fatal("expected validation error when both field and path are set")
	}
}

func TestSortFieldOrderAppendsMissingFieldsAlphabetically(t *testing.T) {
	t.Parallel()

	fieldA := Field{ID: uuid.New(), Name: "alpha"}
	fieldB := Field{ID: uuid.New(), Name: "bravo"}
	fieldC := Field{ID: uuid.New(), Name: "charlie"}

	ordered := SortFieldOrder(
		[]Field{fieldC, fieldA, fieldB},
		TableOptions{FieldOrder: []uuid.UUID{fieldC.ID}},
	)

	if len(ordered) != 3 {
		t.Fatalf("expected 3 fields in order, got %d", len(ordered))
	}
	if ordered[0] != fieldC.ID || ordered[1] != fieldA.ID || ordered[2] != fieldB.ID {
		t.Fatalf("unexpected order: %v", ordered)
	}
}
