package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestLookupGuards(t *testing.T) {
	db, database, tenant, table, version := writeFixture(t)
	ddl, err := LookupGuardSchema(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	run := func(ctx context.Context, keys []string) error {
		return mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			if _, err := LockWriteSnapshot(ctx, tx, database, tenant, table, version, 1000); err != nil {
				return err
			}
			return LockLookupKeys(ctx, tx, database, tenant, table, keys, 8192, 16384)
		})
	}
	t.Run("missing_key_serialized", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := LockWriteSnapshot(ctx, tx, database, tenant, table, version, 1000); err != nil {
			t.Fatal(err)
		}
		if err := LockLookupKeys(ctx, tx, database, tenant, table, []string{"missing"}, 8192, 16384); err != nil {
			t.Fatal(err)
		}
		short, stop := context.WithTimeout(ctx, 150*time.Millisecond)
		defer stop()
		if err := run(short, []string{"missing"}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("missing key lock bypassed", err)
		}
		if err := run(ctx, []string{"independent"}); err != nil {
			t.Fatal("independent key blocked", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if err := run(ctx, []string{"missing"}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("opposite_batch_orders", func(t *testing.T) {
		keys := []string{"A", "a", "a ", "a\x00", strings.Repeat("\u00e9", 1000)}
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		start := make(chan struct{})
		for i := 0; i < 8; i++ {
			batch := append([]string(nil), keys...)
			if i%2 == 0 {
				for a, b := 0, len(batch)-1; a < b; a, b = a+1, b-1 {
					batch[a], batch[b] = batch[b], batch[a]
				}
			}
			wg.Add(1)
			go func() { defer wg.Done(); <-start; errs <- run(ctx, batch) }()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("collision_only_serializes", func(t *testing.T) {
		if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			return lockLookupKeys(ctx, tx, database, tenant, table, []string{"collision1", "collision2"}, 8192, 16384, func([]byte) [32]byte { return [32]byte{1} })
		}); err != nil {
			t.Fatal(err)
		}
		var count int
		hash := [32]byte{1}
		if err := db.QueryRow("SELECT COUNT(*) FROM ingestion_lookup_guards WHERE key_digest=?", hash[:]).Scan(&count); err != nil || count != 1 {
			t.Fatal(count, err)
		}
	})
	t.Run("invalid_keys_and_scope", func(t *testing.T) {
		for _, keys := range [][]string{nil, {""}, {"duplicate", "duplicate"}, {"\xff"}, {strings.Repeat("x", 8193)}} {
			if err := run(ctx, keys); err == nil {
				t.Fatal("invalid keys accepted")
			}
		}
		if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			return LockLookupKeys(ctx, tx, database, uuid.New(), table, []string{"foreign"}, 8192, 16384)
		}); err == nil {
			t.Fatal("foreign relationship accepted")
		}
	})
}

func TestRestrictedWriteCoordination(t *testing.T) {
	db, database, tenant, table, version := writeFixture(t)
	ddl, err := LookupGuardSchema(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	user := "iw_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	account := "'" + user + "'@'%'"
	if _, err := db.Exec("CREATE USER " + account + " IDENTIFIED BY 'isolated-writer-only'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP USER " + account); err != nil {
			t.Error(err)
		}
	})
	schema, _ := mysqlstore.Identifier(database)
	for _, object := range []string{"data_model_tables", "data_model_fields", "data_model_physical_tables", "data_model_physical_fields"} {
		if _, err := db.Exec("GRANT SELECT ON " + schema + "." + object + " TO " + account); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("GRANT INSERT,SELECT(key_digest),UPDATE(key_digest) ON " + schema + ".ingestion_lookup_guards TO " + account); err != nil {
		t.Fatal(err)
	}
	pool, err := mysqlstore.Open(context.Background(), receiptConfig(database, user, "isolated-writer-only"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error(err)
		}
	})
	pool.SetMaxOpenConns(1)
	for i := 0; i < 2; i++ {
		if err := mysqlstore.Mutation(context.Background(), pool, func(tx *sql.Tx) error {
			if _, err := LockWriteSnapshot(context.Background(), tx, database, tenant, table, version, 1000); err != nil {
				return err
			}
			return LockLookupKeys(context.Background(), tx, database, tenant, table, []string{"same"}, 32, 32)
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		"UPDATE data_model_physical_tables SET write_state='open'",
		"UPDATE data_model_physical_fields SET field_label='forged'",
		"DELETE FROM ingestion_lookup_guards",
		"CREATE TABLE bypass(id INT)",
	} {
		if _, err := pool.Exec(statement); err == nil {
			t.Fatal("restricted bypass", statement)
		}
	}
}
