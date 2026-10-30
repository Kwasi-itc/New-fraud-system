package testsupport

import (
	"context"
	"database/sql"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
	"github.com/google/uuid"
	"os"
	"strings"
	"testing"
	"time"
)

// DecisionMySQLDatabase installs candidate DDL only in a generated database
// on the pinned private Compose fixture. No application DSN is accepted. This
// is component test setup, NOT a frozen release/grants/upgrade rehearsal.
func DecisionMySQLDatabase(t *testing.T) (*sql.DB, string) {
	db, name, _, _ := decisionMySQLReference(t, false)
	return db, name
}

func decisionMySQLReference(t *testing.T, observe bool) (*sql.DB, string, string, []migrations.Step) {
	t.Helper()
	if os.Getenv("DECISION_MYSQL_TEST_ADDRESS") == "" {
		if os.Getenv("DECISION_MYSQL_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("private MySQL fixture required")
		}
		t.Skip("private MySQL fixture not configured")
	}
	if os.Getenv("DECISION_MYSQL_TEST_ADDRESS") != "mysql:3306" {
		t.Fatal("only private MySQL Compose fixture is accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config := mysqlstore.Config{Address: "mysql:3306", Database: "decision_platform_test", User: "root", Password: "isolated-test-root-only", AllowInsecureTransport: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute}
	admin, err := mysqlstore.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	var version string
	if err := admin.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil || version != "8.4.8" {
		t.Fatal("pinned MySQL required", version, err)
	}
	name := "decision_component_fixture_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	schema, err := mysqlstore.Identifier(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+schema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP DATABASE "+schema); err != nil {
			t.Error(err)
		}
	})
	config.Database = name
	db, err := mysqlstore.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	definer := "de_component_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	account := "`" + definer + "`@`localhost`"
	if _, err := admin.ExecContext(ctx, "CREATE USER "+account+" ACCOUNT LOCK"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), "DROP USER "+account); err != nil {
			t.Error(err)
		}
	})
	steps, err := metadata.DispatchStatements(name, definer)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for i, step := range steps {
		if observe && strings.HasPrefix(step.SQL, "ALTER ") {
			steps[i].Before, err = migrations.Definition(ctx, conn, name, step.Kind, step.Name)
			if err != nil {
				t.Fatal(step.ID, err)
			}
		}
		if _, err := db.ExecContext(ctx, step.SQL); err != nil {
			t.Fatal(step.ID, err)
		}
		if observe {
			steps[i].After, err = migrations.Definition(ctx, conn, name, step.Kind, step.Name)
			if err != nil {
				t.Fatal(step.ID, err)
			}
		}
	}
	grants, err := metadata.DispatchDefinerGrants(name, definer)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range grants {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	return db, name, definer, steps
}

// DecisionMySQLRuntimeDatabase makes reference observations ONLY in a generated
// private fixture, then installs/records that plan in ANOTHER generated DB.
// These observations are not frozen production release definitions.
func DecisionMySQLRuntimeDatabase(t *testing.T) (*sql.DB, string, string, []migrations.Step) {
	t.Helper()
	ref, refName, definer, observed := decisionMySQLReference(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	name := "decision_runtime_fixture_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	schema, _ := mysqlstore.Identifier(name)
	if _, err := ref.ExecContext(ctx, "CREATE DATABASE "+schema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := ref.ExecContext(context.Background(), "DROP DATABASE "+schema); err != nil {
			t.Error(err)
		}
	})
	db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: "mysql:3306", Database: name, User: "root", Password: "isolated-test-root-only", AllowInsecureTransport: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
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
	plan, err := metadata.DispatchPlan(name, definer, steps)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := migrations.New(db, "decision_engine", plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	grants, err := metadata.DispatchDefinerGrants(name, definer)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if _, err := ref.ExecContext(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	if err := runner.Verify(ctx, nil); err != nil {
		t.Fatal(err)
	}
	return db, name, definer, steps
}
