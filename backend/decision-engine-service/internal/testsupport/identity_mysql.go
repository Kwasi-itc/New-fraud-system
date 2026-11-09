package testsupport

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlaccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

// InstallDecisionAccessOwners installs actual independently tracked auth/case
// plans, captured only on a generated disposable reference database.
func InstallDecisionAccessOwners(t *testing.T, db *sql.DB, database string) {
	t.Helper()
	ctx := context.Background()
	ref, refName, _, _ := DecisionMySQLRuntimeDatabase(t)
	for _, owner := range []struct {
		name       string
		statements func(string, string) ([]migrations.Step, error)
		plan       func(string, string, []migrations.Step) (migrations.Plan, error)
	}{
		{"identity", mysqlidentity.Statements, mysqlidentity.Plan}, {"case_manager", mysqlaccess.Statements, mysqlaccess.Plan},
	} {
		source, err := owner.statements(refName, "root")
		if err != nil {
			t.Fatal(err)
		}
		target, err := owner.statements(database, "root")
		if err != nil {
			t.Fatal(err)
		}
		conn, err := ref.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i, s := range source {
			if _, err := conn.ExecContext(ctx, s.SQL); err != nil {
				conn.Close()
				t.Fatal(err)
			}
			def, err := migrations.Definition(ctx, conn, refName, s.Kind, s.Name)
			if err != nil {
				conn.Close()
				t.Fatal(err)
			}
			target[i].After = strings.ReplaceAll(def, refName, database)
		}
		conn.Close()
		plan, err := owner.plan(database, "root", target)
		if err != nil {
			t.Fatal(err)
		}
		runner, err := migrations.NewForOwner(db, owner.name, plan)
		if err != nil {
			t.Fatal(err)
		}
		if err := runner.Apply(ctx); err != nil {
			t.Fatal(err)
		}
		if err := runner.Verify(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
}
