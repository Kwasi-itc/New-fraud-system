package metadata_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestPhysicalReconciliationPages(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tenantID := uuid.New()
	ids := []uuid.UUID{uuid.MustParse("00000000-0000-0000-0000-000000000001"), uuid.MustParse("00000000-0000-0000-0000-000000000002"), uuid.MustParse("00000000-0000-0000-0000-000000000003")}
	tables := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, tenantID, 1000, 1<<20)
		if err != nil {
			return err
		}
		if err := r.Tenants.Create(ctx, tenant.New(tenantID, at, "recovery", nil)); err != nil {
			return err
		}
		registry, err := store.NewPhysicalRegistry(tx, database, tenantID, 1000)
		if err != nil {
			return err
		}
		for i, table := range tables {
			if err := r.Tables.Create(ctx, datamodel.Table{ID: table, TenantID: tenantID, Name: fmt.Sprintf("records_%d", i), CreatedAt: at, UpdatedAt: at}); err != nil {
				return err
			}
			if err := registry.RegisterTable(ctx, table); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	intents, err := store.NewDDLIntentStore(db, database, tenantID, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := mysqlstore.Identifier(database)
	for i, table := range tables {
		var name string
		if err := db.QueryRowContext(ctx, "SELECT physical_name FROM data_model_physical_tables WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(table)).Scan(&name); err != nil {
			t.Fatal(err)
		}
		quoted, _ := mysqlstore.Identifier(name)
		step := migrations.Step{ID: "create_records", Kind: "TABLE", Name: name, SQL: "CREATE TABLE " + schema + "." + quoted + " (id BINARY(16) NOT NULL PRIMARY KEY) ENGINE=InnoDB"}
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(ctx, step.SQL); err != nil {
			t.Fatal(err)
		}
		step.After, err = migrations.Definition(ctx, conn, database, "TABLE", name)
		if err != nil {
			t.Fatal(err)
		}
		// Leave the second object unjournaled to prove one drifting table does
		// not hide or prevent recovery of unrelated operations on later pages.
		if i != 1 {
			if _, err := conn.ExecContext(ctx, "DROP TABLE "+schema+"."+quoted); err != nil {
				t.Fatal(err)
			}
		}
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		shape, err := json.Marshal(store.PhysicalDDLPlan{Version: 1, Steps: []migrations.Step{step}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := intents.Prepare(ctx, store.DDLIntent{TableID: table, OperationID: ids[i], RequestHash: sha256.Sum256([]byte(step.SQL)), ActorKind: "migrate", ActorID: "recovery-fixture", Shape: shape}); err != nil {
			t.Fatal(err)
		}
	}
	executor, err := store.NewPhysicalExecutor(db, database, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	lockOwner, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(database + ":" + tenantID.String() + ":" + tables[0].String()))
	lock := fmt.Sprintf("data_model_ddl_%x", sum[:24])
	var held int
	if err := lockOwner.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lock).Scan(&held); err != nil || held != 1 {
		t.Fatalf("lock %d %v", held, err)
	}
	t.Cleanup(func() { lockOwner.Close() })
	page, err := executor.ReconcilePage(ctx, "prepared", uuid.Nil, 2, 5*time.Second)
	if !errors.Is(err, mysqlstore.ErrDDLLockBusy) || !errors.Is(err, migrations.ErrDrift) || page.Exhausted || page.After != ids[1] || len(page.Results) != 2 || page.Results[0].Outcome != "lock_busy" || page.Results[1].Outcome != "schema_drift" {
		t.Fatalf("first page: %+v %v", page, err)
	}
	if err := lockOwner.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lock).Scan(&held); err != nil || held != 1 {
		t.Fatalf("unlock %d %v", held, err)
	}
	if err := lockOwner.Close(); err != nil {
		t.Fatal(err)
	}
	// Recovery must release its selection rows before borrowing a DDL session.
	db.SetMaxOpenConns(1)
	page, err = executor.ReconcilePage(ctx, "prepared", page.After, 2, 5*time.Second)
	if err != nil || !page.Exhausted || len(page.Results) != 1 || page.After != ids[2] || page.Results[0].Outcome != "awaiting_publication" {
		t.Fatalf("continuation: %+v %v", page, err)
	}
	page, err = executor.ReconcilePage(ctx, "prepared", uuid.Nil, 2, 5*time.Second)
	if err != nil || !page.Exhausted || len(page.Results) != 1 || page.After != ids[0] {
		t.Fatalf("new sweep did not retry busy item: %+v %v", page, err)
	}
	page, err = executor.ReconcilePage(ctx, "blocked", uuid.Nil, 2, 5*time.Second)
	if !errors.Is(err, migrations.ErrDrift) || len(page.Results) != 1 || page.After != ids[1] {
		t.Fatalf("blocked drift adopted: %+v %v", page, err)
	}
	var closed int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_physical_tables WHERE tenant_id=? AND write_state='closed' AND published_version IS NULL", mysqlstore.UUID(tenantID)).Scan(&closed); err != nil || closed != 3 {
		t.Fatalf("recovery published: %d %v", closed, err)
	}
}
