package localdev

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

// Only the independent reference server may use captured shapes without a
// journal. A partial journaled replay must go through the normal strict runner.
func referenceReady(ctx context.Context, db *sql.DB, plan migrations.Plan, through string) (bool, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	check := func(steps []migrations.Step) (bool, int, error) {
		latest := map[string]migrations.Step{}
		for _, step := range steps {
			latest[step.Kind+":"+step.Name] = step
		}
		complete, present := true, 0
		for _, step := range latest {
			actual, err := migrations.Definition(ctx, conn, plan.Database, step.Kind, step.Name)
			if err != nil {
				return false, 0, err
			}
			if actual != "" {
				present++
			}
			if actual != step.After {
				complete = false
			}
		}
		return complete, present, nil
	}
	complete, present, err := check(plan.Steps)
	if err != nil || complete {
		return complete, err
	}
	var journal bool
	if err := conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema=? AND table_name=?)", plan.Database, plan.Owner+"_schema_migrations").Scan(&journal); err != nil {
		return false, err
	}
	if journal {
		return false, nil
	}
	if through != "" {
		found := false
		for i, step := range plan.Steps {
			if step.ID != through {
				continue
			}
			found = true
			complete, present, err = check(plan.Steps[:i+1])
			if err != nil || complete {
				return complete, err
			}
			break
		}
		if !found {
			return false, errors.New("reference prerequisite is absent from retained plan")
		}
	}
	if present != 0 {
		return false, errors.New("partial unjournaled reference schema; recreate only the disposable reference container")
	}
	return false, nil
}
