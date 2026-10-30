package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
	"github.com/google/uuid"
)

func workFixture(t *testing.T) (recordFixtureState, *WorkOwner) {
	t.Helper()
	f, owner, _ := ownerFixture(t)
	ddl, err := WorkSchema(f.database)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range ddl {
		if _, err := f.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	pointer, err := WorkRedrivePointer(f.database)
	if err != nil {
		t.Fatal(err)
	}
	fixtureExec(t, f.db, pointer)
	w, err := NewWorkOwner(owner, "uploads", "deferred", 1<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	return f, w
}
func workInput(kind string) WorkInput {
	payload := []byte(`{"object_id":"one","number":7}`)
	if kind == UploadWork {
		payload = []byte("object_id,number\none,7\ntwo,8\n")
	}
	return WorkInput{ID: uuid.New(), Kind: kind, Mode: ingestion.ModePatch, Payload: payload, ScheduledAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}
func claimWork(t *testing.T, w *WorkOwner, kind string) work.Lease {
	t.Helper()
	queue := "deferred"
	if kind == UploadWork {
		queue = "uploads"
	}
	leases, err := w.Queue().Claim(context.Background(), queue, 1, time.Minute)
	if err != nil || len(leases) != 1 {
		t.Fatal(leases, err)
	}
	return leases[0]
}

func TestIngestionWorkLifecycle(t *testing.T) {
	for _, scenario := range []string{"deferred", "upload", "validation", "malformed_csv", "stale", "completion_rollback", "enqueue_rollback", "identity", "public_key", "future"} {
		t.Run(scenario, func(t *testing.T) {
			f, w := workFixture(t)
			in := workInput(DeferredWork)
			ctx := context.Background()
			s := w.owner.receipts.schema
			if scenario == "upload" || scenario == "malformed_csv" {
				in = workInput(UploadWork)
			}
			if scenario == "validation" {
				in.Payload = []byte(`{"object_id":"one","number":"invalid"}`)
			}
			if scenario == "malformed_csv" {
				in.Payload = []byte("object_id,object_id\none,two\n")
			}
			if scenario == "public_key" {
				key := "public"
				in.RequestKey = &key
			}
			if scenario == "future" {
				in.ScheduledAt = time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
			}
			if scenario == "enqueue_rollback" {
				if _, err := f.db.Exec("CREATE TRIGGER " + s + ".deny_enqueue BEFORE INSERT ON " + s + ".ingestion_work_jobs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='queue failure'"); err != nil {
					t.Fatal(err)
				}
				if _, err := w.Create(ctx, in); err == nil {
					t.Fatal("enqueue error ignored")
				}
				var n int
				if err := f.db.QueryRow("SELECT COUNT(*) FROM " + s + ".ingestion_work_resources").Scan(&n); err != nil || n != 0 {
					t.Fatal(n, err)
				}
				return
			}
			created, err := w.Create(ctx, in)
			if err != nil || created.State != "available" {
				t.Fatal(created, err)
			}
			if _, err := w.Create(ctx, in); err != nil {
				t.Fatal("creation replay", err)
			}
			if scenario == "identity" {
				in.Payload = []byte(`{"object_id":"different"}`)
				if _, err := w.Create(ctx, in); !errors.Is(err, ErrWorkIdentity) {
					t.Fatal(err)
				}
				return
			}
			if scenario == "future" {
				l, err := w.Queue().Claim(ctx, "deferred", 1, time.Minute)
				if err != nil || len(l) != 0 {
					t.Fatal(l, err)
				}
				return
			}
			lease := claimWork(t, w, in.Kind)
			if scenario == "stale" {
				if _, err := f.db.Exec("UPDATE "+s+".ingestion_work_jobs SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(in.ID)); err != nil {
					t.Fatal(err)
				}
				if err := w.Handle(ctx, lease); !errors.Is(err, work.ErrLease) {
					t.Fatal(err)
				}
				if n, err := w.Queue().Reap(ctx, 10, time.Second); err != nil || n != 1 {
					t.Fatal(n, err)
				}
				if _, err := f.db.Exec("UPDATE "+s+".ingestion_work_jobs SET available_at=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(in.ID)); err != nil {
					t.Fatal(err)
				}
				current := claimWork(t, w, in.Kind)
				if err := w.Handle(ctx, lease); !errors.Is(err, work.ErrLease) {
					t.Fatal(err)
				}
				lease = current
			}
			if scenario == "completion_rollback" {
				if _, err := f.db.Exec("CREATE TRIGGER " + s + ".deny_complete BEFORE UPDATE ON " + s + ".ingestion_work_jobs FOR EACH ROW BEGIN IF NEW.state='completed' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='completion failure'; END IF; END"); err != nil {
					t.Fatal(err)
				}
				if err := w.Handle(ctx, lease); err == nil {
					t.Fatal("completion error ignored")
				}
				assertOwnerCounts(t, f, 0, 0, 0, 0)
				var n int
				if err := f.db.QueryRow("SELECT COUNT(*) FROM " + s + ".ingestion_execution_receipts").Scan(&n); err != nil || n != 0 {
					t.Fatal(n, err)
				}
				if _, err := f.db.Exec("DROP TRIGGER " + s + ".deny_complete"); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Handle(ctx, lease); err != nil {
				t.Fatal(err)
			}
			result, err := w.Get(ctx, in.ID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "validation" || scenario == "malformed_csv" {
				if result.State != "discarded" || result.CompletedAt == nil {
					t.Fatal(result)
				}
				audits := 0
				if scenario == "validation" {
					audits = 1
				}
				assertOwnerCounts(t, f, 0, audits, 0, 0)
			} else {
				if result.State != "completed" || result.CompletedAt == nil {
					t.Fatal(result)
				}
				n, events, receipts := 1, 1, 0
				if scenario == "upload" {
					n, events = 2, 3
				}
				if scenario == "public_key" {
					receipts = 1
				}
				assertOwnerCounts(t, f, n, n, events, receipts)
				if result.SuccessfulRows != n || result.TotalRows != n {
					t.Fatal(result)
				}
			}
			if err := w.Handle(ctx, lease); !errors.Is(err, work.ErrLease) {
				t.Fatal("terminal redelivery", err)
			}
			var active int
			if err := f.db.QueryRow("SELECT COUNT(*) FROM " + s + ".ingestion_work_reservations").Scan(&active); err != nil || active != 0 {
				t.Fatal(active, err)
			}
		})
	}
}

func TestIngestionWorkRunnerAndConcurrentClaims(t *testing.T) {
	f, w := workFixture(t)
	ctx := context.Background()
	var ids []uuid.UUID
	for i := 0; i < 8; i++ {
		in := workInput(DeferredWork)
		in.Payload = []byte(`{"object_id":"` + uuid.NewString() + `"}`)
		if _, err := w.Create(ctx, in); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, in.ID)
	}
	leases := make(chan work.Lease, 8)
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ls, err := w.Queue().Claim(ctx, "deferred", 1, time.Minute)
			errs <- err
			for _, l := range ls {
				leases <- l
			}
		}()
	}
	wg.Wait()
	close(leases)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[uuid.UUID]bool{}
	for l := range leases {
		if seen[l.ID] {
			t.Fatal("double claim")
		}
		seen[l.ID] = true
		if err := w.Handle(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 8 {
		t.Fatal(len(seen))
	}
	in := workInput(DeferredWork)
	in.Payload = []byte(`{"object_id":"runner"}`)
	if _, err := w.Create(ctx, in); err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	observed := make(chan error, 10)
	go func() {
		done <- w.Queue().Run(runCtx, work.RunnerConfig{Queue: "deferred", Concurrency: 2, Lease: 3 * time.Second, Timeout: 5 * time.Second, Poll: 10 * time.Millisecond, RetryBase: time.Second, RetryMax: time.Minute}, w.Handle, func(err error) { observed <- err })
	}()
	for {
		r, err := w.Get(ctx, in.ID)
		if err != nil {
			t.Fatal(err)
		}
		if r.State == "completed" {
			break
		}
		if runCtx.Err() != nil {
			t.Fatal("runner did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case err := <-observed:
		t.Fatal(err)
	default:
	}
	assertOwnerCounts(t, f, 9, 9, 9, 0)
}

func TestIngestionWorkScopeAndPrivateReceipts(t *testing.T) {
	f, w := workFixture(t)
	in := workInput(DeferredWork)
	// Public key bytes identical to an internal execution ID do not collide.
	c := ingestion.ReceiptCommand{TenantID: f.model.TenantID, TableID: w.owner.table, Mode: ingestion.ModePatch, Kind: "single", Records: []map[string]any{{"object_id": "public"}}}
	if _, err := w.owner.Execute(context.Background(), c, in.ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Create(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	l := claimWork(t, w, in.Kind)
	wrong := l
	wrong.TenantID = uuid.New()
	if err := w.Handle(context.Background(), wrong); !errors.Is(err, work.ErrLease) {
		t.Fatal(err)
	}
	if err := w.Handle(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	assertOwnerCounts(t, f, 2, 2, 2, 1)
	denied := errors.New("revoked")
	w.owner.receipts.authorizeTx = func(context.Context, *sql.Tx, ReceiptRequest) error { return denied }
	if _, err := w.Get(context.Background(), in.ID); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if _, err := w.Create(context.Background(), in); !errors.Is(err, denied) {
		t.Fatal(err)
	}
}

func TestWorkCSVBounds(t *testing.T) {
	for _, value := range []string{"object_id\n", "a,a\n1,2\n", "a,b\n1\n", "object_id\n" + strings.Repeat("one\n", 501)} {
		if _, err := decodeWorkCSV([]byte(value)); err == nil {
			t.Fatal("accepted malformed/oversized CSV")
		}
	}
}
