package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestRecoveryCommandMySQL(t *testing.T) {
	address, password := os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS"), os.Getenv("DATA_MODEL_MYSQL_TEST_ROOT_PASSWORD")
	if address == "" || password == "" {
		if os.Getenv("DATA_MODEL_REQUIRE_MYSQL_TEST") == "1" {
			t.Fatal("required disposable fixture missing")
		}
		t.Skip("disposable MySQL fixture not configured")
	}
	if address != "mysql:3306" || password != "isolated-test-root-only" || os.Getenv("DATA_MODEL_REQUIRE_MYSQL_TEST") != "1" {
		t.Fatal("only disposable fixture allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	open := func(database string) *sql.DB {
		t.Helper()
		db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: address, Database: database, User: "root", Password: password, AllowInsecureTransport: true, MaxOpen: 2, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	admin := open("data_model_publication_test")
	t.Cleanup(func() {
		if err := admin.Close(); err != nil {
			t.Error(err)
		}
	})
	database := "data_model_recovery_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+schema+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.ExecContext(cleanup, "DROP DATABASE "+schema); err != nil {
			t.Error(err)
		}
	})
	db := open(database)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	definitions, err := metadata.Definitions(database)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range definitions {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	tenantID, tableID, operation := uuid.New(), uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, tenantID, 1000, 1<<20)
		if err != nil {
			return err
		}
		if err := r.Tenants.Create(ctx, tenant.New(tenantID, at, "command", nil)); err != nil {
			return err
		}
		if err := r.Tables.Create(ctx, datamodel.Table{ID: tableID, TenantID: tenantID, Name: "records", CreatedAt: at, UpdatedAt: at}); err != nil {
			return err
		}
		registry, err := store.NewPhysicalRegistry(tx, database, tenantID, 1000)
		if err != nil {
			return err
		}
		return registry.RegisterTable(ctx, tableID)
	}); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.QueryRowContext(ctx, "SELECT physical_name FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&name); err != nil {
		t.Fatal(err)
	}
	quoted, _ := mysqlstore.Identifier(name)
	step := migrations.Step{ID: "create_records", Kind: "TABLE", Name: name, SQL: "CREATE TABLE " + schema + "." + quoted + " (id BINARY(16) NOT NULL PRIMARY KEY) ENGINE=InnoDB"}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, step.SQL); err != nil {
		t.Fatal(err)
	}
	step.After, err = migrations.Definition(ctx, conn, database, "TABLE", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "DROP TABLE "+schema+"."+quoted); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	shape, err := json.Marshal(store.PhysicalDDLPlan{Version: 1, Steps: []migrations.Step{step}})
	if err != nil {
		t.Fatal(err)
	}
	intents, err := store.NewDDLIntentStore(db, database, tenantID, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intents.Prepare(ctx, store.DDLIntent{TableID: tableID, OperationID: operation, RequestHash: sha256.Sum256([]byte(step.SQL)), ActorKind: "migrate", ActorID: "command-fixture", Shape: shape}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_MODEL_MYSQL_RECOVERY_PASSWORD", password)
	args := []string{"-address", address, "-database", database, "-user", "root", "-tenant", tenantID.String(), "-state", "prepared", "-isolated-fixture", "-timeout", "20s", "-operation-timeout", "5s"}
	var out bytes.Buffer
	if err := run(ctx, args, &out); err != nil {
		t.Fatal(err)
	}
	var page store.PhysicalRecoveryPage
	if err := json.Unmarshal(out.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if !page.Exhausted || page.After != operation || len(page.Results) != 1 || page.Results[0].Outcome != "awaiting_publication" {
		t.Fatalf("command output: %s", out.String())
	}
	var gate string
	if err := db.QueryRowContext(ctx, "SELECT write_state FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&gate); err != nil || gate != "closed" {
		t.Fatalf("command opened gate: %s %v", gate, err)
	}
	if _, err := db.ExecContext(ctx, "ALTER TABLE "+schema+"."+quoted+" ADD COLUMN drift INT"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err = run(ctx, append(args, "-state", "verifying"), &out)
	if !errors.Is(err, migrations.ErrDrift) {
		t.Fatalf("command returned success for drift: %s %v", out.String(), err)
	}
	if err := json.Unmarshal(out.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 1 || page.Results[0].Outcome != "schema_drift" {
		t.Fatalf("missing partial error output: %s", out.String())
	}
	out.Reset()
	if err := run(ctx, append(args, "-tenant", uuid.NewString(), "-state", "blocked"), &out); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &page); err != nil || len(page.Results) != 0 {
		t.Fatalf("foreign tenant output: %s %v", out.String(), err)
	}
}
