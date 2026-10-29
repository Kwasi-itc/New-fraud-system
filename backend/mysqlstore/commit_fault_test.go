package mysqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// ackLossConn is a test-only plaintext protocol fault. It forwards a command,
// consumes the real server OK packet, then hides that acknowledgement from the
// driver. This exercises real command completion with a lost acknowledgement,
// not merely a mock error, for COMMIT, ROLLBACK and DDL tests.
type ackLossConn struct {
	net.Conn
	drop     atomic.Bool
	observed *atomic.Bool
	command  string
}

func (c *ackLossConn) Write(b []byte) (int, error) {
	if len(b) >= 5 && b[4] == 3 && strings.EqualFold(string(b[5:]), c.command) {
		c.drop.Store(true)
	}
	return c.Conn.Write(b)
}

func (c *ackLossConn) Read(b []byte) (int, error) {
	if !c.drop.Swap(false) {
		return c.Conn.Read(b)
	}
	var header [4]byte
	if _, err := io.ReadFull(c.Conn, header[:]); err != nil {
		return 0, err
	}
	n := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if n < 1 || n > 1024 {
		return 0, fmt.Errorf("unexpected %s response length %d", c.command, n)
	}
	packet := make([]byte, n)
	if _, err := io.ReadFull(c.Conn, packet); err != nil {
		return 0, err
	}
	if packet[0] != 0 {
		return 0, fmt.Errorf("%s did not return OK", c.command)
	}
	c.observed.Store(true)
	_ = c.Conn.Close()
	return 0, io.ErrUnexpectedEOF
}

func TestMySQLLostCommitAcknowledgement(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name, _ := Identifier(fmt.Sprintf("receipt_%d", time.Now().UnixNano()))
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+name+" (operation_id BINARY(16) PRIMARY KEY, result INT NOT NULL) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	var observed atomic.Bool
	network := fmt.Sprintf("commitfault%d", time.Now().UnixNano())
	mysql.RegisterDialContext(network, func(ctx context.Context, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, err
		}
		return &ackLossConn{Conn: conn, observed: &observed, command: "COMMIT"}, nil
	})
	defer mysql.DeregisterDialContext(network)
	c := testConfig()
	c.Address = testAddress(t)
	c.Password = testPassword()
	c.AllowInsecureTransport = true
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
	operation := UUID{15: 42}
	calls := 0
	metrics := &TransactionMetrics{}
	err = metrics.Mutation(ctx, faultDB, func(tx *sql.Tx) error {
		calls++
		_, err := tx.ExecContext(ctx, "INSERT INTO "+name+" VALUES(?,?)", operation, 123)
		return err
	})
	var commit *CommitError
	if !errors.As(err, &commit) || !observed.Load() || calls != 1 || Classify(err) != Other {
		t.Fatalf("outcome not preserved: err=%v observed=%v calls=%d", err, observed.Load(), calls)
	}
	if snapshot := metrics.Snapshot(); snapshot.Uncertain != 1 || snapshot.Completed != 0 || snapshot.Active != 0 {
		t.Fatalf("ambiguous commit miscounted: %+v", snapshot)
	}
	// Reconcile on a separate healthy connection by stable operation identity.
	var result int
	if err := db.QueryRowContext(ctx, "SELECT result FROM "+name+" WHERE operation_id=?", operation).Scan(&result); err != nil || result != 123 {
		t.Fatalf("committed receipt missing: %d %v", result, err)
	}
}
