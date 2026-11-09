package mysql

import (
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/google/uuid"
)

func (p *PhysicalPlanner) textField(id uuid.UUID) (plannedField, error) {
	fields, err := p.selectFields([]uuid.UUID{id})
	if err != nil {
		return plannedField{}, err
	}
	f := fields[0]
	if f.field.DataType != datamodel.DataTypeString || f.field.Name == "object_id" {
		return f, ErrPhysicalPlanningUnsupported
	}
	return f, nil
}

// textGuardSource renders the same immutable guards for creation and promotion.
// It does not mutate the source snapshot, which remains the pre-update metadata.
func (p *PhysicalPlanner) textGuardSource(id uuid.UUID, definer installation.Definer) ([]PhysicalStatement, error) {
	f, err := p.textField(id)
	if err != nil {
		return nil, err
	}
	copy := *p
	copy.fields = make(map[uuid.UUID]plannedField, len(p.fields))
	for id, value := range p.fields {
		copy.fields[id] = value
	}
	f.field.IsUnique = true
	copy.fields[id] = f
	return copy.AddUniqueText(id, definer)
}

// EnableTextUnique first installs nullable keys and their guards. Before the
// final CHECK, execution fills existing keys in bounded, restartable batches.
// The unique index is present throughout backfill, making duplicates fail closed.
func (p *PhysicalPlanner) EnableTextUnique(id uuid.UUID, definer installation.Definer) ([]PhysicalStatement, error) {
	f, err := p.textField(id)
	if err != nil {
		return nil, err
	}
	if f.field.IsUnique {
		return nil, ErrPhysicalConflict
	}
	source, err := p.textGuardSource(id, definer)
	if err != nil {
		return nil, err
	}
	key := "`record_text_key_" + physicalUUID(id) + "`"
	sql := "ALTER TABLE " + p.qualified + " ADD COLUMN " + key + " BINARY(16) NULL, ADD UNIQUE INDEX `record_unique_" + physicalUUID(id) + "` (" + key + "), ADD CONSTRAINT `record_text_ref_" + physicalUUID(id) + "` FOREIGN KEY (" + key + ") REFERENCES " + p.schema + ".data_model_exact_keys(id)"
	start := p.statement("enable_text_unique", sql).withNative(PhysicalNativeChange{Kind: "enable_text_unique", FieldID: id})
	guards := source[1:]
	guards[0].native.Kind = "enable_text_insert"
	guards[1].native.Kind = "enable_text_update"
	value := f.columns["value"]
	check := p.statement("text_unique_check", "ALTER TABLE "+p.qualified+" ADD CONSTRAINT `record_text_present_"+physicalUUID(id)+"` CHECK (("+value+" IS NULL AND "+key+" IS NULL) OR ("+value+" IS NOT NULL AND "+key+" IS NOT NULL))").withNative(PhysicalNativeChange{Kind: "text_unique_check", FieldID: id})
	return []PhysicalStatement{start, guards[0], guards[1], check}, nil
}

func (p *PhysicalPlanner) DisableTextUnique(id uuid.UUID, definer installation.Definer) ([]PhysicalStatement, error) {
	f, err := p.textField(id)
	if err != nil {
		return nil, err
	}
	if !f.field.IsUnique {
		return nil, ErrPhysicalConflict
	}
	source, err := p.textGuardSource(id, definer)
	if err != nil {
		return nil, err
	}
	start := p.statement("disable_text_unique", "ALTER TABLE "+p.qualified+" DROP CHECK `record_text_present_"+physicalUUID(id)+"`").withNative(PhysicalNativeChange{Kind: "disable_text_unique", FieldID: id})
	result := []PhysicalStatement{start}
	for i, kind := range []string{"disable_text_insert", "disable_text_update"} {
		guard := source[i+1].step
		statement := p.statement(kind, "DROP TRIGGER "+p.schema+".`"+guard.Name+"`").withNative(PhysicalNativeChange{Kind: kind, FieldID: id})
		statement.step.Kind = "TRIGGER"
		statement.step.Name = guard.Name
		result = append(result, statement)
	}
	end := p.statement("remove_text_key", "ALTER TABLE "+p.qualified+" DROP FOREIGN KEY `record_text_ref_"+physicalUUID(id)+"`, DROP INDEX `record_unique_"+physicalUUID(id)+"`, DROP COLUMN `record_text_key_"+physicalUUID(id)+"`").withNative(PhysicalNativeChange{Kind: "remove_text_key", FieldID: id})
	return append(result, end), nil
}

