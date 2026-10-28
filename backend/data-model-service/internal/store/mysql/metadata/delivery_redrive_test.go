package metadata_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/tenant"
	store "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestDeliveryRedrive(t *testing.T) {
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
		return r.Tenants.Create(ctx, tenant.New(tenantID, at, "redrive", nil))
	}); err != nil {
		t.Fatal(err)
	}
	setup := func(t *testing.T) (*store.IndexDeliveryStore, store.DeliveryRedriveRequest) {
		t.Helper()
		q, err := store.NewIndexDeliveryStore(db, database, "q_"+uuid.NewString(), 1, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		r := store.DeliveryRedriveRequest{RequestID: uuid.New(), TenantID: tenantID, JobID: uuid.New(), Actor: "trusted-operator", Reason: "retry after repaired dependency", ScheduledAt: at.Add(time.Hour)}
		err = mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error {
			repos, e := store.NewMetadataRepositories(tx, database, tenantID, 100, 10000)
			if e != nil {
				return e
			}
			if e = repos.Jobs.Create(ctx, datamodel.IndexJob{ID: r.JobID, TenantID: tenantID, TableName: "records", IndexType: "search", Columns: []string{}, Status: "pending", AttemptCount: 3, RequestedAt: at}); e != nil {
				return e
			}
			r.SourceID, e = q.EnqueueTx(ctx, tx, tenantID, r.JobID, nil)
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
		lease, err := q.Claim(ctx)
		if err != nil || lease == nil {
			t.Fatalf("claim %v %v", lease, err)
		}
		if err = q.Finish(ctx, *lease, true, 0, "failure", nil); err != nil {
			t.Fatal(err)
		}
		return q, r
	}
	count := func(t *testing.T, job uuid.UUID, want int) {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM data_model_index_deliveries WHERE tenant_id=? AND job_id=?", mysqlstore.UUID(tenantID), mysqlstore.UUID(job)).Scan(&n); err != nil || n != want {
			t.Fatalf("deliveries %d want %d: %v", n, want, err)
		}
	}
	t.Run("atomic_audit_schedule_attempts_replay_and_cleanup", func(t *testing.T) {
		q, r := setup(t)
		id, err := q.RedriveDiscarded(ctx, r)
		if err != nil || id == uuid.Nil {
			t.Fatalf("redrive %s %v", id, err)
		}
		inspector, err := store.NewDeliveryInspector(db, database, tenantID)
		if err != nil {
			t.Fatal(err)
		}
		row, err := inspector.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if row.State != "available" || row.Attempt != 0 || row.JobAttempt != 3 || row.MaxAttempts != 1 || !row.Reserved || !row.AvailableAt.Equal(r.ScheduledAt) {
			t.Fatalf("redrive state %+v", row)
		}
		var principal, actor string
		if err := db.QueryRowContext(ctx, "SELECT JSON_UNQUOTE(JSON_EXTRACT(details,'$.database_principal')),JSON_UNQUOTE(JSON_EXTRACT(details,'$.request.Actor')) FROM data_model_schema_change_log WHERE id=?", mysqlstore.UUID(r.SourceID)).Scan(&principal, &actor); err != nil || principal == "" || actor != r.Actor {
			t.Fatalf("audit %s %s %v", principal, actor, err)
		}
		again, err := q.RedriveDiscarded(ctx, r)
		if err != nil || again != id {
			t.Fatalf("replay %s %v", again, err)
		}
		changed := r
		changed.Reason = "different"
		if _, err = q.RedriveDiscarded(ctx, changed); !errors.Is(err, store.ErrDeliveryRedriveConflict) {
			t.Fatal(err)
		}
		// Retention deletion cannot erase the source's redrive identity.
		if _, err = db.ExecContext(ctx, "DELETE FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(r.SourceID)); err != nil {
			t.Fatal(err)
		}
		again, err = q.RedriveDiscarded(ctx, r)
		if err != nil || again != id {
			t.Fatalf("retention replay %s %v", again, err)
		}
		changed = r
		changed.RequestID = uuid.New()
		if _, err = q.RedriveDiscarded(ctx, changed); !errors.Is(err, store.ErrDeliveryRedriveConflict) {
			t.Fatal(err)
		}
		count(t, r.JobID, 1)
	})
	t.Run("concurrent_same_request", func(t *testing.T) {
		q, r := setup(t)
		var wg sync.WaitGroup
		ids := make([]uuid.UUID, 8)
		errs := make([]error, 8)
		for i := range ids {
			wg.Add(1)
			go func() { defer wg.Done(); ids[i], errs[i] = q.RedriveDiscarded(ctx, r) }()
		}
		wg.Wait()
		for i := range ids {
			if errs[i] != nil || ids[i] == uuid.Nil || ids[i] != ids[0] {
				t.Fatalf("concurrent %v %v", ids, errs)
			}
		}
		count(t, r.JobID, 2)
	})
	t.Run("uncertain_commit_replay", func(t *testing.T) {
		q, r := setup(t)
		var observed atomic.Bool
		fault := deliveryFaultDB(t, database, &observed)
		var queue string
		if err := db.QueryRowContext(ctx, "SELECT queue_name FROM data_model_index_deliveries WHERE id=?", mysqlstore.UUID(r.SourceID)).Scan(&queue); err != nil {
			t.Fatal(err)
		}
		broken, err := store.NewIndexDeliveryStore(fault, database, queue, 1, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		id, err := broken.RedriveDiscarded(ctx, r)
		var ambiguous *mysqlstore.CommitError
		if id != uuid.Nil || !errors.As(err, &ambiguous) || !observed.Load() {
			t.Fatalf("commit %s %v", id, err)
		}
		id, err = q.RedriveDiscarded(ctx, r)
		if err != nil || id == uuid.Nil {
			t.Fatalf("reconcile %s %v", id, err)
		}
		count(t, r.JobID, 2)
	})
	for _, status := range []string{"running", "failed", "applied", "cancelled"} {
		t.Run("reject_domain_"+status, func(t *testing.T) {
			q, r := setup(t)
			if _, err := db.ExecContext(ctx, "UPDATE data_model_index_jobs SET status=? WHERE id=?", status, mysqlstore.UUID(r.JobID)); err != nil {
				t.Fatal(err)
			}
			if _, err := q.RedriveDiscarded(ctx, r); !errors.Is(err, store.ErrDeliveryRedriveIneligible) {
				t.Fatal(err)
			}
			count(t, r.JobID, 1)
		})
	}
	t.Run("foreign_tenant_and_source", func(t *testing.T) {
		q, r := setup(t)
		foreign := r
		foreign.TenantID = uuid.New()
		if _, err := q.RedriveDiscarded(ctx, foreign); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		_, other := setup(t)
		foreign = r
		foreign.SourceID = other.SourceID
		if _, err := q.RedriveDiscarded(ctx, foreign); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		count(t, r.JobID, 1)
	})
	t.Run("reservation_on_another_queue", func(t *testing.T) {
		q, r := setup(t)
		other, err := store.NewIndexDeliveryStore(db, database, "other", 5, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err = mysqlstore.Mutation(ctx, db, func(tx *sql.Tx) error { _, e := other.EnqueueTx(ctx, tx, tenantID, r.JobID, nil); return e }); err != nil {
			t.Fatal(err)
		}
		if _, err = q.RedriveDiscarded(ctx, r); !errors.Is(err, store.ErrDeliveryRedriveIneligible) {
			t.Fatal(err)
		}
		count(t, r.JobID, 2)
	})
	t.Run("audit_failure_rolls_back_enqueue", func(t *testing.T) {
		q, r := setup(t)
		if _, err := db.ExecContext(ctx, "CREATE TRIGGER reject_redrive BEFORE INSERT ON data_model_schema_change_log FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected audit failure'"); err != nil {
			t.Fatal(err)
		}
		_, err := q.RedriveDiscarded(ctx, r)
		if _, dropErr := db.ExecContext(ctx, "DROP TRIGGER reject_redrive"); dropErr != nil {
			t.Fatal(dropErr)
		}
		if err == nil {
			t.Fatal("audit error swallowed")
		}
		count(t, r.JobID, 1)
		if _, err = q.RedriveDiscarded(ctx, r); err != nil {
			t.Fatal(err)
		}
	})
}
