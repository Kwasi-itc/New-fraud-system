package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/contract"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Only a publication-column fixture: not the production metadata migration.
// Fresh isolated database; never DROP/TRUNCATE a configured application database.
func publicationDB(t *testing.T) *sql.DB {
	t.Helper()
	address := os.Getenv("DATA_MODEL_MYSQL_TEST_ADDRESS")
	if address == "" {
		if os.Getenv("DATA_MODEL_REQUIRE_MYSQL_TEST") == "1" {
			t.Fatal("required MySQL fixture missing")
		}
		t.Skip("isolated MySQL publication fixture not configured")
	}
	if address != "mysql:3306" {
		t.Fatal("fixture only permits isolated Compose hostname mysql:3306")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := mysqlstore.Open(ctx, mysqlstore.Config{Address: address, Database: "data_model_publication_test", User: "migration_test", Password: "isolated-test-only", AllowInsecureTransport: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, ddl := range []string{
		// Candidate worker columns only; not a production metadata lineage.
		`CREATE TABLE data_model_index_jobs (id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, table_id BINARY(16) NULL, table_name TEXT NOT NULL, index_type VARCHAR(20) NOT NULL, columns_json JSON NOT NULL, status VARCHAR(20) NOT NULL, requested_by_operation TEXT NOT NULL, error_message TEXT NULL, attempt_count INT NOT NULL DEFAULT 0, requested_at DATETIME(6) NOT NULL, started_at DATETIME(6) NULL, completed_at DATETIME(6) NULL, scheduled_at DATETIME(6) NULL, dedupe_key TEXT NOT NULL) ENGINE=InnoDB`,
		`CREATE TABLE data_model_schema_change_log (id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, operation TEXT NOT NULL, resource_type TEXT NOT NULL, resource_id BINARY(16) NOT NULL, status TEXT NOT NULL, details JSON NOT NULL, created_at DATETIME(6) NOT NULL) ENGINE=InnoDB`,
		`CREATE TABLE data_model_tables (id BINARY(16) PRIMARY KEY, tenant_id BINARY(16) NOT NULL, name TEXT NOT NULL, archived BOOLEAN NOT NULL DEFAULT FALSE, UNIQUE(tenant_id,id)) ENGINE=InnoDB`,
		`CREATE TABLE data_model_fields (id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,table_id BINARY(16) NOT NULL,name TEXT NOT NULL,data_type VARCHAR(20) NOT NULL,nullable BOOLEAN NOT NULL DEFAULT FALSE,is_enum BOOLEAN NOT NULL DEFAULT FALSE,is_unique BOOLEAN NOT NULL DEFAULT FALSE,archived BOOLEAN NOT NULL DEFAULT FALSE,UNIQUE(tenant_id,id),KEY(tenant_id,table_id,id),FOREIGN KEY(tenant_id,table_id) REFERENCES data_model_tables(tenant_id,id)) ENGINE=InnoDB`,
		`CREATE TABLE data_model_field_enum_values (id BINARY(16) PRIMARY KEY,tenant_id BINARY(16) NOT NULL,field_id BINARY(16) NOT NULL,value TEXT NOT NULL,label TEXT NOT NULL,sort_order INT NOT NULL,KEY(tenant_id,field_id,id),FOREIGN KEY(tenant_id,field_id) REFERENCES data_model_fields(tenant_id,id)) ENGINE=InnoDB`,
		// Test-only constraint injects a database publication-write failure.
		`CREATE TABLE data_model_ingestion_contracts (tenant_id BINARY(16) NOT NULL,table_id BINARY(16) NOT NULL,table_name LONGTEXT NOT NULL,version BINARY(16) NOT NULL,payload JSON NOT NULL,PRIMARY KEY(tenant_id,table_id),FOREIGN KEY(tenant_id,table_id) REFERENCES data_model_tables(tenant_id,id),CHECK(JSON_UNQUOTE(JSON_EXTRACT(payload,'$.name')) <> 'reject_publication')) ENGINE=InnoDB`,
		// Test-only hash CHECK injects receipt failure after publication writes.
		`CREATE TABLE data_model_publication_operations (tenant_id BINARY(16) NOT NULL,table_id BINARY(16) NOT NULL,operation_id BINARY(16) NOT NULL,request_hash BINARY(32) NOT NULL,condition_mode VARCHAR(8) NOT NULL,expected_version BINARY(16) NOT NULL,version BINARY(16) NOT NULL,payload JSON NOT NULL,PRIMARY KEY(tenant_id,table_id,operation_id),FOREIGN KEY(tenant_id,table_id) REFERENCES data_model_tables(tenant_id,id),CHECK(request_hash <> UNHEX(REPEAT('ff',32)))) ENGINE=InnoDB`,
	} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestMySQLPublication(t *testing.T) {
	db := publicationDB(t)
	t.Run("worker_execution", func(t *testing.T) { testIndexJobExecution(t, db) })
	t.Run("byte_limits", func(t *testing.T) { testPublicationByteLimits(t, db) })
	t.Run("operation_recovery", func(t *testing.T) { testPublicationOperations(t, db) })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	store, err := NewPublicationStore(db, "data_model_publication_test", 100, 100, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	seed := func() (uuid.UUID, uuid.UUID) {
		t.Helper()
		tenantID, tableID := uuid.New(), uuid.New()
		if _, err := db.ExecContext(ctx, "INSERT INTO data_model_tables(id,tenant_id,name) VALUES (?,?,?)", mysqlstore.UUID(tableID), mysqlstore.UUID(tenantID), "records"); err != nil {
			t.Fatal(err)
		}
		return tenantID, tableID
	}
	field := func(tx *sql.Tx, tenantID, tableID uuid.UUID, name string) (uuid.UUID, error) {
		id := uuid.New()
		_, err := tx.ExecContext(ctx, "INSERT INTO data_model_fields(id,tenant_id,table_id,name,data_type,is_enum) VALUES (?,?,?,?,?,true)", mysqlstore.UUID(id), mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID), name, "string")
		return id, err
	}
	read := func(tenantID, tableID uuid.UUID) contract.Publication {
		t.Helper()
		p := contract.Publication{TenantID: tenantID, TableID: tableID}
		var version mysqlstore.UUID
		if err := db.QueryRowContext(ctx, "SELECT version,payload FROM data_model_ingestion_contracts WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&version, &p.Payload); err != nil {
			t.Fatal(err)
		}
		p.Version = uuid.UUID(version)
		return p
	}
	noChange := func(*sql.Tx) error { return nil }
	t.Run("publish_and_retain_token", func(t *testing.T) {
		tenantID, tableID := seed()
		first, err := store.MutateAndPublish(ctx, tenantID, tableID, func(tx *sql.Tx) error {
			id, err := field(tx, tenantID, tableID, "state")
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, "INSERT INTO data_model_field_enum_values(id,tenant_id,field_id,value,label,sort_order) VALUES (?,?,?,?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenantID), mysqlstore.UUID(id), "a", "Alpha", 1)
			return err
		})
		if err != nil || first.Version == uuid.Nil {
			t.Fatal(first, err)
		}
		persisted := read(tenantID, tableID)
		if first.Version != persisted.Version {
			t.Fatal("committed version differs")
		}
		var doc struct {
			Fields map[string]struct {
				EnumValues []struct {
					Label string `json:"label"`
				} `json:"enum_values"`
			} `json:"fields"`
		}
		if err := json.Unmarshal(persisted.Payload, &doc); err != nil || len(doc.Fields["state"].EnumValues) != 1 || doc.Fields["state"].EnumValues[0].Label != "Alpha" {
			t.Fatal("incomplete snapshot", err)
		}
		second, err := store.MutateAndPublish(ctx, tenantID, tableID, noChange)
		if err != nil || second.Version != first.Version {
			t.Fatal("unchanged publication rotated", err)
		}
	})
	t.Run("rollback_and_wrong_tenant", func(t *testing.T) {
		tenantID, tableID := seed()
		first, err := store.MutateAndPublish(ctx, tenantID, tableID, noChange)
		if err != nil {
			t.Fatal(err)
		}
		failure := errors.New("owner mutation failed")
		result, err := store.MutateAndPublish(ctx, tenantID, tableID, func(tx *sql.Tx) error {
			if _, err := field(tx, tenantID, tableID, "failed"); err != nil {
				return err
			}
			return failure
		})
		if !errors.Is(err, failure) || result.Payload != nil {
			t.Fatal("rollback returned success", err)
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM data_model_fields WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&count); err != nil || count != 0 {
			t.Fatal("mutation leaked", err)
		}
		if read(tenantID, tableID).Version != first.Version {
			t.Fatal("rollback changed token")
		}
		_, err = store.MutateAndPublish(ctx, uuid.New(), tableID, func(*sql.Tx) error { t.Fatal("wrong tenant entered mutation"); return nil })
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("wrong tenant not rejected", err)
		}
	})
	t.Run("snapshot_limit_rolls_back", func(t *testing.T) {
		tenantID, tableID := seed()
		bounded, err := NewPublicationStore(db, "data_model_publication_test", 1, 1, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		_, err = bounded.MutateAndPublish(ctx, tenantID, tableID, func(tx *sql.Tx) error {
			if _, err := field(tx, tenantID, tableID, "first"); err != nil {
				return err
			}
			_, err := field(tx, tenantID, tableID, "second")
			return err
		})
		if !errors.Is(err, ErrSnapshotLimit) {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM data_model_fields WHERE table_id=?", mysqlstore.UUID(tableID)).Scan(&count); err != nil || count != 0 {
			t.Fatal("limit failure leaked metadata", err)
		}
	})
	t.Run("concurrent_edits_and_independent_tables", func(t *testing.T) {
		tenantID, tableID := seed()
		otherTable := uuid.New()
		if _, err := db.ExecContext(ctx, "INSERT INTO data_model_tables(id,tenant_id,name) VALUES (?,?,?)", mysqlstore.UUID(otherTable), mysqlstore.UUID(tenantID), "other_records"); err != nil {
			t.Fatal(err)
		}
		locked, release := make(chan struct{}), make(chan struct{})
		var releaseOnce sync.Once
		unlock := func() { releaseOnce.Do(func() { close(release) }) }
		defer unlock()
		firstDone, secondDone := make(chan error, 1), make(chan error, 1)
		go func() {
			_, err := store.MutateAndPublish(ctx, tenantID, tableID, func(tx *sql.Tx) error {
				if _, err := field(tx, tenantID, tableID, "first"); err != nil {
					return err
				}
				close(locked)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
				return nil
			})
			firstDone <- err
		}()
		select {
		case <-locked:
		case err := <-firstDone:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		go func() {
			_, err := store.MutateAndPublish(ctx, tenantID, tableID, func(tx *sql.Tx) error { _, err := field(tx, tenantID, tableID, "second"); return err })
			secondDone <- err
		}()
		independentCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		_, err := store.MutateAndPublish(independentCtx, tenantID, otherTable, noChange)
		stop()
		if err != nil {
			t.Fatal("unrelated table blocked", err)
		}
		unlock()
		for _, done := range []chan error{firstDone, secondDone} {
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		var doc struct {
			Fields map[string]any `json:"fields"`
		}
		if err := json.Unmarshal(read(tenantID, tableID).Payload, &doc); err != nil || len(doc.Fields) != 2 || doc.Fields["first"] == nil || doc.Fields["second"] == nil {
			t.Fatal("concurrent field lost", err)
		}
	})
	t.Run("publication_write_failure_rolls_back_metadata", func(t *testing.T) {
		tenantID, tableID := seed()
		before, err := store.MutateAndPublish(ctx, tenantID, tableID, noChange)
		if err != nil {
			t.Fatal(err)
		}
		result, err := store.MutateAndPublish(ctx, tenantID, tableID, func(tx *sql.Tx) error {
			if _, err := field(tx, tenantID, tableID, "rolled_back"); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "UPDATE data_model_tables SET name='reject_publication' WHERE tenant_id=? AND id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID))
			return err
		})
		if err == nil || result.Payload != nil {
			t.Fatal("database rejection reported success")
		}
		if read(tenantID, tableID).Version != before.Version {
			t.Fatal("rejected publication changed version")
		}
		var name string
		var count int
		if err := db.QueryRowContext(ctx, "SELECT name FROM data_model_tables WHERE id=?", mysqlstore.UUID(tableID)).Scan(&name); err != nil || name != "records" {
			t.Fatal("table edit escaped rollback", err)
		}
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM data_model_fields WHERE table_id=?", mysqlstore.UUID(tableID)).Scan(&count); err != nil || count != 0 {
			t.Fatal("field edit escaped rollback", err)
		}
	})
	t.Run("lock_wait_cancellation_and_recovery", func(t *testing.T) {
		tenantID, tableID := seed()
		blocker, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer blocker.Rollback()
		var id mysqlstore.UUID
		if err := blocker.QueryRowContext(ctx, "SELECT id FROM data_model_tables WHERE tenant_id=? AND id=? FOR UPDATE", mysqlstore.UUID(tenantID), mysqlstore.UUID(tableID)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		waiting, stop := context.WithTimeout(ctx, 200*time.Millisecond)
		result, err := store.MutateAndPublish(waiting, tenantID, tableID, func(*sql.Tx) error { t.Error("entered mutation while table locked"); return nil })
		stop()
		if !errors.Is(err, context.DeadlineExceeded) || result.Payload != nil {
			t.Fatal("wait cancellation not surfaced", err)
		}
		if err := blocker.Rollback(); err != nil {
			t.Fatal(err)
		}
		if _, err := store.MutateAndPublish(ctx, tenantID, tableID, noChange); err != nil {
			t.Fatal("cancelled waiter prevented later publication", err)
		}
	})
	t.Run("enum_limit_and_tenant_fk", func(t *testing.T) {
		tenantID, tableID := seed()
		bounded, err := NewPublicationStore(db, "data_model_publication_test", 10, 1, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		_, err = bounded.MutateAndPublish(ctx, tenantID, tableID, func(tx *sql.Tx) error {
			id, err := field(tx, tenantID, tableID, "state")
			if err != nil {
				return err
			}
			for _, value := range []string{"a", "b"} {
				if _, err := tx.ExecContext(ctx, "INSERT INTO data_model_field_enum_values(id,tenant_id,field_id,value,label,sort_order) VALUES (?,?,?,?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tenantID), mysqlstore.UUID(id), value, value, 1); err != nil {
					return err
				}
			}
			return nil
		})
		if !errors.Is(err, ErrSnapshotLimit) {
			t.Fatal("enum limit not enforced", err)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO data_model_fields(id,tenant_id,table_id,name,data_type) VALUES (?,?,?,?,?)", mysqlstore.UUID(uuid.New()), mysqlstore.UUID(uuid.New()), mysqlstore.UUID(tableID), "foreign", "string"); err == nil {
			t.Fatal("cross-tenant child accepted")
		}
	})
}
