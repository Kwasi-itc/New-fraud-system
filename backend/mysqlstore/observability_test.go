package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
)

func TestTransactionMetrics(t *testing.T) {
	m := &TransactionMetrics{}
	failure := errors.New("callback failed")
	for _, outcome := range []error{nil, failure, &CommitError{Cause: failure}, &RollbackError{Cause: failure}} {
		if err := m.measure(func() error { return outcome }); !errors.Is(err, outcome) {
			t.Fatal(err)
		}
	}
	func() {
		defer func() {
			if recover() != "panic" {
				t.Error("panic swallowed")
			}
		}()
		_ = m.measure(func() error { panic("panic") })
	}()
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() { defer wg.Done(); _ = m.measure(func() error { _ = m.Snapshot(); return nil }) }()
	}
	wg.Wait()
	s := m.Snapshot()
	if s.Started != 25 || s.Completed != 21 || s.Failed != 2 || s.Uncertain != 2 || s.Active != 0 {
		t.Fatalf("metrics: %+v", s)
	}
	if _, err := ObservePool(nil); err == nil {
		t.Fatal("nil pool accepted")
	}
}

func TestMySQLTransactionMetrics(t *testing.T) {
	db := integrationDB(t)
	m := &TransactionMetrics{}
	if err := m.Mutation(context.Background(), db, func(tx *sql.Tx) error { var one int; return tx.QueryRow("SELECT 1").Scan(&one) }); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("callback failure")
	if err := m.ReadSnapshot(context.Background(), db, func(*sql.Tx) error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Mutation(ctx, db, func(*sql.Tx) error { t.Error("cancelled callback executed"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s := m.Snapshot()
	if s.Started != 3 || s.Completed != 1 || s.Failed != 2 || s.Active != 0 || s.Uncertain != 0 {
		t.Fatalf("metrics: %+v", s)
	}
	p, err := ObservePool(db)
	if err != nil || p.Maximum != 8 || p.InUse != 0 {
		t.Fatalf("pool: %+v %v", p, err)
	}
}
