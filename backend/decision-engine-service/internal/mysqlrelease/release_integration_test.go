package mysqlrelease

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
)

// Fixture-only application of the existing shared runner. Production tooling
// intentionally does not expose Apply: a pooled preflight is not a same-session
// write guard. Only generated private databases below reach this helper.
func applyPrivateFixture(ctx context.Context, r *Release, db *sql.DB, server string) error {
	if !strings.HasPrefix(r.Identity().Database, "decision_release_fixture_") {
		return errors.New("private generated release fixture required")
	}
	if err := r.CheckTarget(ctx, db, server); err != nil {
		return err
	}
	runner, err := migrations.New(db, "decision_engine", r.Plan())
	if err != nil {
		return err
	}
	if err := runner.Apply(ctx); err != nil {
		return err
	}
	return r.Verify(ctx, db, server)
}

func TestReleaseRehearsalFreshUpgradeInterruptedDDLDriftAndForeignOwnership(t *testing.T) {
	reference, refName, definer, observed := testsupport.DecisionMySQLRuntimeDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, kind := range []string{"fresh", "upgrade"} {
		t.Run(kind, func(t *testing.T) {
			// Rebinding is ONLY for generated fixtures. Production consumes an
			// exact reviewed artifact for its schema/definer without any rewriting.
			name := "decision_release_fixture_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			schema, _ := mysqlstore.Identifier(name)
			if _, err := reference.ExecContext(ctx, "CREATE DATABASE "+schema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := reference.Exec("DROP DATABASE " + schema); err != nil {
					t.Error(err)
				}
			})
			db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: "mysql:3306", Database: name, User: "root", Password: "isolated-test-root-only", AllowInsecureTransport: true, MaxOpen: 4, MaxIdle: 2, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Close() })
			steps, err := metadata.DispatchStatements(name, definer)
			if err != nil {
				t.Fatal(err)
			}
			for i := range steps {
				steps[i].Before = strings.ReplaceAll(observed[i].Before, refName, name)
				steps[i].After = strings.ReplaceAll(observed[i].After, refName, name)
			}
			a := unitArtifact(t)
			a.Database, a.Definer, a.Steps = name, definer, steps
			raw, sum := encodeArtifact(t, a)
			r, err := Decode(bytes.NewReader(raw), sum, a.Identity)
			if err != nil {
				t.Fatal(err)
			}
			var server string
			if err := db.QueryRowContext(ctx, "SELECT @@server_uuid").Scan(&server); err != nil {
				t.Fatal(err)
			}
			for _, sql := range []string{"CREATE TABLE " + schema + ".fixture_other_owner(id INT PRIMARY KEY,value VARCHAR(10))", "INSERT INTO " + schema + ".fixture_other_owner VALUES(1,'preserve')", "CREATE TABLE " + schema + ".fixture_other_schema_migrations(version INT)", "INSERT INTO " + schema + ".fixture_other_schema_migrations VALUES(42)"} {
				if _, err := db.ExecContext(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			// Wrong target identity is rejected before even creating our ledger.
			if err := applyPrivateFixture(ctx, r, db, uuid.NewString()); err == nil {
				t.Fatal("wrong server accepted")
			}
			var count int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name='decision_engine_schema_migrations'", name).Scan(&count); err != nil || count != 0 {
				t.Fatal("preflight wrote ledger", count, err)
			}
			if err := r.Verify(ctx, db, server); err == nil {
				t.Fatal("missing schema ready")
			}
			if kind == "upgrade" {
				for _, length := range []int{140, 145} {
					runner, err := migrations.New(db, "decision_engine", migrations.Plan{Database: name, Steps: steps[:length]})
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
				// Simulate a durable migration intent whose DDL has not run yet.
				step := steps[145]
				body, _ := json.Marshal(struct {
					Database string
					Step     migrations.Step
				}{name, step})
				hash := sha256.Sum256(body)
				if _, err := db.ExecContext(ctx, "INSERT INTO "+schema+".decision_engine_schema_migrations(step_no,step_id,checksum,state,started_at) VALUES(146,?,?,'dirty',UTC_TIMESTAMP(6))", step.ID, hash[:]); err != nil {
					t.Fatal(err)
				}
			}
			if err := applyPrivateFixture(ctx, r, db, server); err != nil {
				t.Fatal(err)
			}
			// Lost applied-ack: DDL exists but ledger still records dirty intent.
			if _, err := db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_schema_migrations SET state='dirty',applied_at=NULL WHERE step_no=149"); err != nil {
				t.Fatal(err)
			}
			if err := r.Verify(ctx, db, server); err == nil {
				t.Fatal("dirty migration reported ready")
			}
			if err := applyPrivateFixture(ctx, r, db, server); err != nil {
				t.Fatal("post-DDL recovery", err)
			}
			if err := applyPrivateFixture(ctx, r, db, server); err != nil {
				t.Fatal("repeat", err)
			}
			if err := r.Verify(ctx, db, server); err != nil {
				t.Fatal(err)
			}
			account := "`" + definer + "`@`localhost`"
			if _, err := db.ExecContext(ctx, "ALTER USER "+account+" ACCOUNT UNLOCK"); err != nil {
				t.Fatal(err)
			}
			checkErr := r.CheckTarget(ctx, db, server)
			if _, err := db.ExecContext(ctx, "ALTER USER "+account+" ACCOUNT LOCK"); err != nil {
				t.Fatal(err)
			}
			if checkErr == nil {
				t.Fatal("unlocked definer accepted")
			}
			if _, err := db.ExecContext(ctx, "ALTER TABLE "+schema+".decision_engine_record_tags ADD COLUMN unexpected INT"); err != nil {
				t.Fatal(err)
			}
			if err := r.Verify(ctx, db, server); !errors.Is(err, migrations.ErrDrift) {
				t.Fatal("drift ready", err)
			}
			if err := applyPrivateFixture(ctx, r, db, server); !errors.Is(err, migrations.ErrDrift) {
				t.Fatal("silently repaired drift", err)
			}
			if _, err := db.ExecContext(ctx, "UPDATE "+schema+".decision_engine_schema_migrations SET checksum=UNHEX(REPEAT('00',32)) WHERE step_no=1"); err != nil {
				t.Fatal(err)
			}
			if err := r.Verify(ctx, db, server); !errors.Is(err, migrations.ErrLineage) {
				t.Fatal("tampered lineage accepted", err)
			}
			var value string
			var version int
			if err := db.QueryRowContext(ctx, "SELECT value FROM "+schema+".fixture_other_owner WHERE id=1").Scan(&value); err != nil || value != "preserve" {
				t.Fatal("other owner changed", value, err)
			}
			if err := db.QueryRowContext(ctx, "SELECT version FROM "+schema+".fixture_other_schema_migrations").Scan(&version); err != nil || version != 42 {
				t.Fatal("other owner lineage changed", version, err)
			}
		})
	}
}
