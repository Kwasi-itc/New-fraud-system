package testsupport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/mysqlidentity"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlaccess"
	model "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/mysqlschema"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	screen "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/mysqlschema"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InstallSharedMySQLOwners extends a disposable decision fixture in dependency
// order. Only a separate generated reference is observed; drift in the target is
// never accepted as a migration artifact. All services share this one database.
func InstallSharedMySQLOwners(t *testing.T, db *sql.DB, database string) {
	t.Helper()
	if !strings.HasPrefix(database, "decision_runtime_fixture_") {
		t.Fatal("shared owner setup requires a generated decision fixture")
	}
	ref, refName, _, _ := DecisionMySQLRuntimeDatabase(t)
	ctx := context.Background()
	for _, owner := range []struct {
		name       string
		statements func(string, string) ([]migrations.Step, error)
		plan       func(string, string, []migrations.Step) (migrations.Plan, error)
	}{
		{"data_model", model.Statements, model.Plan},
		{"identity", mysqlidentity.Statements, mysqlidentity.Plan},
		{"case_manager", mysqlaccess.Statements, mysqlaccess.Plan},
		{"screening", screen.Statements, screen.Plan},
	} {
		definer := "root"
		if owner.name == "data_model" {
			definer = "dm_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
			if _, err := db.ExecContext(ctx, "CREATE USER `"+definer+"`@`localhost` ACCOUNT LOCK"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := db.ExecContext(context.Background(), "DROP USER `"+definer+"`@`localhost`"); err != nil {
					t.Error(err)
				}
			})
		}
		source, err := owner.statements(refName, definer)
		if err != nil {
			t.Fatal(err)
		}
		target, err := owner.statements(database, definer)
		if err != nil {
			t.Fatal(err)
		}
		conn, err := ref.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		prior := map[string]string{}
		for i, s := range source {
			target[i].Before = prior[s.Kind+":"+s.Name]
			if _, err := conn.ExecContext(ctx, s.SQL); err != nil {
				conn.Close()
				t.Fatalf("%s reference %s: %v", owner.name, s.ID, err)
			}
			def, err := migrations.Definition(ctx, conn, refName, s.Kind, s.Name)
			if err != nil {
				conn.Close()
				t.Fatal(err)
			}
			target[i].After = strings.ReplaceAll(def, refName, database)
			prior[s.Kind+":"+s.Name] = target[i].After
		}
		conn.Close()
		plan, err := owner.plan(database, definer, target)
		if err != nil {
			t.Fatal(err)
		}
		runner, err := migrations.NewForOwner(db, owner.name, plan)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := runner.Apply(ctx); err != nil {
				t.Fatal(owner.name, err)
			}
		}
		if err := runner.Verify(ctx, nil); err != nil {
			t.Fatal(err)
		}
		if owner.name == "data_model" {
			grants, err := model.DefinerGrants(database, definer)
			if err != nil {
				t.Fatal(err)
			}
			for _, q := range grants {
				if _, err := db.ExecContext(ctx, q); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

// SharedPostgresOwners uses independently tracked fixture ledgers for the actual
// source migrations. Each file and its digest commit atomically in its owner's
// transaction. The admin URL helper creates a fresh database and guards cleanup.
func SharedPostgresOwners(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	db, err := pgxpool.New(ctx, PostgresDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	for _, owner := range []string{"data-model-service", "ingestion-service", "decision-engine-service", "screening-service", "case-manager-service", "auth-service"} {
		paths, err := filepath.Glob(filepath.Join(root, owner, "internal/migrations/metadata/*.up.sql"))
		if err != nil {
			t.Fatal(err)
		}
		if owner == "auth-service" {
			paths = []string{filepath.Join(root, owner, "internal/platform/schema.sql"), filepath.Join(root, owner, "internal/platform/schema_v2.sql")}
		}
		if len(paths) == 0 {
			t.Fatal("missing owner migrations", owner)
		}
		ledger := pgx.Identifier{"public", "fixture_" + strings.ReplaceAll(owner, "-", "_") + "_migrations"}.Sanitize()
		if _, err := db.Exec(ctx, "CREATE TABLE "+ledger+"(name TEXT PRIMARY KEY,sha256 TEXT NOT NULL)"); err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, string(raw)); err == nil {
				_, err = tx.Exec(ctx, "INSERT INTO "+ledger+" VALUES($1,$2)", filepath.Base(path), fmt.Sprintf("%x", sha256.Sum256(raw)))
			}
			if err != nil {
				tx.Rollback(ctx)
				t.Fatal(path, err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	return db
}