func (p *PhysicalPlanner) SetTextNullable(id uuid.UUID, nullable bool) (PhysicalStatement, error) {
	f, err := p.textField(id)
	if err != nil {
		return PhysicalStatement{}, err
	}
	nullSQL := " NOT NULL"
	if nullable {
		nullSQL = " NULL"
	}
	return p.statement("text_nullability", "ALTER TABLE "+p.qualified+" MODIFY COLUMN "+f.columns["value"]+" LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"+nullSQL).withNative(PhysicalNativeChange{Kind: "text_nullability", FieldID: id, Nullable: nullable}), nil
}

func (p *PhysicalPlanner) regenerateTextConstraint(v PhysicalNativeChange, definer installation.Definer) (PhysicalStatement, error) {
	if v.Enum || v.Unique || v.IndexID != uuid.Nil || len(v.FieldIDs) != 0 || (v.Nullable && v.Kind != "text_nullability") {
		return PhysicalStatement{}, errors.New("invalid text constraint source")
	}
	if v.Kind == "text_nullability" {
		return p.SetTextNullable(v.FieldID, v.Nullable)
	}
	var statements []PhysicalStatement
	var err error
	if strings.HasPrefix(v.Kind, "enable_text_") || v.Kind == "text_unique_check" {
		statements, err = p.EnableTextUnique(v.FieldID, definer)
	} else {
		statements, err = p.DisableTextUnique(v.FieldID, definer)
	}
	if err != nil {
		return PhysicalStatement{}, err
	}
	for _, s := range statements {
		if s.native.Kind == v.Kind {
			return s, nil
		}
	}
	return PhysicalStatement{}, ErrPhysicalPlanningUnsupported
}

// ActiveGuards is the final published guard inventory, including retirement.
func (p PhysicalDDLPlan) ActiveGuards() []migrations.Step {
	if p.Version == 2 {
		return append([]migrations.Step(nil), p.Steps[1:]...)
	}
	removed := map[string]bool{}
	for _, s := range p.Steps {
		if s.Kind == "TRIGGER" && s.After == "" {
			removed[s.Name] = true
		}
	}
	var guards []migrations.Step
	for _, s := range p.RetainedGuards {
		if !removed[s.Name] {
			guards = append(guards, s)
		}
	}
	for _, s := range p.Steps {
		if s.Kind == "TRIGGER" && s.After != "" {
			guards = append(guards, s)
		}
	}
	return guards
}

func (p PhysicalDDLPlan) TextBackfillColumn() string {
	if p.Native == nil {
		return ""
	}
	for _, c := range p.Native.Changes {
		if c.Kind == "enable_text_unique" {
			for _, f := range p.Native.Fields {
				if f.Field.ID == c.FieldID && len(f.Components) == 1 {
					return f.Components[0].Name
				}
			}
		}
	}
	return ""
}

func validateTextConstraintSequences(plan PhysicalDDLPlan) error {
	enabled := 0
	for i, v := range plan.Native.Changes {
		var kinds []string
		switch v.Kind {
		case "enable_text_unique":
			kinds = []string{"enable_text_unique", "enable_text_insert", "enable_text_update", "text_unique_check"}
			enabled++
		case "disable_text_unique":
			kinds = []string{"disable_text_unique", "disable_text_insert", "disable_text_update", "remove_text_key"}
		case "enable_text_insert", "enable_text_update", "text_unique_check", "disable_text_insert", "disable_text_update", "remove_text_key":
			offset := 1
			switch v.Kind {
			case "enable_text_update", "disable_text_update":
				offset = 2
			case "text_unique_check", "remove_text_key":
				offset = 3
			}
			root := "enable_text_unique"
			if strings.HasPrefix(v.Kind, "disable_") || v.Kind == "remove_text_key" {
				root = "disable_text_unique"
			}
			if i < offset || plan.Native.Changes[i-offset].Kind != root || plan.Native.Changes[i-offset].FieldID != v.FieldID {
				return errors.New("orphan text constraint step")
			}
		}
		for j, kind := range kinds {
			if i+j >= len(plan.Native.Changes) || plan.Native.Changes[i+j].Kind != kind || plan.Native.Changes[i+j].FieldID != v.FieldID {
				return errors.New("complete ordered text constraint sequence required")
			}
		}
	}
	if enabled > 1 {
		return errors.New("one existing text field backfill per operation required")
	}
	return nil
}
