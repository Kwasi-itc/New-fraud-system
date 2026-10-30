package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	driver "github.com/go-sql-driver/mysql"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
	"github.com/google/uuid"
)

func TestFullSharedRedrive(t *testing.T) {
	for _, kind := range []string{DeferredWork, UploadWork} {
		t.Run(kind, func(t *testing.T) {
			shared, f := fullRecordFixture(t)
			owner, _ := fullOwner(t, shared, f, "worker")
			w, e := NewWorkOwner(owner, "uploads", "deferred", 1<<20, 1)
			if e != nil {
				t.Fatal(e)
			}
			ctx := WithServiceCredential(context.Background(), "fixture-token")
			policy := func(_ context.Context, _ *sql.Tx, s WorkOperatorScope) (string, error) {
				if s.TenantID != owner.receipts.tenant || s.TableID != owner.table || (s.Action != "redrive" && s.Action != "cancel") {
					return "", ErrOwnerAuthorization
				}
				return "verified-operator", nil
			}
			pool := fullFixturePrincipal(t, shared, "operator")
			var account string
			if e = pool.QueryRow("SELECT SUBSTRING_INDEX(CURRENT_USER(),'@',1)").Scan(&account); e != nil {
				t.Fatal(e)
			}
			fixtureExec(t, f.db, "REVOKE ALL PRIVILEGES, GRANT OPTION FROM '"+account+"'@'%'")
			grants, e := RedriveGrants(shared.Database, account)
			if e != nil {
				t.Fatal(e)
			}
			metadata, e := ContractReadGrants(shared.Database, account)
			if e != nil {
				t.Fatal(e)
			}
			for _, q := range append(grants, metadata...) {
				fixtureExec(t, f.db, q)
			}
			bound, e := NewAuthorizedRecordOwner(pool, shared.Database, owner.receipts.tenant, owner.table, "records", "fixture-token", owner.limits, 1<<20)
			if e != nil {
				t.Fatal(e)
			}
			restricted, e := NewWorkOwner(bound, "uploads", "deferred", 1<<20, 1)
			if e != nil {
				t.Fatal(e)
			}
			operator, e := NewWorkOperator(restricted, policy)
			if e != nil {
				t.Fatal(e)
			}
			in := workInput(kind)
			if _, e = w.Create(ctx, in); e != nil {
				t.Fatal(e)
			}
			command := RedriveWorkRequest{in.ID, in.ID, uuid.New(), "retry_requested", in.ScheduledAt}
			if id, e := operator.Redrive(ctx, command); !errors.Is(e, ErrRedriveState) || id != uuid.Nil {
				t.Fatal(id, e)
			}
			old := claimWork(t, w, kind)
			if e = w.Queue().Retry(ctx, old, time.Second, "handler_failed"); e != nil {
				t.Fatal(e)
			}
			var before string
			if e = f.db.QueryRow("SELECT CONCAT(state,':',attempt,':',finished_at,':',last_error) FROM ingestion_work_jobs WHERE id=?", mysqlstore.UUID(old.ID)).Scan(&before); e != nil {
				t.Fatal(e)
			}
			// Exact duplicate concurrent actions reconcile to one new job, retaining one resource.
			start := make(chan struct{})
			results := make(chan error, 8)
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					id, e := operator.Redrive(ctx, command)
					if e == nil && id != command.ActionID {
						e = errors.New("wrong job")
					}
					results <- e
				}()
			}
			close(start)
			wg.Wait()
			close(results)
			for e := range results {
				if e != nil {
					t.Fatal(e)
				}
			}
			changed := command
			changed.ActionID = uuid.New()
			if _, e = operator.Redrive(ctx, changed); !errors.Is(e, ErrOperatorActionConflict) {
				t.Fatal(e)
			}
			changed = command
			changed.ReasonCode = "different"
			if _, e = operator.Redrive(ctx, changed); !errors.Is(e, ErrOperatorActionConflict) {
				t.Fatal(e)
			}
			changed = command
			changed.ScheduledAt = changed.ScheduledAt.Add(time.Second)
			if _, e = operator.Redrive(ctx, changed); !errors.Is(e, ErrOperatorActionConflict) {
				t.Fatal(e)
			}
			if _, e = operator.Redrive(context.Background(), command); !errors.Is(e, ErrOwnerAuthorization) {
				t.Fatal(e)
			}
			revoked, _ := NewWorkOperator(restricted, func(context.Context, *sql.Tx, WorkOperatorScope) (string, error) { return "", ErrOwnerAuthorization })
			if _, e = revoked.Redrive(ctx, command); !errors.Is(e, ErrOwnerAuthorization) {
				t.Fatal(e)
			}
			if e = w.Handle(ctx, old); !errors.Is(e, work.ErrLease) {
				t.Fatal(e)
			}
			current, e := w.Get(ctx, in.ID)
			if e != nil || current.State != "available" || current.Attempt != 0 || current.CompletedAt != nil {
				t.Fatal(current, e)
			}
			next := claimWork(t, w, kind)
			if next.ID != command.ActionID || next.ResourceID != in.ID {
				t.Fatal(next)
			}
			if e = w.Handle(ctx, next); e != nil {
				t.Fatal(e)
			}
			current, e = w.Get(ctx, in.ID)
			if e != nil || current.State != "completed" {
				t.Fatal(current, e)
			}
			if id, e := operator.Redrive(ctx, command); e != nil || id != command.ActionID {
				t.Fatal(id, e)
			}
			if _, e = operator.Redrive(ctx, RedriveWorkRequest{in.ID, next.ID, uuid.New(), "retry", in.ScheduledAt}); !errors.Is(e, ErrRedriveState) {
				t.Fatal(e)
			}
			var after string
			if e = f.db.QueryRow("SELECT CONCAT(state,':',attempt,':',finished_at,':',last_error) FROM ingestion_work_jobs WHERE id=?", mysqlstore.UUID(old.ID)).Scan(&after); e != nil || after != before {
				t.Fatal(before, after, e)
			}
			for table, want := range map[string]int{"ingestion_work_resources": 1, "ingestion_work_jobs": 2, "ingestion_work_redrives": 1, "ingestion_work_reservations": 0} {
				var n int
				if e = f.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); e != nil || n != want {
					t.Fatal(table, n, e)
				}
			}
			for _, q := range []string{"UPDATE ingestion_work_jobs SET state='cancelled' WHERE state='discarded'", "UPDATE ingestion_work_resources SET payload='{}'", "SELECT payload FROM ingestion_work_resources", "DELETE FROM ingestion_work_redrives", "DELETE FROM ingestion_request_receipts", "SELECT raw_value FROM data_model_exact_keys"} {
				if _, e = pool.Exec(q); e == nil {
					t.Fatal("unexpected permission", q)
				}
			}
			expected, events := 1, 1
			if kind == UploadWork {
				expected, events = 2, 3
			}
			assertOwnerCounts(t, f, expected, expected, events, 0)
			// Fail each required append/update independently, then reconcile one retry.
			blocked := workInput(kind)
			if _, e = w.Create(ctx, blocked); e != nil {
				t.Fatal(e)
			}
			lease := claimWork(t, w, kind)
			if e = w.Queue().Retry(ctx, lease, time.Second, "handler_failed"); e != nil {
				t.Fatal(e)
			}
			retry := RedriveWorkRequest{blocked.ID, blocked.ID, uuid.New(), "retry", blocked.ScheduledAt}
			for _, denial := range []struct{ table, priv string }{{"ingestion_work_jobs", "INSERT"}, {"ingestion_work_reservations", "INSERT"}, {"ingestion_work_resources", "UPDATE(current_job_id,completed_at)"}, {"ingestion_work_redrives", "INSERT"}} {
				fixtureExec(t, f.db, "REVOKE "+denial.priv+" ON "+shared.Database+"."+denial.table+" FROM '"+account+"'@'%'")
				if id, e := operator.Redrive(ctx, retry); e == nil || id != uuid.Nil {
					t.Fatal(denial, id, e)
				}
				var n int
				if e = f.db.QueryRow("SELECT COUNT(*) FROM ingestion_work_jobs WHERE id=?", mysqlstore.UUID(retry.ActionID)).Scan(&n); e != nil || n != 0 {
					t.Fatal(n, e)
				}
				var pointer mysqlstore.UUID
				if e = f.db.QueryRow("SELECT COALESCE(current_job_id,id) FROM ingestion_work_resources WHERE id=?", mysqlstore.UUID(blocked.ID)).Scan(&pointer); e != nil || uuid.UUID(pointer) != blocked.ID {
					t.Fatal(pointer, e)
				}
				fixtureExec(t, f.db, "GRANT "+denial.priv+" ON "+shared.Database+"."+denial.table+" TO '"+account+"'@'%'")
			}
			if _, e = operator.Redrive(ctx, retry); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func redriveFixture(t *testing.T) (recordFixtureState, *WorkOwner, *WorkOperator) {
	f, w, o := operatorFixture(t)
	ddl, e := WorkRedriveSchema(f.database)
	if e != nil {
		t.Fatal(e)
	}
	fixtureExec(t, f.db, ddl)
	o.authorize = func(context.Context, *sql.Tx, WorkOperatorScope) (string, error) { return "verified-operator", nil }
	w.maxAttempts = 1
	return f, w, o
}
func TestWorkRedriveIneligible(t *testing.T) {
	for _, scenario := range []string{"cancelled", "invalid_payload", "validation_failed", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			f, w, o := redriveFixture(t)
			ctx := context.Background()
			in := workInput(DeferredWork)
			if _, e := w.Create(ctx, in); e != nil {
				t.Fatal(e)
			}
			if scenario == "cancelled" {
				if _, e := o.Cancel(ctx, CancelWorkRequest{in.ID, uuid.New(), "cancelled"}); e != nil {
					t.Fatal(e)
				}
			} else {
				lease := claimWork(t, w, DeferredWork)
				if e := w.Queue().Retry(ctx, lease, time.Second, scenario); e != nil {
					t.Fatal(e)
				}
			}
			if id, e := o.Redrive(ctx, RedriveWorkRequest{in.ID, in.ID, uuid.New(), "retry", in.ScheduledAt}); !errors.Is(e, ErrRedriveState) || id != uuid.Nil {
				t.Fatal(id, e)
			}
			assertOwnerCounts(t, f, 0, 0, 0, 0)
		})
	}
}
func TestWorkRedriveReceiptAndCancellation(t *testing.T) {
	for _, public := range []bool{false, true} {
		t.Run(map[bool]string{false: "private", true: "public"}[public], func(t *testing.T) {
			f, w, o := redriveFixture(t)
			ctx := context.Background()
			in := workInput(DeferredWork)
			if public {
				key := "shared-receipt"
				in.RequestKey = &key
			}
			if _, e := w.Create(ctx, in); e != nil {
				t.Fatal(e)
			}
			// Seed the exact retained successful receipt without completing the queue job,
			// representing a pre-existing historical/recovery boundary. The new delivery
			// must reconcile it without repeating physical/audit/outbox effects.
			command := fullCommand(f)
			command.Records = []map[string]any{{"object_id": "one", "number": float64(7)}}
			copyOwner := *w.owner
			key := in.ID.String()
			if public {
				key = *in.RequestKey
			} else {
				manager := *w.owner.receipts
				manager.bucketTable = manager.schema + ".ingestion_execution_buckets"
				manager.receiptTable = manager.schema + ".ingestion_execution_receipts"
				copyOwner.receipts = &manager
			}
			// Match the deferred decoder's numeric spelling exactly.
			command.Records[0]["number"] = json.Number("7")
			if _, e := copyOwner.Execute(ctx, command, key); e != nil {
				t.Fatal(e)
			}
			old := claimWork(t, w, DeferredWork)
			if e := w.Queue().Retry(ctx, old, time.Second, "handler_failed"); e != nil {
				t.Fatal(e)
			}
			redrive := RedriveWorkRequest{in.ID, in.ID, uuid.New(), "retry", in.ScheduledAt}
			if _, e := o.Redrive(ctx, redrive); e != nil {
				t.Fatal(e)
			}
			if e := w.Handle(ctx, claimWork(t, w, DeferredWork)); e != nil {
				t.Fatal(e)
			}
			receipts := 0
			if public {
				receipts = 1
			}
			assertOwnerCounts(t, f, 1, 1, 1, receipts)
			// Cancellation targets the new current job; completed work remains completed.
			if result, e := o.Cancel(ctx, CancelWorkRequest{in.ID, uuid.New(), "cancelled"}); e != nil || result.State != "completed" || result.Changed {
				t.Fatal(result, e)
			}
		})
	}
}

