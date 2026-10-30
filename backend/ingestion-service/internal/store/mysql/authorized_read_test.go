package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestFullSharedContractTransitions(t *testing.T) {
	f := fullSharedFixture(t)
	tenant, table, version := seedFullContract(t, f)
	pool := fullFixturePrincipal(t, f, "reader")
	store, e := NewIngestionContractStore(pool, f.Database, 4096)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	fixtureExec(t, f.DB, "UPDATE data_model_ingestion_contracts SET table_name='renamed' WHERE table_id=?", mysqlstore.UUID(table))
	state, e := store.ReadContract(ctx, tenant, "transactions", table, version)
	if e != nil || state.TableID != uuid.Nil {
		t.Fatal(state, e)
	}
	state, e = store.ReadContract(ctx, tenant, "renamed", table, version)
	if e != nil || state.TableID != table {
		t.Fatal(state, e)
	}
	// A new table using an old public name must never be a cached hit for old ID.
	replacement := uuid.New()
	next := uuid.New()
	fixtureExec(t, f.DB, "UPDATE data_model_tables SET name='renamed' WHERE id=?", mysqlstore.UUID(table))
	fixtureExec(t, f.DB, "INSERT INTO data_model_tables(id,tenant_id,name,created_at,updated_at) VALUES(?,?,'transactions',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", mysqlstore.UUID(replacement), mysqlstore.UUID(tenant))
	fixtureExec(t, f.DB, "INSERT INTO data_model_ingestion_contracts(table_id,tenant_id,table_name,version,payload) VALUES(?,?,'transactions',?,JSON_OBJECT('id',?,'name','transactions','fields',JSON_OBJECT()))", mysqlstore.UUID(replacement), mysqlstore.UUID(tenant), mysqlstore.UUID(next), replacement.String())
	fixtureExec(t, f.DB, "INSERT INTO data_model_physical_tables(tenant_id,table_id,tenant_label,table_label,published_version,write_state) VALUES(?,?,'test','records',?,'open')", mysqlstore.UUID(tenant), mysqlstore.UUID(replacement), mysqlstore.UUID(next))
	state, e = store.ReadContract(ctx, tenant, "transactions", table, version)
	if e != nil || state.TableID != replacement || state.Version != next || len(state.Payload) == 0 {
		t.Fatal(state, e)
	}
}
func TestFullSharedAuthorizedReadLifetime(t *testing.T) {
	shared, f := fullRecordFixture(t)
	if _, e := f.write(context.Background(), []map[string]any{{"object_id": "one"}}); e != nil {
		t.Fatal(e)
	}
	pool := fullFixturePrincipal(t, shared, "reader")
	var account string
	if e := pool.QueryRow("SELECT SUBSTRING_INDEX(CURRENT_USER(),'@',1)").Scan(&account); e != nil {
		t.Fatal(e)
	}
	fixtureExec(t, shared.DB, "GRANT SELECT ON "+f.qualified+" TO '"+account+"'@'%'")
	store, e := NewIngestionContractStore(pool, shared.Database, 1<<20)
	if e != nil {
		t.Fatal(e)
	}
	ctx := WithServiceCredential(context.Background(), "fixture-token")
	entered := false
	run := func(ctx context.Context, tx *sql.Tx, p *RecordProjection) error {
		entered = true
		columns, table := p.SQL()
		var value string
		if e := tx.QueryRowContext(ctx, "SELECT "+columns+" FROM "+table).Scan(&value); e != nil {
			return e
		}
		if value != "one" {
			t.Fatal(value)
		}
		// Competing owner publication cannot close the held table gate.
		blocked, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, e := shared.DB.ExecContext(blocked, "UPDATE data_model_physical_tables SET write_state='closed' WHERE table_id=?", mysqlstore.UUID(f.model.Tables["records"].ID))
		if e == nil || !errors.Is(blocked.Err(), context.DeadlineExceeded) {
			t.Fatal("publication crossed read barrier", e)
		}
		return nil
	}
	if e := store.WithRecordRead(context.Background(), f.model.TenantID, "records", "fixture-token", []string{"object_id"}, run); !errors.Is(e, ErrOwnerAuthorization) || entered {
		t.Fatal(e, entered)
	}
	if e := store.WithRecordRead(ctx, f.model.TenantID, "records", "fixture-token", []string{"object_id"}, run); e != nil || !entered {
		t.Fatal(e, entered)
	}
	fixtureExec(t, shared.DB, "UPDATE data_model_physical_tables SET write_state='closed' WHERE table_id=?", mysqlstore.UUID(f.model.Tables["records"].ID))
	entered = false
	if e := store.WithRecordRead(ctx, f.model.TenantID, "records", "fixture-token", []string{"object_id"}, run); !errors.Is(e, ErrWriteUnavailable) || entered {
		t.Fatal(e, entered)
	}
}
