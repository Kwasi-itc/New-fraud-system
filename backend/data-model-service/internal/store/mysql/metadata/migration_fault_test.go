package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

type migrationLostDDL struct {
	net.Conn
	statement string
	armed     atomic.Bool
	observed  *atomic.Bool
}

func (c *migrationLostDDL) Write(p []byte) (int, error) {
	if len(p) > 5 && p[4] == 3 && string(p[5:]) == c.statement {
		c.armed.Store(true)
	}
	return c.Conn.Write(p)
}

func (c *migrationLostDDL) Read(p []byte) (int, error) {
	if !c.armed.Swap(false) {
		return c.Conn.Read(p)
	}
	var header [4]byte
	if _, err := io.ReadFull(c.Conn, header[:]); err != nil {
		return 0, err
	}
	n := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if n < 1 || n > 1024 {
		return 0, errors.New("unexpected DDL acknowledgement packet")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(c.Conn, body); err != nil {
		return 0, err
	}
	if body[0] != 0 {
		return 0, errors.New("DDL not acknowledged by server")
	}
	c.observed.Store(true)
	if err := c.Conn.Close(); err != nil {
		return 0, err
	}
	return 0, io.ErrUnexpectedEOF
}

func TestMigrationLostDDLAcknowledgement(t *testing.T) {
	db, database := metadataDB(t) // Validates isolated fixture before fault credentials.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	schema, _ := mysqlstore.Identifier(database)
	statement := "CREATE TABLE " + schema + ".data_model_fault_probe (id INT PRIMARY KEY) ENGINE=InnoDB"
	if _, err := db.ExecContext(ctx, statement); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	after, err := migrations.Definition(ctx, conn, database, "TABLE", "data_model_fault_probe")
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE "+schema+".data_model_fault_probe"); err != nil {
		t.Fatal(err)
	}
	plan := migrations.Plan{Database: database, Steps: []migrations.Step{{ID: "create_fault_probe", Kind: "TABLE", Name: "data_model_fault_probe", SQL: statement, After: after}}}
	var observed atomic.Bool
	network := "data_model_ddl_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	driver.RegisterDialContext(network, func(ctx context.Context, address string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, err
		}
		return &migrationLostDDL{Conn: c, statement: statement, observed: &observed}, nil
	})
	defer driver.DeregisterDialContext(network)
	cfg := driver.NewConfig()
	cfg.Net = network
	cfg.Addr = "mysql:3306"
	cfg.DBName = database
	cfg.User = "root"
	cfg.Passwd = "isolated-test-root-only"
	cfg.ParseTime = true
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 5 * time.Second
	cfg.WriteTimeout = 5 * time.Second
	connector, err := driver.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	faultDB := sql.OpenDB(connector)
	defer faultDB.Close()
	fault, err := migrations.New(faultDB, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := fault.Apply(ctx); err == nil || !observed.Load() {
		t.Fatalf("observed=%v err=%v", observed.Load(), err)
	}
	healthy, err := migrations.New(db, plan)
	if err != nil {
		t.Fatal(err)
	}
	h, err := healthy.Status(ctx)
	if err != nil || len(h) != 1 || h[0].State != "dirty" {
		t.Fatalf("history=%v err=%v", h, err)
	}
	// CREATE without IF NOT EXISTS would fail if blindly replayed.
	if err := healthy.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	h, err = healthy.Status(ctx)
	if err != nil || len(h) != 1 || h[0].State != "applied" {
		t.Fatalf("recovered=%v err=%v", h, err)
	}
}
