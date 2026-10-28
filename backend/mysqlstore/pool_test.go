package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMySQLPoolSaturationCancellation(t *testing.T) {
	db := integrationDB(t)
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	first, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	baseline := db.Stats()
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	const callers = 16
	results := make(chan error, callers)
	var callbacks atomic.Int32
	for i := 0; i < callers; i++ {
		go func() {
			results <- Mutation(waitCtx, db, func(*sql.Tx) error {
				callbacks.Add(1)
				return nil
			})
		}()
	}
	// Wait for the pool's observable acquisition barrier, not a scheduling sleep.
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for db.Stats().WaitCount-baseline.WaitCount < callers {
		select {
		case <-ctx.Done():
			t.Fatal("callers did not reach the pool acquisition barrier")
		case <-ticker.C:
		}
	}
	if stats := db.Stats(); stats.OpenConnections != 2 || stats.InUse != 2 {
		t.Fatalf("pool exceeded or lost its connection budget: %+v", stats)
	}
	cancel()
	for i := 0; i < callers; i++ {
		select {
		case err := <-results:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("waiting mutation returned %v", err)
			}
		case <-ctx.Done():
			t.Fatal("waiting mutations failed to cancel")
		}
	}
	if callbacks.Load() != 0 {
		t.Fatal("callback ran without acquiring a connection")
	}
	if stats := db.Stats(); stats.WaitDuration <= baseline.WaitDuration || stats.InUse != 2 {
		t.Fatalf("canceled wait metrics or held connections changed unexpectedly: %+v", stats)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Mutation(ctx, db, func(tx *sql.Tx) error {
		var one int
		return tx.QueryRowContext(ctx, "SELECT 1").Scan(&one)
	}); err != nil {
		t.Fatalf("pool unusable after cancellation: %v", err)
	}
	if stats := db.Stats(); stats.InUse != 0 || stats.OpenConnections > 2 {
		t.Fatalf("connection leak after recovery: %+v", stats)
	}
}

func TestMySQLSessionReuseAndReplacement(t *testing.T) {
	db := integrationDB(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	check := func() int64 {
		t.Helper()
		var id int64
		var zone, isolation, mode string
		var readOnly int
		var autocommit, foreignKeys, uniqueKeys int
		var completion string
		if err := db.QueryRowContext(ctx, "SELECT @@session.autocommit, @@session.completion_type, @@session.foreign_key_checks, @@session.unique_checks").Scan(&autocommit, &completion, &foreignKeys, &uniqueKeys); err != nil {
			t.Fatal(err)
		}
		if autocommit != 1 || completion != "NO_CHAIN" || foreignKeys != 1 || uniqueKeys != 1 {
			t.Fatalf("unsafe integrity/transaction settings: %d %s %d %d", autocommit, completion, foreignKeys, uniqueKeys)
		}
		if err := db.QueryRowContext(ctx, "SELECT CONNECTION_ID(), @@session.time_zone, @@session.transaction_isolation, @@session.transaction_read_only, @@session.sql_mode").Scan(&id, &zone, &isolation, &readOnly, &mode); err != nil {
			t.Fatal(err)
		}
		if zone != "+00:00" || isolation != "READ-COMMITTED" || readOnly != 0 {
			t.Fatalf("session leaked transaction settings: %s %s %d", zone, isolation, readOnly)
		}
		modes := make(map[string]bool)
		for _, m := range strings.Split(mode, ",") {
			modes[m] = true
		}
		for _, required := range []string{"STRICT_TRANS_TABLES", "ERROR_FOR_DIVISION_BY_ZERO", "NO_ZERO_DATE", "NO_ZERO_IN_DATE", "NO_ENGINE_SUBSTITUTION", "ONLY_FULL_GROUP_BY"} {
			if !modes[required] {
				t.Fatalf("session missing required mode %s: %s", required, mode)
			}
		}
		return id
	}
	initial := check()
	sentinel := errors.New("snapshot callback failure")
	for _, failure := range []error{nil, sentinel} {
		err := Snapshot(ctx, db, func(tx *sql.Tx) error {
			var one int
			if err := tx.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
				return err
			}
			return failure
		})
		if !errors.Is(err, failure) {
			t.Fatalf("snapshot result: %v", err)
		}
		if reused := check(); reused != initial {
			t.Fatal("did not exercise reuse of the same physical connection")
		}
	}
	// Evict the idle connection without touching any other client's sessions.
	db.SetMaxIdleConns(0)
	db.SetMaxIdleConns(1)
	if replacement := check(); replacement == initial {
		t.Fatal("did not exercise replacement of the physical connection")
	}
}
