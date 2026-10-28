package metadata_test

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

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

// This fault consumes a real server COMMIT OK before hiding it from the driver.
// Only the validated disposable fixture uses plaintext and installer credentials.
type deliveryLostCommitConn struct {
	net.Conn
	armed    atomic.Bool
	observed *atomic.Bool
}

func (c *deliveryLostCommitConn) Write(p []byte) (int, error) {
	if len(p) >= 5 && p[4] == 3 && strings.EqualFold(string(p[5:]), "COMMIT") {
		c.armed.Store(true)
	}
	return c.Conn.Write(p)
}
func (c *deliveryLostCommitConn) Read(p []byte) (int, error) {
	if !c.armed.Swap(false) {
		return c.Conn.Read(p)
	}
	var header [4]byte
	if _, err := io.ReadFull(c.Conn, header[:]); err != nil {
		return 0, err
	}
	n := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if n < 1 || n > 1024 {
		return 0, fmt.Errorf("unexpected commit packet size %d", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(c.Conn, body); err != nil {
		return 0, err
	}
	if body[0] != 0 {
		return 0, errors.New("commit not acknowledged by server")
	}
	c.observed.Store(true)
	if err := c.Conn.Close(); err != nil {
		return 0, err
	}
	return 0, io.ErrUnexpectedEOF
}
func deliveryFaultDB(t *testing.T, database string, observed *atomic.Bool) *sql.DB {
	t.Helper()
	network := "data_model_delivery_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	driver.RegisterDialContext(network, func(ctx context.Context, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, err
		}
		return &deliveryLostCommitConn{Conn: conn, observed: observed}, nil
	})
	t.Cleanup(func() { driver.DeregisterDialContext(network) })
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
	db := sql.OpenDB(connector)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func TestIndexDeliveryLostCommitAcknowledgement(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	at := time.Now().UTC().Truncate(time.Microsecond)
	tenantID := uuid.New()
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
		if err != nil {
			return err
		}
		return r.Tenants.Create(ctx, tenant.New(tenantID, at, "fault", nil))
	}); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"enqueue", "claim", "finish"} {
		t.Run(phase, func(t *testing.T) {
			healthy, err := store.NewIndexDeliveryStore(db, database, phase, 3, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			var observed atomic.Bool
			faultDB := deliveryFaultDB(t, database, &observed)
			fault, err := store.NewIndexDeliveryStore(faultDB, database, phase, 3, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			job := uuid.New()
			var delivery uuid.UUID
			if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
				if err != nil {
					return err
				}
				return r.Jobs.Create(ctx, datamodel.IndexJob{ID: job, TenantID: tenantID, TableName: "records", IndexType: "search", Columns: []string{}, Status: "pending", RequestedAt: at})
			}); err != nil {
				t.Fatal(err)
			}
			enqueue := func(target *sql.DB) error {
				return mysqlstore.Mutation(ctx, target, func(tx *sql.Tx) error {
					var err error
					delivery, err = healthy.EnqueueTx(ctx, tx, tenantID, job, nil)
					return err
				})
			}
			if phase == "enqueue" {
				err = enqueue(faultDB)
			} else {
				if err := enqueue(db); err != nil {
					t.Fatal(err)
				}
				if phase == "claim" {
					var got *store.DeliveryLease
					got, err = fault.Claim(ctx)
					if got != nil {
						t.Fatal("uncertain claim returned a lease")
					}
				} else {
					lease, claimErr := healthy.Claim(ctx)
					if claimErr != nil || lease == nil {
						t.Fatalf("claim %+v %v", lease, claimErr)
					}
					calls := 0
					err = fault.Finish(ctx, *lease, false, 0, "", func(tx *sql.Tx) error {
						calls++
						r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
						if err != nil {
							return err
						}
						return r.Jobs.MarkApplied(ctx, job, at)
					})
					if calls != 1 {
						t.Fatal("effects retried")
					}
				}
			}
			var uncertain *mysqlstore.CommitError
			if !errors.As(err, &uncertain) || !observed.Load() {
				t.Fatalf("lost commit not surfaced: %v observed=%v", err, observed.Load())
			}
			var state, status string
			var count int
			if err := db.QueryRowContext(ctx, "SELECT state FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(delivery)).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, "SELECT status FROM data_model_index_jobs WHERE id=?", mysqlstore.UUID(job)).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_index_deliveries WHERE tenant_id=? AND job_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(job)).Scan(&count); err != nil || count != 1 {
				t.Fatalf("duplicates %d %v", count, err)
			}
			switch phase {
			case "enqueue":
				if state != "available" {
					t.Fatal(state)
				}
			case "claim":
				if state != "running" {
					t.Fatal(state)
				}
				if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(delivery)); err != nil {
					t.Fatal(err)
				}
				if n, err := healthy.ReapExpired(ctx, 10); err != nil || n != 1 {
					t.Fatalf("lost claim recovery %d %v", n, err)
				}
				lease, err := healthy.Claim(ctx)
				if err != nil || lease == nil || lease.Attempt != 2 {
					t.Fatalf("recovered lease %+v %v", lease, err)
				}
			case "finish":
				if state != "completed" || status != "applied" {
					t.Fatalf("completion not atomic %s %s", state, status)
				}
			}
			var replay uuid.UUID
			if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
				var err error
				replay, err = healthy.EnqueueTx(ctx, tx, tenantID, job, nil)
				return err
			}); err != nil || replay != delivery {
				t.Fatalf("enqueue reconciliation changed identity %v", err)
			}
		})
	}
}
