package contract

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

func TestBuildIngestion(t *testing.T) {
	table := datamodel.Table{ID: uuid.New(), TenantID: uuid.New(), Name: "records", Archived: true}
	f := datamodel.Field{ID: uuid.New(), TenantID: table.TenantID, TableID: table.ID, Name: "state", DataType: datamodel.DataTypeString, IsEnum: true, Archived: true}
	a := datamodel.FieldEnumValue{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), TenantID: table.TenantID, FieldID: f.ID, Value: "a", Label: "A", SortOrder: 1}
	b := a
	b.ID = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	b.Value = "b"
	values := []datamodel.FieldEnumValue{b, a}
	first, err := BuildIngestion(table, []datamodel.Field{f}, values)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildIngestion(table, []datamodel.Field{f}, []datamodel.FieldEnumValue{a, b})
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("unstable payload", err)
	}
	if values[0].ID != b.ID {
		t.Fatal("caller ordering mutated")
	}
	var decoded document
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Archived || !decoded.Fields[f.Name].Archived || decoded.Fields[f.Name].EnumValues[0].ID != a.ID {
		t.Fatal("archive or enum order lost")
	}
	table.Description = "presentation only"
	f.Alias = "alias"
	unchanged, err := BuildIngestion(table, []datamodel.Field{f}, values)
	if err != nil || !bytes.Equal(first, unchanged) {
		t.Fatal("presentation changed ingestion contract")
	}
	empty, err := BuildIngestion(table, nil, nil)
	if err != nil || !bytes.Contains(empty, []byte(`"fields":{}`)) {
		t.Fatal("empty fields must be object", err)
	}
	f.IsEnum = false
	noEnum, err := BuildIngestion(table, []datamodel.Field{f}, values)
	if err != nil || !bytes.Contains(noEnum, []byte(`"enum_values":[]`)) {
		t.Fatal("non-enum values leaked", err)
	}
}

func TestBuildIngestionRejectsInconsistentOwnership(t *testing.T) {
	table := datamodel.Table{ID: uuid.New(), TenantID: uuid.New(), Name: "records"}
	f := datamodel.Field{ID: uuid.New(), TenantID: table.TenantID, TableID: table.ID, Name: "state"}
	other := f
	other.TenantID = uuid.New()
	wrongTable := f
	wrongTable.TableID = uuid.New()
	duplicateName := f
	duplicateName.ID = uuid.New()
	for _, fields := range [][]datamodel.Field{{other}, {wrongTable}, {f, f}, {f, duplicateName}} {
		if data, err := BuildIngestion(table, fields, nil); err == nil || data != nil {
			t.Fatal("inconsistent fields accepted")
		}
	}
	for _, v := range []datamodel.FieldEnumValue{
		{ID: uuid.New(), TenantID: uuid.New(), FieldID: f.ID},
		{ID: uuid.New(), TenantID: table.TenantID, FieldID: uuid.New()},
	} {
		if data, err := BuildIngestion(table, []datamodel.Field{f}, []datamodel.FieldEnumValue{v}); err == nil || data != nil {
			t.Fatal("inconsistent enum accepted")
		}
	}
}

func TestBuildIngestionRejectsLossyUTF8Encoding(t *testing.T) {
	table := datamodel.Table{ID: uuid.New(), TenantID: uuid.New(), Name: "records"}
	f := datamodel.Field{ID: uuid.New(), TenantID: table.TenantID, TableID: table.ID, Name: "state", IsEnum: true}
	v := datamodel.FieldEnumValue{ID: uuid.New(), TenantID: table.TenantID, FieldID: f.ID, Value: "state", Label: "State"}
	invalid := string([]byte{0xff})
	for _, target := range []string{"table", "field", "type", "value", "label"} {
		t.Run(target, func(t *testing.T) {
			tab, field, value := table, f, v
			switch target {
			case "table":
				tab.Name = invalid
			case "field":
				field.Name = invalid
			case "type":
				field.DataType = datamodel.DataType(invalid)
			case "value":
				value.Value = invalid
			case "label":
				value.Label = invalid
			}
			if payload, err := BuildIngestion(tab, []datamodel.Field{field}, []datamodel.FieldEnumValue{value}); err == nil || payload != nil {
				t.Fatal("invalid UTF-8 silently replaced in publication")
			}
		})
	}
}
