package mysqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

func lockFixture(t *testing.T, db *sql.DB) string {
	t.Helper()
	name, err := Identifier(fmt.Sprintf("locks_%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE " + name + " (id INT PRIMARY KEY, n INT NOT NULL) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO " + name + " VALUES (1,0),(2,0)"); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestMySQLDeadlockRollback(t *testing.T) {
	db := integrationDB(t)
	name := lockFixture(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	results := make(chan error, 2)
	for first := 1; first <= 2; first++ {
		go func(first int) {
			results <- Mutation(ctx, db, func(tx *sql.Tx) error {
				if _, err := tx.ExecContext(ctx, "UPDATE "+name+" SET n=n+1 WHERE id=?", first); err != nil {
					return err
				}
				ready <- struct{}{}
				select {
				case <-start:
				case <-ctx.Done():
					return ctx.Err()
				}
				_, err := tx.ExecContext(ctx, "UPDATE "+name+" SET n=n+1 WHERE id=?", 3-first)
				return err
			})
		}(first)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("lock barrier: ", ctx.Err())
		}
	}
	close(start)
	winners, victims := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			winners++
		} else if Classify(err) == Deadlock {
			victims++
		} else {
			t.Fatalf("unexpected failure: %v", err)
		}
	}
	if winners != 1 || victims != 1 {
		t.Fatalf("winners=%d victims=%d", winners, victims)
	}
	var total int
	if err := db.QueryRowContext(ctx, "SELECT SUM(n) FROM "+name).Scan(&total); err != nil || total != 2 {
		t.Fatalf("partial victim writes: sum=%d err=%v", total, err)
	}
}

func TestMySQLLockTimeoutRollsBackEarlierWrites(t *testing.T) {
	db := integrationDB(t)
	name := lockFixture(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	if _, err := holder.ExecContext(ctx, "UPDATE "+name+" SET n=n+1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	// Separate pool: test-only session setting cannot leak into normal callers.
	c := testConfig()
	c.Address = testAddress(t)
	c.Password = testPassword()
	c.AllowInsecureTransport = true
	c.MaxOpen, c.MaxIdle = 1, 1
	waiter, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer waiter.Close()
	if _, err := waiter.ExecContext(ctx, "SET SESSION innodb_lock_wait_timeout=1"); err != nil {
		t.Fatal(err)
	}
	err = Mutation(ctx, waiter, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE "+name+" SET n=10 WHERE id=2"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE "+name+" SET n=10 WHERE id=1")
		return err
	})
	if Classify(err) != LockTimeout {
		t.Fatalf("expected lock timeout: %v", err)
	}
	if err := holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := db.QueryRowContext(ctx, "SELECT SUM(n) FROM "+name).Scan(&total); err != nil || total != 0 {
		t.Fatalf("timeout retained earlier mutation: %d %v", total, err)
	}
	if err := Mutation(ctx, waiter, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE "+name+" SET n=1 WHERE id=2")
		return err
	}); err != nil {
		t.Fatal("pool unusable after timeout: ", err)
	}
}
