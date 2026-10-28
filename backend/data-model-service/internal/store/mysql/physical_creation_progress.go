package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
)

// verifyCreationProgress checks all three independent objects before any further
// DDL. An applied table or earlier trigger must not be silently trusted just
// because the current dirty trigger matches. Unjournaled objects are never
// adopted. Call only with a validated v2 plan, checked journal and pinned lock.
func verifyCreationProgress(ctx context.Context, conn *sql.Conn, database, name string, plan PhysicalDDLPlan, states []string) error {
	for i, step := range plan.Steps {
		actual, err := migrations.Definition(ctx, conn, database, step.Kind, step.Name)
		if err != nil {
			return err
		}
		state := ""
		if i < len(states) {
			state = states[i]
		}
		valid := false
		switch state {
		case "":
			valid = actual == step.Before
		case "dirty":
			valid = actual == step.Before || actual == step.After
		case "applied":
			valid = actual == step.After
		}
		if !valid {
			return migrations.ErrDrift
		}
	}
	// SHOW CREATE TABLE excludes triggers. Reject any unplanned trigger, with a
	// bounded query (at most two expected, the third proves drift).
	rows, err := conn.QueryContext(ctx, "SELECT TRIGGER_NAME FROM information_schema.TRIGGERS WHERE EVENT_OBJECT_SCHEMA=? AND EVENT_OBJECT_TABLE=? ORDER BY TRIGGER_NAME LIMIT 3", database, name)
	if err != nil {
		return err
	}
	for rows.Next() {
		var trigger string
		if err = rows.Scan(&trigger); err != nil {
			break
		}
		if trigger != plan.Steps[1].Name && trigger != plan.Steps[2].Name {
			err = migrations.ErrDrift
			break
		}
	}
	return errors.Join(err, rows.Err(), rows.Close())
}
