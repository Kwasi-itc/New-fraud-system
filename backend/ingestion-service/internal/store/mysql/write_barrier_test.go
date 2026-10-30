package mysql

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Installs the owner's actual registry DDL, with minimal referenced metadata.
// Privileged gate setup is a fixture, not production publication acceptance.
func writeFixture(t *testing.T) (*sql.DB, string, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	db, name := receiptFixture(t)
	for _, q := range []string{
		"CREATE TABLE data_model_tables(tenant_id BINARY(16),id BINARY(16),archived BOOLEAN NOT NULL DEFAULT FALSE,PRIMARY KEY(tenant_id,id)) ENGINE=InnoDB",
		"CREATE TABLE data_model_fields(tenant_id BINARY(16),table_id BINARY(16),id BINARY(16),data_type VARCHAR(20),archived BOOLEAN NOT NULL DEFAULT FALSE,PRIMARY KEY(tenant_id,table_id,id)) ENGINE=InnoDB",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	schema, _ := mysqlstore.Identifier(name)
	for _, file := range []string{"022_physical_tables.sql", "023_physical_fields.sql"} {
		path := filepath.Join("..", "..", "..", "..", "data-model-service", "internal", "store", "mysql", "metadata", "sql", file)
		ddl, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(strings.ReplaceAll(string(ddl), "{{database}}", schema)); err != nil {
			t.Fatal(err)
		}
	}
	tenant, table, version := uuid.New(), uuid.New(), uuid.New()
	if _, err := db.Exec("INSERT INTO data_model_tables(tenant_id,id) VALUES (?,?)", mysqlstore.UUID(tenant), mysqlstore.UUID(table)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO data_model_physical_tables(tenant_id,table_id,tenant_label,table_label,published_version,write_state) VALUES (?,?,'test','records',?,'open')", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(version)); err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"string", "timestamp", "int", "float", "bool", "ip_address"} {
		id := uuid.New()
		if _, err := db.Exec("INSERT INTO data_model_fields(tenant_id,table_id,id,data_type) VALUES (?,?,?,?)", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(id), typ); err != nil {
			t.Fatal(err)
		}
		parts := []string{"value"}
		if typ == "ip_address" {
			parts = []string{"address", "family", "prefix"}
		}
		for _, part := range parts {
			if _, err := db.Exec("INSERT INTO data_model_physical_fields(tenant_id,table_id,field_id,component,field_label) VALUES (?,?,?,?,'value')", mysqlstore.UUID(tenant), mysqlstore.UUID(table), mysqlstore.UUID(id), part); err != nil {
				t.Fatal(err)
			}
		}
	}
	return db, name, tenant, table, version
}

func TestWriteBarrierSnapshot(t *testing.T) {
	for _, scenario := range []string{"ready", "stale", "tenant", "closed", "archived", "retired", "active_operation", "missing_mapping", "bad_name", "field_limit"} {
		t.Run(scenario, func(t *testing.T) {
			db, name, tenant, table, version := writeFixture(t)
			var statement string
			switch scenario {
			case "stale":
				version = uuid.New()
			case "tenant":
				tenant = uuid.New()
			case "closed":
				statement = "UPDATE data_model_physical_tables SET write_state='closed'"
			case "archived":
				statement = "UPDATE data_model_tables SET archived=TRUE"
			case "retired":
				statement = "UPDATE data_model_physical_tables SET write_state='retired'"
			case "active_operation":
				statement = "UPDATE data_model_physical_tables SET write_state='closed',active_operation=UUID_TO_BIN(UUID())"
			case "missing_mapping":
				statement = "DELETE FROM data_model_physical_fields WHERE component='prefix'"
			case "bad_name":
				statement = "UPDATE data_model_physical_fields SET field_label='bad-name'"
			}
			if statement != "" {
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			limit := 1000
			if scenario == "field_limit" {
				limit = 2
			}
			err := mysqlstore.Mutation(context.Background(), db, func(tx *sql.Tx) error {
				snapshot, err := LockWriteSnapshot(context.Background(), tx, name, tenant, table, version, limit)
				if scenario == "ready" {
					if err != nil || len(snapshot.Fields) != 6 || snapshot.Version != version || snapshot.TableID != table || snapshot.TenantID != tenant {
						t.Fatal(snapshot, err)
					}
				} else {
					if err == nil || snapshot.PhysicalName != "" || len(snapshot.Fields) != 0 {
						t.Fatal("partial/stale snapshot", snapshot, err)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWriteBarrierBlocksDDLPreparation(t *testing.T) {
	db, name, tenant, table, version := writeFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := LockWriteSnapshot(ctx, tx, name, tenant, table, version, 1000); err != nil {
		t.Fatal(err)
	}
	// Another writer shares the gate and does not wait for the first writer.
	if err := mysqlstore.Mutation(ctx, db, func(other *sql.Tx) error {
		_, err := LockWriteSnapshot(ctx, other, name, tenant, table, version, 1000)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	short, stop := context.WithTimeout(ctx, 150*time.Millisecond)
	defer stop()
	_, err = db.ExecContext(short, "UPDATE data_model_physical_tables SET write_state='closed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("DDL preparation bypassed writer", err)
	}
	other := uuid.New()
	if _, err := db.ExecContext(ctx, "INSERT INTO data_model_tables(tenant_id,id) VALUES (?,?)", mysqlstore.UUID(tenant), mysqlstore.UUID(other)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO data_model_physical_tables(tenant_id,table_id,tenant_label,table_label) VALUES (?,?,'test','independent')", mysqlstore.UUID(tenant), mysqlstore.UUID(other)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE data_model_physical_tables SET epoch=epoch+1 WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(other)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE data_model_physical_tables SET write_state='closed' WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(tenant), mysqlstore.UUID(table)); err != nil {
		t.Fatal(err)
	}
}