func TestWorkRedriveLostCommit(t *testing.T) {
	f, w, o := redriveFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	in := workInput(DeferredWork)
	if _, err := w.Create(ctx, in); err != nil {
		t.Fatal(err)
	}
	old := claimWork(t, w, DeferredWork)
	if err := w.Queue().Retry(ctx, old, time.Second, "handler_failed"); err != nil {
		t.Fatal(err)
	}
	var observed atomic.Bool
	network := "ingestion_redrive_fault_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	fault := sql.OpenDB(connector)
	defer fault.Close()
	w.owner.receipts.db = fault
	command := RedriveWorkRequest{in.ID, in.ID, uuid.New(), "retry", in.ScheduledAt}
	id, err := o.Redrive(ctx, command)
	var uncertain *mysqlstore.CommitError
	if !observed.Load() || !errors.As(err, &uncertain) || id != uuid.Nil {
		t.Fatal(id, err)
	}
	w.owner.receipts.db = f.db
	if id, err = o.Redrive(ctx, command); err != nil || id != command.ActionID {
		t.Fatal(id, err)
	}
	var count int
	if err = f.db.QueryRow("SELECT COUNT(*) FROM ingestion_work_redrives").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err = w.Handle(ctx, claimWork(t, w, DeferredWork)); err != nil {
		t.Fatal(err)
	}
	assertOwnerCounts(t, f, 1, 1, 1, 0)
}
