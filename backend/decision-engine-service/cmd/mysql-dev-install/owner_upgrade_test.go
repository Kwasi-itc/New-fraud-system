package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

func TestLocalOwnerUpgradePreservesPrefixAndRejectsReferenceDrift(t *testing.T) {
	for _, drift := range []bool{false, true} {
		name := "append"
		if drift {
			name = "drift"
		}
		t.Run(name, func(t *testing.T) {
			db, database := testsupport.DecisionMySQLDatabase(t)
			schema, _ := mysqlstore.Identifier(database)
			source := []migrations.Step{
				{ID: "local_fixture_1", Kind: "TABLE", Name: "local_upgrade_fixture", SQL: "CREATE TABLE " + schema + ".local_upgrade_fixture(id INT PRIMARY KEY, state INT NOT NULL) ENGINE=InnoDB"},
				{ID: "local_fixture_2", Kind: "TABLE", Name: "local_upgrade_fixture", SQL: "ALTER TABLE " + schema + ".local_upgrade_fixture ADD KEY local_fixture_state(state,id)"},
			}
			ctx := context.Background()
			if _, err := db.ExecContext(ctx, source[0].SQL); err != nil {
				t.Fatal(err)
			}
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			definition, err := migrations.Definition(ctx, conn, database, "TABLE", source[0].Name)
			conn.Close()
			if err != nil {
				t.Fatal(err)
			}
			step := source[0]
			step.After = definition
			prior := migrations.Plan{Database: database, Owner: "decision_local", Steps: []migrations.Step{step}}
			if drift {
				if _, err := db.ExecContext(ctx, "ALTER TABLE "+schema+".local_upgrade_fixture ADD COLUMN unexpected INT"); err != nil {
					t.Fatal(err)
				}
			}
			out, err := extendLocalOwnerPlan(source)(ctx, db, prior)
			if drift {
				if !errors.Is(err, migrations.ErrDrift) {
					t.Fatal("adopted drift", err)
				}
				return
			}
			if err != nil || len(out.Steps) != 2 || !reflect.DeepEqual(out.Steps[:1], prior.Steps) || out.Steps[1].Before != definition || out.Steps[1].After == definition {
				t.Fatal(out, err)
			}
			if err := out.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
