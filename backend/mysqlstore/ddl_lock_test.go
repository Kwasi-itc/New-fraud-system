package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestDDLLockCleanupClassification(t *testing.T) {
	deadlock := &mysql.MySQLError{Number: 1213}
	for _, err := range []error{
		&DDLLockCleanupError{Cause: deadlock},
		errors.Join(deadlock, &DDLLockCleanupError{Cause: errors.New("release unconfirmed")}),
	} {
		if Classify(err) != Other || !errors.Is(err, deadlock) {
			t.Fatal("uncertain DDL cleanup classified as a plain retryable transaction error")
		}
	}
}

func TestDDLLockValidation(t *testing.T) {
	callback := func(*sql.Conn) error { t.Fatal("invalid input reached callback"); return nil }
	if err := WithDDLLock(context.Background(), nil, "model_ddl_test", callback); err == nil {
		t.Fatal("missing pool accepted")
	}
	if err := WithDDLLock(context.Background(), &sql.DB{}, "model_ddl_test", nil); err == nil {
		t.Fatal("missing callback accepted")
	}
	for _, name := range []string{"", "untrusted;sql", strings.Repeat("a", 65), "UPPER", "tenant.1"} {
		if err := WithDDLLock(context.Background(), &sql.DB{}, name, callback); err == nil {
			t.Fatalf("invalid lock name accepted: %q", name)
		}
	}
}

func TestMySQLDDLLockContention(t *testing.T) {
	db := integrationDB(t)
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	name := fmt.Sprintf("model_ddl_%d", time.Now().UnixNano())
	table, _ := Identifier(name)
	var firstID int64
	err := WithDDLLock(ctx, db, name, func(conn *sql.Conn) error {
		var owner int64
		if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID(), IS_USED_LOCK(?)", name).Scan(&firstID, &owner); err != nil {
			return err
		}
		if firstID != owner {
			return errors.New("callback is not the lock owner")
		}
		contender := func() error {
			return WithDDLLock(ctx, db, name, func(*sql.Conn) error {
				t.Error("competing executor entered the callback")
				return nil
			})
		}
		if err := contender(); !errors.Is(err, ErrDDLLockBusy) {
			return fmt.Errorf("before DDL: %w", err)
		}
		if err := WithDDLLock(ctx, db, name+"_other", func(other *sql.Conn) error {
			var one int
			return other.QueryRowContext(ctx, "SELECT 1").Scan(&one)
		}); err != nil {
			return fmt.Errorf("unrelated lock blocked: %w", err)
		}
		if _, err := conn.ExecContext(ctx, "CREATE TABLE "+table+" (id INT PRIMARY KEY) ENGINE=InnoDB"); err != nil {
			return err
		}
		if err := contender(); !errors.Is(err, ErrDDLLockBusy) {
			return fmt.Errorf("after implicit DDL commit: %w", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := WithDDLLock(ctx, db, name, func(conn *sql.Conn) error {
		var id, owner int64
		if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID(), IS_USED_LOCK(?)", name).Scan(&id, &owner); err != nil {
			return err
		}
		if id == firstID || id != owner {
			return errors.New("lock connection was reused or ownership lost")
		}
		var count int
		return conn.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	if stats := db.Stats(); stats.InUse != 0 {
		t.Fatalf("connection lease leaked: %+v", stats)
	}
}

func TestMySQLDDLLockCleanup(t *testing.T) {
	db := integrationDB(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	sentinel := errors.New("DDL callback failure")
	for _, mode := range []string{"error", "panic", "cancel", "release_failure", "interrupted_query"} {
		t.Run(mode, func(t *testing.T) {
			name := fmt.Sprintf("model_ddl_%d", time.Now().UnixNano())
			table, _ := Identifier(name)
			request, cancel := context.WithCancel(ctx)
			defer cancel()
			var originalID int64
			var result error
			var panicked any
			func() {
				defer func() { panicked = recover() }()
				result = WithDDLLock(request, db, name, func(conn *sql.Conn) error {
					if err := conn.QueryRowContext(request, "SELECT CONNECTION_ID()").Scan(&originalID); err != nil {
						return err
					}
					if _, err := conn.ExecContext(request, "CREATE TABLE "+table+" (id INT PRIMARY KEY) ENGINE=InnoDB"); err != nil {
						return err
					}
					switch mode {
					case "error":
						return sentinel
					case "panic":
						panic(sentinel)
					case "cancel":
						cancel()
						return nil
					case "release_failure":
						// Deliberate contract violation simulates loss of lock ownership.
						var released int
						if err := conn.QueryRowContext(request, "SELECT RELEASE_LOCK(?)", name).Scan(&released); err != nil {
							return err
						}
						return sentinel
					case "interrupted_query":
						queryCtx, done := context.WithTimeout(request, 100*time.Millisecond)
						defer done()
						_, err := conn.ExecContext(queryCtx, "SELECT SLEEP(2)")
						return err
					}
					return nil
				})
			}()
			if mode == "panic" {
				if panicked != sentinel {
					t.Fatalf("panic changed: %v", panicked)
				}
			} else {
				if panicked != nil {
					t.Fatalf("unexpected panic: %v", panicked)
				}
				want := sentinel
				if mode == "cancel" {
					want = context.Canceled
				}
				if mode == "interrupted_query" {
					want = context.DeadlineExceeded
				}
				if !errors.Is(result, want) {
					t.Fatalf("lost original error: %v", result)
				}
			}
			if mode == "release_failure" {
				var cleanup *DDLLockCleanupError
				if !errors.As(result, &cleanup) {
					t.Fatalf("unconfirmed release was hidden: %v", result)
				}
			}
			// Server-side termination after socket cancellation may lag briefly.
			// Observe lock ownership rather than assuming that a sleep released it.
			verify, done := context.WithTimeout(ctx, 5*time.Second)
			defer done()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				var free int
				if err := db.QueryRowContext(verify, "SELECT IS_FREE_LOCK(?)", name).Scan(&free); err != nil {
					t.Fatal(err)
				}
				if free == 1 {
					break
				}
				select {
				case <-ticker.C:
				case <-verify.Done():
					t.Fatal("lock remained held after cleanup")
				}
			}
			if err := WithDDLLock(verify, db, name, func(conn *sql.Conn) error {
				var id int64
				if err := conn.QueryRowContext(verify, "SELECT CONNECTION_ID()").Scan(&id); err != nil {
					return err
				}
				if id == originalID {
					return errors.New("DDL session returned to pool")
				}
				// Error/panic/cancellation is not a promise that DDL rolled back.
				var count int
				return conn.QueryRowContext(verify, "SELECT count(*) FROM "+table).Scan(&count)
			}); err != nil {
				t.Fatal(err)
			}
			if stats := db.Stats(); stats.InUse != 0 {
				t.Fatalf("connection lease leaked: %+v", stats)
			}
		})
	}
}
