package mysqlstore

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func testConfig() Config {
	return Config{Address: "mysql:3306", Database: "migration_test", User: "migration_test",
		MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute}
}

func TestConfiguration(t *testing.T) {
	c := testConfig()
	if _, err := c.connector(); err == nil {
		t.Fatal("plaintext allowed by default")
	}
	c.TLS = &tls.Config{InsecureSkipVerify: true}
	if _, err := c.connector(); err == nil {
		t.Fatal("unverified TLS allowed")
	}
	c.TLS = &tls.Config{ServerName: "db.example"}
	cfg, err := c.connector()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLS.MinVersion < tls.VersionTLS12 || cfg.MultiStatements || cfg.InterpolateParams || cfg.ClientFoundRows || cfg.AllowAllFiles {
		t.Fatal("unsafe driver configuration")
	}
	if c.TLS.MinVersion != 0 {
		t.Fatal("caller TLS config mutated")
	}
	c.MaxIdle = c.MaxOpen + 1
	if _, err := c.connector(); err == nil {
		t.Fatal("unbounded pool accepted")
	}
	for _, name := range []string{"fraud;DROP", "tenant.other", "a`", strings.Repeat("a", 65), ""} {
		if _, err := Identifier(name); err == nil {
			t.Fatalf("accepted identifier %q", name)
		}
	}
}

func TestUUIDAndErrors(t *testing.T) {
	u := UUID{0: 1, 15: 255}
	v, err := u.Value()
	if err != nil {
		t.Fatal(err)
	}
	var got UUID
	if err := got.Scan(v); err != nil || got != u {
		t.Fatalf("UUID roundtrip: %v", err)
	}
	v.([]byte)[0] = 9
	if got != u {
		t.Fatal("scanner retained driver buffer")
	}
	for _, v := range []any{nil, "00000000-0000-0000-0000-000000000000", []byte{1}} {
		if err := got.Scan(v); err == nil {
			t.Fatal("invalid UUID accepted")
		}
	}
	e := fmt.Errorf("wrapped: %w", &mysql.MySQLError{Number: 1213})
	if Classify(e) != Deadlock || Classify(&CommitError{Cause: e}) != Other {
		t.Fatal("unsafe commit classification")
	}
}

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	c := testConfig()
	c.Address = testAddress(t)
	c.Password = testPassword()
	c.AllowInsecureTransport = true
	db, err := Open(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testPassword() string { return os.Getenv("MYSQLSTORE_TEST_PASSWORD") }

func testAddress(t *testing.T) string {
	t.Helper()
	address := os.Getenv("MYSQLSTORE_TEST_ADDRESS")
	if address == "" {
		if os.Getenv("MYSQLSTORE_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("required MySQL integration target is missing")
		}
		t.Skip("real MySQL unavailable: run compose.test.yaml; this skip does not establish parity")
	}
	return address
}

func TestMySQLInterruptedQuery(t *testing.T) {
	db := integrationDB(t)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name, _ := Identifier(fmt.Sprintf("cancel_%d", time.Now().UnixNano()))
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+name+" (id INT PRIMARY KEY) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	err := Mutation(ctx, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+name+" VALUES(1)"); err != nil {
			return err
		}
		queryCtx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
		defer stop()
		_, err := tx.ExecContext(queryCtx, "SELECT SLEEP(2)")
		return err
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected query deadline, got %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+name).Scan(&count); err != nil || count != 0 {
		t.Fatalf("interrupted transaction survived: count=%d error=%v", count, err)
	}
	var zone, isolation string
	if err := db.QueryRowContext(ctx, "SELECT @@session.time_zone, @@session.transaction_isolation").Scan(&zone, &isolation); err != nil {
		t.Fatal(err)
	}
	if zone != "+00:00" || isolation != "READ-COMMITTED" {
		t.Fatal("replacement connection lost session configuration")
	}
}

