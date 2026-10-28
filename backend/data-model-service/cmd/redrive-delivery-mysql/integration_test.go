package main

import (
	"bytes"
	"context"
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
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func TestRedriveCommandMySQL(t *testing.T) {
	address, password := os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS"), os.Getenv("DATA_MODEL_MYSQL_TEST_ROOT_PASSWORD")
	if address == "" || password == "" {
		if os.Getenv("DATA_MODEL_REQUIRE_MYSQL_TEST") == "1" {
			t.Fatal("required fixture missing")
		}
		t.Skip("disposable fixture not configured")
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
	database := "data_model_redrive_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	tenantID, jobID := uuid.New(), uuid.New()
	at := time.Now().UTC().Truncate(time.Microsecond)
	queue, err := store.NewIndexDeliveryStore(db, database, "inspection", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var delivery uuid.UUID
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, tenantID, 1000, 1<<20)
		if err != nil {
			return err
		}
		if err := r.Tenants.Create(ctx, tenant.New(tenantID, at, "command", nil)); err != nil {
			return err
		}
		if err := r.Jobs.Create(ctx, datamodel.IndexJob{ID: jobID, TenantID: tenantID, TableName: "records", IndexType: "search", Columns: []string{}, Status: "pending", RequestedAt: at}); err != nil {
			return err
		}
		delivery, err = queue.EnqueueTx(ctx, tx, tenantID, jobID, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	user := "redrive_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	account := "`" + user + "`@`%`"
	if _, err := db.ExecContext(ctx, "CREATE USER "+account+" IDENTIFIED BY 'isolated-redrive-only'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := db.ExecContext(cleanup, "DROP USER "+account); err != nil {
			t.Error(err)
		}
	})
	policy := installation.AccountPolicy{User: user, Host: "%", Objects: []installation.ObjectGrant{
		{Kind: "TABLE", Name: "data_model_index_jobs", Privileges: []string{"SELECT"}, Columns: map[string][]string{"UPDATE": {"id"}}},
		{Kind: "TABLE", Name: "data_model_index_deliveries", Privileges: []string{"SELECT", "INSERT"}, Columns: map[string][]string{"UPDATE": {"id"}}},
		{Kind: "TABLE", Name: "data_model_index_delivery_reservations", Privileges: []string{"SELECT", "INSERT"}, Columns: map[string][]string{"UPDATE": {"delivery_id"}}},
		{Kind: "TABLE", Name: "data_model_schema_change_log", Privileges: []string{"SELECT", "INSERT"}, Columns: map[string][]string{"UPDATE": {"id"}}},
	}}
	grants, err := installation.GrantStatements(database, policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if _, err := db.ExecContext(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	// Verify the complete restricted policy, including the immutable-column
	// UPDATE grants needed by locking reads; no out-of-policy manual grants.
	inspection, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = installation.VerifyAccountPolicy(ctx, inspection, database, policy)
	closeErr := inspection.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("redrive policy %v %v", err, closeErr)
	}
	lease, err := queue.Claim(ctx)
	if err != nil || lease == nil {
		t.Fatalf("claim %v %v", lease, err)
	}
	if err := queue.Finish(ctx, *lease, true, 0, "fixture_failure", nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_MODEL_MYSQL_REDRIVE_PASSWORD", "isolated-redrive-only")
	request := uuid.NewString()
	args := []string{"-address", address, "-database", database, "-user", user, "-tenant", tenantID.String(), "-job", jobID.String(), "-source", delivery.String(), "-request", request, "-actor", "operator", "-reason", "dependency repaired", "-scheduled-at", at.Add(time.Hour).Format(time.RFC3339Nano), "-queue", "inspection", "-max-attempts", "2", "-isolated-fixture"}
	var out bytes.Buffer
	if err := run(ctx, args, &out); err == nil || out.Len() != 0 {
		t.Fatalf("missing apply accepted: %s %v", out.String(), err)
	}
	args = append(args, "-apply")
	if err := run(ctx, args, &out); err != nil {
		t.Fatal(err)
	}
	var result struct {
		DeliveryID uuid.UUID `json:"delivery_id"`
		RequestID  string    `json:"request_id"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.DeliveryID == uuid.Nil || result.DeliveryID == delivery || result.RequestID != request {
		t.Fatalf("output %s %v", out.String(), err)
	}
	want := out.String()
	out.Reset()
	if err := run(ctx, args, &out); err != nil || out.String() != want {
		t.Fatalf("replay %s %v", out.String(), err)
	}
	out.Reset()
	if err := run(ctx, append(args, "-tenant", uuid.NewString()), &out); !errors.Is(err, sql.ErrNoRows) || out.Len() != 0 {
		t.Fatalf("foreign %s %v", out.String(), err)
	}
	if err := run(ctx, append(args, "-reason", "different"), &out); !errors.Is(err, store.ErrDeliveryRedriveConflict) || out.Len() != 0 {
		t.Fatalf("conflict %s %v", out.String(), err)
	}
	// The command succeeds without permission to mutate domain jobs or rewrite audit.
	operator, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: address, Database: database, User: user, Password: "isolated-redrive-only", AllowInsecureTransport: true, MaxOpen: 1, MaxIdle: 0, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer operator.Close()
	for _, statement := range []string{"UPDATE data_model_index_jobs SET status='pending' WHERE id=?", "DELETE FROM data_model_schema_change_log WHERE id=?"} {
		_, err := operator.ExecContext(ctx, statement, mysqlstore.UUID(jobID))
		var denied *driver.MySQLError
		if !errors.As(err, &denied) || (denied.Number != 1142 && denied.Number != 1143) {
			t.Fatalf("expected denied mutation: %v", err)
		}
	}
	_, err = operator.ExecContext(ctx, "UPDATE data_model_index_jobs SET id=? WHERE id=?", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(jobID))
	var immutable *driver.MySQLError
	if !errors.As(err, &immutable) || immutable.Number != 1644 {
		t.Fatalf("identity update was not rejected by immutable guard: %v", err)
	}
}
