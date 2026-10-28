package metadata_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func TestMigrationRunner(t *testing.T) {
	for _, scenario := range []string{"fresh_upgrade_replay", "checksum_change", "preexisting_object", "applied_drift", "dirty_absent", "dirty_applied", "dirty_conflict", "short_plan", "cancelled", "competing_installer", "ledger_drift"} {
		t.Run(scenario, func(t *testing.T) {
			db, database := metadataDB(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			schema, _ := mysqlstore.Identifier(database)
			create := "CREATE TABLE " + schema + ".data_model_runner_probe (id BINARY(16) PRIMARY KEY) ENGINE=InnoDB DEFAULT CHARSET=ascii COLLATE=ascii_bin"
			alter := "ALTER TABLE " + schema + ".data_model_runner_probe ADD COLUMN revision BIGINT NOT NULL DEFAULT 0"
			exec := func(statement string) {
				t.Helper()
				if _, err := db.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			definition := func() string {
				t.Helper()
				conn, err := db.Conn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				d, err := migrations.Definition(ctx, conn, database, "TABLE", "data_model_runner_probe")
				if err != nil {
					t.Fatal(err)
				}
				return d
			}
			// Build a reviewed fixture expectation, then restore absence. Production
			// manifests must ship these expectations; never derive them from targets.
			exec(create)
			before := definition()
			exec(alter)
			after := definition()
			exec("DROP TABLE " + schema + ".data_model_runner_probe")
			plan := migrations.Plan{Database: database, Steps: []migrations.Step{
				{ID: "create_probe", Kind: "TABLE", Name: "data_model_runner_probe", SQL: create, After: before},
				{ID: "add_revision", Kind: "TABLE", Name: "data_model_runner_probe", SQL: alter, Before: before, After: after},
			}}
			newRunner := func(p migrations.Plan) *migrations.Runner {
				t.Helper()
				r, err := migrations.New(db, p)
				if err != nil {
					t.Fatal(err)
				}
				return r
			}
			if scenario == "preexisting_object" {
				exec(create)
			}
			if strings.HasPrefix(scenario, "dirty_") {
				// Invalid engine leaves a durable dirty record before failed DDL.
				plan.Steps[0].SQL = strings.Replace(create, "ENGINE=InnoDB", "ENGINE=missing_engine", 1)
			}
			r := newRunner(plan)
			if scenario == "competing_installer" {
				hash := sha256.Sum256([]byte(database))
				lock := fmt.Sprintf("data_model_migrate_%x", hash[:22])
				conn, err := db.Conn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				var acquired int
				if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lock).Scan(&acquired); err != nil || acquired != 1 {
					t.Fatalf("lock=%d err=%v", acquired, err)
				}
				if err := r.Apply(ctx); !errors.Is(err, mysqlstore.ErrDDLLockBusy) {
					t.Fatalf("competing installer: %v", err)
				}
				if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lock).Scan(&acquired); err != nil || acquired != 1 {
					t.Fatalf("release=%d err=%v", acquired, err)
				}
			}
			if h, err := r.Status(ctx); err != nil || len(h) != 0 {
				t.Fatalf("initial history=%v err=%v", h, err)
			}
			if scenario == "cancelled" {
				cancel()
				if err := r.Apply(ctx); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				return
			}
			err := r.Apply(ctx)
			if scenario == "preexisting_object" {
				if !errors.Is(err, migrations.ErrDrift) {
					t.Fatal(err)
				}
				return
			}
			if strings.HasPrefix(scenario, "dirty_") {
				if err == nil {
					t.Fatal("failed DDL succeeded")
				}
				h, err := r.Status(ctx)
				if err != nil || len(h) != 1 || h[0].State != "dirty" {
					t.Fatalf("dirty history=%v err=%v", h, err)
				}
				if scenario == "dirty_absent" {
					if err := r.Apply(ctx); err == nil {
						t.Fatal("invalid DDL retry succeeded")
					}
					return
				}
				exec(create)
				if scenario == "dirty_conflict" {
					exec("ALTER TABLE " + schema + ".data_model_runner_probe ADD COLUMN unexpected INT")
					if err := r.Apply(ctx); !errors.Is(err, migrations.ErrDrift) {
						t.Fatal(err)
					}
					return
				}
				// Exact post-DDL state is reconciled without executing invalid SQL.
				if err := r.Apply(ctx); err != nil {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "ledger_drift":
				exec("ALTER TABLE " + schema + ".data_model_schema_migrations DROP CHECK data_model_migration_state")
				if err := r.Apply(ctx); !errors.Is(err, migrations.ErrDrift) {
					t.Fatal(err)
				}
				return
			case "checksum_change":
				plan.Steps[0].SQL += " "
				if err := newRunner(plan).Apply(ctx); !errors.Is(err, migrations.ErrLineage) {
					t.Fatal(err)
				}
				return
			case "short_plan":
				plan.Steps = plan.Steps[:1]
				if err := newRunner(plan).Apply(ctx); !errors.Is(err, migrations.ErrLineage) {
					t.Fatal(err)
				}
				return
			case "applied_drift":
				exec("ALTER TABLE " + schema + ".data_model_runner_probe ADD COLUMN unexpected INT")
				if err := r.Apply(ctx); !errors.Is(err, migrations.ErrDrift) {
					t.Fatal(err)
				}
				return
			}
			if err := r.Apply(ctx); err != nil {
				t.Fatal(err)
			}
			if definition() != after {
				t.Fatal("wrong final shape")
			}
			h, err := r.Status(ctx)
			if err != nil || len(h) != 2 || h[1].State != "applied" {
				t.Fatalf("history=%v err=%v", h, err)
			}
			var pending int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+schema+".data_model_schema_migrations WHERE applied_at IS NULL").Scan(&pending); err != nil || pending != 0 {
				t.Fatalf("unfinalized=%d err=%v", pending, err)
			}
		})
	}
}

func TestMigrationExecutableDefinitions(t *testing.T) {
	for _, kind := range []string{"VIEW", "PROCEDURE", "FUNCTION", "TRIGGER"} {
		t.Run(kind, func(t *testing.T) {
			db, database := metadataDB(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			schema, _ := mysqlstore.Identifier(database)
			name := "data_model_definition_probe"
			statement := "CREATE VIEW " + schema + "." + name + " AS SELECT id FROM " + schema + ".data_model_tenants"
			switch kind {
			case "PROCEDURE":
				statement = "CREATE PROCEDURE " + schema + "." + name + "() SQL SECURITY DEFINER SELECT 1"
			case "FUNCTION":
				statement = "CREATE FUNCTION " + schema + "." + name + "() RETURNS INT DETERMINISTIC SQL SECURITY INVOKER RETURN 1"
			case "TRIGGER":
				statement = "CREATE TRIGGER " + schema + "." + name + " BEFORE INSERT ON " + schema + ".data_model_tenants FOR EACH ROW SET NEW.name=NEW.name"
			}
			if _, err := db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			after, err := migrations.Definition(ctx, conn, database, kind, name)
			conn.Close()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, "DROP "+kind+" "+schema+"."+name); err != nil {
				t.Fatal(err)
			}
			r, err := migrations.New(db, migrations.Plan{Database: database, Steps: []migrations.Step{{ID: "create_definition", Kind: kind, Name: name, SQL: statement, After: after}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Apply(ctx); err != nil {
				t.Fatal(err)
			}
			if err := r.Apply(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
