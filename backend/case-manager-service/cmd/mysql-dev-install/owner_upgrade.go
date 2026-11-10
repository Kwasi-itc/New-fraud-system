package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

// extendLocalOwnerPlan captures append-only upgrades on the independent
// disposable reference server. Retained definitions are verified before DDL;
// the destination's observed schema is never adopted as a trusted artifact.
func extendLocalOwnerPlan(source []migrations.Step) func(context.Context, *sql.DB, migrations.Plan) (migrations.Plan, error) {
	return func(ctx context.Context, reference *sql.DB, prior migrations.Plan) (migrations.Plan, error) {
		if err := prior.Validate(); err != nil {
			return prior, err
		}
		if len(source) <= len(prior.Steps) {
			return prior, migrations.ErrLineage
		}
		latest := map[string]migrations.Step{}
		for i, step := range prior.Steps {
			s := source[i]
			if s.ID != step.ID || s.Kind != step.Kind || s.Name != step.Name || s.SQL != step.SQL {
				return prior, migrations.ErrLineage
			}
			latest[step.Kind+":"+step.Name] = step
		}
		conn, err := reference.Conn(ctx)
		if err != nil {
			return prior, err
		}
		defer conn.Close()
		for _, step := range latest {
			actual, err := migrations.Definition(ctx, conn, prior.Database, step.Kind, step.Name)
			if err != nil {
				return prior, err
			}
			if actual != step.After {
				return prior, fmt.Errorf("reference object %s %s: %w", step.Kind, step.Name, migrations.ErrDrift)
			}
		}
		out := prior
		out.Steps = append([]migrations.Step(nil), prior.Steps...)
		for _, step := range source[len(prior.Steps):] {
			if strings.HasPrefix(step.SQL, "ALTER ") {
				step.Before, err = migrations.Definition(ctx, conn, prior.Database, step.Kind, step.Name)
				if err != nil {
					return prior, err
				}
			}
			if _, err := conn.ExecContext(ctx, step.SQL); err != nil {
				return prior, fmt.Errorf("reference upgrade %s: %w", step.ID, err)
			}
			step.After, err = migrations.Definition(ctx, conn, prior.Database, step.Kind, step.Name)
			if err != nil {
				return prior, err
			}
			out.Steps = append(out.Steps, step)
		}
		return out, out.Validate()
	}
}
