package mysqlruntime

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	domainTenant "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/installation"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/metadata"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// This fixture is only the final metadata phase. Physical creation and portable
// round trips are exercised separately by mysql-portable-smoke.mjs.
func TestPortableMetadataCommitAndRollback(t *testing.T) {
	address := os.Getenv("DATA_MODEL_PORTABLE_TEST_ADDRESS")
	if address == "" {
		address = os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS")
	}
	if address == "" {
		if os.Getenv("DATA_MODEL_REQUIRE_MYSQL_TEST") == "1" {
			t.Fatal("required portable MySQL fixture missing")
		}
		t.Skip("dedicated disposable portable fixture not configured")
	}
	if address != "mysql:3306" || os.Getenv("DATA_MODEL_MYSQL_TEST_ROOT_PASSWORD") != "isolated-test-root-only" {
		t.Fatal("only isolated Compose fixture allowed")
	}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), localAuthorityKey{}, true), 60*time.Second)
	defer cancel()
	open := func(database string) *sql.DB {
		db, e := mysqlstore.Open(ctx, mysqlstore.Config{Address: address, Database: database, User: "root", Password: "isolated-test-root-only", AllowInsecureTransport: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	admin := open("data_model_publication_test")
	// No IF NOT EXISTS: refuse to touch a pre-existing application schema.
	if _, e := admin.ExecContext(ctx, "CREATE DATABASE fraud CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin"); e != nil {
		t.Fatal(e)
	}
	// Only remove the database this test successfully created, on the guarded
	// disposable Compose server. Register before open so its pool closes first.
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, e := admin.ExecContext(cleanup, "DROP DATABASE fraud"); e != nil {
			t.Error(e)
		}
	})
	db := open("fraud")
	defs, e := metadata.Definitions("fraud")
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range defs {
		if _, e = db.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	upgrade, err := installation.DeliveryRetryUpgradeStatement("fraud")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, upgrade.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, parent, child, pf, cf := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	e = mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, e := store.NewMetadataRepositories(tx, "fraud", tenant, 1000, 4<<20)
		if e != nil {
			return e
		}
		now := localClock{}.Now()
		if e = r.Tenants.Create(ctx, domainTenant.New(tenant, now, "portable", nil)); e != nil {
			return e
		}
		if e = r.Tenants.UpdateStatus(ctx, tenant, domainTenant.StatusActive); e != nil {
			return e
		}
		registry, e := store.NewPhysicalRegistry(tx, "fraud", tenant, 1000)
		if e != nil {
			return e
		}
		for i, id := range []uuid.UUID{parent, child} {
			name := []string{"parent", "child"}[i]
			if e = r.Tables.Create(ctx, datamodel.Table{ID: id, TenantID: tenant, Name: name, CreatedAt: now, UpdatedAt: now}); e != nil {
				return e
			}
			if e = registry.RegisterTable(ctx, id); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, "UPDATE fraud.data_model_physical_tables SET published_version=?,write_state='open' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenant), mysqlstore.UUID(id)); e != nil {
				return e
			}
		}
		for i, id := range []uuid.UUID{pf, cf} {
			table := parent
			if i == 1 {
				table = child
			}
			if e = r.Fields.Create(ctx, datamodel.Field{ID: id, TenantID: tenant, TableID: table, Name: "reference", DataType: datamodel.DataTypeString, IsUnique: i == 0, CreatedAt: now, UpdatedAt: now}); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	worker := LocalCreation{Target: db}
	tables := map[string]uuid.UUID{"parent": parent, "child": child}
	fields := map[string]map[string]uuid.UUID{"parent": {"reference": pf}, "child": {"reference": cf}}
	document := service.PortableDataModelDocument{Tables: []service.PortableTable{{Name: "parent", Options: &service.PortableTableOptions{DisplayedFields: []string{"reference"}}}}, Links: []service.PortableLink{{Name: "owner", ParentTable: "parent", ParentField: "reference", ChildTable: "child", ChildField: "reference"}}, Pivots: []service.PortablePivot{{BaseTable: "child", PathLinks: []string{"owner"}}}}
	assertEmpty := func() {
		t.Helper()
		for _, table := range []string{"data_model_links", "data_model_pivots", "data_model_table_options", "data_model_schema_change_log", "data_model_tenant_schema_migrations"} {
			var n int
			if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fraud."+table+" WHERE tenant_id=?", mysqlstore.UUID(tenant)).Scan(&n); e != nil || n != 0 {
				t.Fatalf("%s leaked: %d %v", table, n, e)
			}
		}
	}
	t.Run("authority", func(t *testing.T) {
		if _, e := worker.finishPortable(context.Background(), tenant, uuid.New(), "hash", document, tables, fields, service.PortableImportResult{}); e == nil {
			t.Fatal("missing authority accepted")
		}
		assertEmpty()
	})
	t.Run("cancellation", func(t *testing.T) {
		tx, e := db.BeginTx(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(ctx, "SELECT id FROM fraud.data_model_tables WHERE id=? FOR UPDATE", mysqlstore.UUID(parent)); e != nil {
			t.Fatal(e)
		}
		short, stop := context.WithTimeout(ctx, 100*time.Millisecond)
		defer stop()
		if _, e = worker.finishPortable(short, tenant, uuid.New(), "hash", document, tables, fields, service.PortableImportResult{}); e == nil {
			t.Fatal("blocked request ignored cancellation")
		}
		tx.Rollback()
		assertEmpty()
	})
	t.Run("completion_audit_failure_rolls_back_all", func(t *testing.T) {
		if _, e := db.ExecContext(ctx, "CREATE TRIGGER fraud.portable_test_failure BEFORE INSERT ON fraud.data_model_schema_change_log FOR EACH ROW BEGIN IF NEW.operation='portable_import' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected completion failure'; END IF; END"); e != nil {
			t.Fatal(e)
		}
		_, e := worker.finishPortable(ctx, tenant, uuid.New(), "hash", document, tables, fields, service.PortableImportResult{})
		if e == nil {
			t.Fatal("injected failure accepted")
		}
		assertEmpty()
		if _, e = db.ExecContext(ctx, "DROP TRIGGER fraud.portable_test_failure"); e != nil {
			t.Fatal(e)
		}
	})
	op := uuid.New()
	result, e := worker.finishPortable(ctx, tenant, op, "hash", document, tables, fields, service.PortableImportResult{})
	if e != nil {
		t.Fatal(e)
	}
	if result.LinksCreated != 1 || result.PivotsCreated != 1 || result.TableOptionsUpserted != 1 || result.RevisionID == "" {
		t.Fatal(result)
	}
	saved, e := readPortableReceipt(ctx, db, tenant, portableStep(op, "completed"))
	if e != nil || saved.Result == nil || *saved.Result != result {
		t.Fatal(saved, e)
	}
	if _, e = readPortableReceipt(ctx, db, uuid.New(), portableStep(op, "completed")); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("cross-tenant receipt", e)
	}
}
