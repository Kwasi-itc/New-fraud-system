package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestGuardedApplyRequiresGuardBeforeDatabaseAccess(t *testing.T) {
	if err := (*Runner)(nil).ApplyGuarded(context.Background(), nil); err == nil {
		t.Fatal("nil guard accepted")
	}
}

func TestMySQLGuardedApplyFencesEveryWriteAndRecovers(t *testing.T) {
	db := fixture(t)
	db.SetMaxOpenConns(1) // the guard cannot use a second pool connection
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for rejectAt := 0; rejectAt <= 4; rejectAt++ {
		t.Run(fmt.Sprint(rejectAt), func(t *testing.T) {
			name := fmt.Sprintf("guard_%d_%d", time.Now().UnixNano(), rejectAt)
			reference := name + "_ref"
			ddl := "CREATE TABLE `migration_test`.`" + reference + "` AS SELECT CONNECTION_ID() AS session_id"
			if _, err := db.ExecContext(ctx, ddl); err != nil {
				t.Fatal(err)
			}
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			definition, definitionErr := Definition(ctx, conn, "migration_test", "TABLE", reference)
			if err := errors.Join(definitionErr, conn.Close()); err != nil {
				t.Fatal(err)
			}
			plan := Plan{Database: "migration_test", Owner: name, Steps: []Step{{ID: "create_guarded", Kind: "TABLE", Name: name, SQL: strings.ReplaceAll(ddl, reference, name), After: strings.ReplaceAll(definition, reference, name)}}}
			runner, err := New(db, plan)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			var session uint64
			denied := errors.New("writer policy changed")
			guard := func(ctx context.Context, conn *sql.Conn) error {
				calls++
				var actual uint64
				if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&actual); err != nil {
					return err
				}
				if session == 0 {
					session = actual
				}
				if actual != session {
					return errors.New("guard changed SQL session")
				}
				if calls == rejectAt {
					return denied
				}
				return nil
			}
			err = runner.ApplyGuarded(ctx, guard)
			if rejectAt != 0 && !errors.Is(err, denied) || rejectAt == 0 && err != nil {
				t.Fatal(err)
			}
			var ledgerCount, objectCount int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='migration_test' AND table_name=?", plan.ledgerName()).Scan(&ledgerCount); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='migration_test' AND table_name=?", name).Scan(&objectCount); err != nil {
				t.Fatal(err)
			}
			wantLedger, wantObject := 1, 0
			if rejectAt == 1 {
				wantLedger = 0
			}
			if rejectAt == 0 || rejectAt == 4 {
				wantObject = 1
			}
			if ledgerCount != wantLedger || objectCount != wantObject {
				t.Fatalf("writes past rejection %d: ledger=%d object=%d", rejectAt, ledgerCount, objectCount)
			}
			history, err := runner.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if rejectAt == 1 || rejectAt == 2 {
				if len(history) != 0 {
					t.Fatal("intent written after rejection")
				}
			} else if len(history) != 1 || (rejectAt != 0 && history[0].State != "dirty") {
				t.Fatal("recovery evidence lost", history)
			}
			if wantObject == 1 {
				var actual uint64
				if err := db.QueryRowContext(ctx, "SELECT session_id FROM `migration_test`.`"+name+"`").Scan(&actual); err != nil || actual != session {
					t.Fatal("DDL not on guarded connection", actual, session, err)
				}
			}
			// Resume safely after each rejection, including after DDL but before
			// marking the intent applied. No destructive cleanup or force adopt.
			if err := runner.ApplyGuarded(ctx, func(context.Context, *sql.Conn) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if err := runner.Verify(ctx, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}
