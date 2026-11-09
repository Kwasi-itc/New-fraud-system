package mysql

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
)

// BindRecordChanges binds typed field changes while preserving the immutable
// exact-key guard lineage from a reviewed v2 creation or earlier v3 change.
// It cannot change mandatory columns or their envelope/uniqueness constraints.
// References are isolated observations, never adopted from application drift.
func (p *PhysicalPlanner) BindRecordChanges(previous PhysicalDDLPlan, statements []PhysicalStatement, reference []migrations.Step) (PhysicalDDLPlan, error) {
	raw, err := json.Marshal(previous)
	if err != nil {
		return PhysicalDDLPlan{}, err
	}
	original, err := decodePhysicalPlan(raw, p.database, p.name)
	if err != nil {
		return PhysicalDDLPlan{}, err
	}
	if original.Version != 2 && original.Version != 3 {
		return PhysicalDDLPlan{}, ErrPhysicalPlanningUnsupported
	}
	plan, err := p.bindGuardedReferences(statements, reference)
	if err != nil {
		return PhysicalDDLPlan{}, err
	}
	identity := *original.Creation
	before := original.FinalTableDefinition()
	if plan.Steps[0].Before != before {
		return PhysicalDDLPlan{}, migrations.ErrDrift
	}
	if err := validateNativeMandatory(p, &identity); err != nil {
		return PhysicalDDLPlan{}, err
	}
	plan.Version = 3
	plan.Creation = &identity
	plan.Native = p.nativeSource(statements)
	plan.RetainedGuards = original.ActiveGuards()
	raw, err = json.Marshal(plan)
	if err != nil {
		return PhysicalDDLPlan{}, err
	}
	return decodePhysicalPlan(raw, p.database, p.name)
}

func validateRetainedRecordGuards(plan PhysicalDDLPlan, database, name string) error {
	c := plan.Creation
	if c == nil || len(plan.RetainedGuards) < 2 || len(plan.RetainedGuards) > 2000 {
		return errors.New("guarded native changes require original identity and two guards")
	}
	fields := []MappedPhysicalField{
		{Field: datamodel.Field{ID: c.ObjectFieldID, TenantID: c.TenantID, TableID: c.TableID, Name: "object_id", DataType: datamodel.DataTypeString, IsUnique: true}, Components: []PhysicalField{{FieldID: c.ObjectFieldID, Name: c.ObjectColumn, Component: "value"}}},
		{Field: datamodel.Field{ID: c.UpdatedFieldID, TenantID: c.TenantID, TableID: c.TableID, Name: "updated_at", DataType: datamodel.DataTypeTimestamp}, Components: []PhysicalField{{FieldID: c.UpdatedFieldID, Name: c.UpdatedColumn, Component: "value"}}},
	}
	p, err := NewPhysicalPlanner(database, c.TenantID, PhysicalTable{TableID: c.TableID, Name: name}, fields)
	if err != nil {
		return err
	}
	expected, err := p.CreateTableReference(c.ObjectFieldID, c.UpdatedFieldID, c.Definer)
	if err != nil {
		return err
	}
	for i, got := range plan.RetainedGuards[:2] {
		want := expected.steps[i+1]
		if got.ID != want.ID || got.Kind != want.Kind || got.Name != want.Name || got.SQL != want.SQL || got.Before != "" {
			return errors.New("retained record guard differs from typed source")
		}
	}
	if err := validateRetainedTextGuards(plan, database, name); err != nil {
		return err
	}
	if err := (migrations.Plan{Database: database, Steps: plan.RetainedGuards}).Validate(); err != nil {
		return err
	}
	protected := []string{c.ObjectColumn, c.UpdatedColumn, "id", "valid_from", "valid_until", "record_object_key_id", "record_unique_" + physicalUUID(c.ObjectFieldID), "record_object_key_ref_" + physicalUUID(c.TableID), "record_object_key_present_" + physicalUUID(c.TableID), "PRIMARY"}
	for i, s := range plan.Steps {
		// Typed ADD INDEX never modifies mandatory columns/guards. Its complete
		// SQL is regenerated below; arbitrary ALTERs still fail validation.
		if plan.Native != nil && i < len(plan.Native.Changes) && (plan.Native.Changes[i].Kind == "create_managed_index" || plan.Native.Changes[i].Kind == "create_index") {
			continue
		}
		for _, column := range protected {
			if strings.Contains(s.SQL, "`"+column+"`") {
				return errors.New("mandatory record columns and constraints cannot be changed by a native plan")
			}
		}
	}
	return validateNativeSource(plan, database, name)
}
