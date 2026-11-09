package mysql

import (
	"errors"
	"sort"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/google/uuid"
)

// V3 stores enough typed input to regenerate every supported native statement,
// rather than treating a qualified ALTER prefix as proof of allowed behavior.
type PhysicalNativeSource struct {
	Fields  []MappedPhysicalField  `json:"fields"`
	Changes []PhysicalNativeChange `json:"changes"`
}

type PhysicalNativeChange struct {
	Kind     string      `json:"kind"`
	FieldIDs []uuid.UUID `json:"field_ids,omitempty"`
	FieldID  uuid.UUID   `json:"field_id"`
	IndexID  uuid.UUID   `json:"index_id"`
	Enum     bool        `json:"enum,omitempty"`
	Unique   bool        `json:"unique,omitempty"`
	Nullable bool        `json:"nullable,omitempty"`
}

func (s PhysicalStatement) withNative(v PhysicalNativeChange) PhysicalStatement {
	s.native = v
	return s
}

func (p *PhysicalPlanner) nativeSource(statements []PhysicalStatement) *PhysicalNativeSource {
	source := &PhysicalNativeSource{}
	for _, f := range p.fields {
		mapped := MappedPhysicalField{Field: f.field}
		for component, column := range f.columns {
			mapped.Components = append(mapped.Components, PhysicalField{FieldID: f.field.ID, Component: component, Name: column[1 : len(column)-1]})
		}
		sort.Slice(mapped.Components, func(i, j int) bool { return mapped.Components[i].Component < mapped.Components[j].Component })
		source.Fields = append(source.Fields, mapped)
	}
	sort.Slice(source.Fields, func(i, j int) bool { return source.Fields[i].Field.ID.String() < source.Fields[j].Field.ID.String() })
	for _, statement := range statements {
		v := statement.native
		v.FieldIDs = append([]uuid.UUID(nil), v.FieldIDs...)
		source.Changes = append(source.Changes, v)
	}
	return source
}

func (p *PhysicalPlanner) regenerateNative(v PhysicalNativeChange) (PhysicalStatement, error) {
	invalid := errors.New("invalid typed native change")
	if v.Enum && v.Kind != "field_enum" {
		return PhysicalStatement{}, invalid
	}
	switch v.Kind {
	case "create_managed_index":
		if v.FieldID != uuid.Nil || v.Nullable || v.Unique {
			return PhysicalStatement{}, invalid
		}
		return p.CreateManagedIndex(v.IndexID, v.FieldIDs)
	case "delete_field":
		if v.IndexID != uuid.Nil || len(v.FieldIDs) != 0 || v.Unique || v.Nullable {
			return PhysicalStatement{}, invalid
		}
		return p.DropField(v.FieldID)
	case "add_fields":
		if v.FieldID != uuid.Nil || v.IndexID != uuid.Nil || v.Unique || v.Nullable {
			return PhysicalStatement{}, invalid
		}
		return p.AddFields(v.FieldIDs)
	case "create_index":
		if v.FieldID != uuid.Nil || v.Nullable {
			return PhysicalStatement{}, invalid
		}
		return p.CreateIndex(v.IndexID, v.FieldIDs, v.Unique)
	case "drop_index":
		if v.FieldID != uuid.Nil || len(v.FieldIDs) != 0 || v.Unique || v.Nullable {
			return PhysicalStatement{}, invalid
		}
		return p.DropIndex(v.IndexID)
	case "field_enum":
		if v.IndexID != uuid.Nil || len(v.FieldIDs) != 0 || v.Unique {
			return PhysicalStatement{}, invalid
		}
		return p.SetFieldEnum(v.FieldID, v.Enum, v.Nullable)
	case "create_field_unique", "drop_field_unique", "field_nullability", "field_nullability_checked":
		if v.IndexID != uuid.Nil || len(v.FieldIDs) != 0 || v.Unique {
			return PhysicalStatement{}, invalid
		}
		if v.Kind == "field_nullability_checked" {
			return p.SetFieldNullableChecked(v.FieldID, v.Nullable)
		}
		if v.Kind == "field_nullability" {
			return p.SetFieldNullable(v.FieldID, v.Nullable)
		}
		if v.Nullable {
			return PhysicalStatement{}, invalid
		}
		if v.Kind == "create_field_unique" {
			return p.CreateFieldUniqueIndex(v.FieldID)
		}
		return p.DropFieldUniqueIndex(v.FieldID)
	default:
		return PhysicalStatement{}, ErrPhysicalPlanningUnsupported
	}
}

