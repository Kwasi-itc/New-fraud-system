package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestRollbackErrorClassification(t *testing.T) {
	cause := errors.New("cleanup unavailable")
	for _, number := range []uint16{1205, 1213} {
		original := &mysql.MySQLError{Number: number, Message: "callback failed"}
		err := errors.Join(original, &RollbackError{Cause: cause})
		if Classify(err) != Other || !errors.Is(err, original) || !errors.Is(err, cause) {
			t.Fatalf("lost cleanup ambiguity or original cause: %v", err)
		}
	}
}

func TestMySQLLostRollbackAcknowledgement(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name, _ := Identifier(fmt.Sprintf("rollback_ack_%d", time.Now().UnixNano()))
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+name+" (id INT PRIMARY KEY) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	var observed atomic.Bool
	network := fmt.Sprintf("rollbackfault%d", time.Now().UnixNano())
	mysql.RegisterDialContext(network, func(ctx context.Context, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, err
		}
		return &ackLossConn{Conn: conn, observed: &observed, command: "ROLLBACK"}, nil
	})
	defer mysql.DeregisterDialContext(network)
	c := testConfig()
	c.Address, c.Password, c.AllowInsecureTransport = testAddress(t), testPassword(), true
	cfg, err := c.connector()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Net = network
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	faultDB := sql.OpenDB(connector)
	defer faultDB.Close()
	faultDB.SetMaxOpenConns(1)
	faultDB.SetMaxIdleConns(1)
	// The callback cause is injected independently of the real rollback packet
	// loss. Real deadlocks/lock timeouts are covered by locking_test.go.
	original := &mysql.MySQLError{Number: 1205, Message: "injected callback timeout"}
	calls := 0
	var firstID, nextID int64
	err = Mutation(ctx, faultDB, func(tx *sql.Tx) error {
		calls++
		if err := tx.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&firstID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+name+" VALUES (1)"); err != nil {
			return err
		}
		return original
	})
	var rollback *RollbackError
	if !errors.As(err, &rollback) || !errors.Is(err, original) || Classify(err) != Other || !observed.Load() || calls != 1 {
		t.Fatalf("cleanup outcome lost: err=%v observed=%v calls=%d", err, observed.Load(), calls)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+name).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback effects: count=%d err=%v", count, err)
	}
	if err := Mutation(ctx, faultDB, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&nextID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO "+name+" VALUES (2)")
		return err
	}); err != nil {
		t.Fatalf("replacement connection failed: %v", err)
	}
	if firstID == nextID || faultDB.Stats().InUse != 0 {
		t.Fatal("failed session reused or lease leaked")
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+name+" WHERE id = 2").Scan(&count); err != nil || count != 1 {
		t.Fatalf("replacement transaction not committed: count=%d err=%v", count, err)
	}
}
