package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestIndexDeliveryStore(t *testing.T) {
	db, database := metadataDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	at := time.Now().UTC().Truncate(time.Microsecond)
	tenantID := uuid.New()
	if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
		r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
		if err != nil {
			return err
		}
		return r.Tenants.Create(ctx, tenant.New(tenantID, at, "queue", nil))
	}); err != nil {
		t.Fatal(err)
	}
	queue := func(t *testing.T, name string) *store.IndexDeliveryStore {
		t.Helper()
		q, err := store.NewIndexDeliveryStore(db, database, name, 2, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	makeJob := func(t *testing.T) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
			if err != nil {
				return err
			}
			return r.Jobs.Create(ctx, datamodel.IndexJob{ID: id, TenantID: tenantID, TableName: "records", IndexType: "search", Columns: []string{"name"}, Status: "pending", RequestedAt: at})
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	enqueue := func(t *testing.T, q *store.IndexDeliveryStore, job uuid.UUID, when *time.Time) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error { var err error; id, err = q.EnqueueTx(ctx, tx, tenantID, job, when); return err }); err != nil {
			t.Fatal(err)
		}
		return id
	}
	claim := func(t *testing.T, q *store.IndexDeliveryStore) *store.DeliveryLease {
		t.Helper()
		v, err := q.Claim(ctx)
		if err != nil || v == nil {
			t.Fatalf("claim %+v %v", v, err)
		}
		return v
	}
	expire := func(t *testing.T, id uuid.UUID) {
		t.Helper()
		if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(id)); err != nil {
			t.Fatal(err)
		}
	}
	state := func(t *testing.T, id uuid.UUID) string {
		t.Helper()
		var v string
		if err := db.QueryRowContext(ctx, "SELECT state FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(id)).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	t.Run("enqueue_atomic_schedule_and_cross_queue_dedupe", func(t *testing.T) {
		q, other := queue(t, "scheduled"), queue(t, "other")
		job := makeJob(t)
		future := at.Add(time.Hour)
		id := enqueue(t, q, job, &future)
		if again := enqueue(t, other, job, nil); again != id {
			t.Fatal("dedupe changed across queues")
		}
		if got, err := q.Claim(ctx); err != nil || got != nil {
			t.Fatalf("future claimed %+v %v", got, err)
		}
		injected := errors.New("owner failed")
		newJob := uuid.New()
		err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
			if err != nil {
				return err
			}
			if err := r.Jobs.Create(ctx, datamodel.IndexJob{ID: newJob, TenantID: tenantID, TableName: "records", IndexType: "search", Columns: []string{}, Status: "pending", RequestedAt: at}); err != nil {
				return err
			}
			if _, err := q.EnqueueTx(ctx, tx, tenantID, newJob, nil); err != nil {
				return err
			}
			return injected
		})
		if !errors.Is(err, injected) {
			t.Fatal(err)
		}
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_index_jobs WHERE id=?", mysqlstore.UUID(newJob)).Scan(&n); err != nil || n != 0 {
			t.Fatalf("domain rollback %d %v", n, err)
		}
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_index_deliveries WHERE job_id=?", mysqlstore.UUID(newJob)).Scan(&n); err != nil || n != 0 {
			t.Fatalf("delivery rollback %d %v", n, err)
		}
	})
	t.Run("concurrent_enqueue_and_claim", func(t *testing.T) {
		q := queue(t, "concurrent")
		job := makeJob(t)
		ids := make(chan uuid.UUID, 2)
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() {
				var id uuid.UUID
				err := mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error { var err error; id, err = q.EnqueueTx(ctx, tx, tenantID, job, nil); return err })
				ids <- id
				errs <- err
			}()
		}
		first, second := <-ids, <-ids
		for i := 0; i < 2; i++ {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
		if first != second || first == uuid.Nil {
			t.Fatal("duplicate deliveries")
		}
		leases := make(chan *store.DeliveryLease, 2)
		for i := 0; i < 2; i++ {
			go func() { v, err := q.Claim(ctx); leases <- v; errs <- err }()
		}
		one, two := <-leases, <-leases
		for i := 0; i < 2; i++ {
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		}
		if (one == nil) == (two == nil) {
			t.Fatal("delivery claimed twice or not claimed")
		}
	})
	t.Run("finish_fences_tenant_token_attempt_and_rolls_back_effects", func(t *testing.T) {
		q := queue(t, "effects")
		job := makeJob(t)
		id := enqueue(t, q, job, nil)
		lease := claim(t, q)
		for _, bad := range []store.DeliveryLease{{ID: id, TenantID: uuid.New(), JobID: job, Token: lease.Token, Attempt: lease.Attempt}, {ID: id, TenantID: tenantID, JobID: job, Token: uuid.New(), Attempt: lease.Attempt}, {ID: id, TenantID: tenantID, JobID: job, Token: lease.Token, Attempt: lease.Attempt + 1}} {
			called := false
			if err := q.Finish(ctx, bad, false, 0, "", func(*sql.Tx) error { called = true; return nil }); err == nil || called {
				t.Fatal("invalid lease reached effects")
			}
		}
		if err := q.Heartbeat(ctx, *lease); err != nil {
			t.Fatal(err)
		}
		injected := errors.New("history unavailable")
		err := q.Finish(ctx, *lease, false, 0, "", func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
			if err != nil {
				return err
			}
			if err := r.Jobs.MarkApplied(ctx, job, at); err != nil {
				return err
			}
			return injected
		})
		if !errors.Is(err, injected) || state(t, id) != "running" {
			t.Fatalf("failure %v", err)
		}
		var status string
		if err := db.QueryRowContext(ctx, "SELECT status FROM data_model_index_jobs WHERE id=?", mysqlstore.UUID(job)).Scan(&status); err != nil || status != "pending" {
			t.Fatalf("effects leaked %s %v", status, err)
		}
		err = q.Finish(ctx, *lease, false, 0, "", func(tx *sql.Tx) error {
			r, err := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
			if err != nil {
				return err
			}
			return r.Jobs.MarkApplied(ctx, job, at)
		})
		if err != nil || state(t, id) != "completed" {
			t.Fatal(err)
		}
		if err := q.Finish(ctx, *lease, false, 0, "", nil); !errors.Is(err, store.ErrStaleDeliveryLease) {
			t.Fatal(err)
		}
		if again := enqueue(t, q, job, nil); again != id {
			t.Fatal("retained completed delivery did not dedupe")
		}
	})
	t.Run("expiry_during_effects_rolls_back_all_writes", func(t *testing.T) {
		q := queue(t, "expiry_effects")
		job := makeJob(t)
		id := enqueue(t, q, job, nil)
		lease := claim(t, q)
		err := q.Finish(ctx, *lease, false, 0, "", func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "UPDATE data_model_index_jobs SET status='applied' WHERE id=?", mysqlstore.UUID(job)); err != nil {
				return err
			}
			// Fault injection simulates expiry after the initial lease check without sleep.
			_, err := tx.ExecContext(ctx, "UPDATE data_model_index_deliveries SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(id))
			return err
		})
		if !errors.Is(err, store.ErrStaleDeliveryLease) || state(t, id) != "running" {
			t.Fatal(err)
		}
		var status string
		if err := db.QueryRowContext(ctx, "SELECT status FROM data_model_index_jobs WHERE id=?", mysqlstore.UUID(job)).Scan(&status); err != nil || status != "pending" {
			t.Fatalf("expired effect committed %s %v", status, err)
		}
	})
	t.Run("reaper_fences_old_token_and_discards_at_limit", func(t *testing.T) {
		q := queue(t, "reaper")
		job := makeJob(t)
		id := enqueue(t, q, job, nil)
		old := claim(t, q)
		expire(t, id)
		if err := q.Heartbeat(ctx, *old); !errors.Is(err, store.ErrStaleDeliveryLease) {
			t.Fatal(err)
		}
		if n, err := q.ReapExpired(ctx, 10); err != nil || n != 1 {
			t.Fatalf("reap %d %v", n, err)
		}
		next := claim(t, q)
		if next.Attempt != 2 || next.Token == old.Token {
			t.Fatal("attempt/token not changed")
		}
		if err := q.Finish(ctx, *old, false, 0, "", nil); !errors.Is(err, store.ErrStaleDeliveryLease) {
			t.Fatal(err)
		}
		expire(t, id)
		if n, err := q.ReapExpired(ctx, 10); err != nil || n != 1 || state(t, id) != "discarded" {
			t.Fatalf("discard %d %v", n, err)
		}
		if again := enqueue(t, q, job, nil); again == id {
			t.Fatal("discarded delivery still reserves job")
		}
	})
	t.Run("retry_cancel_and_retention", func(t *testing.T) {
		q := queue(t, "retry")
		job := makeJob(t)
		id := enqueue(t, q, job, nil)
		lease := claim(t, q)
		if err := q.Finish(ctx, *lease, true, time.Hour, "temporary_failure", nil); err != nil || state(t, id) != "available" {
			t.Fatal(err)
		}
		if v, err := q.Claim(ctx); err != nil || v != nil {
			t.Fatalf("retry scheduled early %+v %v", v, err)
		}
		if err := q.Cancel(ctx, tenantID, job, id, nil); err != nil || state(t, id) != "cancelled" {
			t.Fatal(err)
		}
		called := false
		if err := q.Cancel(ctx, tenantID, job, id, func(*sql.Tx) error { called = true; return nil }); err != nil || called {
			t.Fatal("repeat cancellation ran effects")
		}
		fresh := enqueue(t, q, job, nil)
		if fresh == id {
			t.Fatal("cancelled reservation retained")
		}
		current := claim(t, q)
		if err := q.Finish(ctx, *current, false, 0, "", nil); err != nil {
			t.Fatal(err)
		}
		if err := q.Cancel(ctx, tenantID, job, fresh, nil); !errors.Is(err, store.ErrDeliveryTerminal) {
			t.Fatal(err)
		}
		if n, err := q.CleanupTerminal(ctx, 10); err != nil || n != 0 {
			t.Fatalf("early retention %d %v", n, err)
		}
		if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET retain_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE tenant_id=? AND job_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(job)); err != nil {
			t.Fatal(err)
		}
		if n, err := q.CleanupTerminal(ctx, 10); err != nil || n != 2 {
			t.Fatalf("cleanup %d %v", n, err)
		}
		if again := enqueue(t, q, job, nil); again == fresh {
			t.Fatal("cleanup retained reservation")
		}
	})
	t.Run("locked_prefix_does_not_starve_claim_or_maintenance", func(t *testing.T) {
		q := queue(t, "fairness")
		firstJob, secondJob := makeJob(t), makeJob(t)
		firstID := enqueue(t, q, firstJob, nil)
		secondID := enqueue(t, q, secondJob, nil)
		locked, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		var found mysqlstore.UUID
		if err := locked.QueryRowContext(ctx, "SELECT id FROM data_model_index_deliveries WHERE id=? FOR UPDATE", mysqlstore.UUID(firstID)).Scan(&found); err != nil {
			t.Fatal(err)
		}
		second := claim(t, q)
		if second.ID != secondID {
			t.Fatal("claim did not skip locked head")
		}
		if err := locked.Rollback(); err != nil {
			t.Fatal(err)
		}
		first := claim(t, q)
		expire(t, first.ID)
		expire(t, second.ID)
		// Set a known order independent of UUID and statement clock ties.
		if _, err := db.ExecContext(ctx, "UPDATE data_model_index_deliveries SET lease_until=TIMESTAMPADD(SECOND,-10,UTC_TIMESTAMP(6)) WHERE id=?", mysqlstore.UUID(first.ID)); err != nil {
			t.Fatal(err)
		}
		locked, err = db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer locked.Rollback()
		if err := locked.QueryRowContext(ctx, "SELECT id FROM data_model_index_jobs WHERE id=? FOR UPDATE", mysqlstore.UUID(firstJob)).Scan(&found); err != nil {
			t.Fatal(err)
		}
		n, cursor, err := q.MaintainPage(ctx, 1, false, nil)
		if err != nil || n != 0 || cursor == nil {
			t.Fatalf("locked page %d %+v %v", n, cursor, err)
		}
		n, next, err := q.MaintainPage(ctx, 1, false, cursor)
		if err != nil || n != 1 || next == nil || state(t, second.ID) != "available" {
			t.Fatalf("later page %d %+v %v", n, next, err)
		}
		if _, _, err := q.MaintainPage(ctx, 1, true, cursor); err == nil {
			t.Fatal("cursor stream not checked")
		}
		if err := locked.Rollback(); err != nil {
			t.Fatal(err)
		}
		if n, err := q.ReapExpired(ctx, 10); err != nil || n != 1 {
			t.Fatalf("wrap scan %d %v", n, err)
		}
	})
	t.Run("retry_exhaustion_and_cancel_failure_are_atomic", func(t *testing.T) {
		q := queue(t, "terminal_failure")
		job := makeJob(t)
		id := enqueue(t, q, job, nil)
		lease := claim(t, q)
		injected := errors.New("cancel audit failed")
		if err := q.Cancel(ctx, tenantID, job, id, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "UPDATE data_model_index_jobs SET status='cancelled' WHERE id=?", mysqlstore.UUID(job)); err != nil {
				return err
			}
			return injected
		}); !errors.Is(err, injected) || state(t, id) != "running" {
			t.Fatalf("cancel failure %v", err)
		}
		if err := q.Finish(ctx, *lease, true, 0, "temporary_failure", nil); err != nil {
			t.Fatal(err)
		}
		lease = claim(t, q)
		if err := q.Finish(ctx, *lease, true, 0, "attempts_exhausted", nil); err != nil || state(t, id) != "discarded" {
			t.Fatalf("exhaustion %v", err)
		}
		if fresh := enqueue(t, q, job, nil); fresh == id {
			t.Fatal("discarded reservation not released")
		}
	})
	t.Run("invalid_inputs_and_read_only_enqueue", func(t *testing.T) {
		q := queue(t, "validation")
		job := makeJob(t)
		for _, name := range []string{"", "bad quote'", "queue with spaces"} {
			if _, err := store.NewIndexDeliveryStore(db, database, name, 1, time.Minute); err == nil {
				t.Fatal("invalid queue accepted")
			}
		}
		if _, err := q.ReapExpired(ctx, 0); err == nil {
			t.Fatal("unbounded maintenance")
		}
		if err := q.Finish(ctx, store.DeliveryLease{}, false, 0, "", nil); err == nil {
			t.Fatal("invalid lease")
		}
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := q.EnqueueTx(ctx, tx, tenantID, job, nil); err == nil {
			t.Fatal("read only enqueue succeeded")
		}
	})
}
