package mysql

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

func operatorFixture(t *testing.T) (recordFixtureState, *WorkOwner, *WorkOperator) {
	t.Helper()
	f, w := workFixture(t)
	ddl, err := WorkOperatorSchema(f.database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	o, err := NewWorkOperator(w, func(_ context.Context, _ *sql.Tx, s WorkOperatorScope) (string, error) {
		if s.TenantID != w.owner.receipts.tenant || s.TableID != w.owner.table || s.Action != "cancel" {
			return "", ErrOwnerAuthorization
		}
		return "verified-operator", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return f, w, o
}

func TestWorkOperatorCancellation(t *testing.T) {
	for _, scenario := range []string{"queued", "running", "completed", "audit_failure", "denied", "foreign", "conflict", "missing_reservation"} {
		t.Run(scenario, func(t *testing.T) {
			f, w, o := operatorFixture(t)
			ctx := context.Background()
			in := workInput(DeferredWork)
			if _, err := w.Create(ctx, in); err != nil {
				t.Fatal(err)
			}
			command := CancelWorkRequest{ResourceID: in.ID, ActionID: uuid.New(), ReasonCode: "requested_by_owner"}
			var lease work.Lease
			if scenario == "running" || scenario == "completed" {
				lease = claimWork(t, w, DeferredWork)
			}
			if scenario == "completed" {
				if err := w.Handle(ctx, lease); err != nil {
					t.Fatal(err)
				}
			}
			s := w.owner.receipts.schema
			switch scenario {
			case "audit_failure":
				if _, err := f.db.Exec("CREATE TRIGGER " + s + ".deny_operator_audit BEFORE INSERT ON " + s + ".ingestion_work_actions FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='audit unavailable'"); err != nil {
					t.Fatal(err)
				}
			case "denied":
				o.authorize = func(context.Context, *sql.Tx, WorkOperatorScope) (string, error) { return "", ErrOwnerAuthorization }
			case "foreign":
				w.owner.receipts.tenant = uuid.New()
			case "missing_reservation":
				if _, err := f.db.Exec("DELETE FROM " + s + ".ingestion_work_reservations"); err != nil {
					t.Fatal(err)
				}
			}
			result, err := o.Cancel(ctx, command)
			if scenario == "audit_failure" || scenario == "denied" || scenario == "foreign" || scenario == "missing_reservation" {
				if err == nil || result.State != "" {
					t.Fatal(result, err)
				}
				var state string
				var count int
				if err := f.db.QueryRow("SELECT state FROM "+s+".ingestion_work_jobs WHERE id=?", mysqlstore.UUID(in.ID)).Scan(&state); err != nil || state != "available" {
					t.Fatal(state, err)
				}
				if err := f.db.QueryRow("SELECT COUNT(*) FROM " + s + ".ingestion_work_actions").Scan(&count); err != nil || count != 0 {
					t.Fatal(count, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "completed" {
				if result.Changed || result.State != "completed" {
					t.Fatal(result)
				}
			} else if !result.Changed || result.State != "cancelled" {
				t.Fatal(result)
			}
			replay, err := o.Cancel(ctx, command)
			if err != nil || replay != result {
				t.Fatal(replay, err)
			}
			command.ReasonCode = "changed_reason"
			if _, err := o.Cancel(ctx, command); !errors.Is(err, ErrOperatorActionConflict) {
				t.Fatal(err)
			}
			var count int
			var actor string
			if err := f.db.QueryRow("SELECT COUNT(*),MAX(actor_id) FROM "+s+".ingestion_work_actions").Scan(&count, &actor); err != nil || count != 1 || actor != "verified-operator" {
				t.Fatal(count, actor, err)
			}
			if scenario == "running" {
				if err := w.Handle(ctx, lease); !errors.Is(err, work.ErrLease) {
					t.Fatal(err)
				}
			}
			r, err := w.Get(ctx, in.ID)
			if err != nil || r.CompletedAt == nil || r.State != result.State {
				t.Fatal(r, err)
			}
			expected := 0
			if scenario == "completed" {
				expected = 1
			}
			assertOwnerCounts(t, f, expected, expected, expected, 0)
			o.authorize = func(context.Context, *sql.Tx, WorkOperatorScope) (string, error) { return "", ErrOwnerAuthorization }
			if _, err := o.Cancel(ctx, command); !errors.Is(err, ErrOwnerAuthorization) {
				t.Fatal("revoked replay", err)
			}
		})
	}
}

func TestWorkOperatorCancelWorkerRace(t *testing.T) {
	for i := 0; i < 6; i++ {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			f, w, o := operatorFixture(t)
			ctx := context.Background()
			in := workInput(DeferredWork)
			if _, err := w.Create(ctx, in); err != nil {
				t.Fatal(err)
			}
			l := claimWork(t, w, DeferredWork)
			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(2)
			var workerErr, cancelErr error
			var cancellation work.CancelResult
			go func() { defer wg.Done(); <-start; workerErr = w.Handle(ctx, l) }()
			go func() {
				defer wg.Done()
				<-start
				cancellation, cancelErr = o.Cancel(ctx, CancelWorkRequest{in.ID, uuid.New(), "race_test"})
			}()
			close(start)
			wg.Wait()
			if cancelErr != nil {
				t.Fatal(cancelErr)
			}
			r, err := w.Get(ctx, in.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch r.State {
			case "completed":
				if workerErr != nil || cancellation.Changed {
					t.Fatal(workerErr, cancellation)
				}
				assertOwnerCounts(t, f, 1, 1, 1, 0)
			case "cancelled":
				if !errors.Is(workerErr, work.ErrLease) || !cancellation.Changed {
					t.Fatal(workerErr, cancellation)
				}
				assertOwnerCounts(t, f, 0, 0, 0, 0)
			default:
				t.Fatal(r)
			}
		})
	}
}

func TestWorkOperatorRestrictedGrants(t *testing.T) {
	f, w, o := operatorFixture(t)
	ctx := context.Background()
	first, second := workInput(DeferredWork), workInput(DeferredWork)
	for _, in := range []WorkInput{first, second} {
		if _, err := w.Create(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	pool := restrictedReceiptPool(t, f.db, f.database)
	var login string
	if err := pool.QueryRow("SELECT CURRENT_USER()").Scan(&login); err != nil {
		t.Fatal(err)
	}
	account := strings.Split(login, "@")[0]
	target := "'" + account + "'@'%'"
	if _, err := f.db.Exec("REVOKE ALL PRIVILEGES, GRANT OPTION FROM " + target); err != nil {
		t.Fatal(err)
	}
	grants, err := CancellationGrants(f.database, account)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range grants {
		if _, err := f.db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	w.owner.receipts.db = pool
	w.queue, err = work.New(pool, f.database, "ingestion", 4096)
	if err != nil {
		t.Fatal(err)
	}
	command := CancelWorkRequest{first.ID, uuid.New(), "restricted_test"}
	if r, err := o.Cancel(ctx, command); err != nil || !r.Changed {
		t.Fatal(r, err)
	}
	if r, err := o.Cancel(ctx, command); err != nil || !r.Changed {
		t.Fatal(r, err)
	}
	s := w.owner.receipts.schema
	for _, q := range []string{
		"UPDATE " + s + ".ingestion_work_actions SET reason_code='tampered'",
		"DELETE FROM " + s + ".ingestion_work_actions",
		"UPDATE " + s + ".ingestion_work_resources SET payload='{}'",
		"SELECT response_payload FROM " + s + ".ingestion_request_receipts",
		"SELECT * FROM " + f.qualified,
	} {
		if _, err := pool.Exec(q); err == nil {
			t.Fatal("forbidden operator access", q)
		}
	}
	if _, err := f.db.Exec("REVOKE INSERT ON " + s + ".ingestion_work_actions FROM " + target); err != nil {
		t.Fatal(err)
	}
	if r, err := o.Cancel(ctx, CancelWorkRequest{second.ID, uuid.New(), "denied_audit"}); err == nil || r.State != "" {
		t.Fatal(r, err)
	}
	var state string
	if err := f.db.QueryRow("SELECT state FROM "+s+".ingestion_work_jobs WHERE id=?", mysqlstore.UUID(second.ID)).Scan(&state); err != nil || state != "available" {
		t.Fatal(state, err)
	}
}

func TestWorkOperatorLostCommit(t *testing.T) {
	f, w, o := operatorFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	in := workInput(DeferredWork)
	if _, err := w.Create(ctx, in); err != nil {
		t.Fatal(err)
	}
	var observed atomic.Bool
	network := "ingestion_cancel_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	driver.RegisterDialContext(network, func(ctx context.Context, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, err
		}
		return &receiptCommitLoss{Conn: conn, observed: &observed}, nil
	})
	defer driver.DeregisterDialContext(network)
	cfg := driver.NewConfig()
	cfg.Net = network
	cfg.Addr = os.Getenv("INGESTION_MYSQL_TEST_ADDRESS")
	cfg.DBName = f.database
	cfg.User = "root"
	cfg.Passwd = "isolated-test-root-only"
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 5 * time.Second
	cfg.WriteTimeout = 5 * time.Second
	connector, err := driver.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pool := sql.OpenDB(connector)
	defer pool.Close()
	w.owner.receipts.db = pool
	command := CancelWorkRequest{ResourceID: in.ID, ActionID: uuid.New(), ReasonCode: "requested_by_owner"}
	result, err := o.Cancel(ctx, command)
	var uncertain *mysqlstore.CommitError
	if !observed.Load() || !errors.As(err, &uncertain) || result != (work.CancelResult{}) {
		t.Fatal(result, err)
	}
	w.owner.receipts.db = f.db
	result, err = o.Cancel(ctx, command)
	if err != nil || result.State != "cancelled" || !result.Changed {
		t.Fatal(result, err)
	}
	var count int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM " + w.owner.receipts.schema + ".ingestion_work_actions").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	var state string
	if err := f.db.QueryRow("SELECT state FROM "+w.owner.receipts.schema+".ingestion_work_jobs WHERE id=?", mysqlstore.UUID(in.ID)).Scan(&state); err != nil || state != "cancelled" {
		t.Fatal(state, err)
	}
}