func validateNativeSource(plan PhysicalDDLPlan, database, name string) error {
	if plan.Native == nil || len(plan.Native.Changes) != len(plan.Steps) {
		return errors.New("complete typed native source required")
	}
	p, err := NewPhysicalPlanner(database, plan.Creation.TenantID, PhysicalTable{TableID: plan.Creation.TableID, Name: name}, plan.Native.Fields)
	if err != nil {
		return err
	}
	if err := validateNativeMandatory(p, plan.Creation); err != nil {
		return err
	}
	if err := validateTextChangeSequence(plan); err != nil {
		return err
	}
	for i, v := range plan.Native.Changes {
		if v.Kind == "delete_field" {
			f := p.fields[v.FieldID]
			if f.field.DataType == datamodel.DataTypeString && f.field.IsUnique && (i < 4 || plan.Native.Changes[i-1].Kind != "remove_text_key" || plan.Native.Changes[i-1].FieldID != v.FieldID) {
				return errors.New("text field deletion requires prior guard retirement")
			}
		}
	}
	var empty []string
	for i, v := range plan.Native.Changes {
		var generated PhysicalStatement
		var err error
		switch v.Kind {
		case "add_unique_text", "text_guard_insert", "text_guard_update":
			if v.Enum || v.Unique || v.Nullable || v.IndexID != uuid.Nil || len(v.FieldIDs) != 0 {
				return errors.New("invalid text change")
			}
			steps, e := p.AddUniqueText(v.FieldID, plan.Creation.Definer)
			err = e
			if e == nil {
				index := 0
				if v.Kind == "text_guard_insert" {
					index = 1
				}
				if v.Kind == "text_guard_update" {
					index = 2
				}
				generated = steps[index]
			}
		case "enable_text_unique", "enable_text_insert", "enable_text_update", "text_unique_check", "disable_text_unique", "disable_text_insert", "disable_text_update", "remove_text_key", "text_nullability":
			generated, err = p.regenerateTextConstraint(v, plan.Creation.Definer)
		default:
			generated, err = p.regenerateNative(v)
		}
		if err != nil {
			return err
		}
		want, got := generated.step, plan.Steps[i]
		if got.ID != want.ID || got.Kind != want.Kind || got.Name != want.Name || got.SQL != want.SQL {
			return errors.New("native step differs from typed source")
		}
		if generated.requireEmpty {
			empty = append(empty, want.ID)
		}
	}
	if len(empty) != len(plan.EmptyTableBefore) {
		return errors.New("native preconditions differ from typed source")
	}
	for i, id := range empty {
		if plan.EmptyTableBefore[i] != id {
			return errors.New("native precondition order differs from source")
		}
	}
	return nil
}

func validateNativeMandatory(p *PhysicalPlanner, identity *PhysicalCreationIdentity) error {
	object, ok := p.fields[identity.ObjectFieldID]
	updated, hasUpdated := p.fields[identity.UpdatedFieldID]
	if !ok || !hasUpdated || object.field.TenantID != identity.TenantID || updated.field.TenantID != identity.TenantID || object.field.Archived || updated.field.Archived || object.field.Name != "object_id" || updated.field.Name != "updated_at" || object.field.DataType != datamodel.DataTypeString || updated.field.DataType != datamodel.DataTypeTimestamp || object.field.Nullable || updated.field.Nullable || object.field.IsEnum || updated.field.IsEnum || !object.field.IsUnique || updated.field.IsUnique || object.columns["value"] != "`"+identity.ObjectColumn+"`" || updated.columns["value"] != "`"+identity.UpdatedColumn+"`" {
		return ErrPhysicalConflict
	}
	return nil
}
