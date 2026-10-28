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

func TestMySQLLostDDLAcknowledgement(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	name := fmt.Sprintf("ddl_ack_%d", time.Now().UnixNano())
	table, err := Identifier(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+table+" (id INT PRIMARY KEY) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	statement := "ALTER TABLE " + table + " ADD COLUMN revision INT NOT NULL DEFAULT 7"
	var observed atomic.Bool
	network := fmt.Sprintf("ddlfault%d", time.Now().UnixNano())
	mysql.RegisterDialContext(network, func(ctx context.Context, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, err
		}
		return &ackLossConn{Conn: conn, observed: &observed, command: statement}, nil
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
	calls := 0
	var failedSession int64
	var ddlError error
	err = WithDDLLock(ctx, faultDB, name, func(conn *sql.Conn) error {
		calls++
		if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&failedSession); err != nil {
			return err
		}
		_, ddlError = conn.ExecContext(ctx, statement)
		return ddlError
	})
	var cleanup *DDLLockCleanupError
	if err == nil || ddlError == nil || !errors.Is(err, ddlError) || !errors.As(err, &cleanup) || Classify(err) != Other || !observed.Load() || calls != 1 {
		t.Fatalf("DDL uncertainty lost: err=%v observed=%v calls=%d", err, observed.Load(), calls)
	}
	if faultDB.Stats().InUse != 0 {
		t.Fatal("failed DDL leaked its connection lease")
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var free int
		if err := db.QueryRowContext(ctx, "SELECT IS_FREE_LOCK(?)", name).Scan(&free); err != nil {
			t.Fatal(err)
		}
		if free == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
	// Reacquire ownership and inspect the postcondition before deciding to run
	// any DDL. Existing column name alone is not sufficient evidence of success.
	err = WithDDLLock(ctx, db, name, func(conn *sql.Conn) error {
		var recoverySession, owner int64
		if err := conn.QueryRowContext(ctx, "SELECT CONNECTION_ID(), IS_USED_LOCK(?)", name).Scan(&recoverySession, &owner); err != nil {
			return err
		}
		if recoverySession == failedSession || owner != recoverySession {
			return errors.New("recovery session does not own DDL lock")
		}
		var dataType, nullable string
		var defaultValue sql.NullString
		if err := conn.QueryRowContext(ctx, "SELECT COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME='revision'", name).Scan(&dataType, &nullable, &defaultValue); err != nil {
			return err
		}
		if dataType != "int" || nullable != "NO" || !defaultValue.Valid || defaultValue.String != "7" {
			return fmt.Errorf("unexpected DDL postcondition: %s %s %v", dataType, nullable, defaultValue)
		}
		// Desired shape exists: no blind replay of ADD COLUMN.
		return nil
	})
	if err != nil {
		t.Fatalf("shape reconciliation failed: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+table+" (id) VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	var revision int
	if err := db.QueryRowContext(ctx, "SELECT revision FROM "+table+" WHERE id=1").Scan(&revision); err != nil || revision != 7 {
		t.Fatalf("committed DDL default not usable: %d %v", revision, err)
	}
}
