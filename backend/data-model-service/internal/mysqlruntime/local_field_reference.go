package mysqlruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

// Rebuild the complete published shape only on the disposable reference. Capture
// the whole sequence on one connection so trigger removal and later steps see
// their preceding state. Application shape is never adopted as a baseline.
func (p *LocalCreation) nativeReferences(ctx context.Context, table store.PhysicalTable, previous store.PhysicalDDLPlan, statements []store.PhysicalStatement) (refs []migrations.Step, result error) {
	var definition map[string]string
	before := previous.FinalTableDefinition()
	if err := json.Unmarshal([]byte(before), &definition); err != nil {
		return nil, err
	}
	create := definition["Create Table"]
	if !strings.HasPrefix(create, "CREATE TABLE `"+table.Name+"`") {
		return nil, errors.New("retained table definition required")
	}
	name, err := mysqlstore.Identifier(table.Name)
	if err != nil {
		return nil, err
	}
	conn, err := p.Reference.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { result = errors.Join(result, conn.Close()) }()
	if _, err = conn.ExecContext(ctx, "DROP TABLE IF EXISTS fraud."+name); err != nil {
		return nil, err
	}
	if _, err = conn.ExecContext(ctx, create); err != nil {
		return nil, err
	}
	observed, err := migrations.Definition(ctx, conn, "fraud", "TABLE", table.Name)
	if err != nil {
		return nil, err
	}
	if observed != before {
		return nil, migrations.ErrDrift
	}
	for _, guard := range previous.ActiveGuards() {
		if _, err = conn.ExecContext(ctx, guard.SQL); err != nil {
			return nil, err
		}
		actual, err := migrations.Definition(ctx, conn, "fraud", "TRIGGER", guard.Name)
		if err != nil {
			return nil, err
		}
		if actual != guard.After {
			return nil, migrations.ErrDrift
		}
	}
	for _, statement := range statements {
		step := statement.ReferenceStep()
		step.Before, err = migrations.Definition(ctx, conn, "fraud", step.Kind, step.Name)
		if err != nil {
			return nil, err
		}
		if _, err = conn.ExecContext(ctx, step.SQL); err != nil {
			return nil, err
		}
		step.After, err = migrations.Definition(ctx, conn, "fraud", step.Kind, step.Name)
		if err != nil {
			return nil, err
		}
		refs = append(refs, step)
	}
	return refs, nil
}
