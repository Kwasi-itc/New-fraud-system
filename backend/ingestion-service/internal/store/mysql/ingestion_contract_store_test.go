package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/clients/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func fixtureExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, e := db.Exec(q, args...); e != nil {
		t.Fatal(q, e)
	}
}
func fullFixturePrincipal(t *testing.T, f sharedFixture, kind string) *sql.DB {
	t.Helper()
	name := "if_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	account := "'" + name + "'@'%'"
	fixtureExec(t, f.DB, "CREATE USER "+account+" IDENTIFIED BY 'isolated-reader-only'")
	t.Cleanup(func() {
		if _, e := f.DB.Exec("DROP USER " + account); e != nil {
			t.Error(e)
		}
	})
	var grants []string
	var err error
	switch kind {
	case "reader":
		grants, err = ContractReadGrants(f.Database, name)
	case "runtime", "worker":
		grants, err = RuntimeGrants(f.Database, name, kind == "worker")
	case "importer", "preview":
		grants, err = LegacyOperatorGrants(f.Database, name, kind == "preview")
	case "operator":
		grants, err = InspectionGrants(f.Database, name)
	default:
		t.Fatal("unknown fixture principal", kind)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range grants {
		fixtureExec(t, f.DB, q)
	}
	pool, err := mysqlstore.Open(context.Background(), receiptConfig(f.Database, name, "isolated-reader-only"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error(err)
		}
	})
	return pool
}
func seedFullContract(t *testing.T, f sharedFixture) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	tenant, table, version := uuid.New(), uuid.New(), uuid.New()
	fixtureExec(t, f.DB, "INSERT INTO data_model_tenants(id,name,schema_name,status,created_at,updated_at) VALUES(?,'fixture',?,'active',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(tenant), "tenant_"+strings.ReplaceAll(tenant.String(), "-", ""))
	fixtureExec(t, f.DB, "INSERT INTO data_model_tables(id,tenant_id,name,created_at,updated_at) VALUES(?,?,'transactions',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(table), mysqlstore.UUID(tenant))
	payload, _ := json.Marshal(map[string]any{"id": table, "name": "transactions", "fields": map[string]any{}})
	fixtureExec(t, f.DB, "INSERT INTO data_model_ingestion_contracts(table_id,tenant_id,table_name,version,payload) VALUES(?,?,'transactions',?,?)", mysqlstore.UUID(table), mysqlstore.UUID(tenant), mysqlstore.UUID(version), payload)
	fixtureExec(t, f.DB, "INSERT INTO data_model_physical_tables(tenant_id,table_id,tenant_label,table_label,published_version,write_state) VALUES(?,?,'test','records',?,'open')", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(version))
	return tenant, table, version
}
func TestFullSharedContractReader(t *testing.T) {
	f := fullSharedFixture(t)
	tenant, table, version := seedFullContract(t, f)
	pool := fullFixturePrincipal(t, f, "reader")
	store, err := NewIngestionContractStore(pool, f.Database, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	state, err := store.ReadContract(ctx, tenant, "transactions", uuid.Nil, uuid.Nil)
	if err != nil || state.TableID != table || state.Version != version || len(state.Payload) == 0 {
		t.Fatal(state, err)
	}
	cached, err := store.ReadContract(ctx, tenant, "transactions", table, version)
	if err != nil || cached.Payload != nil || cached.TableID != table {
		t.Fatal(cached, err)
	}
	reader := datamodel.NewContractReader(store, nil, 10, 8192)
	for i := 0; i < 2; i++ {
		model, e := reader.GetPublishedTable(ctx, tenant, "transactions")
		if e != nil || model.Tables["transactions"].ID != table {
			t.Fatal(model, e)
		}
	}
	t.Run("missing_and_foreign", func(t *testing.T) {
		state, e := store.ReadContract(ctx, tenant, "absent", uuid.Nil, uuid.Nil)
		if e != nil || state.TableID != uuid.Nil {
			t.Fatal(state, e)
		}
		if _, e = store.ReadContract(ctx, uuid.New(), "transactions", table, version); !errors.Is(e, sql.ErrNoRows) {
			t.Fatal(e)
		}
		if _, e = store.ReadContract(ctx, tenant, "Transactions", uuid.Nil, uuid.Nil); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("archive_and_closed", func(t *testing.T) {
		fixtureExec(t, f.DB, "UPDATE data_model_tables SET archived=TRUE WHERE id=?", mysqlstore.UUID(table))
		if _, e := reader.GetPublishedTable(ctx, tenant, "transactions"); !errors.Is(e, ErrWriteUnavailable) {
			t.Fatal(e)
		}
		fixtureExec(t, f.DB, "UPDATE data_model_tables SET archived=FALSE WHERE id=?", mysqlstore.UUID(table))
		fixtureExec(t, f.DB, "UPDATE data_model_physical_tables SET write_state='closed' WHERE table_id=?", mysqlstore.UUID(table))
		if _, e := reader.GetPublishedTable(ctx, tenant, "transactions"); !errors.Is(e, ErrWriteUnavailable) {
			t.Fatal(e)
		}
		fixtureExec(t, f.DB, "UPDATE data_model_physical_tables SET write_state='open' WHERE table_id=?", mysqlstore.UUID(table))
	})
	t.Run("version_mismatch", func(t *testing.T) {
		fixtureExec(t, f.DB, "UPDATE data_model_physical_tables SET published_version=? WHERE table_id=?", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(table))
		if _, e := reader.GetPublishedTable(ctx, tenant, "transactions"); !errors.Is(e, ErrWriteUnavailable) {
			t.Fatal(e)
		}
		fixtureExec(t, f.DB, "UPDATE data_model_physical_tables SET published_version=? WHERE table_id=?", mysqlstore.UUID(version), mysqlstore.UUID(table))
	})
	t.Run("status_cache_refresh", func(t *testing.T) {
		fixtureExec(t, f.DB, "UPDATE data_model_tenants SET status='pending' WHERE id=?", mysqlstore.UUID(tenant))
		model, e := reader.GetPublishedTable(ctx, tenant, "transactions")
		if e != nil || model.Writable {
			t.Fatal(model, e)
		}
		fixtureExec(t, f.DB, "UPDATE data_model_tenants SET status='active' WHERE id=?", mysqlstore.UUID(tenant))
	})
	t.Run("bounded_payload_even_on_cache_hit", func(t *testing.T) {
		fixtureExec(t, f.DB, "UPDATE data_model_ingestion_contracts SET payload=JSON_OBJECT('padding',?) WHERE table_id=?", strings.Repeat("x", 1025), mysqlstore.UUID(table))
		if _, e := reader.GetPublishedTable(ctx, tenant, "transactions"); !errors.Is(e, ErrContractBounds) {
			t.Fatal(e)
		}
	})
	t.Run("reader_cannot_mutate_or_inspect_dictionary", func(t *testing.T) {
		for _, q := range []string{"SELECT raw_value FROM data_model_exact_keys", "UPDATE data_model_tenants SET status='pending'", "DELETE FROM data_model_ingestion_contracts", "CREATE TABLE unauthorized(id INT)"} {
			rows, e := pool.Query(q)
			if e == nil {
				rows.Close()
				t.Fatal("unexpected grant", q)
			}
		}
	})
}
