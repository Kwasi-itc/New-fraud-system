package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

// Test-only plaintext fixture transport consumes the server's real COMMIT OK
// before dropping the connection. It does not fabricate a successful commit.
type receiptCommitLoss struct {
	net.Conn
	armed    atomic.Bool
	observed *atomic.Bool
}

func (c *receiptCommitLoss) Write(p []byte) (int, error) {
	if len(p) >= 5 && p[4] == 3 && strings.EqualFold(string(p[5:]), "COMMIT") {
		c.armed.Store(true)
	}
	return c.Conn.Write(p)
}
func (c *receiptCommitLoss) Read(p []byte) (int, error) {
	if !c.armed.Swap(false) {
		return c.Conn.Read(p)
	}
	var header [4]byte
	if _, e := io.ReadFull(c.Conn, header[:]); e != nil {
		return 0, e
	}
	n := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if n < 1 || n > 1024 {
		return 0, fmt.Errorf("invalid commit packet size %d", n)
	}
	body := make([]byte, n)
	if _, e := io.ReadFull(c.Conn, body); e != nil {
		return 0, e
	}
	if body[0] != 0 {
		return 0, errors.New("server did not acknowledge commit")
	}
	c.observed.Store(true)
	if e := c.Conn.Close(); e != nil {
		return 0, e
	}
	return 0, io.ErrUnexpectedEOF
}

func TestReceiptLostCommit(t *testing.T) {
	db, database := receiptFixture(t)
	installEffects(t, db, database)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var observed atomic.Bool
	network := "ingestion_receipt_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	driver.RegisterDialContext(network, func(ctx context.Context, address string) (net.Conn, error) {
		c, e := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if e != nil {
			return nil, e
		}
		return &receiptCommitLoss{Conn: c, observed: &observed}, nil
	})
	defer driver.DeregisterDialContext(network)
	cfg := driver.NewConfig()
	cfg.Net = network
	cfg.Addr = os.Getenv("INGESTION_MYSQL_TEST_ADDRESS")
	cfg.DBName = database
	cfg.User = "root"
	cfg.Passwd = "isolated-test-root-only"
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 5 * time.Second
	cfg.WriteTimeout = 5 * time.Second
	connector, e := driver.NewConnector(cfg)
	if e != nil {
		t.Fatal(e)
	}
	fault := sql.OpenDB(connector)
	defer fault.Close()
	tenant := uuid.New()
	r := ReceiptRequest{TenantID: tenant, TableID: uuid.New(), Key: "lost-commit", Mode: "create", Kind: "single", Hash: sha256.Sum256([]byte("command"))}
	makeManager := func(pool *sql.DB) *ReceiptManager {
		m, e := NewReceiptManager(pool, database, tenant, ReceiptLimits{1024, 1024, 8}, func(context.Context, ReceiptRequest) error { return nil }, func(context.Context, *sql.Tx, ReceiptRequest) error { return nil })
		if e != nil {
			t.Fatal(e)
		}
		return m
	}
	calls := 0
	effect := func(ctx context.Context, tx *sql.Tx) (json.RawMessage, error) {
		calls++
		store, err := NewEffectStore(tx, database, tenant, r.TableID, "records", 8192)
		if err != nil {
			return nil, err
		}
		audit, event := effectRows(tenant)
		if err := store.RecordIngestion(ctx, audit, event); err != nil {
			return nil, err
		}
		if _, e := tx.ExecContext(ctx, "INSERT INTO fixture_effects VALUES (?)", mysqlstore.UUID(uuid.New())); e != nil {
			return nil, e
		}
		return json.RawMessage(`{"action":"created","value":"é"}`), nil
	}
	v, e := makeManager(fault).Execute(ctx, r, effect)
	var ambiguous *mysqlstore.CommitError
	if !observed.Load() || !errors.As(e, &ambiguous) || len(v.Payload) != 0 {
		t.Fatal("commit acknowledgement fault", v, e)
	}
	v, e = makeManager(db).Execute(ctx, r, effect)
	if e != nil || !v.Replayed || calls != 1 || string(v.Payload) != `{"action":"created","value":"é"}` {
		t.Fatal("commit recovery", v, e, calls)
	}
	for _, object := range []string{"ingestion_audit", "ingestion_outbox_events"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+object).Scan(&count); err != nil || count != 1 {
			t.Fatal("lost-commit duplicated effects", object, count, err)
		}
	}
	var count int
	if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fixture_effects").Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicated effects", count, e)
	}
}
