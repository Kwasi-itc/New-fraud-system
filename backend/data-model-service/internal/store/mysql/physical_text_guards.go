package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/google/uuid"
)

func (p *PhysicalPlanner) bindGuardedReferences(statements []PhysicalStatement, refs []migrations.Step) (PhysicalDDLPlan, error) {
	plan := PhysicalDDLPlan{Version: 3}
	if len(statements) == 0 || len(statements) > 1000 || len(statements) != len(refs) {
		return plan, errors.New("complete bounded reference chain required")
	}
	for i, s := range statements {
		r := refs[i]
		want := s.step
		if r.ID != want.ID || r.Kind != want.Kind || r.Name != want.Name || r.SQL != want.SQL {
			return plan, errors.New("reference differs from typed statement")
		}
		plan.Steps = append(plan.Steps, r)
		if s.requireEmpty {
			plan.EmptyTableBefore = append(plan.EmptyTableBefore, r.ID)
		}
	}
	return plan, nil
}

// Every previously published unique text field must retain exactly its two
// typed guards. New fields must install both guards in this operation instead.
func validateRetainedTextGuards(plan PhysicalDDLPlan, database, name string) error {
	if plan.Native == nil {
		return errors.New("typed field snapshot required")
	}
	p, err := NewPhysicalPlanner(database, plan.Creation.TenantID, PhysicalTable{TableID: plan.Creation.TableID, Name: name}, plan.Native.Fields)
	if err != nil {
		return err
	}
	added := map[uuid.UUID]bool{}
	for _, c := range plan.Native.Changes {
		if c.Kind == "add_unique_text" || c.Kind == "enable_text_unique" {
			if added[c.FieldID] {
				return ErrPhysicalConflict
			}
			added[c.FieldID] = true
		}
	}
	expected := map[string]migrations.Step{}
	for id, f := range p.fields {
		if id == plan.Creation.ObjectFieldID || f.field.DataType != datamodel.DataTypeString || !f.field.IsUnique || added[id] {
			continue
		}
		steps, err := p.AddUniqueText(id, plan.Creation.Definer)
		if err != nil {
			return err
		}
		for _, s := range steps[1:] {
			expected[s.step.Name] = s.step
		}
	}
	for _, got := range plan.RetainedGuards[2:] {
		want, ok := expected[got.Name]
		if !ok || got.ID != want.ID || got.SQL != want.SQL || got.Kind != want.Kind || got.Before != "" {
			return errors.New("retained text guard differs from typed source")
		}
		delete(expected, got.Name)
	}
	if len(expected) != 0 {
		return errors.New("missing retained text guard")
	}
	return nil
}

func verifyRetainedRecordGuards(ctx context.Context, conn *sql.Conn, database, name string, plan PhysicalDDLPlan) error {
	states := make([]string, len(plan.Steps))
	for i := range states {
		states[i] = "applied"
	}
	return verifyRecordGuardProgress(ctx, conn, database, name, plan, states)
}

// Journal-aware inspection allows only the before/after pair of a dirty step.
// Unjournaled guards, changed guards and missing applied guards are all drift.
func verifyRecordGuardProgress(ctx context.Context, conn *sql.Conn, database, name string, plan PhysicalDDLPlan, states []string) error {
	expected := map[string]bool{}
	changing := map[string]bool{}
	for _, s := range plan.Steps {
		if s.Kind == "TRIGGER" {
			changing[s.Name] = true
		}
	}
	for _, guard := range plan.RetainedGuards {
		if changing[guard.Name] {
			continue
		}
		actual, err := migrations.Definition(ctx, conn, database, "TRIGGER", guard.Name)
		if err != nil {
			return err
		}
		if actual != guard.After {
			return migrations.ErrDrift
		}
		expected[guard.Name] = true
	}
	tableBefore, tableAfter := "", ""
	for i, step := range plan.Steps {
		state := ""
		if i < len(states) {
			state = states[i]
		}
		if step.Kind == "TABLE" {
			if tableBefore == "" {
				tableBefore = step.Before
				tableAfter = step.Before
			}
			if state == "applied" {
				tableBefore = step.After
				tableAfter = step.After
			}
			if state == "dirty" {
				tableBefore = step.Before
				tableAfter = step.After
			}
			continue
		}
		actual, err := migrations.Definition(ctx, conn, database, "TRIGGER", step.Name)
		if err != nil {
			return err
		}
		switch state {
		case "applied":
			if actual != step.After {
				return migrations.ErrDrift
			}
		case "dirty":
			if actual != step.Before && actual != step.After {
				return migrations.ErrDrift
			}
		default:
			if actual != step.Before {
				return migrations.ErrDrift
			}
		}
		if actual != "" {
			expected[step.Name] = true
		}
	}
	actual, err := migrations.Definition(ctx, conn, database, "TABLE", name)
	if err != nil {
		return err
	}
	if actual != tableBefore && actual != tableAfter {
		return migrations.ErrDrift
	}
	rows, err := conn.QueryContext(ctx, "SELECT TRIGGER_NAME FROM information_schema.TRIGGERS WHERE EVENT_OBJECT_SCHEMA=? AND EVENT_OBJECT_TABLE=? ORDER BY TRIGGER_NAME LIMIT 2003", database, name)
	if err != nil {
		return err
	}
	for rows.Next() {
		var trigger string
		if err = rows.Scan(&trigger); err != nil {
			break
		}
		if !expected[trigger] {
			err = migrations.ErrDrift
			break
		}
		delete(expected, trigger)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	if len(expected) != 0 {
		return migrations.ErrDrift
	}
	return nil
}
