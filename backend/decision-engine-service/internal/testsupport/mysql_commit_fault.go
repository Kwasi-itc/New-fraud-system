package testsupport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// DecisionCommitFaultPool is a private-fixture-only real MySQL connector. Arm
// after provider IO to make the NEXT successful SQL commit return a lost-ack
// error AFTER the server committed. It is not an application fault switch and
// never accepts a DSN or a database outside our generated component namespace.
func DecisionCommitFaultPool(t *testing.T, database string) (*sql.DB, func(func())) {
	t.Helper()
	if !strings.HasPrefix(database, "decision_component_fixture_") || len(database) != len("decision_component_fixture_")+32 {
		t.Fatal("generated component database required")
	}
	for _, c := range strings.TrimPrefix(database, "decision_component_fixture_") {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatal("invalid fixture database")
		}
	}
	config := mysqldriver.NewConfig()
	config.User = "root"
	config.Passwd = "isolated-test-root-only"
	config.Net = "tcp"
	config.Addr = "mysql:3306"
	config.DBName = database
	config.ParseTime = true
	config.Loc = time.UTC
	config.Timeout = 5 * time.Second
	config.ReadTimeout = 10 * time.Second
	config.WriteTimeout = 10 * time.Second
	inner, err := mysqldriver.NewConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	fault := &commitFaultConnector{inner: inner}
	db := sql.OpenDB(fault)
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	t.Cleanup(func() { db.Close() })
	return db, func(after func()) { fault.next.Store(&after) }
}

type commitFaultConnector struct {
	inner driver.Connector
	next  atomic.Pointer[func()]
}

func (c *commitFaultConnector) Driver() driver.Driver { return c.inner.Driver() }
func (c *commitFaultConnector) Connect(ctx context.Context) (driver.Conn, error) {
	v, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &commitFaultConn{Conn: v, fault: c}, nil
}

type commitFaultConn struct {
	driver.Conn
	fault *commitFaultConnector
}

func (c *commitFaultConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return commitFaultTx{tx, c.fault}, nil
}
func (c *commitFaultConn) Ping(ctx context.Context) error { return c.Conn.(driver.Pinger).Ping(ctx) }
func (c *commitFaultConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}
func (c *commitFaultConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}
func (c *commitFaultConn) ResetSession(ctx context.Context) error {
	return c.Conn.(driver.SessionResetter).ResetSession(ctx)
}
func (c *commitFaultConn) IsValid() bool { return c.Conn.(driver.Validator).IsValid() }

type commitFaultTx struct {
	driver.Tx
	fault *commitFaultConnector
}

func (t commitFaultTx) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	if after := t.fault.next.Swap(nil); after != nil {
		if *after != nil {
			(*after)()
		}
		return errors.New("fixture: acknowledgement lost after real commit")
	}
	return nil
}
