package mysql

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

func TestManagedIndexBudgetAndTextSemantics(t *testing.T) {
	tenant, table, fields := creationFixture()
	ids := []uuid.UUID{fields[0].Field.ID, fields[1].Field.ID}
	p, err := NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := p.ManagedIndexParts(ids)
	if err != nil || len(parts) != 2 || parts[0].Prefix != 191 || parts[1].Prefix != 0 {
		t.Fatal(parts, err)
	}
	s, err := p.CreateManagedIndex(uuid.New(), ids)
	if err != nil || strings.Contains(s.step.SQL, "UNIQUE") || !strings.Contains(s.step.SQL, "(191)") {
		t.Fatal(s, err)
	}
	ids = nil
	for i := 0; i < 16; i++ {
		id := uuid.New()
		fields = append(fields, MappedPhysicalField{Field: datamodel.Field{ID: id, TenantID: tenant, TableID: table.TableID, Name: "text_" + physicalUUID(id), DataType: datamodel.DataTypeString}, Components: []PhysicalField{{FieldID: id, Name: "field_text_" + physicalUUID(id) + "_value", Component: "value"}}})
		ids = append(ids, id)
	}
	p, err = NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	parts, err = p.ManagedIndexParts(ids)
	if err != nil {
		t.Fatal(err)
	}
	bytes := 0
	for _, part := range parts {
		bytes += 4 * part.Prefix
	}
	if bytes > 3072 || parts[0].Prefix < 1 {
		t.Fatal("key budget", parts)
	}
	if _, err := p.ManagedIndexParts(append(ids, fields[1].Field.ID)); err == nil {
		t.Fatal("seventeen key parts accepted")
	}
}

func TestManagedRepairAcceptsOnlyMissingOwnerKeys(t *testing.T) {
	tenant, table, fields := creationFixture()
	p, err := NewPhysicalPlanner("shared_app", tenant, table, fields)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	statement, err := p.CreateManagedIndex(id, []uuid.UUID{fields[0].Field.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, key, _ := strings.Cut(statement.step.SQL, " ADD INDEX ")
	create := "CREATE TABLE `" + table.Name + "` (\n  `id` binary(16) NOT NULL,\n  PRIMARY KEY (`id`),\n  KEY " + key + "\n) ENGINE=InnoDB"
	raw, _ := json.Marshal(map[string]string{"Table": table.Name, "Create Table": create})
	missing, err := MissingManagedIndexes(string(raw), map[string]bool{"PRIMARY": true})
	if err != nil || len(missing) != 1 || missing[0] != id {
		t.Fatal(missing, err)
	}
	before, err := ManagedRepairBefore(string(raw), missing)
	if err != nil || strings.Contains(before, "record_index_") || strings.Contains(before, ",\\n)") {
		t.Fatal(before, err)
	}
	restore, err := p.RestoreManagedIndexes(string(raw), missing)
	if err != nil || len(restore) != 1 || restore[0].step.SQL != statement.step.SQL {
		t.Fatal(restore, err)
	}
	if _, err := ManagedRepairBefore(string(raw), []uuid.UUID{uuid.New()}); err == nil {
		t.Fatal("unknown key accepted")
	}
	if _, err := ManagedRepairBefore(string(raw), []uuid.UUID{id, id}); err == nil {
		t.Fatal("duplicate repair accepted")
	}
	forged := strings.Replace(string(raw), "(191)", "(17)", 1)
	if _, err := p.RestoreManagedIndexes(forged, missing); err == nil {
		t.Fatal("arbitrary prefix adopted")
	}
}