func TestMySQLTransactions(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	var version, zone, isolation string
	if err := db.QueryRow("SELECT VERSION(), @@session.time_zone, @@session.transaction_isolation").Scan(&version, &zone, &isolation); err != nil {
		t.Fatal(err)
	}
	t.Logf("server=%s timezone=%s isolation=%s", version, zone, isolation)
	if !strings.HasPrefix(version, "8.4.") || zone != "+00:00" || isolation != "READ-COMMITTED" {
		t.Fatal("unexpected session baseline")
	}
	// Fixed fixture database from the disposable stack; unique tables avoid
	// dropping or truncating any preexisting object, even on a mistaken target.
	name := fmt.Sprintf("fixture_%d", time.Now().UnixNano())
	q, _ := Identifier(name)
	child, _ := Identifier(name + "_child")
	if _, err := db.Exec("CREATE TABLE " + q + " (tenant BINARY(16) NOT NULL, id BINARY(16) NOT NULL, n INT NOT NULL CHECK(n>=0), PRIMARY KEY(tenant,id)) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE " + child + " (tenant BINARY(16) NOT NULL, id BINARY(16) NOT NULL, FOREIGN KEY(tenant,id) REFERENCES " + q + "(tenant,id)) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	a, b, id := UUID{15: 1}, UUID{15: 2}, UUID{15: 3}
	insert := "INSERT INTO " + q + "(tenant,id,n) VALUES(?,?,?)"
	sentinel := errors.New("injected failure")
	err := Mutation(ctx, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, insert, a, id, 0); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + q).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial commit: %d %v", count, err)
	}
	if err := Mutation(ctx, db, func(tx *sql.Tx) error { _, e := tx.ExecContext(ctx, insert, a, id, 0); return e }); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO "+child+" VALUES(?,?)", b, id); Classify(err) != ForeignKey {
		t.Fatalf("cross-tenant reference accepted: %v", err)
	}
	if _, err := db.Exec(insert, a, id, 0); Classify(err) != Duplicate {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := db.Exec("UPDATE " + q + " SET n=-1"); Classify(err) != CheckViolation {
		t.Fatalf("check: %v", err)
	}
	result, err := db.Exec("UPDATE "+q+" SET n=0 WHERE tenant=? AND id=?", a, id)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := result.RowsAffected(); n != 0 {
		t.Fatal("no-op must report changed rows, not matches")
	}
	// Serialize only this record's read-modify-write; no lost updates.
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			failures <- Mutation(ctx, db, func(tx *sql.Tx) error {
				var n int
				if err := tx.QueryRowContext(ctx, "SELECT n FROM "+q+" WHERE tenant=? AND id=? FOR UPDATE", a, id).Scan(&n); err != nil {
					return err
				}
				_, err := tx.ExecContext(ctx, "UPDATE "+q+" SET n=? WHERE tenant=? AND id=?", n+1, a, id)
				return err
			})
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow("SELECT n FROM "+q+" WHERE tenant=? AND id=?", a, id).Scan(&count); err != nil || count != 8 {
		t.Fatalf("lost update: %d %v", count, err)
	}
	if err := Snapshot(ctx, db, func(tx *sql.Tx) error {
		var first, second int
		if err := tx.QueryRow("SELECT n FROM " + q).Scan(&first); err != nil {
			return err
		}
		if _, err := db.Exec("UPDATE " + q + " SET n=n+1"); err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT n FROM " + q).Scan(&second); err != nil {
			return err
		}
		if first != second {
			return errors.New("snapshot changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := Snapshot(ctx, db, func(tx *sql.Tx) error { _, err := tx.Exec("UPDATE " + q + " SET n=n+1"); return err }); err == nil {
		t.Fatal("read-only transaction wrote")
	}
	canceled, cancel := context.WithCancel(ctx)
	err = Mutation(canceled, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(canceled, insert, b, id, 0); err != nil {
			return err
		}
		cancel()
		return nil
	})
	cancel()
	if err == nil {
		t.Fatal("canceled mutation succeeded")
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM "+q+" WHERE tenant=?", b).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cancellation committed: %v", err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("callback panic was swallowed")
			}
		}()
		_ = Mutation(ctx, db, func(tx *sql.Tx) error {
			if _, err := tx.Exec(insert, b, id, 0); err != nil {
				t.Fatal(err)
			}
			panic("injected")
		})
	}()
	if err := db.QueryRow("SELECT COUNT(*) FROM "+q+" WHERE tenant=?", b).Scan(&count); err != nil || count != 0 {
		t.Fatalf("panic committed: %v", err)
	}
}
