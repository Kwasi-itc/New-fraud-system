package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func receiptFixture(t *testing.T) (*sql.DB, string) {
	t.Helper()
	address := os.Getenv("INGESTION_MYSQL_TEST_ADDRESS")
	if address == "" {
		if os.Getenv("INGESTION_REQUIRE_MYSQL_TEST") == "1" {
			t.Fatal("required MySQL fixture missing")
		}
		t.Skip("isolated MySQL fixture not configured")
	}
	if address != "mysql:3306" {
		t.Fatal("only isolated compose MySQL address permitted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := mysqlstore.Open(ctx, receiptConfig("mysql", "root", "isolated-test-root-only"))
	if err != nil {
		t.Fatal(err)
	}
	name := "ingestion_mysql_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	schema, _ := mysqlstore.Identifier(name)
	if _, err = admin.ExecContext(ctx, "CREATE DATABASE "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	db, err := mysqlstore.Open(ctx, receiptConfig(name, "root", "isolated-test-root-only"))
	if err != nil {
		admin.Exec("DROP DATABASE " + schema)
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
		if _, e := admin.Exec("DROP DATABASE " + schema); e != nil {
			t.Error(e)
		}
		if e := admin.Close(); e != nil {
			t.Error(e)
		}
	})
	statements, err := ReceiptSchema(name)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range statements {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.ExecContext(ctx, "CREATE TABLE fixture_effects (id BINARY(16) PRIMARY KEY) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	return db, name
}
func receiptConfig(database, user, password string) mysqlstore.Config {
	return mysqlstore.Config{Address: os.Getenv("INGESTION_MYSQL_TEST_ADDRESS"), Database: database, User: user, Password: password, AllowInsecureTransport: true, MaxOpen: 10, MaxIdle: 10, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute}
}

func TestReceiptExecution(t *testing.T) {
	for _, scenario := range []string{"replay", "concurrent", "rollback", "result_limit", "key_collision", "cancel", "restricted"} {
		t.Run(scenario, func(t *testing.T) {
			db, name := receiptFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			tenant, table := uuid.New(), uuid.New()
			var revoked atomic.Bool
			denied := errors.New("receipt permission revoked")
			var arrived atomic.Int32
			ready := make(chan struct{})
			pool := db
			if scenario == "restricted" {
				pool = restrictedReceiptPool(t, db, name)
			}
			m, err := NewReceiptManager(pool, name, tenant, ReceiptLimits{1 << 16, 128, 16}, func(ctx context.Context, r ReceiptRequest) error {
				if revoked.Load() {
					return denied
				}
				if scenario == "concurrent" {
					n := arrived.Add(1)
					if n <= 8 {
						if n == 8 {
							close(ready)
						}
						select {
						case <-ready:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				}
				return nil
			}, func(context.Context, *sql.Tx, ReceiptRequest) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "key_collision" {
				m.digest = func([]byte) [32]byte { return [32]byte{1} }
			}
			r := ReceiptRequest{TenantID: tenant, TableID: table, Key: "request", Mode: "create", Kind: "single", Hash: sha256.Sum256([]byte("command"))}
			var calls atomic.Int32
			failure := errors.New("owner failed")
			effect := func(ctx context.Context, tx *sql.Tx) (json.RawMessage, error) {
				calls.Add(1)
				if _, e := tx.ExecContext(ctx, "INSERT INTO fixture_effects(id) VALUES (?)", mysqlstore.UUID(uuid.New())); e != nil {
					return nil, e
				}
				if scenario == "rollback" {
					return nil, failure
				}
				if scenario == "result_limit" {
					return json.RawMessage(`{"value":"` + strings.Repeat("x", 128) + `"}`), nil
				}
				return json.RawMessage(`{"action":"created"}`), nil
			}
			if scenario == "cancel" {
				block, err := db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer block.Rollback()
				digest := sha256.Sum256([]byte(r.Key))
				if _, err = block.ExecContext(ctx, "INSERT INTO ingestion_receipt_buckets VALUES (?,?)", mysqlstore.UUID(tenant), digest[:]); err != nil {
					t.Fatal(err)
				}
				short, stop := context.WithTimeout(ctx, 100*time.Millisecond)
				defer stop()
				v, err := m.Execute(short, r, effect)
				if !errors.Is(err, context.DeadlineExceeded) || len(v.Payload) != 0 || calls.Load() != 0 {
					t.Fatal("cancelled reservation", v, err)
				}
				if err = block.Rollback(); err != nil {
					t.Fatal(err)
				}
			}
			var first ReceiptResult
			if scenario == "concurrent" {
				var wg sync.WaitGroup
				errs := make(chan error, 8)
				results := make(chan ReceiptResult, 8)
				for i := 0; i < 8; i++ {
					wg.Add(1)
					go func() { defer wg.Done(); v, e := m.Execute(ctx, r, effect); results <- v; errs <- e }()
				}
				wg.Wait()
				close(errs)
				close(results)
				for e := range errs {
					if e != nil {
						t.Fatal(e)
					}
				}
				fresh := 0
				for v := range results {
					if !v.Replayed {
						fresh++
					}
					if string(v.Payload) != `{"action":"created"}` {
						t.Fatal("wrong response", v)
					}
					first = v
				}
				if fresh != 1 {
					t.Fatal("multiple mutations", fresh)
				}
			} else {
				first, err = m.Execute(ctx, r, effect)
			}
			if scenario == "rollback" || scenario == "result_limit" {
				want := failure
				if scenario == "result_limit" {
					want = ErrReceiptLimit
				}
				if !errors.Is(err, want) || len(first.Payload) != 0 {
					t.Fatal("provisional response", first, err)
				}
				for _, table := range []string{"fixture_effects", "ingestion_request_receipts", "ingestion_receipt_buckets"} {
					var count int
					if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); e != nil || count != 0 {
						t.Fatal("rollback leaked", table, count, e)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			replay, err := m.Execute(ctx, r, effect)
			if err != nil || !replay.Replayed || string(replay.Payload) != string(first.Payload) || calls.Load() != 1 {
				t.Fatal("replay reran effects", replay, err, calls.Load())
			}
			// Mutation of returned bytes must not alter the persisted original.
			first.Payload[0] = 'x'
			for _, kind := range []string{"table", "mode", "kind", "hash"} {
				bad := r
				switch kind {
				case "table":
					bad.TableID = uuid.New()
				case "mode":
					bad.Mode = "patch"
				case "kind":
					bad.Kind = "batch"
				case "hash":
					bad.Hash[0] ^= 1
				}
				if v, e := m.Execute(ctx, bad, effect); !errors.Is(e, ErrReceiptConflict) || len(v.Payload) != 0 {
					t.Fatal("identity substitution", kind, e)
				}
			}
			foreign := r
			foreign.TenantID = uuid.New()
			if _, e := m.Execute(ctx, foreign, effect); e == nil {
				t.Fatal("foreign tenant")
			}
			if scenario == "key_collision" {
				for _, key := range []string{"Request", "request ", "request\x00", strings.Repeat("é", 1000), "e\u0301"} {
					next := r
					next.Key = key
					v, e := m.Execute(ctx, next, effect)
					if e != nil || v.Replayed {
						t.Fatal("full-byte collision identity", key, e)
					}
				}
				v, e := m.Execute(ctx, r, effect)
				if e != nil || !v.Replayed {
					t.Fatal("original collision replay", e)
				}
			}
			revoked.Store(true)
			if _, e := m.Execute(ctx, r, effect); !errors.Is(e, denied) {
				t.Fatal("revoked replay", e)
			}
		})
	}
}

func restrictedReceiptPool(t *testing.T, admin *sql.DB, database string) *sql.DB {
	t.Helper()
	ctx := context.Background()
	user := "ir_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]
	account := "'" + user + "'@'%'"
	schema, _ := mysqlstore.Identifier(database)
	if _, e := admin.ExecContext(ctx, "CREATE USER "+account+" IDENTIFIED BY 'isolated-receipt-only'"); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := admin.Exec("DROP USER " + account); e != nil {
			t.Error(e)
		}
	})
	for _, q := range []string{"GRANT INSERT,SELECT(key_digest),UPDATE(key_digest) ON " + schema + ".ingestion_receipt_buckets TO " + account, "GRANT SELECT,INSERT ON " + schema + ".ingestion_request_receipts TO " + account, "GRANT INSERT ON " + schema + ".fixture_effects TO " + account} {
		if _, e := admin.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	db, e := mysqlstore.Open(ctx, receiptConfig(database, user, "isolated-receipt-only"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	})
	for _, q := range []string{"DELETE FROM ingestion_request_receipts", "UPDATE ingestion_request_receipts SET response_payload='{}'", "DELETE FROM ingestion_receipt_buckets", "CREATE TABLE forbidden_receipt (id INT)"} {
		if _, e := db.ExecContext(ctx, q); e == nil {
			t.Fatal("receipt privilege bypass", q)
		}
	}
	return db
}
