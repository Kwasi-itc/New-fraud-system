package mysql

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/google/uuid"
)

// AddUniqueText creates a full-value LONGTEXT column and a private exact-key
// surrogate. NULL has no dictionary entry. Both write guards derive the key,
// including on UPDATE, so callers cannot forge a surrogate to evade uniqueness.
// The dictionary serializes digest buckets but compares complete binary values.
func (p *PhysicalPlanner) AddUniqueText(id uuid.UUID, definer installation.Definer) ([]PhysicalStatement, error) {
	f, ok := p.fields[id]
	if !ok || f.field.DataType != datamodel.DataTypeString || !f.field.IsUnique || f.field.Name == "object_id" {
		return nil, ErrPhysicalPlanningUnsupported
	}
	account, err := definer.SQLAccount()
	if err != nil {
		return nil, err
	}
	key := "`record_text_key_" + physicalUUID(id) + "`"
	value := f.columns["value"]
	nullable := " NULL"
	if !f.field.Nullable {
		nullable = " NOT NULL"
	}
	ddl := "ALTER TABLE " + p.qualified + " ADD COLUMN " + value + " LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin" + nullable +
		", ADD COLUMN " + key + " BINARY(16) NULL" +
		", ADD CONSTRAINT `record_text_present_" + physicalUUID(id) + "` CHECK ((" + value + " IS NULL AND " + key + " IS NULL) OR (" + value + " IS NOT NULL AND " + key + " IS NOT NULL))" +
		", ADD UNIQUE INDEX `record_unique_" + physicalUUID(id) + "` (" + key + ")" +
		", ADD CONSTRAINT `record_text_ref_" + physicalUUID(id) + "` FOREIGN KEY (" + key + ") REFERENCES " + p.schema + ".data_model_exact_keys(id)"
	alter := p.statement("add_unique_text", ddl).withNative(PhysicalNativeChange{Kind: "add_unique_text", FieldID: id})
	alter.requireEmpty = !f.field.Nullable
	result := []PhysicalStatement{alter}
	for _, event := range []struct{ sql, suffix string }{{"INSERT", "insert"}, {"UPDATE", "update"}} {
		name := "record_text_" + event.suffix + "_" + physicalUUID(id)
		sql := "CREATE DEFINER=" + account + " TRIGGER " + p.schema + ".`" + name + "` BEFORE " + event.sql + " ON " + p.qualified + " FOR EACH ROW BEGIN\n" +
			"DECLARE derived_key BINARY(16);\n" +
			"IF @@session.unique_checks<>1 OR @@session.foreign_key_checks<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='record integrity checks must be enabled'; END IF;\n" +
			"IF NEW." + value + " IS NOT NULL THEN\n" +
			"CALL " + p.schema + ".data_model_resolve_exact_key(_binary'record_field_value',X'" + physicalUUID(f.field.TenantID) + physicalUUID(id) + "',CAST(NEW." + value + " AS BINARY),derived_key);\nEND IF;\n" +
			"SET NEW." + key + "=derived_key;\nEND"
		sum := sha256.Sum256([]byte(sql))
		result = append(result, PhysicalStatement{step: migrations.Step{ID: fmt.Sprintf("text_guard_%x", sum[:16]), Kind: "TRIGGER", Name: name, SQL: sql}, native: PhysicalNativeChange{Kind: "text_guard_" + event.suffix, FieldID: id}})
	}
	return result, nil
}

// FinalTableDefinition excludes trigger definitions from the table lineage.
func (p PhysicalDDLPlan) FinalTableDefinition() string {
	for i := len(p.Steps) - 1; i >= 0; i-- {
		if p.Steps[i].Kind == "TABLE" {
			return p.Steps[i].After
		}
	}
	return ""
}

func (p PhysicalDDLPlan) InstallsTextGuards() bool {
	for _, s := range p.Steps {
		if p.Version == 3 && s.Kind == "TRIGGER" && s.After != "" {
			return true
		}
	}
	return false
}

func validateTextChangeSequence(plan PhysicalDDLPlan) error {
	if err := validateTextConstraintSequences(plan); err != nil {
		return err
	}
	for i, v := range plan.Native.Changes {
		switch v.Kind {
		case "add_unique_text":
			if i+2 >= len(plan.Native.Changes) || plan.Native.Changes[i+1].Kind != "text_guard_insert" || plan.Native.Changes[i+2].Kind != "text_guard_update" || plan.Native.Changes[i+1].FieldID != v.FieldID || plan.Native.Changes[i+2].FieldID != v.FieldID {
				return errors.New("unique text requires both guards immediately after its column")
			}
		case "text_guard_insert", "text_guard_update":
			offset := 1
			if v.Kind == "text_guard_update" {
				offset = 2
			}
			if i < offset || plan.Native.Changes[i-offset].Kind != "add_unique_text" || plan.Native.Changes[i-offset].FieldID != v.FieldID {
				return errors.New("orphan text guard")
			}
		}
	}
	return nil
}
