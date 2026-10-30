package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
)

type sharedFixture struct {
	DB                   *sql.DB
	Database             string
	DataModel, Ingestion migrations.Plan
}
type fixtureManifest struct {
	Steps                []migrations.Step
	Create, Grants, Drop []string
}

// fullSharedFixture uses real owner-authored source, including v005/v006, without
// importing another service's internal packages or inventing metadata tables.
// It creates and drops ONLY its own fresh random database/accounts on Compose.
func fullSharedFixture(t *testing.T) sharedFixture {
	t.Helper()
	if os.Getenv("INGESTION_MYSQL_TEST_ADDRESS") != "mysql:3306" || os.Getenv("INGESTION_REQUIRE_MYSQL_TEST") != "1" {
		t.Fatal("explicit isolated full fixture required")
	}
	binary := os.Getenv("INGESTION_DATA_MODEL_FIXTURE_EXPORTER")
	if binary == "" {
		t.Fatal("owner fixture exporter required; build cmd/export-mysql-fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := "ingestion_full_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	output, err := exec.CommandContext(ctx, binary, name, suffix).Output()
	if err != nil {
		t.Fatal("owner source export", err)
	}
	var manifest fixtureManifest
	if len(output) > 32<<20 {
		t.Fatal("oversized fixture manifest")
	}
	if err = json.Unmarshal(output, &manifest); err != nil {
		t.Fatal(err)
	}
	admin, err := mysqlstore.Open(ctx, receiptConfig("mysql", "root", "isolated-test-root-only"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Close(); err != nil {
			t.Error(err)
		}
	})
	var server string
	if err = admin.QueryRowContext(ctx, "SELECT VERSION()").Scan(&server); err != nil || server != "8.4.8" {
		t.Fatal("pinned MySQL 8.4.8 required", server, err)
	}
	schema, _ := mysqlstore.Identifier(name)
	if _, err = admin.ExecContext(ctx, "CREATE DATABASE "+schema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, e := admin.Exec("DROP DATABASE " + schema); e != nil {
			t.Error(e)
		}
	})
	for i, q := range manifest.Create {
		if _, err = admin.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
		drop := manifest.Drop[i]
		t.Cleanup(func() {
			if _, e := admin.Exec(drop); e != nil {
				t.Error(e)
			}
		})
	}
	db, err := mysqlstore.Open(ctx, receiptConfig(name, "root", "isolated-test-root-only"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	ingestionSteps, err := InstallationStatements(name)
	if err != nil {
		t.Fatal(err)
	}
	capture := func(steps []migrations.Step) {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		for i := range steps {
			s := &steps[i]
			s.Before, err = migrations.Definition(ctx, conn, name, s.Kind, s.Name)
			if err != nil {
				t.Fatal(s.ID, err)
			}
			if _, err = conn.ExecContext(ctx, s.SQL); err != nil {
				t.Fatal(s.ID, err)
			}
			s.After, err = migrations.Definition(ctx, conn, name, s.Kind, s.Name)
			if err != nil {
				t.Fatal(s.ID, err)
			}
		}
	}
	capture(manifest.Steps)
	capture(ingestionSteps)
	dm := migrations.Plan{Database: name, Owner: "data_model", Steps: manifest.Steps}
	ing, err := InstallationPlan(name, ingestionSteps)
	if err != nil {
		t.Fatal(err)
	}
	if err = dm.Validate(); err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("INGESTION_FULL_ARTIFACT_DIR"); directory != "" {
		destination := filepath.Join(directory, name)
		if err := os.Mkdir(destination, 0700); err != nil {
			t.Fatal(err)
		}
		for file, value := range map[string]any{"data_model_plan.json": dm, "ingestion_plan.json": ing, "owner_source.json": manifest} {
			data, err := json.MarshalIndent(value, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(destination, file), append(data, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Reference and target are separate incarnations of this owned disposable DB.
	if _, err = admin.ExecContext(ctx, "DROP DATABASE "+schema); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.ExecContext(ctx, "CREATE DATABASE "+schema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
		t.Fatal(err)
	}
	for _, plan := range []migrations.Plan{dm, ing} {
		runner, err := migrations.New(db, plan)
		if err != nil {
			t.Fatal(err)
		}
		if err = runner.Apply(ctx); err != nil {
			t.Fatal(plan.Owner, err)
		}
		if err = runner.Verify(ctx, nil); err != nil {
			t.Fatal(plan.Owner, err)
		}
	}
	for _, q := range manifest.Grants {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("complete shared fixture: MySQL=%s data_model_steps=%d ingestion_steps=%d", server, len(dm.Steps), len(ing.Steps))
	return sharedFixture{db, name, dm, ing}
}

func TestFullSharedInstallation(t *testing.T) {
	f := fullSharedFixture(t)
	for _, plan := range []migrations.Plan{f.DataModel, f.Ingestion} {
		runner, err := migrations.New(f.DB, plan)
		if err != nil {
			t.Fatal(err)
		}
		if err = runner.Apply(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err = runner.Verify(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		history, err := runner.Status(context.Background())
		if err != nil || len(history) != len(plan.Steps) {
			t.Fatal(history, err)
		}
	}
	var missing []byte
	if err := f.DB.QueryRow("SELECT "+f.Database+".data_model_find_ingestion_contract_key_v1(?,?)", mysqlstore.UUID(uuid.New()), []byte("missing")).Scan(&missing); err != nil || missing != nil {
		t.Fatal(missing, err)
	}
}
