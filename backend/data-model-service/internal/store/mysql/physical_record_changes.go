package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
)

// BindRecordChanges binds typed native ALTERs while preserving the immutable
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
	plan, err := p.Bind(statements, reference)
	if err != nil {
		return PhysicalDDLPlan{}, err
	}
	identity := *original.Creation
	before := original.Steps[0].After
	if original.Version == 3 {
		before = original.Steps[len(original.Steps)-1].After
	}
	if plan.Steps[0].Before != before {
		return PhysicalDDLPlan{}, migrations.ErrDrift
	}
	if err := validateNativeMandatory(p, &identity); err != nil {
		return PhysicalDDLPlan{}, err
	}
	plan.Version = 3
	plan.Creation = &identity
	plan.Native = p.nativeSource(statements)
	guards := original.RetainedGuards
	if original.Version == 2 {
		guards = original.Steps[1:]
	}
	plan.RetainedGuards = append([]migrations.Step(nil), guards...)
	raw, err = json.Marshal(plan)
	if err != nil {
		return PhysicalDDLPlan{}, err
	}
	return decodePhysicalPlan(raw, p.database, p.name)
}

func validateRetainedRecordGuards(plan PhysicalDDLPlan, database, name string) error {
	c := plan.Creation
	if c == nil || len(plan.RetainedGuards) != 2 {
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
	for i, got := range plan.RetainedGuards {
		want := expected.steps[i+1]
		if got.ID != want.ID || got.Kind != want.Kind || got.Name != want.Name || got.SQL != want.SQL || got.Before != "" {
			return errors.New("retained record guard differs from typed source")
		}
	}
	if err := (migrations.Plan{Database: database, Steps: plan.RetainedGuards}).Validate(); err != nil {
		return err
	}
	protected := []string{c.ObjectColumn, c.UpdatedColumn, "id", "valid_from", "valid_until", "record_object_key_id", "record_unique_" + physicalUUID(c.ObjectFieldID), "record_object_key_ref_" + physicalUUID(c.TableID), "record_object_key_present_" + physicalUUID(c.TableID), "PRIMARY"}
	for _, s := range plan.Steps {
		for _, column := range protected {
			if strings.Contains(s.SQL, "`"+column+"`") {
				return errors.New("mandatory record columns and constraints cannot be changed by a native plan")
			}
		}
	}
	return validateNativeSource(plan, database, name)
}

// The caller holds the execution lock. Guards are neither journaled anew nor
// recreated: missing/changed/extra guards are drift before any further ALTER.
func verifyRetainedRecordGuards(ctx context.Context, conn *sql.Conn, database, name string, plan PhysicalDDLPlan) error {
	for _, guard := range plan.RetainedGuards {
		actual, err := migrations.Definition(ctx, conn, database, "TRIGGER", guard.Name)
		if err != nil {
			return err
		}
		if actual != guard.After {
			return migrations.ErrDrift
		}
	}
	rows, err := conn.QueryContext(ctx, "SELECT TRIGGER_NAME FROM information_schema.TRIGGERS WHERE EVENT_OBJECT_SCHEMA=? AND EVENT_OBJECT_TABLE=? ORDER BY TRIGGER_NAME LIMIT 3", database, name)
	if err != nil {
		return err
	}
	count := 0
	for rows.Next() {
		var trigger string
		if err = rows.Scan(&trigger); err != nil {
			break
		}
		count++
		if trigger != plan.RetainedGuards[0].Name && trigger != plan.RetainedGuards[1].Name {
			err = migrations.ErrDrift
			break
		}
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	if count != 2 {
		return migrations.ErrDrift
	}
	return nil
}
