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
	"github.com/google/uuid"
)

func TestInspectionCommandMySQL(t *testing.T) {
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
	database := "data_model_inspect_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	user := "inspect_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	account := "`" + user + "`@`%`"
	if _, err := db.ExecContext(ctx, "CREATE USER "+account+" IDENTIFIED BY 'isolated-inspection-only'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := db.ExecContext(cleanup, "DROP USER "+account); err != nil {
			t.Error(err)
		}
	})
	policy := installation.AccountPolicy{User: user, Host: "%"}
	for _, name := range []string{"data_model_index_deliveries", "data_model_index_jobs", "data_model_index_delivery_reservations"} {
		policy.Objects = append(policy.Objects, installation.ObjectGrant{Kind: "TABLE", Name: name, Privileges: []string{"SELECT"}})
	}
	grants, err := installation.GrantStatements(database, policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range grants {
		if _, err := db.ExecContext(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DATA_MODEL_MYSQL_INSPECTION_PASSWORD", "isolated-inspection-only")
	args := []string{"-address", address, "-database", database, "-user", user, "-tenant", tenantID.String(), "-isolated-fixture", "-limit", "1"}
	var out bytes.Buffer
	if err := run(ctx, args, &out); err != nil {
		t.Fatal(err)
	}
	var page store.DeliveryInspectionPage
	if err := json.Unmarshal(out.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Next != nil || page.Items[0].ID != delivery || page.Items[0].State != "available" || !page.Items[0].Reserved {
		t.Fatalf("list output: %s", out.String())
	}
	lease, err := queue.Claim(ctx)
	if err != nil || lease == nil {
		t.Fatalf("claim %+v %v", lease, err)
	}
	if err := queue.Finish(ctx, *lease, true, 0, "fixture_failure", nil); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run(ctx, append(args, "-delivery", delivery.String()), &out); err != nil {
		t.Fatal(err)
	}
	var detail store.DeliveryInspection
	if err := json.Unmarshal(out.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.State != "discarded" || detail.JobStatus != "pending" || detail.Reserved || detail.LastErrorCode != "delivery_attempts_exhausted" {
		t.Fatalf("hidden divergence: %s", out.String())
	}
	out.Reset()
	if err := run(ctx, append(args, "-before-id", delivery.String(), "-before-created-at", detail.CreatedAt.Format(time.RFC3339Nano)), &out); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &page); err != nil || len(page.Items) != 0 {
		t.Fatalf("cursor output: %s %v", out.String(), err)
	}
	out.Reset()
	if err := run(ctx, append(args, "-tenant", uuid.NewString(), "-delivery", delivery.String()), &out); !errors.Is(err, sql.ErrNoRows) || out.Len() != 0 {
		t.Fatalf("foreign output: %s %v", out.String(), err)
	}
}
