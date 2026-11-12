package localdev

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

func referenceFixture(t *testing.T) *sql.DB {
	t.Helper()
	address := os.Getenv("MYSQLSTORE_TEST_ADDRESS")
	if address == "" {
		if os.Getenv("MYSQLSTORE_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("private MySQL fixture required")
		}
		t.Skip("private MySQL fixture not configured")
	}
	if address != "mysql:3306" {
		t.Fatal("private Compose fixture required")
	}
	db, err := mysqlstore.Open(context.Background(), mysqlstore.Config{Address: address, Database: "migration_test", User: "migration_test", Password: os.Getenv("MYSQLSTORE_TEST_PASSWORD"), AllowInsecureTransport: true, MaxOpen: 4, MaxIdle: 2, ConnectTimeout: time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func referencePlan(t *testing.T, db *sql.DB) migrations.Plan {
	t.Helper()
	suffix := fmt.Sprint(time.Now().UnixNano())
	p := migrations.Plan{Database: "migration_test", Owner: "reference_" + suffix}
	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("reference_%s_%d", suffix, i)
		ref := name + "_ref"
		q := "CREATE TABLE migration_test." + ref + "(id INT PRIMARY KEY) ENGINE=InnoDB"
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		after, err := migrations.Definition(context.Background(), conn, p.Database, "TABLE", ref)
		conn.Close()
		if err != nil {
			t.Fatal(err)
		}
		p.Steps = append(p.Steps, migrations.Step{ID: fmt.Sprintf("step_%d", i), Kind: "TABLE", Name: name, SQL: strings.ReplaceAll(q, ref, name), After: strings.ReplaceAll(after, ref, name)})
	}
	return p
}
func TestMySQLReferenceReplayResumesOnlyJournaledPartialSchemas(t *testing.T) {
	db := referenceFixture(t)
	ctx := context.Background()
	p := referencePlan(t, db)
	r, err := migrations.NewForOwner(db, p.Owner, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ApplyThrough(ctx, p.Steps[0].ID); err != nil {
		t.Fatal(err)
	}
	if ready, err := referenceReady(ctx, db, p, ""); err != nil || ready {
		t.Fatal("journaled partial replay rejected", ready, err)
	}
	if err := r.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if ready, err := referenceReady(ctx, db, p, ""); err != nil || !ready {
		t.Fatal(ready, err)
	}
	raw := referencePlan(t, db)
	if _, err := db.Exec(raw.Steps[0].SQL); err != nil {
		t.Fatal(err)
	}
	if _, err := referenceReady(ctx, db, raw, ""); err == nil {
		t.Fatal("unjournaled partial reference adopted")
	}
	if ready, err := referenceReady(ctx, db, raw, raw.Steps[0].ID); err != nil || !ready {
		t.Fatal("exact captured prerequisite rejected", ready, err)
	}
}
