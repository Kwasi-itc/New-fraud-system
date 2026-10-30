package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

func installationFixture(t *testing.T) (*sql.DB, migrations.Plan, func()) {
	t.Helper()
	db, database, tenant, _, _ := writeFixture(t)
	ctx := context.Background()
	s, _ := mysqlstore.Identifier(database)
	// Existing fixture owns these generated tables; no application database is
	// accepted by writeFixture. Build a reference, then restore its empty target.
	for _, q := range []string{"DROP TABLE " + s + ".ingestion_request_receipts", "DROP TABLE " + s + ".ingestion_receipt_buckets", "CREATE TABLE " + s + ".data_model_tenants(id BINARY(16) PRIMARY KEY) ENGINE=InnoDB"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO "+s+".data_model_tenants VALUES(?)", mysqlstore.UUID(tenant)); err != nil {
		t.Fatal(err)
	}
	steps, err := InstallationStatements(database)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range steps {
		step := &steps[i]
		step.Before, err = migrations.Definition(ctx, conn, database, step.Kind, step.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(ctx, step.SQL); err != nil {
			t.Fatal(step.ID, err)
		}
		step.After, err = migrations.Definition(ctx, conn, database, step.Kind, step.Name)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	reset := func() {
		var pointer int
		if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=? AND CONSTRAINT_NAME='ingestion_current_job_fk'", database).Scan(&pointer); err != nil {
			t.Fatal(err)
		}
		if pointer > 0 {
			if _, err := db.Exec("ALTER TABLE " + s + ".ingestion_work_resources DROP FOREIGN KEY ingestion_current_job_fk"); err != nil {
				t.Fatal(err)
			}
		}

		dropped := map[string]bool{}
		// Reverse first-creation order (ALTER steps do not affect creation order).
		var created []string
		for _, step := range steps {
			if step.Before == "" {
				created = append(created, step.Name)
			}
		}
		for i := len(created) - 1; i >= 0; i-- {
			name := created[i]
			if !dropped[name] {
				if _, err := db.Exec("DROP TABLE IF EXISTS " + s + "." + name); err != nil {
					t.Fatal(name, err)
				}
				dropped[name] = true
			}
		}

		if _, err := db.Exec("DROP TABLE IF EXISTS " + s + ".ingestion_schema_migrations"); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	plan, err := InstallationPlan(database, steps)
	if err != nil {
		t.Fatal(err)
	}
	return db, plan, reset
}

func TestIngestionInstallationLineage(t *testing.T) {
	db, plan, _ := installationFixture(t)
	ctx := context.Background()
	s, _ := mysqlstore.Identifier(plan.Database)
	steps := plan.Steps
	runner, err := migrations.New(db, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Verify(ctx, nil); !errors.Is(err, migrations.ErrLineage) {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := runner.Apply(ctx); err != nil {
			t.Fatal(err)
		}
		if err := runner.Verify(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	history, err := runner.Status(ctx)
	if err != nil || len(history) != len(steps) {
		t.Fatal(history, err)
	}
	if _, err := db.Exec("ALTER TABLE " + s + ".ingestion_audit ADD COLUMN unexpected INT NULL"); err != nil {
		t.Fatal(err)
	}
	if err := runner.Verify(ctx, nil); !errors.Is(err, migrations.ErrDrift) {
		t.Fatal(err)
	}
	bad := append([]migrations.Step(nil), steps...)
	bad[0].SQL += " "
	if _, err := InstallationPlan(plan.Database, bad); err == nil {
		t.Fatal("changed installer SQL accepted")
	}
}

// Simulate durable journal/DDL boundaries, not elapsed-time fault injection.
// Every reset is limited to the generated fixture's explicit ingestion objects.
func TestIngestionInstallationRecoveryBoundaries(t *testing.T) {
	db, plan, reset := installationFixture(t)
	ctx := context.Background()
	schema, _ := mysqlstore.Identifier(plan.Database)
	for boundary, step := range plan.Steps {
		for _, state := range []string{"before_ddl", "after_ddl", "drift"} {
			t.Run(step.ID+"/"+state, func(t *testing.T) {
				reset()
				// Bootstrap the ledger with the first step then remove only that step's
				// generated object/journal row when testing boundary zero.
				prefix := plan
				n := boundary
				if n == 0 {
					n = 1
				}
				prefix.Steps = plan.Steps[:n]
				initial, err := migrations.New(db, prefix)
				if err != nil {
					t.Fatal(err)
				}
				if err = initial.Apply(ctx); err != nil {
					t.Fatal(err)
				}
				if boundary == 0 {
					if _, err = db.Exec("DROP TABLE " + schema + "." + step.Name); err != nil {
						t.Fatal(err)
					}
					if _, err = db.Exec("DELETE FROM " + schema + ".ingestion_schema_migrations"); err != nil {
						t.Fatal(err)
					}
				}
				sum := migrations.Checksum(plan.Database, step)
				if _, err = db.Exec("INSERT INTO "+schema+".ingestion_schema_migrations(step_no,step_id,checksum,state,started_at) VALUES(?,?,?,'dirty',UTC_TIMESTAMP(6))", boundary+1, step.ID, sum[:]); err != nil {
					t.Fatal(err)
				}
				if state != "before_ddl" {
					if _, err = db.Exec(step.SQL); err != nil {
						t.Fatal(err)
					}
				}
				if state == "drift" {
					if _, err = db.Exec("ALTER TABLE " + schema + "." + step.Name + " ADD COLUMN unexpected_recovery_column INT NULL"); err != nil {
						t.Fatal(err)
					}
				}
				runner, err := migrations.New(db, plan)
				if err != nil {
					t.Fatal(err)
				}
				err = runner.Apply(ctx)
				if state == "drift" {
					if !errors.Is(err, migrations.ErrDrift) {
						t.Fatal("dirty drift accepted", err)
					}
					history, e := runner.Status(ctx)
					if e != nil || len(history) != boundary+1 || history[boundary].State != "dirty" {
						t.Fatal(history, e)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if err = runner.Verify(ctx, nil); err != nil {
					t.Fatal(err)
				}
				if err = runner.Apply(ctx); err != nil {
					t.Fatal("repeated resume", err)
				}
			})
		}
	}
}
